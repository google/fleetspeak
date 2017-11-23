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

// Package testserver configures and creates a Fleetspeak server instance
// suitable for unit tests. It also provides utility methods for directly
// adjusting the server state in tests.
package testserver

import (
	"crypto/rand"
	"crypto/rsa"
	"net"
	"path"
	"testing"
	"time"

	"log"
	"context"
	"github.com/golang/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"
	"github.com/google/fleetspeak/fleetspeak/src/server/sqlite"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// Server is a test server, with related structures and interfaces to allow
// tests to manipulate it.
type Server struct {
	S  *server.Server
	DS *sqlite.Datastore
	CC comms.Context
}

// FakeCommunicator implements comms.Communicator to do nothing by
// save the comms.Context to a Server. Most users should simply call
// Make, but this is exposed in order to support more flexible setup
// of test servers.
type FakeCommunicator struct {
	Dest *Server
}

// Setup implements comms.Communicator.
func (c FakeCommunicator) Setup(cc comms.Context) error {
	c.Dest.CC = cc
	return nil
}

// Start implements comms.Communicator.
func (c FakeCommunicator) Start() error { return nil }

// Stop implements comms.Communicator.
func (c FakeCommunicator) Stop() {}

// Make creates a server.Server using the provided communicators. It creates and
// attaches it to an sqlite datastore based on the test and test case names.
func Make(t *testing.T, testName, caseName string, comms []comms.Communicator) Server {
	tempDir, tmpDirCleanup := comtesting.GetTempDir(testName)
	defer tmpDirCleanup()
	p := path.Join(tempDir, caseName+".sqlite")
	ds, err := sqlite.MakeDatastore(p)
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("Created database: %s", p)

	var ret Server

	s, err := server.MakeServer(
		&spb.ServerConfig{
			Services: []*spb.ServiceConfig{{
				Name:           "TestService",
				Factory:        "NOOP",
				MaxParallelism: 5,
			}},
		},
		server.Components{
			Datastore:        ds,
			ServiceFactories: map[string]service.Factory{"NOOP": service.NOOPFactory},
			Communicators:    append(comms, FakeCommunicator{&ret}),
		})
	if err != nil {
		t.Fatal(err)
	}
	ret.S = s
	ret.DS = ds
	return ret
}

// AddClient adds a new client with a random id to a server.
func (s Server) AddClient() (common.ClientID, error) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return common.ClientID{}, err
	}
	id, err := common.MakeClientID(k.Public())
	if err != nil {
		return common.ClientID{}, err
	}
	if _, err := s.CC.AddClient(context.Background(), id, k.Public()); err != nil {
		return common.ClientID{}, err
	}
	return id, err
}

// ProcessMessageFromClient delivers a message to a server, simulating that it was
// provided by a client. It then waits up to 30 seconds for it to be processed.
func (s Server) ProcessMessageFromClient(id common.ClientID, msg *fspb.Message) error {
	ctx := context.Background()

	mid, err := common.BytesToMessageID(msg.MessageId)
	if err != nil {
		return err
	}

	if _, err := s.SimulateContactFromClient(ctx, id, []*fspb.Message{msg}); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		msg := s.GetMessage(ctx, mid)
		if msg.Result != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// SimulateContactFromClient accepts zero or more messages as if they came from
// a client, and returns any messages pending for delivery to the client.
func (s Server) SimulateContactFromClient(ctx context.Context, id common.ClientID, msgs []*fspb.Message) ([]*fspb.Message, error) {
	info, err := s.CC.GetClientInfo(ctx, id)
	if err != nil {
		return nil, err
	}

	cd := fspb.ContactData{Messages: msgs}
	cdb, err := proto.Marshal(&cd)
	if err != nil {
		return nil, err
	}
	rcd, err := s.CC.HandleClientContact(ctx, info,
		&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 123},
		&fspb.WrappedContactData{ContactData: cdb})
	if err != nil {
		return nil, err
	}
	return rcd.Messages, nil
}

// GetMessage retrieves a single message from the datastore, or dies trying.
func (s Server) GetMessage(ctx context.Context, id common.MessageID) *fspb.Message {
	msgs, err := s.DS.GetMessages(ctx, []common.MessageID{id}, true)
	if err != nil {
		log.Fatal(err)
	}
	if len(msgs) != 1 {
		log.Fatalf("Expected 1 message, got: %v", msgs)
	}
	return msgs[0]
}
