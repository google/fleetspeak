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

package client

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/google/fleetspeak/fleetspeak/src/client/clienttestutils"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// A fakeService implements Service and passes all received messages into a channel.
type fakeService struct {
	c  chan *fspb.Message
	sc service.Context
}

func (s *fakeService) Start(sc service.Context) error {
	s.sc = sc
	return nil
}

func (s *fakeService) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	s.c <- m
	return nil
}

func (s *fakeService) Stop() error {
	return nil
}

func TestMessageDelivery(t *testing.T) {
	msgs := make(chan *fspb.Message)
	fs := fakeService{c: msgs}

	fakeServiceFactory := func(*fspb.ClientServiceConfig) (service.Service, error) {
		return &fs, nil
	}

	cl, err := New(
		config.Configuration{
			FixedServices: []*fspb.ClientServiceConfig{{Name: "FakeService", Factory: "FakeService"}},
		},
		Components{
			ServiceFactories: map[string]service.Factory{
				"NOOP":        service.NOOPFactory,
				"FakeService": fakeServiceFactory,
			},
		})
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}
	defer cl.Stop()

	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("unable to create message id: %v", err)
	}

	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				MessageId: mid.Bytes(),
				Source:    &fspb.Address{ServiceName: "FakeService"},
				Destination: &fspb.Address{
					ClientId:    cl.config.ClientID().Bytes(),
					ServiceName: "FakeService"},
			}}); err != nil {
		t.Fatalf("unable to process message: %v", err)
	}

	m := <-msgs

	if !bytes.Equal(m.MessageId, mid.Bytes()) {
		t.Errorf("Expected message with id: %v, got: %v", mid, m)
	}

	// Check that the service.Context provided to fs has a working LocalInfo.
	// TODO: move this to a separate test.
	li := fs.sc.GetLocalInfo()
	if li.ClientID != cl.config.ClientID() {
		t.Errorf("Expected LocalInfo to have ClientID: %v, got %v", cl.config.ClientID(), li.ClientID)
	}
	if !reflect.DeepEqual(li.Services, []string{"FakeService"}) {
		t.Errorf("Expected LocalInfo to be %v, got %v", []string{"FakeService"}, li.Services)
	}
}

func TestRekey(t *testing.T) {
	cl, err := New(
		config.Configuration{
			FixedServices: []*fspb.ClientServiceConfig{{Name: "FakeService", Factory: "FakeService"}},
		},
		Components{
			ServiceFactories: map[string]service.Factory{
				"NOOP": service.NOOPFactory,
			},
		})
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}
	defer cl.Stop()

	oid := cl.config.ClientID()

	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("unable to create message id: %v", err)
	}

	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				MessageId: mid.Bytes(),
				Source:    &fspb.Address{ServiceName: "system"},
				Destination: &fspb.Address{
					ClientId:    oid.Bytes(),
					ServiceName: "system"},
				MessageType: "RekeyRequest",
			},
		}); err != nil {
		t.Fatalf("unable to process message: %v", err)
	}

	tk := time.NewTicker(200 * time.Millisecond)
	start := time.Now()
	var nid common.ClientID
	defer tk.Stop()
	for range tk.C {
		nid = cl.config.ClientID()
		if nid != oid {
			break
		}
		if time.Since(start) > 20*time.Second {
			t.Errorf("Timed out waiting for id to change.")
			break
		}
	}
}

func TestMessageValidation(t *testing.T) {
	msgs := make(chan *fspb.Message)
	fs := fakeService{c: msgs}

	fakeServiceFactory := func(*fspb.ClientServiceConfig) (service.Service, error) {
		return &fs, nil
	}

	cl, err := New(
		config.Configuration{
			FixedServices: []*fspb.ClientServiceConfig{{Name: "FakeService", Factory: "FakeService"}},
		},
		Components{
			ServiceFactories: map[string]service.Factory{
				"NOOP":        service.NOOPFactory,
				"FakeService": fakeServiceFactory,
			},
		})
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}

	for _, tc := range []struct {
		m    fspb.Message
		want string
	}{
		{m: fspb.Message{},
			want: "destination must have ServiceName",
		},
		{m: fspb.Message{Destination: &fspb.Address{ServiceName: ""}},
			want: "destination must have ServiceName",
		},
		{m: fspb.Message{Destination: &fspb.Address{ServiceName: "FakeService", ClientId: []byte("abcdef")}},
			want: "cannot send directly to client",
		},
	} {
		err := cl.ProcessMessage(context.Background(), service.AckMessage{M: &tc.m})
		if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("ProcessMessage(%v) should give error starting with [%v] but got [%v]", tc.m, tc.want, err)
		}
	}

	cl.Stop()
}

func TestServiceValidation(t *testing.T) {
	tmpPath, fin := comtesting.GetTempDir("client_service_validation")
	defer fin()

	k, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		t.Fatalf("Unable to generate deployment key: %v", err)
	}
	pk := *(k.Public().(*rsa.PublicKey))

	sp := filepath.Join(tmpPath, "services")
	if err := os.Mkdir(sp, 0777); err != nil {
		t.Fatalf("Unable to create services path [%s]: %v", sp, err)
	}

	msgs := make(chan *fspb.Message, 1)
	fakeServiceFactory := func(*fspb.ClientServiceConfig) (service.Service, error) {
		return &fakeService{c: msgs}, nil
	}

	// This factory is used for service configs that shouldn't validate - if it does, it
	// causes the test to fail.
	failingServiceFactory := func(cfg *fspb.ClientServiceConfig) (service.Service, error) {
		t.Fatalf("failingServiceFactory called on %s", cfg.Name)
		return nil, fmt.Errorf("failingServiceFactory called")
	}

	// A service which should work.
	cfg := signServiceConfig(t, k, &fspb.ClientServiceConfig{
		Name:           "FakeService",
		Factory:        "FakeService",
		RequiredLabels: []*fspb.Label{{ServiceName: "client", Label: "linux"}},
	})
	if err := clienttestutils.WriteSignedServiceConfig(sp, "FakeService.signed", cfg); err != nil {
		t.Fatal(err)
	}

	// A service signed with the wrong key - should not be started.
	bk, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		t.Fatalf("Unable to generate bad deployment key: %v", err)
	}
	cfg = signServiceConfig(t, bk, &fspb.ClientServiceConfig{Name: "FailingServiceBadSig", Factory: "FailingService"})
	if err := clienttestutils.WriteSignedServiceConfig(sp, "FailingServiceBadSig.signed", cfg); err != nil {
		t.Fatal(err)
	}

	// A service signed with the right key, but requiring the wrong label.
	cfg = signServiceConfig(t, bk, &fspb.ClientServiceConfig{
		Name:           "FailingServiceBadLabel",
		Factory:        "FailingService",
		RequiredLabels: []*fspb.Label{{ServiceName: "client", Label: "windows"}},
	})
	if err := clienttestutils.WriteSignedServiceConfig(sp, "FailingServiceBadLabel.signed", cfg); err != nil {
		t.Fatal(err)
	}

	ph, err := config.NewFilesystemPersistenceHandler(tmpPath, "")
	if err != nil {
		t.Fatal(err)
	}

	cl, err := New(
		config.Configuration{
			DeploymentPublicKeys: []rsa.PublicKey{pk},
			PersistenceHandler:   ph,
			ClientLabels: []*fspb.Label{
				{ServiceName: "client", Label: "TestClient"},
				{ServiceName: "client", Label: "linux"}},
		},
		Components{
			ServiceFactories: map[string]service.Factory{
				"NOOP":           service.NOOPFactory,
				"FakeService":    fakeServiceFactory,
				"FailingService": failingServiceFactory,
			},
		})
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}
	defer cl.Stop()

	// Check that the good service started by passing a message through it.
	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("unable to create message id: %v", err)
	}
	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				MessageId: mid.Bytes(),
				Source:    &fspb.Address{ServiceName: "FakeService"},
				Destination: &fspb.Address{
					ClientId:    cl.config.ClientID().Bytes(),
					ServiceName: "FakeService"},
			}}); err != nil {
		t.Fatalf("unable to process message: %v", err)
	}

	m := <-msgs

	if !bytes.Equal(m.MessageId, mid.Bytes()) {
		t.Errorf("Expected message with id: %v, got: %v", mid, m)
	}
}

func TestTextServiceConfig(t *testing.T) {
	tmpPath, fin := comtesting.GetTempDir("TestTextServiceConfig")
	defer fin()

	tsp := filepath.Join(tmpPath, "textservices")
	if err := os.Mkdir(tsp, 0777); err != nil {
		t.Fatalf("Unable to create services path [%s]: %v", tsp, err)
	}

	msgs := make(chan *fspb.Message, 1)
	fakeServiceFactory := func(*fspb.ClientServiceConfig) (service.Service, error) {
		return &fakeService{c: msgs}, nil
	}

	// A text service.
	cfg := &fspb.ClientServiceConfig{
		Name:           "FakeService",
		Factory:        "FakeService",
		RequiredLabels: []*fspb.Label{{ServiceName: "client", Label: "linux"}},
	}
	if err := clienttestutils.WriteServiceConfig(tsp, "FakeService.txt", cfg); err != nil {
		t.Fatal(err)
	}

	ph, err := config.NewFilesystemPersistenceHandler(tmpPath, "")
	if err != nil {
		t.Fatal(err)
	}

	cl, err := New(
		config.Configuration{
			PersistenceHandler: ph,
			ClientLabels: []*fspb.Label{
				{ServiceName: "client", Label: "TestClient"},
				{ServiceName: "client", Label: "linux"}},
		},
		Components{
			ServiceFactories: map[string]service.Factory{
				"NOOP":        service.NOOPFactory,
				"FakeService": fakeServiceFactory,
			},
		})
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}
	defer cl.Stop()

	// Check that the service started by passing a message through it.
	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("unable to create message id: %v", err)
	}
	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				MessageId: mid.Bytes(),
				Source:    &fspb.Address{ServiceName: "FakeService"},
				Destination: &fspb.Address{
					ClientId:    cl.config.ClientID().Bytes(),
					ServiceName: "FakeService"},
			}}); err != nil {
		t.Fatalf("unable to process message: %v", err)
	}

	m := <-msgs

	if !bytes.Equal(m.MessageId, mid.Bytes()) {
		t.Errorf("Expected message with id: %v, got: %v", mid, m)
	}
}

func signServiceConfig(t *testing.T, k *rsa.PrivateKey, cfg *fspb.ClientServiceConfig) *fspb.SignedClientServiceConfig {
	b, err := proto.Marshal(cfg)
	if err != nil {
		t.Fatalf("Unable to serialize service config: %v", err)
	}
	h := sha256.Sum256(b)
	sig, err := rsa.SignPSS(rand.Reader, k, crypto.SHA256, h[:], nil)
	if err != nil {
		t.Fatalf("Unable to sign service config: %v", err)
	}
	return &fspb.SignedClientServiceConfig{ServiceConfig: b, Signature: sig}
}
