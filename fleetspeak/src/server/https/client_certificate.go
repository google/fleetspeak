package https

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"

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
	case frontendConfig.GetHttpsHeaderConfig() != nil:
		return getCertFromHeader(frontendConfig.GetHttpsHeaderConfig().GetClientCertificateHeader(), req.Header)
	}

	// Given the above switch statement is exhaustive, this error should never be reached
	return nil, errors.New("invalid frontend_config")
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
