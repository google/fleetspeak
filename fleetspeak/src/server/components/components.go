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

// Package components defines and instantiates the components needed by a
// generic Fleetspeak server.
//
// Installations requiring specialized components should branch this, or
// otherwise create a server.Components according to their needs.
package components

import (
	"database/sql"
	"errors"
	"fmt"
	"google.golang.org/grpc"
	"net"
	"net/http"

	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/admin"
	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	cauthorizer "github.com/google/fleetspeak/fleetspeak/src/server/components/authorizer"
	chttps "github.com/google/fleetspeak/fleetspeak/src/server/components/https"
	cnotifications "github.com/google/fleetspeak/fleetspeak/src/server/components/notifications"
	"github.com/google/fleetspeak/fleetspeak/src/server/grpcservice"
	"github.com/google/fleetspeak/fleetspeak/src/server/https"
	inotifications "github.com/google/fleetspeak/fleetspeak/src/server/internal/notifications"
	"github.com/google/fleetspeak/fleetspeak/src/server/mysql"
	"github.com/google/fleetspeak/fleetspeak/src/server/notifications"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	cpb "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// MakeComponents creates server components from a given config.
func MakeComponents(cfg cpb.Config) (*server.Components, error) {
	if cfg.MysqlDataSourceName == "" {
		return nil, errors.New("mysql_data_source_name is required")
	}
	if cfg.HttpsConfig == nil {
		return nil, errors.New("https_config is required")
	}
	hcfg := cfg.HttpsConfig
	if hcfg.ListenAddress == "" || hcfg.Certificates == "" || hcfg.Key == "" {
		return nil, errors.New("https_config requires listen_address, certificates and key")
	}

	acfg := cfg.AdminConfig
	if acfg != nil && acfg.ListenAddress == "" {
		return nil, errors.New("admin_config.listen_address can't be empty")
	}

	// Database setup
	con, err := sql.Open("mysql", cfg.MysqlDataSourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}

	db, err := mysql.MakeDatastore(con)
	if err != nil {
		return nil, fmt.Errorf("failed to create datastore: %v", err)
	}

	// Authorizer setup
	var auth authorizer.Authorizer
	if cfg.RequiredLabel == "" {
		auth = authorizer.PermissiveAuthorizer{}
	} else {
		auth = cauthorizer.LabelFilter{Label: cfg.RequiredLabel}
	}

	// HTTPS setup
	l, err := net.Listen("tcp", hcfg.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on [%v]: %v", cfg.HttpsConfig.ListenAddress, err)
	}
	if cfg.ProxyProtocol {
		l = &chttps.ProxyListener{l}
	}
	comm, err := https.NewCommunicator(https.Params{
		Listener:  l,
		Cert:      []byte(hcfg.Certificates),
		Key:       []byte(hcfg.Key),
		Streaming: !hcfg.DisableStreaming,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create communicator: %v", err)
	}
	// Notification setup.
	var nn notifications.Notifier
	var nl notifications.Listener
	if cfg.NotificationListenAddress != "" {
		nn = &cnotifications.HttpNotifier{}
		nl = &cnotifications.HttpListener{
			BindAddress:       cfg.NotificationListenAddress,
			AdvertisedAddress: cfg.NotificationPublicAddress,
		}
	} else {
		llc := inotifications.LocalListenerNotifier{}
		nn = &llc
		nl = &llc
	}

	var admSrv *grpc.Server
	if acfg != nil {
		as := admin.NewServer(db, nn)
		admSrv := grpc.NewServer()
		spb.RegisterAdminServer(admSrv, as)
		aas, err := net.ResolveTCPAddr("tcp", acfg.ListenAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize admin server: %v", err)
		}
		asl, err := net.ListenTCP("tcp", aas)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize admin server: %v", err)
		}
		go func() {
			admSrv.Serve(asl)
		}()
	}

	// Stats setup
	scfg := cfg.StatsConfig
	var statsCollector stats.Collector
	if scfg != nil {
		addressToExportStats := scfg.Address
		if addressToExportStats != "" {
			statsCollector = server.PrometheusStatsCollector{}
			http.Handle("/metrics", promhttp.Handler())
			go http.ListenAndServe(addressToExportStats, nil)
		}
	}

	// Final assembly
	return &server.Components{
		Datastore: db,
		ServiceFactories: map[string]service.Factory{
			"GRPC": grpcservice.Factory,
			"NOOP": service.NOOPFactory,
		},
		Communicators: []comms.Communicator{comm},
		Authorizer:    auth,
		Stats:		   statsCollector,
		Notifier:      nn,
		Listener:      nl,
		Admin:         admSrv,
	}, nil
}
