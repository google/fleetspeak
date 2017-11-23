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

// Package main tests a fleetspeak server talking through a grpcserver to a python loopback process.
package main

import (
	"net"
	"path"
	"time"

	"flag"
	"log"
	"context"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/admin"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/grpcservice"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"
	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"
	"github.com/google/fleetspeak/fleetspeak/src/server/sqlite"
	"github.com/google/fleetspeak/fleetspeak/src/server/testserver"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	gpb "github.com/google/fleetspeak/fleetspeak/src/server/grpcservice/proto/fleetspeak_grpcservice"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	sgrpc "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

var (
	adminAddr   = flag.String("admin_addr", "", "Bind address for the admin rpc server.")
	messageAddr = flag.String("message_addr", "", "The address to send messages to over grpc.")
)

func main() {
	flag.Parse()
	tempDir, tmpDirCleanup := comtesting.GetTempDir("grpcservice")
	defer tmpDirCleanup()
	p := path.Join(tempDir, "client_test.sqlite")
	ds, err := sqlite.MakeDatastore(p)

	sertesting.SetServerRetryTime(func(_ uint32) time.Time {
		return db.Now().Add(time.Second)
	})

	if err != nil {
		log.Fatalf("Unable to create datastore[%s]: %v", p, err)
	}
	ts := testserver.Server{
		DS: ds,
	}
	gc, err := ptypes.MarshalAny(&gpb.Config{
		Target:   *messageAddr,
		Insecure: true,
	})
	if err != nil {
		log.Fatalf("Unable to marshal grpcservice config: %v", err)
	}
	s, err := server.MakeServer(
		&spb.ServerConfig{
			Services: []*spb.ServiceConfig{{
				Name:    "TestService",
				Factory: "GRPC",
				Config:  gc,
			}},
		},
		server.Components{
			Datastore:        ds,
			ServiceFactories: map[string]service.Factory{"GRPC": grpcservice.Factory},
			Communicators:    []comms.Communicator{&testserver.FakeCommunicator{&ts}},
		},
	)
	ts.S = s
	if err != nil {
		log.Fatalf("Unable to create server: %v", err)
	}
	serveAdminInterface(s, ds)

	id, err := ts.AddClient()
	if err != nil {
		log.Fatalf("Unable to add client: %v", err)
	}

	m := &fspb.Message{
		SourceMessageId: []byte("2"),
		Source: &fspb.Address{
			ClientId:    id.Bytes(),
			ServiceName: "TestService",
		},
		Destination: &fspb.Address{
			ServiceName: "TestService",
		},
		MessageType: "TestMessage",
	}
	mid := common.MakeMessageID(m.Source, m.SourceMessageId)
	m.MessageId = mid.Bytes()
	ctx, fin := context.WithTimeout(context.Background(), 30*time.Second)
	defer fin()
	// Because of grpc retry logic and the possibility of getting an error when
	// the message was successfully sent, the message might pass through the
	// loopback during the first SimulateContact call.
	msgs, err := ts.SimulateContactFromClient(ctx, id, []*fspb.Message{m})
	if err != nil {
		log.Fatalf("Error putting message into system: %v", err)
	}
	var resp *fspb.Message
	for len(msgs) == 0 {
		msgs, err = ts.SimulateContactFromClient(ctx, id, nil)
		if err != nil {
			log.Fatalf("Error while waiting for loopback: %v", err)
		}
		if len(msgs) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(msgs) > 1 {
		log.Fatalf("Expected 1 message back, got %d", len(msgs))
	}
	resp = msgs[0]

	log.Printf("Received through loopback: %+v", resp)
	if resp.MessageType != "TestMessage" {
		log.Fatalf("Looped message had unexpected MessageType, got [%v] want [%v]", resp.MessageType, "TestMessage")
	}
}

func serveAdminInterface(fs *server.Server, ds db.Store) *grpc.Server {
	gs := grpc.NewServer()
	as := admin.NewServer(ds)
	sgrpc.RegisterAdminServer(gs, as)
	addr, err := net.ResolveTCPAddr("tcp", *adminAddr)
	if err != nil {
		log.Fatalf("Unable to resolve admin listener address [%v]: %v", *adminAddr, err)
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Fatalf("Unable to listen on [%v]: %v", addr, err)
	}
	go func() {
		err := gs.Serve(l)
		log.Printf("Admin server finished with error: %v", err)
	}()
	log.Printf("Admin interface started, listening for clients on: %v", l.Addr())
	return gs
}
