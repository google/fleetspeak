// Copyright 2018 Google Inc.
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

// Package plugins allows the dynamic creation and loading of fleetspeak
// components, to be included in a Fleetspeak server.
package plugins

import (
	"fmt"
	"plugin"

	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/notifications"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	pb "github.com/google/fleetspeak/fleetspeak/src/server/plugins/proto/plugins"
)

// DatabaseFactory is a function which creates a db.Store from a configuration
// string.
type DatabaseFactory func(string) (db.Store, error)

// ServiceFactoryFactory is a function which creates a named service.Factory from a
// configuration string.
type ServiceFactoryFactory func(string) (string, service.Factory, error)

// CommunicatorFactory is a function which creates a comms.Communicator from a
// configuration string.
type CommunicatorFactory func(string) (comms.Communicator, error)

// StatsCollectorFactory is a function which creates a stats.Collector from a
// configuration string.
type StatsCollectorFactory func(string) (stats.Collector, error)

// AuthorizerFactory is a function which creates an authorizer.Authorizer from a
// configuration string.
type AuthorizerFactory func(string) (authorizer.Authorizer, error)

// NotifierFactory is a function which creates an notifications.Notifier from a
// configuration string.
type NotifierFactory func(string) (notifications.Notifier, error)

// ListenerFactory is a function which creates an notifications.Listener from a
// configuration string.
type ListenerFactory func(string) (notifications.Listener, error)

func Load(conf *pb.Config) (server.Components, error) {
	ds, err := LoadDatastore(conf.Datastore)
	if err != nil {
		return server.Components{}, err
	}

	sfs := make(map[string]service.Factory)
	for _, p := range conf.ServiceFactory {
		n, f, err := LoadServiceFactory(p)
		if err != nil {
			return server.Components{}, err
		}
		if sfs[n] != nil {
			return server.Components{}, fmt.Errorf("duplicate service factory name [%v] created by %v.", n, p)
		}
		sfs[n] = f
	}

	var cs []comms.Communicator
	for _, p := range conf.Communicator {
		c, err := LoadCommunicator(p)
		if err != nil {
			return server.Components{}, err
		}
		cs = append(cs, c)
	}

	var sc stats.Collector
	if conf.StatsCollector != nil {
		var err error
		sc, err = LoadStatsCollector(conf.StatsCollector)
		if err != nil {
			return server.Components{}, err
		}
	}

	var au authorizer.Authorizer
	if conf.Authorizer != nil {
		var err error
		au, err = LoadAuthorizer(conf.Authorizer)
		if err != nil {
			return server.Components{}, err
		}
	}

	var n notifications.Notifier
	if conf.Notifier != nil {
		var err error
		n, err = LoadNotifier(conf.Notifier)
		if err != nil {
			return server.Components{}, err
		}
	}

	var l notifications.Listener
	if conf.Listener != nil {
		var err error
		l, err = LoadListener(conf.Listener)
		if err != nil {
			return server.Components{}, err
		}
	}

	return server.Components{
		Datastore:        ds,
		ServiceFactories: sfs,
		Communicators:    cs,
		Stats:            sc,
		Authorizer:       au,
		Notifier:         n,
		Listener:         l,
	}, nil
}

func LoadDatastore(p *pb.Plugin) (db.Store, error) {
	f, err := loadFactory(p)
	if err != nil {
		return nil, err
	}
	df, ok := f.(DatabaseFactory)
	if !ok {
		return nil, fmt.Errorf("unable to load datastore from (%s:%s), expected DatabaseFactory got %T", p.Path, p.FactoryName, df)
	}
	return df(p.Config)
}

func LoadServiceFactory(p *pb.Plugin) (string, service.Factory, error) {
	f, err := loadFactory(p)
	if err != nil {
		return "", nil, err
	}
	df, ok := f.(ServiceFactoryFactory)
	if !ok {
		return "", nil, fmt.Errorf("unable to load datastore from (%s:%s), expected ServiceFactoryFactory got %T", p.Path, p.FactoryName, df)
	}
	return df(p.Config)
}

func LoadCommunicator(p *pb.Plugin) (comms.Communicator, error) {
	f, err := loadFactory(p)
	if err != nil {
		return nil, err
	}
	df, ok := f.(CommunicatorFactory)
	if !ok {
		return nil, fmt.Errorf("unable to load communicator from (%s:%s), expected CommunicatorFactory got %T", p.Path, p.FactoryName, df)
	}
	return df(p.Config)
}

func LoadStatsCollector(p *pb.Plugin) (stats.Collector, error) {
	f, err := loadFactory(p)
	if err != nil {
		return nil, err
	}
	df, ok := f.(StatsCollectorFactory)
	if !ok {
		return nil, fmt.Errorf("unable to load stats collector from (%s:%s), expected StatsCollectorFactory got %T", p.Path, p.FactoryName, df)
	}
	return df(p.Config)
}

func LoadAuthorizer(p *pb.Plugin) (authorizer.Authorizer, error) {
	f, err := loadFactory(p)
	if err != nil {
		return nil, err
	}
	df, ok := f.(AuthorizerFactory)
	if !ok {
		return nil, fmt.Errorf("unable to load authorizer from (%s:%s), expected AuthorizerFactory got %T", p.Path, p.FactoryName, df)
	}
	return df(p.Config)
}

func LoadNotifier(p *pb.Plugin) (notifications.Notifier, error) {
	f, err := loadFactory(p)
	if err != nil {
		return nil, err
	}
	df, ok := f.(NotifierFactory)
	if !ok {
		return nil, fmt.Errorf("unable to load notifier from (%s:%s), expected NotifierFactory got %T", p.Path, p.FactoryName, df)
	}
	return df(p.Config)
}

func LoadListener(p *pb.Plugin) (notifications.Listener, error) {
	f, err := loadFactory(p)
	if err != nil {
		return nil, err
	}
	df, ok := f.(ListenerFactory)
	if !ok {
		return nil, fmt.Errorf("unable to load listener from (%s:%s), expected ListenerFactory got %T", p.Path, p.FactoryName, df)
	}
	return df(p.Config)
}

func loadFactory(p *pb.Plugin) (plugin.Symbol, error) {
	pl, err := plugin.Open(p.Path)
	if err != nil {
		return nil, err
	}
	return pl.Lookup(p.FactoryName)
}
