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
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"io/ioutil"
	"testing"

	"log"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/common"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func TestRekey(t *testing.T) {

	m, err := StartManager(&config.Configuration{
		Ephemeral:     true,
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
	if err := m.rekey(); err != nil {
		t.Errorf("unable to rekey: %v", err)
	}
	id2 := m.ClientID()
	if (id2 == common.ClientID{}) || id2 == id1 {
		t.Errorf("ClientID after rekey is %v, expected to be non-trivial and different from %v", id2, id1)
	}
}

func TestWriteback(t *testing.T) {
	d, err := ioutil.TempDir("", "TestWriteback_")
	if err != nil {
		t.Fatalf("unable to create temp directory: %v", err)
	}
	log.Printf("Create temp directory: %v", d)

	m1, err := StartManager(&config.Configuration{
		ConfigurationPath: d,
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

	m2, err := StartManager(&config.Configuration{
		ConfigurationPath: d,
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

func TestValidateServiceConfig(t *testing.T) {
	k1, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Errorf("unable to generate deployment key: %v", err)
		return
	}

	k2, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Errorf("unable to generate deployment key: %v", err)
		return
	}

	m, err := StartManager(
		&config.Configuration{
			DeploymentPublicKeys: []rsa.PublicKey{k1.PublicKey, k2.PublicKey},
			Ephemeral:            true,
			FixedServices:        make([]*fspb.ClientServiceConfig, 0),
		}, make(chan *fspb.ClientInfoData))

	for _, k := range []*rsa.PrivateKey{k1, k2} {
		sd := fspb.SignedClientServiceConfig{ServiceConfig: []byte("A serialized proto")}
		hashed := sha256.Sum256(sd.ServiceConfig)
		sd.Signature, err = rsa.SignPSS(rand.Reader, k, crypto.SHA256, hashed[:], nil)
		if err != nil {
			t.Errorf("unable to sign ServiceData: %v", err)
		}
		if err := m.ValidateServiceConfig(&sd); err != nil {
			t.Errorf("ValidateServiceConfig with valid key should not error, got: %v", err)
		}
		sd.ServiceConfig = []byte("A corrupted serialized proto")
		if err := m.ValidateServiceConfig(&sd); err == nil {
			t.Error("ValidateServiceConfig on corrupt proto should error, got nil")
		}
	}
}
