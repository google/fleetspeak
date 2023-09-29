package https

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	cpb "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
)

// GetClientCert returns the client certificate from either the request header or TLS connection state.
func GetClientCert(req *http.Request, hn string, frontendMode cpb.FrontendMode) (*x509.Certificate, error) {
	switch frontendMode {
	case cpb.FrontendMode_MTLS:
		if hn == "" {
			return getCertFromTLS(req)
		}
	case cpb.FrontendMode_HEADER_TLS:
		if hn != "" {
			return getCertFromHeader(hn, req.Header)
		}
	}
	return nil, fmt.Errorf("received invalid frontend mode combination: frontendMode=%s, clientCertHeader=%s", frontendMode, hn)
}

func calcClientCertSha256(clientCert string) (string) {
	// Decode the PEM string
	block, _ := pem.Decode([]byte(clientCert))
	if block == nil {
		fmt.Println("Failed to decode PEM certificate")
		return ""
	}
	// Calculate the SHA-256 digest of the DER certificate
	sha256Digest := sha256.Sum256(block.Bytes)

	// Convert the SHA-256 digest to a hexadecimal string
	sha256HexStr := fmt.Sprintf("%x", sha256Digest)

	sha256Binary, err := hex.DecodeString(sha256HexStr)
	if err != nil {
		fmt.Sprintf("error decoding hexdump: %v\n", err)
		return ""
	}

	// Convert the hexadecimal string to a base64 encoded string
	base64EncodedStr := base64.StdEncoding.EncodeToString(sha256Binary)

	// Rreturn the base64 encoded string
	return base64EncodedStr
}

func getCertFromHeader(hn string, rh http.Header) (*x509.Certificate, error) {
	headerCert := rh.Get(hn)
	if headerCert == "" {
		return nil, errors.New("no certificate found in header")
	}
	// Most certificates are URL PEM encoded
	if decodedCert, err := url.PathUnescape(headerCert); err != nil {
		return nil, err
	} else {
		headerCert = decodedCert
	}
	block, rest := pem.Decode([]byte(headerCert))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("failed to decode PEM block containing certificate")
	}
	if len(rest) != 0 {
		return nil, errors.New("received more than 1 client cert")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	fmt.Println("")
	fmt.Println("--------------------------- received cert in header")
	//fmt.Println(headerCert)
	clientCertSha256Fingerprint := rh.Get("X-Client-Cert-Hash")
	if clientCertSha256Fingerprint != "" {
		fmt.Println("----- received client_cert_sha256_fingerprint:")
		fmt.Println(clientCertSha256Fingerprint)
		fmt.Println("----- calculated client cert sha256 fingerprint:")
		calcClientCertSha256 := calcClientCertSha256(headerCert)
		fmt.Println(calcClientCertSha256)
	}
	return cert, err
}

func getCertFromTLS(req *http.Request) (*x509.Certificate, error) {
	if req.TLS == nil {
		return nil, errors.New("TLS information not found")
	}
	if len(req.TLS.PeerCertificates) != 1 {
		return nil, fmt.Errorf("expected 1 client cert, received %v", len(req.TLS.PeerCertificates))
	}
	cert := req.TLS.PeerCertificates[0]
	return cert, nil
}
