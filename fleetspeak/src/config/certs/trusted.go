package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"time"

	log "github.com/golang/glog"

	cpb "github.com/google/fleetspeak/fleetspeak/src/config/proto/fleetspeak_config"
)

// GetTrustedCert returns the trusted certificate associated with cfg, creating
// it if necessary.  If available, priv is the private key associated with cert.
func GetTrustedCert(cfg cpb.Config) (cert *x509.Certificate, priv interface{}, err error) {
	if cfg.TrustedCertFile == "" {
		return nil, nil, errors.New("trusted_cert_file not set")
	}
	if _, err := os.Stat(cfg.TrustedCertFile); err != nil {
		if os.IsNotExist(err) {
			// Attempt to create a CA certificate.
			if err := makeCACert(cfg); err != nil {
				return nil, nil, err
			}
		} else {
			return nil, nil, fmt.Errorf("unable to stat trusted_cert_file [%s]: %v", cfg.TrustedCertFile, err)
		}
	}
	return getTrustedCert(cfg)
}

func makeCACert(cfg cpb.Config) error {
	if cfg.TrustedCertKeyFile == "" {
		return errors.New("unable to create a CA cert: trusted_cert_key_file not set")
	}
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("unable to create a CA cert: key generation failed: %v", err)
	}
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return fmt.Errorf("unable to create a CA cert: serial number generation failed: %v", err)
	}
	tmpl := x509.Certificate{
		Version:               1,
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: fmt.Sprintf("%s Fleetspeak CA", cfg.ConfigurationName)},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 24 * 365 * time.Hour), // 10 years from now
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	cert, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, privKey.Public(), privKey)
	if err != nil {
		return fmt.Errorf("unable to create a CA cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})

	key, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return fmt.Errorf("unable to create CA cert: failed to marshal private key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: key})

	if err := ioutil.WriteFile(cfg.TrustedCertFile, certPEM, 0644); err != nil {
		return fmt.Errorf("failed to write CA cert file [%s]: %v", cfg.TrustedCertFile, err)
	}
	if err := ioutil.WriteFile(cfg.TrustedCertKeyFile, keyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write CA key file [%s]: %v", cfg.TrustedCertKeyFile, err)
	}

	return nil
}

func getTrustedCert(cfg cpb.Config) (cert *x509.Certificate, priv interface{}, err error) {
	// Read and validate the cert.
	certPEM, err := ioutil.ReadFile(cfg.TrustedCertFile)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read trusted certificate file [%s]: %v", cfg.TrustedCertFile, err)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, nil, fmt.Errorf("trusted certificate file [%s] does not appear to contain a PEM format certificate", cfg.TrustedCertFile)
	}
	cert, err = x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to parse trusted certificate file [%s]: %v", cfg.TrustedCertFile, err)
	}

	// Read the key file, if present.
	keyPEM, err := ioutil.ReadFile(cfg.TrustedCertKeyFile)
	if err != nil {
		log.Infof("unable to read the trusted certificate key file [%s], server certificate creation disabled: %v", cfg.TrustedCertKeyFile, err)
		return cert, nil, nil
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, nil, fmt.Errorf("trusted certificate key file [%s] does not appear to contain a PEM format key", cfg.TrustedCertKeyFile)
	}
	switch keyBlock.Type {
	case "RSA PRIVATE KEY":
		priv, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to parse RSA key in certificate key file [%s]: %v", cfg.TrustedCertKeyFile, err)
		}
		return cert, priv, nil
	case "EC PRIVATE KEY":
		priv, err = x509.ParseECPrivateKey(keyBlock.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to parse EC key in certificate key file [%s]: %v", cfg.TrustedCertKeyFile, err)
		}
		return cert, priv, nil
	default:
		return nil, nil, fmt.Errorf("unsupport PEM block type [%s] in certificate key file [%s]", keyBlock.Type, cfg.TrustedCertKeyFile)
	}

	return cert, priv, nil
}
