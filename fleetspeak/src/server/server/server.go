// Copyright 2018 Google Inc.
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

// Package main defines an entry point for a general purpose fleetspeak server.
package main

import (
	"flag"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/components"

	cpb "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

var componentConfigPath = flag.String("component_config_path", "/etc/fleetspeak-server/components.config", "File describing the server component configuration.")
var serverConfigPath = flag.String("server_config_path", "/etc/fleetspeak-server/server.config", "File describing the overall server configuration.")

func main() {
	flag.Parse()
	s, err := server.MakeServer(readConfig(), loadComponents())
	if err != nil {
		log.Exitf("Unable to initialize Fleetspeak server: %v", err)
	}
	defer s.Stop()
	log.Infof("Fleetspeak server started.")

	// Wait for a sign that we should stop.
	sig := make(chan os.Signal)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	signal.Reset(os.Interrupt, syscall.SIGTERM)
}

func loadComponents() server.Components {
	b, err := ioutil.ReadFile(*componentConfigPath)
	if err != nil {
		log.Exitf("Unable to read component config file [%s]: %v", *componentConfigPath, err)
	}
	var c cpb.Config
	if err := proto.UnmarshalText(string(b), &c); err != nil {
		log.Exitf("Unable to parse component config file [%s]: %v", *componentConfigPath, err)
	}
	r, err := components.MakeComponents(c)
	if err != nil {
		log.Exitf("Failed to load components: %v", err)
	}
	return *r
}

func readConfig() *spb.ServerConfig {
	cb, err := ioutil.ReadFile(*serverConfigPath)
	if err != nil {
		log.Exitf("Unable to read server configuration file [%v]: %v", *serverConfigPath, err)
	}
	var conf spb.ServerConfig
	if err := proto.UnmarshalText(string(cb), &conf); err != nil {
		log.Exitf("Unable to parse server configuration file [%v]: %v", *serverConfigPath, err)
	}
	return &conf
}
