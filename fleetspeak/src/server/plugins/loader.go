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

// Package plugins allows the dynamic creation and loading of fleetspeak
// components, to be included in a Fleetspeak server.
package plugins

import (
	"fmt"
	"plugin"

	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"

	pb "github.com/google/fleetspeak/fleetspeak/src/server/plugins/proto/plugins"
)

type DatabaseFactory func(string) (db.Store, error)

type ServiceFactoryFactory func(string) (string, service.Factory, error)

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
			return server.Components{}, fmt.Errorf("Duplicate service factory name [%v] created by %v.", n, p)
		}
		sfs[n] = f
	}
	return server.Components{
		Datastore: ds,
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

func loadFactory(p *pb.Plugin) (plugin.Symbol, error) {
	pl, err := plugin.Open(p.Path)
	if err != nil {
		return nil, err
	}
	return pl.Lookup(p.FactoryName)
}
