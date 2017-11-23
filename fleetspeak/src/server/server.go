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

// Package server contains the components and utilities that every Fleetspeak server should include.
package server

import (
	"sync"
	"time"

	"log"
	"context"
	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/broadcasts"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/services"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	dpb "github.com/golang/protobuf/ptypes/duration"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// Components gathers the external components required to instantiate a Fleetspeak Server.
type Components struct {
	Datastore        db.Store                   // Required, used to store all server state.
	ServiceFactories map[string]service.Factory // Required, used to configure services according to the ServerConfig.
	Communicators    []comms.Communicator       // Required to communicate with clients.
	Stats            stats.Collector            // If set, will be notified about interesting events.
	Authorizer       authorizer.Authorizer      // If set, will control and validate contacts from clients.
}

// A Server is an active fleetspeak server instance.
type Server struct {
	config           *spb.ServerConfig
	dataStore        db.Store
	done             chan struct{}
	serviceConfig    *services.Manager
	comms            []comms.Communicator
	processing       sync.WaitGroup
	broadcastManager *broadcasts.Manager
	statsCollector   stats.Collector
	authorizer       authorizer.Authorizer
}

// MakeServer builds and initializes a fleetspeak server using the provided components.
func MakeServer(c *spb.ServerConfig, sc Components) (*Server, error) {
	if sc.Stats == nil {
		sc.Stats = noopStatsCollector{}
	} else {
		sc.Datastore = MonitoredDatastore{
			D: sc.Datastore,
			C: sc.Stats,
		}
	}
	if sc.Authorizer == nil {
		sc.Authorizer = authorizer.PermissiveAuthorizer{}
	}
	s := Server{
		config:         c,
		dataStore:      sc.Datastore,
		done:           make(chan struct{}),
		comms:          sc.Communicators,
		statsCollector: sc.Stats,
		authorizer:     sc.Authorizer,
	}

	s.serviceConfig = services.NewManager(sc.Datastore, sc.ServiceFactories, sc.Stats)

	for _, pc := range c.Services {
		if err := s.serviceConfig.Install(pc); err != nil {
			return nil, err
		}
	}
	for _, cm := range s.comms {
		if err := cm.Setup(commsContext{&s}); err != nil {
			return nil, err
		}
	}

	for i, c := range s.comms {
		if err := c.Start(); err != nil {
			for j := 0; j < i; j++ {
				s.comms[j].Stop()
			}
			return nil, err
		}
	}
	if c.BroadcastPollTime == nil {
		c.BroadcastPollTime = &dpb.Duration{Seconds: 60}
	}
	bm, err := broadcasts.MakeManager(context.Background(), sc.Datastore,
		time.Duration(c.BroadcastPollTime.Seconds)*time.Second+time.Duration(c.BroadcastPollTime.Nanos)*time.Nanosecond)
	if err != nil {
		return nil, err
	}
	s.broadcastManager = bm

	s.dataStore.RegisterMessageProcessor(s.serviceConfig)
	return &s, nil
}

// Stop shuts down the server.
func (s *Server) Stop() {
	s.dataStore.StopMessageProcessor()
	for _, c := range s.comms {
		c.Stop()
	}
	close(s.done)
	s.processing.Wait()
	s.serviceConfig.Stop()
	if err := s.broadcastManager.Close(context.Background()); err != nil {
		log.Printf("Error closing BroadcastManager: %v", err)
	}
	if err := s.dataStore.Close(); err != nil {
		log.Printf("Error closing datastore: %v", err)
	}
}
