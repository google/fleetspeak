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

	log "github.com/golang/glog"
	cpb "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
)

// GetClientCert returns the client certificate from either the request header or TLS connection state.
func GetClientCert(req *http.Request, frontendConfig *cpb.FrontendConfig) (*x509.Certificate, error) {
	// Default to using mTLS if frontend_config or frontend_mode have not been set
	if frontendConfig.GetFrontendMode() == nil {
		return getCertFromTLS(req)
	}

	switch {
	case frontendConfig.GetMtlsConfig() != nil:
		return getCertFromTLS(req)
	case frontendConfig.GetCleartextHeaderConfig() != nil:
		return getCertFromHeader(frontendConfig.GetCleartextHeaderConfig().GetClientCertificateHeader(), req.Header)
	case frontendConfig.GetHttpsHeaderConfig() != nil:
		return getCertFromHeader(frontendConfig.GetHttpsHeaderConfig().GetClientCertificateHeader(), req.Header)
	case frontendConfig.GetCleartextHeaderChecksumConfig() != nil:
		cert, err := getCertFromHeader(frontendConfig.GetCleartextHeaderChecksumConfig().GetClientCertificateHeader(), req.Header)
		if err != nil {
			return nil, err
		}
		err = verifyCertSha256Checksum(req.Header.Get(frontendConfig.GetCleartextHeaderChecksumConfig().GetClientCertificateHeader()),
			req.Header.Get(frontendConfig.GetCleartextHeaderChecksumConfig().GetClientCertificateChecksumHeader()))
		if err != nil {
			return nil, err
		}
		return cert, nil
	case frontendConfig.GetHttpsHeaderChecksumConfig() != nil:
		cert, err := getCertFromHeader(frontendConfig.GetHttpsHeaderChecksumConfig().GetClientCertificateHeader(), req.Header)
		if err != nil {
			return nil, err
		}
		err = verifyCertSha256Checksum(req.Header.Get(frontendConfig.GetHttpsHeaderChecksumConfig().GetClientCertificateHeader()),
			req.Header.Get(frontendConfig.GetHttpsHeaderChecksumConfig().GetClientCertificateChecksumHeader()))
		if err != nil {
			return nil, err
		}
		return cert, nil
	case frontendConfig.GetCleartextXfccConfig() != nil:
		cert, err := getCertFromXfcc(frontendConfig.GetCleartextXfccConfig().GetClientCertificateHeader(), req.Header)
		if err != nil {
			return nil, err
		}
		return cert, nil
	}

	// Given the above switch statement is exhaustive, this error should never be reached
	return nil, errors.New("invalid frontend_config")
}

// This function is calculating the client certificate checksum in the same fashion the GLB7 does.
// We can also do so on the command line using openssl to calculate the certificate checksum.
// openssl x509 -in mclient.crt -outform DER | openssl dgst -sha256 | cut -d ' ' -f2 | xxd -r -p - | openssl enc -a
// For more info check out: https://gist.github.com/salrashid123/6e2a1eb9be95fb49506f1554e2d3d392
func calculateClientCertificateChecksum(clientCert string) string {
	// Most certificates are URL PEM encoded
	if decodedCert, err := url.PathUnescape(clientCert); err != nil {
		return ""
	} else {
		clientCert = decodedCert
	}
	// Decode the PEM string
	block, rest := pem.Decode([]byte(clientCert))
	if block == nil || len(rest) != 0 {
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

func verifyCertSha256Checksum(headerCert string, clientCertSha256Checksum string) error {
	if clientCertSha256Checksum == "" {
		return errors.New("no client certificate checksum received in header")
	}

	calculatedClientCertSha256 := calculateClientCertificateChecksum(headerCert)
	if calculatedClientCertSha256 != clientCertSha256Checksum {
		log.Infof("received client certificate checksum for client cert header %q is invalid, calculated: %s, received: %s", headerCert, calculatedClientCertSha256, clientCertSha256Checksum)
		return errors.New("received client certificate checksum is invalid")
	}

	return nil
}

const (
	key int = iota
	valueStart
	value
	quotedValue
)

type xfccParser struct {
	header string
}

func (x *xfccParser) Next() (string, string) {
	var keyStr, valueStr strings.Builder
	state := key
	var i int
L:
	for i = 0; i < len(x.header); i++ {
		switch state {
		case key:
			if string(x.header[i]) == "=" {
				state = valueStart
			} else {
				keyStr.Write([]byte{x.header[i]})
			}
		case valueStart:
			if string(x.header[i]) == `"` {
				state = quotedValue
				continue L
			} else {
				state = value
			}
			fallthrough
		case value:
			if string(x.header[i]) == ";" {
				break L
			} else if string(x.header[i]) == "," {
				break L
			}
			if string(x.header[i]) == `\` {
				if len(x.header) == i+1 {
					return "", ""
				}
				i++
			}
			valueStr.Write([]byte{x.header[i]})
		case quotedValue:
			if string(x.header[i]) == `"` {
				state = value
				continue L
			}
			if string(x.header[i]) == `\` {
				if len(x.header) == i+1 {
					return "", ""
				}
				i++
			}
			valueStr.Write([]byte{x.header[i]})
		}
	}
	if len(x.header) > i {
		x.header = x.header[i+1:]
	} else {
		x.header = ""
	}
	return keyStr.String(), valueStr.String()
}

// parses the X-Forwarded-Client-Cert header as defined by envoy
// see: https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/headers#x-forwarded-client-cert
// if multiple client certs are found, takes the first one
func extractField(fieldName, headerCert string) string {
	headerReader := &xfccParser{
		header: headerCert,
	}
	for {
		keyStr, valueStr := headerReader.Next()
		if keyStr == "" {
			return ""
		}
		if keyStr == fieldName {
			return valueStr
		}
	}
}

func getCertFromXfcc(hn string, rh http.Header) (*x509.Certificate, error) {
	headerCert := rh.Get(hn)
	if headerCert == "" {
		return nil, errors.New("no certificate found in header")
	}
	// support for envoy encoded xfcc header:
	if certField := extractField("Cert", headerCert); certField != "" {
		headerCert = certField
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
	return cert, err
}

func getCertFromHeader(hn string, rh http.Header) (*x509.Certificate, error) {
	headerCert := rh.Get(hn)
	if headerCert == "" {
		return nil, fmt.Errorf("no certificate found in header with name %q", hn)
	}

	decodedCert, err := url.PathUnescape(headerCert)
	if err != nil {
		return nil, err
	}

	// Most certificates are URL PEM encoded
	block, rest := pem.Decode([]byte(decodedCert))
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}
	if block.Type != "CERTIFICATE" {
		return nil, errors.New("PEM block is not a certificate")
	}
	if len(rest) != 0 {
		return nil, errors.New("received more than 1 client cert")
	}
	return x509.ParseCertificate(block.Bytes)
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
