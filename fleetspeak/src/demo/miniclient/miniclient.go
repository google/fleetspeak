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

// Package main implements a fleetspeak client containing the standard
// fleetspeak components.
//
// It reads all configuration data from command line parameters, however a
// typical installation should consider implementing their own main function,
// creating a config.Configuration with key values hardcoded.
//
// In addition, forking this file allows an installation to include local client
// components and labels in their client binary.
package main

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"

	"flag"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/client"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/daemonservice"
	"github.com/google/fleetspeak/fleetspeak/src/client/https"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/socketservice"
	"github.com/google/fleetspeak/fleetspeak/src/client/stdinservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

var (
	configPath        = flag.String("config_path", "", "Directory for configuration files and client state.")
	server            = flag.String("server", "", "The server to connect to: '<hostname>:<port>'")
	trustedCertFile   = flag.String("trusted_cert_file", "", "A PEM file contain one or more certificates to trust when identifying servers.")
	deploymentKeyFile = flag.String("deployment_key_file", "", "A PEM file containing one or more deployment keys (public exponents) to trust.")
)

func main() {
	flag.Parse()

	ph, err := config.NewFilesystemPersistenceHandler(*configPath, filepath.Join(*configPath, "writeback"))
	if err != nil {
		log.Fatal(err)
	}

	cl, err := client.New(
		config.Configuration{
			TrustedCerts:         readTrustedCerts(),
			DeploymentPublicKeys: nil,
			Servers:              []string{*server},
			ClientLabels: []*fspb.Label{
				{ServiceName: "client", Label: runtime.GOARCH},
				{ServiceName: "client", Label: runtime.GOOS},
				{ServiceName: "client", Label: "miniclient"},
			},
			PersistenceHandler: ph,
		},
		client.Components{
			ServiceFactories: map[string]service.Factory{
				"Daemon": daemonservice.Factory,
				"NOOP":   service.NOOPFactory,
				"Socket": socketservice.Factory,
				"Stdin":  stdinservice.Factory,
			},
			Communicator: &https.Communicator{},
		})
	if err != nil {
		log.Exitf("Unable to create client: %v", err)
	}

	s := make(chan os.Signal)
	signal.Notify(s, os.Interrupt)
	<-s
	signal.Reset(os.Interrupt)
	cl.Stop()
}

func readTrustedCerts() *x509.CertPool {
	b, err := ioutil.ReadFile(*trustedCertFile)
	if err != nil {
		log.Exitf("Unable to read trusted_cert_file [%s]: %v", *trustedCertFile, err)
	}
	p := x509.NewCertPool()
	if !p.AppendCertsFromPEM(b) {
		log.Exitf("No certs found in trusted_cert_file [%s]", *trustedCertFile)
	}
	return p
}

func readDeploymentKeys() []rsa.PublicKey {
	b, err := ioutil.ReadFile(*deploymentKeyFile)
	if err != nil {
		log.Exitf("Unable to read deployment_key_file [%s]: %v", *deploymentKeyFile, err)
	}
	var ks []rsa.PublicKey
	for {
		var p *pem.Block
		p, b = pem.Decode(b)
		if p == nil {
			break
		}
		if p.Type != "PUBLIC KEY" {
			log.Exitf("Unexpected PEM type [%s] in [%s], expected [PUBLIC KEY]", p.Type, *deploymentKeyFile)
		}
		k, err := x509.ParsePKIXPublicKey(p.Bytes)
		if err != nil {
			log.Exitf("Error parsing public key in [%s]: %v", *deploymentKeyFile, err)
		}
		key, ok := k.(*rsa.PublicKey)
		if !ok {
			log.Exitf("Expected [*rsa.PublicKey] for deployment public key, got [%T]", k)
		}
		ks = append(ks, *key)
	}
	return ks
}
