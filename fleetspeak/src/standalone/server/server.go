// Copyright 2019 Google Inc.
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

// Package main implements a fleetspeak standalone server which backs
// to a MySQL database and expects server plugins to be talking to
// the server through GRPC.
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
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"

	"database/sql"

	mysql "github.com/go-sql-driver/mysql"

	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/admin"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/grpcservice"
	"github.com/google/fleetspeak/fleetspeak/src/server/https"
	mysqlds "github.com/google/fleetspeak/fleetspeak/src/server/mysql"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"

	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	saspb "github.com/google/fleetspeak/fleetspeak/src/standalone/proto/fleetspeak_standalone_server"
)

var (
	configPath = flag.String("config", "", "Path to the configuration file.")
)

func main() {
	flag.Parse()
	conf := readConfig()

	ds := initDB(conf)
	com, httpAddr := initCommunicator(conf)

	serverConf := buildServerConfig(conf)
	// This starts the Fleetspeak server and begins listening for clients.
	s, err := server.MakeServer(serverConf,
		server.Components{
			Datastore: ds,
			ServiceFactories: map[string]service.Factory{
				"NOOP": service.NOOPFactory,
				"GRPC": grpcservice.Factory,
			},
			Communicators: []comms.Communicator{com}})
	if err != nil {
		log.Exitf("Unable to initialize Fleetspeak server: %v", err)
	}
	defer s.Stop()
	log.Infof("Fleetspeak server started, listening for clients on: %v", httpAddr)

	// Separately start listening for admin requests.
	as := serveAdminInterface(conf.AdminAddr, s, ds)
	if as != nil {
		defer as.GracefulStop()
	}

	// Wait for a sign that we should stop.
	sig := make(chan os.Signal)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	signal.Reset(os.Interrupt, syscall.SIGTERM)
}

func readConfig() *saspb.StandaloneServerConfig {
	cb, err := ioutil.ReadFile(*configPath)
	if err != nil {
		log.Exitf("Unable to read configuration file [%v]: %v", *configPath, err)
	}
	var conf saspb.StandaloneServerConfig
	if err := proto.UnmarshalText(string(cb), &conf); err != nil {
		log.Exitf("Unable to parse configuration file [%v]: %v", *configPath, err)
	}
	return &conf
}

func buildServerConfig(conf *saspb.StandaloneServerConfig) *spb.ServerConfig {
	res := spb.ServerConfig{
		BroadcastPollTime: conf.BroadcastPollTime,
	}
	res.Services = make([]*spb.ServiceConfig, len(conf.Services))
	for i, srv := range conf.Services {
		any, err := ptypes.MarshalAny(srv.Config)
		if err != nil {
			log.Exitf("Unable to marshal service config [%v]: %v", srv, err)
		}

		res.Services[i] = &spb.ServiceConfig{
			Name:           srv.Name,
			Factory:        "GRPC",
			MaxParallelism: srv.MaxParallelism,
			Config:         any,
		}
	}

	return &res
}

func initDB(conf *saspb.StandaloneServerConfig) db.Store {
	c := mysql.Config{
		User:   conf.MysqlUser,
		Passwd: conf.MysqlPassword,
		DBName: conf.MysqlDatabase,
		Addr:   conf.MysqlAddr,

		AllowNativePasswords: true,
		MultiStatements:      true,
	}
	dsn := c.FormatDSN()

	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Exitf("Unable to initialize MySQL connection [%v]: %v", dsn, err)
	}

	ds, err := mysqlds.MakeDatastore(conn)
	if err != nil {
		log.Exitf("Unable to initialize MySQL datastore: %v", err)
	}
	return ds
}

func initCommunicator(conf *saspb.StandaloneServerConfig) (comms.Communicator, net.Addr) {
	cert, err := ioutil.ReadFile(conf.ServerCert)
	if err != nil {
		log.Exitf("Unable to read server cert file [%v]: %v", conf.ServerCert, err)
	}
	key, err := ioutil.ReadFile(conf.ServerKey)
	if err != nil {
		log.Exitf("Unable to read server key file [%v]: %v", conf.ServerKey, err)
	}
	addr, err := net.ResolveTCPAddr("tcp", conf.HttpsAddr)
	if err != nil {
		log.Exitf("Unable to resolve https listener address [%v]: %v", conf.HttpsAddr, err)
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Exitf("Unable to listen on [%v]: %v", addr, err)
	}
	com, err := https.NewCommunicator(https.Params{Listener: l, Cert: cert, Key: key})
	if err != nil {
		log.Exitf("Unable to initialize https communications: %v", err)
	}
	return com, l.Addr()
}

func serveAdminInterface(adminAddr string, fs *server.Server, ds db.Store) *grpc.Server {
	if adminAddr == "" {
		return nil
	}
	gs := grpc.NewServer()
	as := admin.NewServer(ds, nil)
	spb.RegisterAdminServer(gs, as)
	addr, err := net.ResolveTCPAddr("tcp", adminAddr)
	if err != nil {
		log.Exitf("Unable to resolve admin listener address [%v]: %v", adminAddr, err)
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
