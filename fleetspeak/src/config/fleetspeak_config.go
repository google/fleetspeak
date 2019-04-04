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

// Package main implements a configuration tool which (partially) automates the
// configuration of a Fleetspeak installation. Advanced users may want to
// perform these steps manually, especially with regards to key management.
package main

import (
	"flag"
	"io/ioutil"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/config/certs"
	"github.com/google/fleetspeak/fleetspeak/src/config/server"

	cpb "github.com/google/fleetspeak/fleetspeak/src/config/proto/fleetspeak_config"
)

var configFile = flag.String("config_file", "", "Configuration file to read. Should contain a text format fleetspeak.config.Config protocol buffer. See /usr/share/doc/fleetspeak-server/example.config as a starting point.")

func main() {
	flag.Parse()

	b, err := ioutil.ReadFile(*configFile)
	if err != nil {
		log.Exitf("Unable to read configuration file [%s]: %v", *configFile, err)
	}

	var cfg cpb.Config
	if err := proto.UnmarshalText(string(b), &cfg); err != nil {
		log.Exitf("Unable to parse config file [%s]: %v", *configFile, err)
	}

	if cfg.ConfigurationName == "" {
		log.Exitf("configuration_name required, not found in [%s]", *configFile)
	}

	caCert, caKey, err := certs.GetTrustedCert(cfg)
	if err != nil {
		log.Exit(err)
	}

	serverCert, serverKey, err := certs.GetServerCert(cfg, caCert, caKey)
	if err != nil {
		log.Exit(err)
	}

	if err := server.WriteConfig(cfg, serverCert, serverKey); err != nil {
		log.Exit(err)
	}
}
