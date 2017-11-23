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

// Package integrationtest defines methods to implement integration
// tests in which a server and one or more clients are brought up and
// exercised.
package integrationtest

import (
	"bytes"
	"crypto/x509"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"log"
	"context"
	"github.com/golang/protobuf/ptypes"

	chttps "github.com/google/fleetspeak/fleetspeak/src/client/https"
	"github.com/google/fleetspeak/fleetspeak/src/server/admin"
	"google.golang.org/grpc"

	"github.com/google/fleetspeak/fleetspeak/src/client"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	cservice "github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/inttesting/frr"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/https"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"
	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	dpb "github.com/golang/protobuf/ptypes/duration"
	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
	fpb "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	fgrpc "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	sgrpc "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

const numClients = 5

// statsCounter is a simple server.StatsCollector. It only counts messages for the "FRR" service.
type statsCounter struct {
	messagesIngested, payloadBytesSaved, messagesProcessed, messagesErrored, messagesDropped, clientPolls, datastoreOperations int64
}

func (c *statsCounter) MessageIngested(service, messageType string, backlogged bool, payloadBytes int) {
	if service == "FRR" {
		atomic.AddInt64(&c.messagesIngested, 1)
	}
}

func (c *statsCounter) MessageSaved(service, messageType string, forClient bool, savedPayloadBytes int) {
	if service == "FRR" {
		atomic.AddInt64(&c.payloadBytesSaved, int64(savedPayloadBytes))
	}
}

func (c *statsCounter) MessageProcessed(start, end time.Time, service, messageType string) {
	if service == "FRR" {
		atomic.AddInt64(&c.messagesProcessed, 1)
	}
}

func (c *statsCounter) MessageErrored(start, end time.Time, service, messageType string, isTemp bool) {
	if service == "FRR" {
		atomic.AddInt64(&c.messagesErrored, 1)
	}
}

func (c *statsCounter) MessageDropped(service, messageType string) {
	if service == "FRR" {
		atomic.AddInt64(&c.messagesDropped, 1)
	}
}

func (c *statsCounter) ClientPoll(stats.PollInfo) {
	atomic.AddInt64(&c.clientPolls, 1)
}

func (c *statsCounter) DatastoreOperation(start, end time.Time, operation string, err error) {
	atomic.AddInt64(&c.datastoreOperations, 1)
}

func (c *statsCounter) ResourceUsageDataReceived(rud mpb.ResourceUsageData) {
}

// FRRIntegrationTest spins up a small FRR installation, backed by the provided datastore
// and exercies it.
func FRRIntegrationTest(t *testing.T, ds db.Store, tmpDir string) {
	fin := sertesting.SetServerRetryTime(func(_ uint32) time.Time {
		return db.Now().Add(time.Second)
	})
	defer fin()

	ctx := context.Background()

	if err := ds.StoreFile(ctx, "system", "RevokedCertificates", bytes.NewReader([]byte{})); err != nil {
		t.Errorf("Unable to store RevokedCertificate file: %v", err)
	}

	// Create a FRR master server and start it listening.
	masterServer := frr.NewMasterServer(nil)
	gms := grpc.NewServer()
	fgrpc.RegisterMasterServer(gms, masterServer)
	ad, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	tl, err := net.ListenTCP("tcp", ad)
	if err != nil {
		t.Fatal(err)
	}
	defer gms.Stop()
	go func() {
		log.Printf("Finished with MasterServer[%v]: %v", tl.Addr(), gms.Serve(tl))
	}()

	// Create FS server certs and server communicator.
	cert, key, err := comtesting.ServerCert()
	if err != nil {
		t.Fatal(err)
	}
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	com, err := https.NewCommunicator(listener, cert, key)
	if err != nil {
		t.Fatal(err)
	}
	log.Printf("Communicator listening to: %v", listener.Addr())

	// Create authorizer and signers.
	auth, signs := makeAuthorizerSigners(t)

	// Create and start a FS server.
	c := fpb.Config{MasterServer: tl.Addr().String()}
	cfg, err := ptypes.MarshalAny(&c)
	if err != nil {
		t.Fatal(err)
	}
	var stats statsCounter
	FSServer, err := server.MakeServer(
		&spb.ServerConfig{
			Services: []*spb.ServiceConfig{{
				Name:           "FRR",
				Factory:        "FRR",
				MaxParallelism: 5,
				Config:         cfg,
			}},
			BroadcastPollTime: &dpb.Duration{Seconds: 1},
		},
		server.Components{
			Datastore:        ds,
			ServiceFactories: map[string]service.Factory{"FRR": frr.ServerServiceFactory},
			Communicators:    []comms.Communicator{com},
			Stats:            &stats,
			Authorizer:       auth})
	if err != nil {
		t.Fatal(err)
	}

	// Create a FS Admin interface, and start it listening.
	as := admin.NewServer(ds)
	gas := grpc.NewServer()
	sgrpc.RegisterAdminServer(gas, as)
	aas, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	asl, err := net.ListenTCP("tcp", aas)
	if err != nil {
		t.Fatal(err)
	}
	defer gas.Stop()
	go func() {
		log.Printf("Finished with AdminServer[%v]: %v", asl.Addr(), gas.Serve(asl))
	}()

	// Connect the FRR master to the resulting FS AdminInterface.
	conn, err := grpc.Dial(asl.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("unable to connect to FS AdminInterface")
	}
	defer conn.Close()
	masterServer.SetAdminClient(sgrpc.NewAdminClient(conn))

	// Start watching for completed clients, create a broadcast to initiate
	// a hunt.  Because the number of messages coming at once is larger than
	// 3*MaxParallelism, messages tend to end up backlogged.
	completed := masterServer.WatchCompleted()
	if err := masterServer.CreateHunt(ctx,
		&fpb.TrafficRequestData{
			RequestId:      0,
			NumMessages:    20,
			MessageDelayMs: 20,
			Jitter:         1.0},
		numClients); err != nil {
		t.Errorf("unable to create hunt: %v", err)
	}

	// Prepare a general client config.
	conf := config.Configuration{
		Ephemeral:    true,
		TrustedCerts: x509.NewCertPool(),
		ClientLabels: []*fspb.Label{
			{ServiceName: "client", Label: "integration_test"},
		},
		FixedServices: []*fspb.ClientServiceConfig{{Name: "FRR", Factory: "FRR"}},
		Servers:       []string{listener.Addr().String()},
		CommunicatorConfig: &clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    2,
			MaxBufferDelaySeconds:  1,
			MinFailureDelaySeconds: 1,
		},
	}
	if !conf.TrustedCerts.AppendCertsFromPEM(cert) {
		t.Fatal("unable to add server cert to pool")
	}

	clients := make([]*client.Client, 0, numClients)
	// Create numClient clients.
	for i := 0; i < numClients; i++ {
		cl, err := client.New(
			conf,
			client.Components{
				ServiceFactories: map[string]cservice.Factory{"FRR": frr.ClientServiceFactory},
				Communicator:     &chttps.Communicator{},
				Signers:          signs,
			})
		if err != nil {
			t.Errorf("Unable to start client %v: %v", i, err)
		}
		clients = append(clients, cl)
	}
	log.Printf("%v clients started", numClients)

	// We expect each client to create one response for the hunt.
	for i := 0; i < numClients; i++ {
		<-completed
		log.Printf("%v clients finished", i+1)
	}

	// Now check file handling. Store a file on the server and broadcast a
	// request for all clients to read it.
	if err := ds.StoreFile(ctx, "FRR", "TestFile.txt",
		bytes.NewReader([]byte("This is a test file. It has 42 characters.")),
	); err != nil {
		log.Fatalf("Unable to store test file: %v", err)
	}
	if err := masterServer.CreateFileDownloadHunt(ctx, "TestFile.txt", numClients); err != nil {
		log.Fatalf("Unable to create file download hunt: %v", err)
	}
	// We expect each client to create one response for the hunt.
	for i := 0; i < numClients; i++ {
		<-completed
		log.Printf("%v clients finished download", i+1)
	}

	// Shut down everything before reading counters, to avoid even the
	// appearance of a race.
	for _, cl := range clients {
		cl.Stop()
	}
	FSServer.Stop()

	// Each client should have polled at least once.
	if stats.clientPolls < numClients {
		t.Errorf("Got %v messages processed, expected at least %v.", stats.clientPolls, numClients)
	}
	// Each client should have produced between 20 and 40 messages, each should be processed exactly once.
	if stats.messagesProcessed < 20*numClients || stats.messagesProcessed > 40*numClients {
		t.Errorf("Got %v messages processed, expected %v <= x <= %v", stats.messagesProcessed, 20*numClients+1, 40*numClients+1)
	}

	// Each client should have produced at least 20 and 40, and each should have been sorted at least once.
	if stats.messagesIngested < 20*numClients {
		t.Errorf("Got %v messages sorted, expected at least %v", stats.messagesIngested, 20*numClients+1)
	}

	// Sanity check that the authorizer is actually seeing things.
	if auth.authCount < numClients {
		t.Errorf("Got %d messages authorized, expected at least %d", auth.authCount, numClients)
	}
}
