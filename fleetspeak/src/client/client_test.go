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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/client/clienttestutils"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"go.uber.org/atomic"
	"google.golang.org/protobuf/proto"
	anypb "google.golang.org/protobuf/types/known/anypb"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

var (
	fakeServiceStopCount     = 0
	fakeServiceStartCount    = 0
	fakeServiceCountersMutex = &sync.Mutex{}
)

// A fakeService implements Service and passes all received messages into a channel.
type fakeService struct {
	c  chan *fspb.Message
	sc service.Context
}

func (s *fakeService) Start(sc service.Context) error {
	s.sc = sc
	fakeServiceCountersMutex.Lock()
	fakeServiceStartCount++
	fakeServiceCountersMutex.Unlock()
	return nil
}

func (s *fakeService) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	s.c <- m
	return nil
}

func (s *fakeService) Stop() error {
	fakeServiceCountersMutex.Lock()
	fakeServiceStopCount++
	fakeServiceCountersMutex.Unlock()
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
		t.Fatalf("Unable to create client: %v", err)
	}
	defer cl.Stop()

	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
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
		t.Fatalf("Unable to process message: %v", err)
	}

	m := <-msgs

	if !bytes.Equal(m.MessageId, mid.Bytes()) {
		t.Errorf("Got message with id: %v, want: %v", m, mid)
	}

	// Check that the service.Context provided to fs has a working LocalInfo.
	// TODO: move this to a separate test.
	li := fs.sc.GetLocalInfo()
	if li.ClientID != cl.config.ClientID() {
		t.Errorf("Got LocalInfo.ClientID: %v, want %v", li.ClientID, cl.config.ClientID())
	}
	if !reflect.DeepEqual(li.Services, []string{"FakeService"}) {
		t.Errorf("Got LocalInfo.Services: %v, want %v", li.Services, []string{"FakeService"})
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
		t.Fatalf("Unable to create client: %v", err)
	}
	defer cl.Stop()

	oid := cl.config.ClientID()

	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
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
		t.Fatalf("Unable to process message: %v", err)
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

func triggerDeath(force bool, t *testing.T) {
	cl, err := New(
		config.Configuration{},
		Components{})
	if err != nil {
		t.Fatalf("Unable to create client: %v", err)
	}
	defer cl.Stop()

	oid := cl.config.ClientID()

	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
	}

	data, err := anypb.New(&fspb.DieRequest{
		Force: force,
	})
	if err != nil {
		t.Fatalf("Can't marshal the DieRequest: %v", err)
	}

	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				MessageId: mid.Bytes(),
				Source:    &fspb.Address{ServiceName: "system"},
				Destination: &fspb.Address{
					ClientId:    oid.Bytes(),
					ServiceName: "system"},
				MessageType: "Die",
				Data:        data,
			},
		}); err != nil {
		t.Fatalf("Unable to process message: %v", err)
	}

	tk := time.NewTicker(200 * time.Millisecond)
	start := time.Now()
	defer tk.Stop()
	for range tk.C {
		if time.Since(start) > 20*time.Second {
			t.Errorf("Timed out waiting for the client to die.")
			break
		}
	}
}

func TestDie(t *testing.T) {
	// Using a workaround from https://talks.golang.org/2014/testing.slide#23 to test os.Exit behavior.
	if os.Getenv("TRIGGER_DEATH") == "1" {
		force, err := strconv.ParseBool(os.Getenv("TRIGGER_DEATH_FORCE"))
		if err != nil {
			t.Fatalf("Can't parse TRIGGER_DEATH_FORCE env variable: %v", err)
		}
		triggerDeath(force, t)
		return
	}

	for _, force := range []bool{true, false} {
		t.Run(fmt.Sprintf("TestDie[force=%v]", force), func(t *testing.T) {

			cmd := exec.Command(os.Args[0], "-test.run=TestDie")
			cmd.Env = append(os.Environ(), "TRIGGER_DEATH=1", fmt.Sprintf("TRIGGER_DEATH_FORCE=%v", force))
			err := cmd.Run()
			if e, ok := err.(*exec.ExitError); ok {
				if status, ok := e.Sys().(syscall.WaitStatus); ok && status.ExitStatus() == SuicideExitCode {
					return
				}
			}
			t.Fatalf("Process ran with err %v, want exit status %d", err, SuicideExitCode)
		})
	}
}

func TestDieDoesNotAck(t *testing.T) {
	cl, err := New(
		config.Configuration{},
		Components{},
	)
	if err != nil {
		t.Fatalf("Unable to create client: %v", err)
	}

	clientStopped := false
	stopClientOnce := func() {
		if !clientStopped {
			cl.Stop()
			clientStopped = true
		}
	}

	defer stopClientOnce()

	oid := cl.config.ClientID()

	midDie, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
	}
	midFoo, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
	}

	// Replace system service with a fake service

	cl.sc.services["system"].service.Stop()
	cl.sc.services["system"].service = &fakeService{c: make(chan *fspb.Message, 5)}

	// Send a Die message

	am := service.AckMessage{
		M: &fspb.Message{
			MessageId: midDie.Bytes(),
			Source:    &fspb.Address{ServiceName: "system"},
			Destination: &fspb.Address{
				ClientId:    oid.Bytes(),
				ServiceName: "system"},
			MessageType: "Die",
		},
	}

	if err := cl.ProcessMessage(context.Background(), am); err != nil {
		t.Fatalf("Unable to process message: %v", err)
	}

	// Send a Foo message

	am = service.AckMessage{
		M: &fspb.Message{
			MessageId: midFoo.Bytes(),
			Source:    &fspb.Address{ServiceName: "system"},
			Destination: &fspb.Address{
				ClientId:    oid.Bytes(),
				ServiceName: "system"},
			MessageType: "Foo",
		},
	}

	if err := cl.ProcessMessage(context.Background(), am); err != nil {
		t.Fatalf("Unable to process message: %v", err)
	}

	// Stop the client to make sure processing has finished.

	stopClientOnce()

	if len(cl.acks) != 1 {
		t.Fatalf("Got %v acks, want 1", len(cl.acks))
	}
}

func TestRestartService(t *testing.T) {
	fakeServiceCountersMutex.Lock()
	prevStartCount := fakeServiceStartCount
	prevStopCount := fakeServiceStopCount
	fakeServiceCountersMutex.Unlock()

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
		t.Fatalf("Unable to create client: %v", err)
	}
	defer cl.Stop()

	oid := cl.config.ClientID()

	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
	}

	data, err := anypb.New(&fspb.RestartServiceRequest{
		Name: "FakeService",
	})
	if err != nil {
		t.Fatalf("Can't marshal the RestartServiceRequest: %v", err)
	}

	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				MessageId: mid.Bytes(),
				Source:    &fspb.Address{ServiceName: "system"},
				Destination: &fspb.Address{
					ClientId:    oid.Bytes(),
					ServiceName: "system"},
				MessageType: "RestartService",
				Data:        data,
			},
		}); err != nil {
		t.Fatalf("Unable to process message: %v", err)
	}

	tk := time.NewTicker(200 * time.Millisecond)
	start := time.Now()
	defer tk.Stop()
	for range tk.C {
		fakeServiceCountersMutex.Lock()
		startDiff := fakeServiceStartCount - prevStartCount
		stopDiff := fakeServiceStopCount - prevStopCount
		fakeServiceCountersMutex.Unlock()

		// fakeService is started once and then restarted, a restart is a Stop()
		// followed by Start(), meaning that overall 2 starts and 1 stop are expected.
		if startDiff == 2 && stopDiff == 1 {
			break
		}

		if time.Since(start) > 20*time.Second {
			t.Errorf("Timed out waiting for the server restart.")
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
		t.Fatalf("Unable to create client: %v", err)
	}

	for _, tc := range []struct {
		m    *fspb.Message
		want string
	}{
		{m: &fspb.Message{},
			want: "destination must have ServiceName",
		},
		{m: &fspb.Message{Destination: &fspb.Address{ServiceName: ""}},
			want: "destination must have ServiceName",
		},
		{m: &fspb.Message{Destination: &fspb.Address{ServiceName: "FakeService", ClientId: []byte("abcdef")}},
			want: "cannot send directly to client",
		},
	} {
		err := cl.ProcessMessage(context.Background(), service.AckMessage{M: tc.m})
		if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("ProcessMessage(%v) got [%v] but should give error starting with [%v]", tc.m.String(), err, tc.want)
		}
	}

	cl.Stop()
}

func TestServiceValidation(t *testing.T) {
	tmpPath, fin := comtesting.GetTempDir("client_service_validation")
	defer fin()

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
	cfg := signServiceConfig(t, &fspb.ClientServiceConfig{
		Name:           "FakeService",
		Factory:        "FakeService",
		RequiredLabels: []*fspb.Label{{ServiceName: "client", Label: "linux"}},
	})
	if err := clienttestutils.WriteSignedServiceConfig(sp, "FakeService.signed", cfg); err != nil {
		t.Fatal(err)
	}

	// A service requiring the wrong label.
	cfg = signServiceConfig(t, &fspb.ClientServiceConfig{
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
			PersistenceHandler: ph,
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
		t.Fatalf("Unable to create client: %v", err)
	}
	defer cl.Stop()

	// Check that the good service started by passing a message through it.
	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
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
		t.Fatalf("Unable to process message: %v", err)
	}

	m := <-msgs

	if !bytes.Equal(m.MessageId, mid.Bytes()) {
		t.Errorf("Got message with id: %v, want: %v", m, mid)
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
		t.Fatalf("Unable to create client: %v", err)
	}
	defer cl.Stop()

	// Check that the service started by passing a message through it.
	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
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
		t.Fatalf("Unable to process message: %v", err)
	}

	m := <-msgs

	if !bytes.Equal(m.MessageId, mid.Bytes()) {
		t.Errorf("Got message with id: %v, want: %v", m, mid)
	}
}

type clientStats struct {
	stats.ClientCollector
	messages atomic.Int32
}

func (cs *clientStats) AfterMessageProcessed(msg *fspb.Message, isLocal bool, err error) {
	if msg.GetSource().GetServiceName() == "NOOPService" {
		cs.messages.Inc()
	}
}

func TestClientStats(t *testing.T) {
	cs := &clientStats{}

	cl, err := New(
		config.Configuration{
			FixedServices: []*fspb.ClientServiceConfig{{Name: "NOOPService", Factory: "NOOP"}},
		},
		Components{
			ServiceFactories: map[string]service.Factory{
				"NOOP": service.NOOPFactory,
			},
		})
	if err != nil {
		t.Fatalf("Unable to create client: %v", err)
	}
	defer cl.Stop()

	// Before overwriting the client's stats collector we have to wait until any
	// initial system service messages have been processed to not end up in a data race.
	// The client does not offer synchronization capabilities for this, but 1 second is
	// enough to prevent simultaneous data access.
	time.Sleep(time.Second)
	cl.stats = cs

	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
	}

	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				MessageId: mid.Bytes(),
				Source:    &fspb.Address{ServiceName: "NOOPService"},
				Destination: &fspb.Address{
					ClientId:    cl.config.ClientID().Bytes(),
					ServiceName: "NOOPService",
				},
			},
		}); err != nil {
		t.Fatalf("Unable to process message: %v", err)
	}

	messageCount := cs.messages.Load()
	if messageCount != 1 {
		t.Errorf("Unexpected number of messages reported, got: %d, want: 1", messageCount)
	}
}

func signServiceConfig(t *testing.T, cfg *fspb.ClientServiceConfig) *fspb.SignedClientServiceConfig {
	b, err := proto.Marshal(cfg)
	if err != nil {
		t.Fatalf("Unable to serialize service config: %v", err)
	}
	return &fspb.SignedClientServiceConfig{ServiceConfig: b}
}
