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
	"github.com/google/fleetspeak/fleetspeak/src/server/plugins"

	ppb "github.com/google/fleetspeak/fleetspeak/src/server/plugins/proto/plugins"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

var pluginConfigPath = flag.String("plugin_config_path", "/etc/fleetspeak-server/plugins.config", "File describing the plugins to load.")
var serverConfigPath = flag.String("server_config_path", "/etc/fleetspeak-server/server.config", "File describing the overal server configuration.")

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
	b, err := ioutil.ReadFile(*pluginConfigPath)
	if err != nil {
		log.Exitf("Unable to read component config file [%s]: %v", *pluginConfigPath, err)
	}
	var c ppb.Config
	if err := proto.UnmarshalText(string(b), &c); err != nil {
		log.Exitf("Unable to parse component config file [%s]: %v", *pluginConfigPath, err)
	}
	r, err := plugins.Load(&c)
	if err != nil {
		log.Exitf("Failed to load components: %v", err)
	}
	return r
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
