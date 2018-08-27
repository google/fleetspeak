package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"sync"

	log "github.com/golang/glog"
	osquery "github.com/kolide/osquery-go"

	"github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/client"
	"github.com/google/fleetspeak/fleetspeak/src/osquery/plugin"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

const version = "0.1"

var (
	socketPath  = flag.String("socket", "", "path to osqueryd extensions socket")
	logService  = flag.String("log_service", "", "If set, a logger extention will be registered which logs to this Fleetspeak service.")
	managerName = flag.String("manager_name", "Fleetspeak", "Name to register the extension manager as, also used as a prefix for the plugin names.")
)

func main() {
	flag.Parse()

	ch, err := client.Init(version)
	if err != nil {
		log.Exitf("Unable to initialize FS connection: %v", err)
	}

	server, err := osquery.NewExtensionManagerServer(*managerName, *socketPath)
	if err != nil {
		log.Exitf("Unable to create osquery extension manager: %v", err)
	}

	stop := make(chan struct{})
	var working sync.WaitGroup
	defer func() {
		close(stop)
		working.Wait()
	}()
	in := make(chan *fspb.Message, 20)

	working.Add(1)
	go func() {
		defer working.Done()
		for {
			select {
			case m := <-ch.In:
				select {
				case <-stop:
					return
				case err := <-ch.Err:
					log.Exitf("Error from channel: %v", err)
				case in <- m:
				}
			case <-stop:
				return
			case err := <-ch.Err:
				log.Exitf("Error from channel: %v", err)
			}
		}
	}()

	server.RegisterPlugin(plugin.MakeDistributed(*managerName+"Queries", in, ch.Out))

	if *logService != "" {
		server.RegisterPlugin(plugin.MakeLogger(*managerName+"Logger", *logService, ch.Out))
	}

	done := make(chan struct{})
	go func() {
		if err := server.Start(); err != nil {
			log.Errorf("server.Start() returned error: %v", err)
		} else {
			log.Infof("server.Start() terminated normally")
		}
		close(done)
	}()

	s := make(chan os.Signal)
	signal.Notify(s, os.Interrupt)
	select {
	case <-s:
		server.Shutdown(context.Background())
		log.Infof("Interrupt received, waiting for server to finish.")
		<-done
	case <-done:
	}
	signal.Reset(os.Interrupt)
}
