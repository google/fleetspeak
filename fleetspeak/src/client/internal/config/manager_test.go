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

package config

import (
	"path/filepath"
	"testing"

	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func TestRekey(t *testing.T) {

	m, err := StartManager(&config.Configuration{
		FixedServices: make([]*fspb.ClientServiceConfig, 0),
	}, make(chan *fspb.ClientInfoData))
	if err != nil {
		t.Errorf("unable to create config manager: %v", err)
		return
	}
	defer m.Stop()

	id1 := m.ClientID()
	if (id1 == common.ClientID{}) {
		t.Errorf("new config manager should provide non-trivial ClientID")
	}
	if err := m.Rekey(); err != nil {
		t.Errorf("unable to rekey: %v", err)
	}
	id2 := m.ClientID()
	if (id2 == common.ClientID{}) || id2 == id1 {
		t.Errorf("ClientID after rekey is %v, expected to be non-trivial and different from %v", id2, id1)
	}
}

func TestWriteback(t *testing.T) {
	tmpPath, fin := comtesting.GetTempDir("TestWriteback")
	defer fin()

	ph, err := config.NewFilesystemPersistenceHandler(tmpPath, filepath.Join(tmpPath, "writeback"))
	if err != nil {
		t.Fatal(err)
	}

	m1, err := StartManager(&config.Configuration{
		PersistenceHandler: ph,
	}, make(chan *fspb.ClientInfoData))
	if err != nil {
		t.Errorf("unable to create config manager: %v", err)
		return
	}
	id1 := m1.ClientID()
	if (id1 == common.ClientID{}) {
		t.Errorf("New config manager should provide non-trivial ClientID.")
	}
	m1.Stop()

	ph, err = config.NewFilesystemPersistenceHandler(tmpPath, filepath.Join(tmpPath, "writeback"))
	if err != nil {
		t.Fatal(err)
	}

	m2, err := StartManager(&config.Configuration{
		PersistenceHandler: ph,
	}, make(chan *fspb.ClientInfoData))
	if err != nil {
		t.Errorf("Unable to create new config manager: %v", err)
		return
	}
	defer m2.Stop()
	id2 := m2.ClientID()
	if id2 != id1 {
		t.Errorf("Got clientID=%v in reconstituted config, expected %v", id2, id1)
	}
}
