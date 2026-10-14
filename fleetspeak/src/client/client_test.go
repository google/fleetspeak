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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/client/clienttestutils"
	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/common/anypbtest"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"google.golang.org/protobuf/proto"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	anypb "google.golang.org/protobuf/types/known/anypb"
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
					ServiceName: "FakeService",
				},
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
					ServiceName: "system",
				},
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

	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				MessageId: mid.Bytes(),
				Source:    &fspb.Address{ServiceName: "system"},
				Destination: &fspb.Address{
					ClientId:    oid.Bytes(),
					ServiceName: "system",
				},
				MessageType: "Die",
				Data: anypbtest.New(t, &fspb.DieRequest{
					Force: force,
				}),
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

	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				MessageId: mid.Bytes(),
				Source:    &fspb.Address{ServiceName: "system"},
				Destination: &fspb.Address{
					ClientId:    oid.Bytes(),
					ServiceName: "system"},
				MessageType: "RestartService",
				Data: anypbtest.New(t, &fspb.RestartServiceRequest{
					Name: "FakeService",
				}),
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
					ServiceName: "FakeService",
				},
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
					ServiceName: "FakeService",
				},
			}}); err != nil {
		t.Fatalf("Unable to process message: %v", err)
	}

	m := <-msgs

	if !bytes.Equal(m.MessageId, mid.Bytes()) {
		t.Errorf("Got message with id: %v, want: %v", m, mid)
	}
}

type clientStats struct {
	stats.NoopCollector
	messages atomic.Int32
}

func (cs *clientStats) AfterMessageProcessed(msg *fspb.Message, isLocal bool, err error) {
	if msg.GetSource().GetServiceName() == "NOOPService" {
		cs.messages.Add(1)
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
			Stats: cs,
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

// stopTimeout bounds how long the tests below wait for Client.Stop. Stop only
// has to stop in-process components and ack in-memory messages, so it returns
// well within this time unless it deadlocks.
const stopTimeout = 10 * time.Second

// queueServerMessages passes n messages for the server with the given priority
// and payload size to cl. It returns a counter of the calls to the messages'
// Ack callbacks.
func queueServerMessages(t *testing.T, cl *Client, n int, priority fspb.Message_Priority, payloadSize int) *atomic.Int32 {
	t.Helper()
	acks := &atomic.Int32{}
	for i := range n {
		ctx, cancel := context.WithTimeout(t.Context(), stopTimeout)
		err := cl.ProcessMessage(ctx, service.AckMessage{
			M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: "RemoteService"},
				Priority:    priority,
				Data:        &anypb.Any{Value: make([]byte, payloadSize)},
			},
			Ack: func() { acks.Add(1) },
		})
		cancel()
		if err != nil {
			t.Fatalf("ProcessMessage() of message %d/%d failed: %v", i+1, n, err)
		}
	}
	return acks
}

// stopWithTimeout calls cl.Stop and reports whether it returned within
// stopTimeout. If it did not, Stop keeps running in the background.
func stopWithTimeout(cl *Client) bool {
	stopped := make(chan struct{})
	go func() {
		cl.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		return true
	case <-time.After(stopTimeout):
		return false
	}
}

func TestStopAcksPendingMessages(t *testing.T) {
	// Without a communicator, nothing acks the messages for the server, so they
	// stay pending in the RetryLoop of their priority until Stop acks them. A
	// RetryLoop stops reading new messages while it holds maxBufferCount
	// messages or maxBufferBytes of data. HIGH isn't covered because the system
	// service sends HIGH messages concurrently, which makes the RetryLoop's
	// state unpredictable; all three RetryLoops run the same code.
	for _, tc := range []struct {
		name        string
		priority    fspb.Message_Priority
		messages    int
		payloadSize int
	}{
		{
			name:     "LowBelowLimits",
			priority: fspb.Message_LOW,
			messages: maxBufferCount - 1,
		},
		{
			name:     "LowAtCountLimit",
			priority: fspb.Message_LOW,
			messages: maxBufferCount,
		},
		{
			name:     "MediumAtCountLimit",
			priority: fspb.Message_MEDIUM,
			messages: maxBufferCount,
		},
		{
			name:        "LowAtSizeLimit",
			priority:    fspb.Message_LOW,
			messages:    1,
			payloadSize: maxBufferBytes,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl, err := New(config.Configuration{}, Components{})
			if err != nil {
				t.Fatalf("Unable to create client: %v", err)
			}
			acks := queueServerMessages(t, cl, tc.messages, tc.priority, tc.payloadSize)

			if !stopWithTimeout(cl) {
				t.Fatalf("Stop() with %d pending %v messages of %d bytes did not return within %v", tc.messages, tc.priority, tc.payloadSize, stopTimeout)
			}
			if got := acks.Load(); got != int32(tc.messages) {
				t.Errorf("Stop() acked %d of %d pending messages, want all of them acked once", got, tc.messages)
			}
		})
	}
}

// offlineCommunicator is a comms.Communicator which takes messages from the
// outbox but never delivers them, like a communicator which can't reach the
// server. Like the HTTPS communicators, it nacks the messages it holds when it
// is stopped.
type offlineCommunicator struct {
	comms.Communicator

	cctx comms.Context
	stop chan struct{}
	done chan struct{}
}

func (c *offlineCommunicator) Setup(cctx comms.Context) error {
	c.cctx = cctx
	return nil
}

func (c *offlineCommunicator) Start() error {
	c.stop = make(chan struct{})
	c.done = make(chan struct{})
	go func() {
		defer close(c.done)
		var held []comms.MessageInfo
		for {
			select {
			case m := <-c.cctx.Outbox():
				held = append(held, m)
			case <-c.stop:
				for _, m := range held {
					m.Nack()
				}
				return
			}
		}
	}()
	return nil
}

func (c *offlineCommunicator) Stop() {
	close(c.stop)
	<-c.done
}

func (c *offlineCommunicator) GetFileIfModified(context.Context, string, string, time.Time) (io.ReadCloser, time.Time, error) {
	return nil, time.Time{}, errors.New("server unreachable")
}

func TestStopWhileServerUnreachable(t *testing.T) {
	cl, err := New(config.Configuration{}, Components{Communicator: &offlineCommunicator{}})
	if err != nil {
		t.Fatalf("Unable to create client: %v", err)
	}
	// Fill the LOW RetryLoop with messages which the communicator takes but
	// can't deliver, as happens when a machine is offline for a while.
	queueServerMessages(t, cl, maxBufferCount, fspb.Message_LOW, 0)

	// Unlike TestStopAcksPendingMessages, don't count the acks: Stop only acks
	// the messages which the RetryLoop resends after the communicator nacks
	// them, and the RetryLoop may terminate before resending all of them.
	if !stopWithTimeout(cl) {
		t.Fatalf("Stop() with %d undeliverable messages did not return within %v", maxBufferCount, stopTimeout)
	}
}
