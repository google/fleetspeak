// Copyright 2019 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package certs contains utility methods for reading and verifying the
// certificates needed to configure a Fleetspeak installation.
package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"time"

	cpb "github.com/google/fleetspeak/fleetspeak/src/config/proto/fleetspeak_config"
)

// GetServerCert returns the server certificate associated with cfg, creating
// it if necessary.  If available, priv is the private key associated with cert.
func GetServerCert(cfg cpb.Config, ca *x509.Certificate, caPriv interface{}) (cert, key []byte, err error) {
	if cfg.ServerCertFile == "" {
		return nil, nil, errors.New("server_cert_file not set")
	}
	if _, err := os.Stat(cfg.ServerCertFile); err != nil {
		if os.IsNotExist(err) {
			// Attempt to create a server certificate.
			if caPriv == nil {
				return nil, nil, errors.New("unable to create server certificate: CA key not loaded")
			}
			if err := makeServerCert(cfg, ca, caPriv); err != nil {
				return nil, nil, err
			}
		} else {
			return nil, nil, fmt.Errorf("unable to stat server_cert_file [%s]: %v", cfg.ServerCertFile, err)
		}
	}
	return getServerCert(cfg)
}

func makeServerCert(cfg cpb.Config, ca *x509.Certificate, caPriv interface{}) error {
	if cfg.ServerCertKeyFile == "" {
		return errors.New("unable to create a server cert: server_cert_key_file not set")
	}
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("unable to create a server cert: key generation failed: %v", err)
	}
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return fmt.Errorf("unable to create a server cert: serial number generation failed: %v", err)
	}
	tmpl := x509.Certificate{
		Version:               1,
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: fmt.Sprintf("%s Fleetspeak Server", cfg.ConfigurationName)},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * 365 * time.Hour), // 1 year from now
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	for _, hp := range cfg.PublicHostPort {
		h, _, err := net.SplitHostPort(hp)
		if err != nil {
			return fmt.Errorf("unable to create a server cert: unable to parse host-port [%s]: %v", hp, err)
		}
		// If it quacks like a duck.
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	cert, err := x509.CreateCertificate(rand.Reader, &tmpl, ca, privKey.Public(), caPriv)
	if err != nil {
		return fmt.Errorf("unable to create a server cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})

	key, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return fmt.Errorf("unable to create server cert: failed to marshal private key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: key})

	if err := ioutil.WriteFile(cfg.ServerCertFile, certPEM, 0644); err != nil {
		return fmt.Errorf("failed to write server cert file [%s]: %v", cfg.ServerCertFile, err)
	}
	if err := ioutil.WriteFile(cfg.ServerCertKeyFile, keyPEM, 0600); err != nil {
		return fmt.Errorf("failed to write server key file [%s]: %v", cfg.ServerCertKeyFile, err)
	}

	return nil
}

func getServerCert(cfg cpb.Config) (cert, key []byte, err error) {
	// Read the cert.
	certPEM, err := ioutil.ReadFile(cfg.ServerCertFile)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to read server certificate file [%s]: %v", cfg.ServerCertFile, err)
	}

	// Read the key.
	keyPEM, err := ioutil.ReadFile(cfg.ServerCertKeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("uanble to read the server certificate key file [%s]: %v", cfg.ServerCertKeyFile, err)
	}

	// Direct check that the server will be able to instantiate a tls
	// configuration using this PEM data. In particular, this fails if the
	// key does not match the cert.
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return nil, nil, fmt.Errorf("error in server certificate/key [%s]/[%s] configuration: %v", cfg.ServerCertFile, cfg.ServerCertKeyFile, err)
	}
	return certPEM, keyPEM, nil
}
