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
	"context"
	"flag"
	"io/ioutil"
	"os"
	"os/signal"
	"syscall"

	log "github.com/golang/glog"
	"google.golang.org/protobuf/encoding/prototext"

	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/components"

	cpb "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

var componentsConfigPath = flag.String("components_config", "/etc/fleetspeak-server/server.components.config", "File describing the server component configuration.")
var servicesConfigPath = flag.String("services_config", "/etc/fleetspeak-server/server.services.config", "File describing the server services configuration.")

func main() {
	flag.Parse()
	s, err := server.MakeServer(readServicesConfig(), loadComponents())
	if err != nil {
		log.Exitf("Unable to initialize Fleetspeak server: %v", err)
	}
	defer s.Stop()
	log.Infof("Fleetspeak server started.")

	// Wait for a sign that we should stop.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}

func loadComponents() server.Components {
	b, err := ioutil.ReadFile(*componentsConfigPath)
	if err != nil {
		log.Exitf("Unable to read component config file [%s]: %v", *componentsConfigPath, err)
	}
	c := &cpb.Config{}
	if err := prototext.Unmarshal(b, c); err != nil {
		log.Exitf("Unable to parse component config file [%s]: %v", *componentsConfigPath, err)
	}
	r, err := components.MakeComponents(c)
	if err != nil {
		log.Exitf("Failed to load components: %v", err)
	}
	return *r
}

func readServicesConfig() *spb.ServerConfig {
	cb, err := ioutil.ReadFile(*servicesConfigPath)
	if err != nil {
		log.Exitf("Unable to read services configuration file [%v]: %v", *servicesConfigPath, err)
	}
	conf := &spb.ServerConfig{}
	if err := prototext.Unmarshal(cb, conf); err != nil {
		log.Exitf("Unable to parse services configuration file [%v]: %v", *servicesConfigPath, err)
	}
	return conf
}
