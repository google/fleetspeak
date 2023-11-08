package main

import (
	"flag"
	"io/ioutil"

	log "github.com/golang/glog"
	"google.golang.org/protobuf/encoding/prototext"

	"github.com/google/fleetspeak/fleetspeak/src/client"
	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/daemonservice"
	"github.com/google/fleetspeak/fleetspeak/src/client/entry"
	"github.com/google/fleetspeak/fleetspeak/src/client/generic"
	"github.com/google/fleetspeak/fleetspeak/src/client/https"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/socketservice"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
	"github.com/google/fleetspeak/fleetspeak/src/client/stdinservice"

	gpb "github.com/google/fleetspeak/fleetspeak/src/client/generic/proto/fleetspeak_client_generic"
)

var configFile = flag.String("config", "", "Client configuration file, required.")
var profileDir = flag.String("profile-dir", "/tmp", "Profile directory.")

func innerMain() {
	flag.Parse()

	b, err := ioutil.ReadFile(*configFile)
	if err != nil {
		log.Exitf("Unable to read configuration file [%s]: %v", *configFile, err)
	}
	cfgPB := &gpb.Config{}
	if err := prototext.Unmarshal(b, cfgPB); err != nil {
		log.Exitf("Unable to parse configuration file [%s]: %v", *configFile, err)
	}

	cfg, err := generic.MakeConfiguration(cfgPB)
	if err != nil {
		log.Exitf("Error in configuration file: %v", err)
	}

	var com comms.Communicator
	if cfgPB.Streaming {
		com = &https.StreamingCommunicator{}
	} else {
		com = &https.Communicator{}
	}

	cl, err := client.New(cfg,
		client.Components{
			ServiceFactories: map[string]service.Factory{
				"Daemon": daemonservice.Factory,
				"NOOP":   service.NOOPFactory,
				"Socket": socketservice.Factory,
				"Stdin":  stdinservice.Factory,
			},
			Communicator: com,
			Stats:        stats.NoopCollector{},
		})
	if err != nil {
		log.Exitf("Error starting client: %v", err)
	}

	entry.Wait(cl, *profileDir)
}

func main() {
	entry.RunMain(innerMain, "FleetspeakService")
}
