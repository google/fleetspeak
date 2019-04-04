package main

import (
	"flag"
	"io/ioutil"
	"os"
	"os/signal"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client"
	"github.com/google/fleetspeak/fleetspeak/src/client/daemonservice"
	"github.com/google/fleetspeak/fleetspeak/src/client/generic"
	"github.com/google/fleetspeak/fleetspeak/src/client/https"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/socketservice"
	"github.com/google/fleetspeak/fleetspeak/src/client/stdinservice"

	gpb "github.com/google/fleetspeak/fleetspeak/src/client/generic/proto/fleetspeak_client_generic"
)

var configFile = flag.String("config_file", "", "Client configuration file, required.")

func main() {
	flag.Parse()

	b, err := ioutil.ReadFile(*configFile)
	if err != nil {
		log.Exitf("Unable to read configuration file [%s]: %v", *configFile, err)
	}
	var cfgPB gpb.Config
	if err := proto.UnmarshalText(string(b), &cfgPB); err != nil {
		log.Exitf("Unable to parse configuration file [%s]: %v", *configFile, err)
	}

	cfg, err := generic.MakeConfiguration(cfgPB)
	if err != nil {
		log.Exitf("Error in configuration file: %v", err)
	}
	cl, err := client.New(cfg,
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
		log.Exitf("Error starting client: %v", err)
	}

	s := make(chan os.Signal)
	signal.Notify(s, os.Interrupt)
	<-s
	signal.Reset(os.Interrupt)
	cl.Stop()
}
