// Copyright 2024 Google Inc.
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
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

type testStatsCollector struct {
	stats.NoopCollector
	created, processed atomic.Int32
}

// ContactDataCreated implements stats.CommsContextCollector
func (c *testStatsCollector) ContactDataCreated(wcd *fspb.WrappedContactData, err error) {
	c.created.Add(1)
}

// ContactDataCreated implements stats.CommsContextCollector
func (c *testStatsCollector) ContactDataProcessed(cd *fspb.ContactData, streaming bool, err error) {
	c.processed.Add(1)
}

type testCommunicator struct {
	comms.Communicator
	t *testing.T

	cctx   comms.Context
	cancel context.CancelFunc
}

func (c *testCommunicator) Setup(cctx comms.Context) error {
	c.cctx = cctx
	return nil
}

func (c *testCommunicator) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msgi := <-c.cctx.Outbox():
				if err := c.processMessage(ctx, msgi); err != nil {
					c.t.Errorf("Error while processing message: %v", err)
					cancel()
				}
			}
		}
	}()
	return nil
}

func (c *testCommunicator) Stop() {
	c.cancel()
}

func (c *testCommunicator) GetFileIfModified(ctx context.Context, service, name string, modSince time.Time) (data io.ReadCloser, mod time.Time, err error) {
	// GetFileIfModified gets called by the client to poll list of revoked certs,
	// returning an error causes the client to continue without it.
	return nil, time.Time{}, errors.New("file unavailable")
}

func (c *testCommunicator) processMessage(ctx context.Context, msgi comms.MessageInfo) error {
	defer msgi.Ack()
	if msgi.M.GetDestination().GetServiceName() != "RemoteService" {
		return nil
	}
	// Simulate sending the message to the server
	if _, _, err := c.cctx.MakeContactData([]*fspb.Message{msgi.M}, nil); err != nil {
		return err
	}
	// A real communicator would send the returned WrappedContactData to the server now

	// Simulate a response from the server service addressed to a client service.
	mid, err := common.RandomMessageID()
	if err != nil {
		return err
	}
	cd := &fspb.ContactData{
		Messages: []*fspb.Message{&fspb.Message{
			MessageId: mid.Bytes(),
			Source:    &fspb.Address{ServiceName: "RemoteService"},
			Destination: &fspb.Address{
				ClientId:    c.cctx.CurrentID().Bytes(),
				ServiceName: "NOOPService",
			},
		}},
	}
	return c.cctx.ProcessContactData(ctx, cd, false)
}

func TestCommsContextReportsStats(t *testing.T) {
	sc := &testStatsCollector{}

	// Create client with the testCommunicator and a NOOP client service.
	cl, err := New(
		config.Configuration{
			FixedServices: []*fspb.ClientServiceConfig{{
				Name:    "NOOPService",
				Factory: "NOOP",
			}},
		},
		Components{
			ServiceFactories: map[string]service.Factory{
				"NOOP": service.NOOPFactory,
			},
			Communicator: &testCommunicator{t: t},
			Stats:        sc,
		})
	if err != nil {
		t.Fatalf("Unable to create client: %v", err)
	}
	defer cl.Stop()

	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
	}

	// Simulate a message coming from a client service addressed to a server service,
	// This invokes the testCommunicator.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg := service.AckMessage{
		M: &fspb.Message{
			MessageId: mid.Bytes(),
			Source: &fspb.Address{
				ClientId:    cl.config.ClientID().Bytes(),
				ServiceName: "NOOPService",
			},
			Destination: &fspb.Address{ServiceName: "RemoteService"},
		},
		Ack: cancel,
	}
	if err := cl.ProcessMessage(ctx, msg); err != nil {
		t.Fatalf("Unable to process message: %v", err)
	}

	// Wait for the Ack callback which gets called when testCommunicator.processMessage returns
	<-ctx.Done()

	created := sc.created.Load()
	if created != 1 {
		t.Errorf("Got %d contact data created, want 1", created)
	}
	processed := sc.processed.Load()
	if processed != 1 {
		t.Errorf("Got %d contact data processed, want 1", processed)
	}
}
