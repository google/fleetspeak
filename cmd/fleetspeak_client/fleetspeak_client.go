package main

import (
	"context"
	"flag"
	"fmt"
	"os"

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

func innerMain(ctx context.Context, cfgReloadSignals <-chan os.Signal) error {
	for {
		cl, err := createClient()
		if err != nil {
			return fmt.Errorf("error starting client: %v", err)
		}

		select {
		case <-cfgReloadSignals:
			// We implement config reloading by tearing down the client and creating a
			// new one.
			log.Info("Config reload requested")
			cl.Stop()
			continue
		case <-ctx.Done():
			cl.Stop()
			return nil
		}
	}
}

func createClient() (*client.Client, error) {
	b, err := os.ReadFile(*configFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read configuration file %q: %v", *configFile, err)
	}
	cfgPB := &gpb.Config{}
	if err := prototext.Unmarshal(b, cfgPB); err != nil {
		return nil, fmt.Errorf("unable to parse configuration file %q: %v", *configFile, err)
	}
	cfg, err := generic.MakeConfiguration(cfgPB)
	if err != nil {
		return nil, fmt.Errorf("error in configuration file: %v", err)
	}

	var com comms.Communicator
	if cfgPB.Streaming {
		com = &https.StreamingCommunicator{}
	} else {
		com = &https.Communicator{}
	}

	return client.New(
		cfg,
		client.Components{
			ServiceFactories: map[string]service.Factory{
				"Daemon": daemonservice.Factory,
				"NOOP":   service.NOOPFactory,
				"Socket": socketservice.Factory,
				"Stdin":  stdinservice.Factory,
			},
			Communicator: com,
			Stats:        stats.NoopCollector{},
		},
	)
}

func main() {
	flag.Parse()
	entry.RunMain(innerMain, "FleetspeakService")
}
