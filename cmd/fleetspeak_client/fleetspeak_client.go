package main

import (
	"context"
	"flag"
	"fmt"
	"os"

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

func innerMain(ctx context.Context) error {
	b, err := os.ReadFile(*configFile)
	if err != nil {
		return fmt.Errorf("unable to read configuration file %q: %v", *configFile, err)
	}
	cfgPB := &gpb.Config{}
	if err := prototext.Unmarshal(b, cfgPB); err != nil {
		return fmt.Errorf("unable to parse configuration file %q: %v", *configFile, err)
	}
	cfg, err := generic.MakeConfiguration(cfgPB)
	if err != nil {
		return fmt.Errorf("error in configuration file: %v", err)
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
		return fmt.Errorf("error starting client: %v", err)
	}

	select {
	case <-ctx.Done():
		cl.Stop()
	}
	return nil
}

func main() {
	flag.Parse()
	entry.RunMain(innerMain, "FleetspeakService")
}
