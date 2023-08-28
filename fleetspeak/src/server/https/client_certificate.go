package https

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// GetClientCert returns the client certificate from either the request header or TLS connection state.
func GetClientCert(req *http.Request, hn string) (*x509.Certificate, error) {
	if hn != "" {
		return getCertFromHeader(hn, req.Header)
	} else {
		return getCertFromTLS(req)
	}
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
