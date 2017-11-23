// Copyright 2017 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package main implements a certificate and key generation tool. It is meant to be used
// with the configurator tool to set up a CA based server identification scheme. It creates
// the following files in the --config_dir directory:
//
// ca_cert.pem     - A PEM encoded X509 CA certificate.
// ca_key.pem      - The private key associated with ca_cert.pem.
// server_cert.pem - A PEM encoded X509 CA certificate, signed by ca_cert.pem.
// server_key.pem  - The private key associated with server_cert.pem.
//
// On execution, this program will perform the following operations:
//
// 1) If ca_cert.pem does not exist, a self signed certificate is created based
//    on the flags ca_*, and the corresponding key is saved in ca_key.pem.
//
// 2) If server_cert.pem does not exist, a certificate is created based on the
//    flags server_* and signed by ca_key. The corresponding server key is saved
//    in server_key.pem.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"flag"
	"log"
)

var (
	configDir = flag.String("config_dir", "/tmp/fs_config", "Where to find and place certs and keys.")

	caCountryStr      = flag.String("ca_country", "", "Comma-separated list. The country code to embed when generating a CA cert.")
	caCountry         []string
	caOrganizationStr = flag.String("ca_organization", "", "Comma-separated list. The organization name to embed when generating a CA cert.")
	caOrganization    []string
	caCommonName      = flag.String("ca_common_name", "", "The common name to embed when generating a CA cert.")
	caExpiresAfter    = flag.Duration("ca_expires_after", 10*365*24*time.Hour, "For how long a generated CA cert should be valid.")

	serverCountryStr      = flag.String("server_country", "", "Comma-separated list. The country code to embed when generating a server cert.")
	serverCountry         []string
	serverOrganizationStr = flag.String("server_organization", "", "Comma-separated list. The organization name to embed when generating a server cert.")
	serverOrganization    []string
	serverCommonName      = flag.String("server_common_name", "", "The common name to embed when generating a server cert.")
	serverExpiresAfter    = flag.Duration("server_expires_after", 1*365*24*time.Hour, "For how long a generated server cert should be valid.")
	serverHostnamesStr    = flag.String("server_hostnames", "", "Comma-separated list. The hostnames which should be embedded in the server cert.")
	serverHostnames       []string
)

func main() {
	flag.Parse()

	caCountry = strings.Split(*caCountryStr, ",")
	caOrganization = strings.Split(*caOrganizationStr, ",")
	serverCountry = strings.Split(*serverCountryStr, ",")
	serverOrganization = strings.Split(*serverOrganizationStr, ",")
	serverHostnames = strings.Split(*serverHostnamesStr, ",")

	i, err := os.Stat(*configDir)
	if err != nil {
		log.Fatalf("Unable to stat config directory [%v]: %v", *configDir, err)
	}
	if !i.Mode().IsDir() {
		log.Fatalf("Config directory path [%v] is not a directory.", *configDir)
	}

	createCACert()
	createServerCert()
}

// createCACert creates a new CA certificate if ca_cert.pem is not
// found. Returns true if a certificate was created.
func createCACert() bool {
	f := path.Join(*configDir, "ca_cert.pem")
	s, err := os.Stat(f)
	if err == nil {
		if s.Mode().IsRegular() {
			// We don't need to create a cert - it will be opened
			// and validated later.
			log.Printf("Using existing CA cert file: %v", f)
			return false
		}
		log.Fatalf("Existing path %v is not a regular file: %v", f, s.Mode())
	}
	// If the file doesn't exist, we will create it. But if we have any other error
	// something odd is going on.
	if !os.IsNotExist(err) {
		log.Fatalf("Unable to stat %v: %v", f, err)
	}

	if *caCommonName == "" {
		log.Fatal("ca_common_name must be set to create CA cert")
	}

	privKey, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		log.Fatalf("Unable to generate new CA key: %v", err)
	}

	// Create a random serial number.
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		log.Fatalf("Unable to generate serial number: %s", err)
	}

	tmpl := x509.Certificate{
		Version:      1,
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Country:      caCountry,
			Organization: caOrganization,
			CommonName:   *caCommonName},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(*caExpiresAfter),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:      true,
		BasicConstraintsValid: true,
	}
	dc, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, privKey.Public(), privKey)
	if err != nil {
		log.Fatalf("Unable to create CA cert: %v", err)
	}
	c := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: dc})
	if err := ioutil.WriteFile(f, c, 0644); err != nil {
		log.Fatalf("Unable to write CA cert file [%v]: %v", f, err)
	}

	kf := path.Join(*configDir, "ca_key.pem")
	dk, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		log.Fatalf("Unable to marshal key: %v", err)
	}
	k := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: dk})
	if err := ioutil.WriteFile(kf, k, 0600); err != nil {
		log.Fatalf("Unable to write CA key file [%v]: %v", kf, err)
	}
	return true
}

// createServerCert creates a new server certificate if server_cert.pem is not
// found.
func createServerCert() {
	f := path.Join(*configDir, "server_cert.pem")
	s, err := os.Stat(f)
	if err == nil {
		if s.Mode().IsRegular() {
			return
		}
		log.Fatalf("Existing path %v is not a regular file: %v", f, s.Mode())
	}
	// If the file doesn't exist, we will create it. But if we have any other error
	// something odd is going on.
	if !os.IsNotExist(err) {
		log.Fatalf("Unable to stat %v: %v", f, err)
	}
	if *serverCommonName == "" {
		log.Fatal("server_common _name must be set to create a server cert")
	}
	if len(serverHostnames) == 0 {
		log.Fatal("server_hostnames must be set to create a server cert")
	}

	caCert, caKey := loadCA()

	privKey, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		log.Fatalf("Unable to generate new CA key: %v", err)
	}

	// Create a random serial number.
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		log.Fatalf("Unable to generate serial number: %s", err)
	}

	tmpl := x509.Certificate{
		Version:      1,
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Country:      serverCountry,
			Organization: serverOrganization,
			CommonName:   *serverCommonName},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(*serverExpiresAfter),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:        false,
		BasicConstraintsValid: true,
	}
	for _, h := range serverHostnames {
		ip := net.ParseIP(h)
		if ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}

	dc, err := x509.CreateCertificate(rand.Reader, &tmpl, caCert, privKey.Public(), caKey)
	if err != nil {
		log.Fatalf("Unable to create server cert: %v", err)
	}

	c := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: dc})
	if err := ioutil.WriteFile(f, c, 0644); err != nil {
		log.Fatalf("Unable to write server cert file [%v]: %v", f, err)
	}

	kf := path.Join(*configDir, "server_key.pem")
	dk, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		log.Fatalf("Unable to marshal key: %v", err)
	}
	k := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: dk})
	if err := ioutil.WriteFile(kf, k, 0600); err != nil {
		log.Fatalf("Unable to write server key file [%v]: %v", kf, err)
	}

}

func loadCA() (*x509.Certificate, interface{}) {
	f := path.Join(*configDir, "ca_cert.pem")
	b, err := ioutil.ReadFile(f)
	if err != nil {
		log.Fatalf("Unable to read CA certificate [%v]: %v", f, err)
	}
	cb, _ := pem.Decode(b)
	if cb == nil || cb.Type != "CERTIFICATE" {
		log.Fatalf("Did not find valid pem block in CA certificate file [%v]", f)
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		log.Fatalf("Unable to parse certificate in CA certificate file [%v]: %v", f, err)
	}

	kf := path.Join(*configDir, "ca_key.pem")
	kfb, err := ioutil.ReadFile(kf)
	if err != nil {
		log.Fatalf("Unable to read CA key [%v]: %v", kf, err)
	}
	db, _ := pem.Decode(kfb)
	if db == nil {
		log.Fatalf("Unable to parse PEM in CA key[%v].", kf)
	}
	var key interface{}
	switch db.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(db.Bytes)
		if err != nil {
			log.Fatalf("Unable to parse RSA key in CA key file [%v]: %v", kf, err)
		}
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(db.Bytes)
		if err != nil {
			log.Fatalf("Unable to parse EC key in CA key file [%v]: %v", kf, err)
		}
	default:
		log.Fatalf("Unsupported PEM block [%v] in CA key file [%v].", db.Type, err)
	}

	return cert, key
}
