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
	"strings"

	cpb "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
	log "github.com/golang/glog"
)

// GetClientCert returns the client certificate from either the request header or TLS connection state.
func GetClientCert(req *http.Request, hn string, mode cpb.FrontendMode, checksumHeader string) (*x509.Certificate, error) {
	switch {
	case mode == cpb.FrontendMode_MTLS && hn == "" && checksumHeader == "":
		// If running in MTLS mode neither the client certficate nor the checksum headers should be set
		return getCertFromTLS(req)
	case mode == cpb.FrontendMode_HEADER_TLS && hn != "" && checksumHeader == "":
		// If running in HEADER TLS mode only the client certficate header should be set
		cert, _, err := getCertFromHeader(hn, req.Header)
		return cert, err
	case mode == cpb.FrontendMode_HEADER_TLS_CHECKSUM && hn != "" && checksumHeader != "":
		// If running in HEADER TLS CHECKSUM mode both the client certficate and the checksum headers must be set
		cert, headerCert, err := getCertFromHeader(hn, req.Header)
		if err != nil {
			return nil, err
		}
		err = verifyCertSha256Fingerprint(headerCert, req.Header.Get(checksumHeader))
		if err != nil {
			return nil, err
		}
		return cert, nil
	}
	log.Warningln("#####################################################################")
	log.Warningln("# Valid combinations are:                                           #")
	log.Warningln("# Frontend Mode       | clientCertHeader | clientCertChecksumHeader #")
	log.Warningln("# ------------------------------------------------------------------#")
	log.Warningln("# MTLS                |       no         |           no             #")
	log.Warningln("# HEADER_TLS          |       yes        |           no             #")
	log.Warningln("# HEADER_TLS_CHECKSUM |       yes        |           yes            #")
	log.Warningln("###################################################################################")
	return nil, fmt.Errorf("received invalid frontend mode combination: frontendMode=%s, clientCertHeader=%s, clientCertChecksumHeader=%s",
				mode, hn, checksumHeader)
}

// This function is mimicking the behaviour of how the GLB7 is calculating the the cert fingerprint.
// We can also do so on the command line using openssl to calculate the cert fingerprint.
// openssl x509 -in mclient.crt -outform DER | openssl dgst -sha256 | cut -d ' ' -f2 | xxd -r -p - | openssl enc -a
// For more info check out: https://gist.github.com/salrashid123/6e2a1eb9be95fb49506f1554e2d3d392
func mimickGLB7FingerprintCalc(clientCert string) (string) {
	// Decode the PEM string
	block, rest := pem.Decode([]byte(clientCert))
	if block == nil || len(rest) !=0 {
		log.Warningln("Failed to decode PEM certificate")
		return ""
	}
	// Calculate the SHA-256 digest of the DER certificate
	sha256Digest := sha256.Sum256(block.Bytes)

	// Convert the SHA-256 digest to a hexadecimal string
	// sha256HexStr equivalent to: openssl x509 -n mclient.crt -outform DER | openssl dgst -sha256
	sha256HexStr := fmt.Sprintf("%x", sha256Digest)

	// sha256Binaryequivalent to: openssl x509 -n mclient.crt -outform DER | openssl dgst -sha256 | xxd -r -p -
	sha256Binary, err := hex.DecodeString(sha256HexStr)
	if err != nil {
		log.Warningf("error decoding hexdump: %v\n", err)
		return ""
	}

	// Convert the hexadecimal string to a base64 encoded string
	// base64EncodedStr equivalent to: openssl x509 -n mclient.crt -outform DER | openssl dgst -sha256 | xxd -r -p - | openssl enc -a
	// It also removes trailing "=" padding characters
	base64EncodedStr := strings.TrimRight(base64.StdEncoding.EncodeToString(sha256Binary), "=")

	// Return the base64 encoded string
	return base64EncodedStr
}

func verifyCertSha256Fingerprint(headerCert string, clientCertSha256Fingerprint string) (error) {
	if clientCertSha256Fingerprint == "" {
		return errors.New("no client certificate checksum received in header")
	}

	calculatedClientCertSha256 := mimickGLB7FingerprintCalc(headerCert)
	if (calculatedClientCertSha256 != clientCertSha256Fingerprint) {
		return errors.New("received client certificate checksum is invalid")
	}

	return nil
}

func getCertFromHeader(hn string, rh http.Header) (*x509.Certificate, string, error) {
	headerCert := rh.Get(hn)
	if headerCert == "" {
		return nil, "", errors.New("no certificate found in header")
	}
	// Most certificates are URL PEM encoded
	if decodedCert, err := url.PathUnescape(headerCert); err != nil {
		return nil, "", err
	} else {
		headerCert = decodedCert
	}
	block, rest := pem.Decode([]byte(headerCert))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, "", errors.New("failed to decode PEM block containing certificate")
	}
	if len(rest) != 0 {
		return nil, "", errors.New("received more than 1 client cert")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, "", err
	}
	return cert, headerCert, err
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
