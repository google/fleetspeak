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

// Package main implements a fleetspeak server which backs to an sqlite
// database and includes the standard fleetspeak components.
//
// It is meant for testing and for very small installations. Installations which
// require a more scalable (or more redundant) database or non-standard
// components should fork this and make the needed changes for their
// installation.
package main

import (
	"io/ioutil"
	"net"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"flag"
	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"

	"github.com/google/fleetspeak/fleetspeak/src/demo/messagesaver"
	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/admin"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/grpcservice"
	"github.com/google/fleetspeak/fleetspeak/src/server/https"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"
	"github.com/google/fleetspeak/fleetspeak/src/server/sqlite"

	sgrpc "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"

	// Import protos that might be used in received messages, so that messagesave
	// will be able to produce a nice output.
	_ "github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/proto/fleetspeak_daemonservice"
	_ "github.com/google/fleetspeak/fleetspeak/src/client/stdinservice/proto/fleetspeak_stdinservice"
)

var (
	adminAddr    = flag.String("admin_addr", "", "Bind address for the admin rpc server.")
	databasePath = flag.String("database_path", "", "Path to the sqlite database.")
	configPath   = flag.String("config_path", "", "Path to configuration file.")
	httpsAddr    = flag.String("https_addr", "", "Bind address to listen for https connections from clients.")
	serverCert   = flag.String("server_cert", "", "Name of a certificate file to use to identify the server to clients.")
	serverKey    = flag.String("server_key", "", "Name of a key file to use to identify the server to clients.")
)

func main() {
	flag.Parse()

	ds := initDB()
	com, httpAddr := initCommunicator()
	conf := readConfig()

	// This starts the Fleetspeak server and begins listening for clients.
	s, err := server.MakeServer(conf,
		server.Components{
			Datastore: ds,
			ServiceFactories: map[string]service.Factory{
				"NOOP":         service.NOOPFactory,
				"MessageSaver": messagesaver.Factory,
				"GRPC":         grpcservice.Factory,
			},
			Communicators: []comms.Communicator{com}})
	if err != nil {
		log.Exitf("Unable to initialize Fleetspeak server: %v", err)
	}
	defer s.Stop()
	log.Infof("Fleetspeak server started, listening for clients on: %v", httpAddr)

	// Separately start listening for admin requests.
	as := serveAdminInterface(s, ds)
	if as != nil {
		defer as.GracefulStop()
	}

	// Wait for a sign that we should stop.
	sig := make(chan os.Signal)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	signal.Reset(os.Interrupt, syscall.SIGTERM)
}

func initDB() db.Store {
	ds, err := sqlite.MakeDatastore(*databasePath)
	if err != nil {
		log.Exitf("Unable to open or create sqlite database[%v]: %v", *databasePath, err)
	}
	return ds
}

func readConfig() *spb.ServerConfig {
	cb, err := ioutil.ReadFile(*configPath)
	if err != nil {
		log.Exitf("Unable to read configuration file [%v]: %v", *configPath, err)
	}
	var conf spb.ServerConfig
	if err := proto.UnmarshalText(string(cb), &conf); err != nil {
		log.Exitf("Unable to parse configuration file [%v]: %v", *configPath, err)
	}
	return &conf
}

func initCommunicator() (comms.Communicator, net.Addr) {
	cert, err := ioutil.ReadFile(*serverCert)
	if err != nil {
		log.Exitf("Unable to read server cert file [%v]: %v", *serverCert, err)
	}
	key, err := ioutil.ReadFile(*serverKey)
	if err != nil {
		log.Exitf("Unable to read server key file [%v]: %v", *serverKey, err)
	}
	addr, err := net.ResolveTCPAddr("tcp", *httpsAddr)
	if err != nil {
		log.Exitf("Unable to resolve https listener address [%v]: %v", *httpsAddr, err)
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Exitf("Unable to listen on [%v]: %v", addr, err)
	}
	com, err := https.NewCommunicator(l, cert, key)
	if err != nil {
		log.Exitf("Unable to initialize https communications: %v", err)
	}
	return com, l.Addr()
}

func serveAdminInterface(fs *server.Server, ds db.Store) *grpc.Server {
	if *adminAddr == "" {
		return nil
	}
	gs := grpc.NewServer()
	as := admin.NewServer(ds)
	sgrpc.RegisterAdminServer(gs, as)
	addr, err := net.ResolveTCPAddr("tcp", *adminAddr)
	if err != nil {
		log.Exitf("Unable to resolve admin listener address [%v]: %v", *adminAddr, err)
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Exitf("Unable to listen on [%v]: %v", addr, err)
	}
	go func() {
		err := gs.Serve(l)
		log.Errorf("Admin server finished with error: %v", err)
	}()
	log.Infof("Admin interface started, listening for clients on: %v", l.Addr())
	return gs
}
