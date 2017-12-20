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

// Package main implements a configuration tool for the Fleetspeak miniclient
// and miniserver test/demo binaries. Specifically, it creates any missing keys
// and certificates, and then updates the client and server configuration files
// accordingly. Using miniclient and miniserver, a fleetspeak configuration
// consists of the following files:
//
// Required on client machines:
//
// server_cert.pem           - A PEM encoded X509 certificate that the client
//                             will trust when identifying servers.
// deployment_public_key.pem - A PEM encoded RSA public key, used to validate
//                             client sevices.
// <config_path>             - A directory for client persistent runtime state
//                             and dynamic configuration.
// <config_path>/communicator.txt - A text fleetspeak.client.CommunicatorConfig
//                                  protocol buffer. (optional)
// <config_path>/services/*  - One or more files, each containing a
//                             fleetspeak.SignedClientServiceConfig binary
//                             protocol buffer.
//
// Required on the server machine:
//
// server_config.txt - A text representation of the ServerConfig protocol buffer.
// server_cert.pem   - A PEM encoded X509 certificate chain that the server should
//                     use to identify itself.
// server_key.pem    - The PEM encoded private key matching server_cert.pem.
//                     (not created if server_cert.pem is already present)
//
// For use when configuring client services:
//
// deployment_key.pem         - A PEM encoded RSA private key, used to sign client
//                              services.
// client_service_configs.txt - A Text representation of a ClientServiceConfigs
//                              protocol buffer. (required)
//
//
// On execution, this program will perform the following operations:
//
// 1) If server_cert.pem does not exist, a self signed certificate is created,
//    and the corresponded key is saved in server_key.pem.
//
// 2) If deployment_key.pem does not exist, a new RSA key will be created.
//
// 3) The deployment_public_key.pem file is set to the public
//    exponent of key stored in deployment_key.pem.
//
// 4) For each service in client_service_configs.txt, a file will be written to
//    the services directory containing a signed service configuration.
package main

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
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

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"

	// We need import protos referenced in any Any declarations in the configuration.
	_ "github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/proto/fleetspeak_daemonservice"
	_ "github.com/google/fleetspeak/fleetspeak/src/client/stdinservice/proto/fleetspeak_stdinservice"
	_ "github.com/google/fleetspeak/fleetspeak/src/demo/proto/fleetspeak_messagesaver"
	_ "github.com/google/fleetspeak/fleetspeak/src/server/grpcservice/proto/fleetspeak_grpcservice"
)

var certCommonName = flag.String("cert_common_name", "FS Miniserver Installation", "Common name to use in server_cert, if needed.")
var configDir = flag.String("config_dir", "/tmp/fs_config", "Where to build the configuration.")
var serverHostPortsStr = flag.String("server_host_ports", "localhost:6060", "Comma-separated list. The (host:port) pairs that the clients should attempt to reach the server at, e.g. localhost:6060.")
var serverHostPorts []string
var skipServerConfigValidation = flag.Bool("skip_server_config_validation", false, "If true, the server config will not be validated.")

func main() {
	flag.Parse()

	serverHostPorts = strings.Split(*serverHostPortsStr, ",")

	i, err := os.Stat(*configDir)
	if err != nil {
		log.Exitf("Unable to stat config directory [%v]: %v", *configDir, err)
	}
	if !i.Mode().IsDir() {
		log.Exitf("Config directory path [%v] is not a directory.", *configDir)
	}

	createServerCert()
	createDeploymentKey()

	if !*skipServerConfigValidation {
		validateServerConfig()
	}
	signServiceConfigs()
}

// createServerCert creates a new server certificate if server_cert.pem is not
// found. Returns true if a certificate was created.
func createServerCert() bool {
	f := path.Join(*configDir, "server_cert.pem")
	s, err := os.Stat(f)
	if err == nil {
		if s.Mode().IsRegular() {
			// We don't need to create a cert - it will be opened
			// and validated later.
			log.Infof("Using existing server cert: %v", f)
			return false
		}
		log.Exitf("Existing path %v is not a regular file: %v", f, s.Mode())
	}
	// If the file doesn't exist, we will create it. But if we have any other error
	// something odd is going on.
	if !os.IsNotExist(err) {
		log.Exitf("Unable to stat %v: %v", f, err)
	}

	log.Infof("Creating server cert file: %v", f)

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Exitf("Unable to generate new server key: %v", err)
	}

	// Create a random serial number.
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		log.Exitf("Unable to generate serial number: %s", err)
	}

	// Create a self signed certificate good for 2 years, valid for the
	// targeted hosts.
	tmpl := x509.Certificate{
		Version:      1,
		SerialNumber: serialNumber,
		Subject:      pkix.Name{CommonName: *certCommonName},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(2 * 24 * 365 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
		BasicConstraintsValid: true,
	}
	for _, hp := range serverHostPorts {
		h, _, err := net.SplitHostPort(hp)
		if err != nil {
			log.Exitf("Unable to split host and port of %v: %v", hp, err)
		}
		// If it looks like an IP address, assume it is one.
		ip := net.ParseIP(h)
		if ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}

	dc, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, privKey.Public(), privKey)
	if err != nil {
		log.Exitf("Unable to create server cert: %v", err)
	}
	c := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: dc})
	if err := ioutil.WriteFile(f, c, 0644); err != nil {
		log.Exitf("Unable to write server cert file [%v]: %v", f, err)
	}

	kf := path.Join(*configDir, "server_key.pem")
	dk, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		log.Exitf("Unable to marshal key: %v", err)
	}
	k := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: dk})
	if err := ioutil.WriteFile(kf, k, 0600); err != nil {
		log.Exitf("Unable to write server key file [%v]: %v", kf, err)
	}
	return true
}

func createDeploymentKey() {
	f := path.Join(*configDir, "deployment_key.pem")
	s, err := os.Stat(f)
	if err == nil {
		if s.Mode().IsRegular() {
			// We don't need to create a key - it will be opened
			// and validated later.
			return
		}
		log.Exitf("Existing path %v is not a regular file: %v", f, s.Mode())
	}
	// If the file doesn't exist, we will create it. But if we have any other error
	// something odd is going on.
	if !os.IsNotExist(err) {
		log.Exitf("Unable to stat %v: %v", f, err)
	}

	log.Infof("Creating deployment key file: %v", f)

	k, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		log.Exitf("Unable to generate deployment key: %v", err)
	}
	pk := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	if err := ioutil.WriteFile(f, pk, 0600); err != nil {
		log.Exitf("Unable to write deployment key file [%v]: %v", f, err)
	}

	f = path.Join(*configDir, "deployment_public_key.pem")
	pb, err := x509.MarshalPKIXPublicKey(k.Public())
	if err != nil {
		log.Exitf("Unable to encode public key: %v", err)
	}
	pk = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pb})
	if err := ioutil.WriteFile(f, pk, 0644); err != nil {
		log.Exitf("Unable to write public key file [%v]: %v", f, err)
	}
}

func validateServerConfig() {
	f := path.Join(*configDir, "server_config.txt")
	b, err := ioutil.ReadFile(f)
	if err != nil {
		log.Exitf("Unable to read server configuration file [%v]: %v", f, err)
	}
	var conf spb.ServerConfig
	if err := proto.UnmarshalText(string(b), &conf); err != nil {
		log.Exitf("Unable to parse server configuration file [%v]: %v", f, err)
	}
}

func signServiceConfigs() {
	df := path.Join(*configDir, "deployment_key.pem")
	dfb, err := ioutil.ReadFile(df)
	if err != nil {
		log.Exitf("Unable to read deployment key [%v]: %v", dfb, err)
	}
	db, _ := pem.Decode(dfb)
	if db == nil || db.Type != "RSA PRIVATE KEY" {
		log.Exitf("Did not find valid RSA pem block in deployment key file [%v]", df)
	}
	dk, err := x509.ParsePKCS1PrivateKey(db.Bytes)
	if err != nil {
		log.Exitf("Unable to parse key in deployment key file [%v]: %v", df, err)
	}

	csf := path.Join(*configDir, "client_service_configs.txt")
	csb, err := ioutil.ReadFile(csf)
	if err != nil {
		log.Exitf("Unable to read client service file [%v]: %v", csf, err)
	}
	var cspb fspb.ClientServiceConfigs
	if err := proto.UnmarshalText(string(csb), &cspb); err != nil {
		log.Exitf("Unable to parse client service file [%v]: %v", csf, err)
	}

	sp := path.Join(*configDir, "client", "services")
	if err := os.MkdirAll(sp, 0755); err != nil {
		log.Exitf("Unable to create service directory [%s]: %v", sp, err)
	}

	for i, sc := range cspb.Config {
		if sc.Name == "" {
			log.Exitf("Service %d does not have a name.", i)
		}
		serialized, err := proto.Marshal(sc)
		if err != nil {
			log.Exitf("Unable to serialize service config %v: %v", i, err)
		}
		hashed := sha256.Sum256(serialized)
		sig, err := rsa.SignPSS(rand.Reader, dk, crypto.SHA256, hashed[:], nil)
		if err != nil {
			log.Exitf("Unable to sign service config %v: %v", i, err)
		}
		ssp := path.Join(sp, sc.Name)
		b, err := proto.Marshal(&fspb.SignedClientServiceConfig{ServiceConfig: serialized, Signature: sig})
		if err != nil {
			log.Exitf("Unable to serialize service config: %v", err)
		}
		if err := ioutil.WriteFile(ssp, b, 0644); err != nil {
			log.Exitf("Unable to write service config file [%s]: %v", ssp, err)
		}
	}
}
