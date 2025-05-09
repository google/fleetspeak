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
	"context"
	"crypto/x509"
	"net"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/golang/glog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/google/fleetspeak/fleetspeak/src/client"
	ccomms "github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	chttps "github.com/google/fleetspeak/fleetspeak/src/client/https"
	cservice "github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/common/anypbtest"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/inttesting/frr"
	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/admin"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/https"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
	fgrpc "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	fpb "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	sgrpc "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	durationpb "google.golang.org/protobuf/types/known/durationpb"
)

const numClients = 5

// statsCounter is a simple server.StatsCollector. It only counts messages for the "FRR" service.
type statsCounter struct {
	messagesIngested, messagesSent, payloadBytesSaved, messagesProcessed, messagesErrored, messagesDropped, clientPolls, datastoreOperations int64
}

func (c *statsCounter) MessageIngested(backlogged bool, m *fspb.Message, cd *db.ClientData) {
	if m.Destination.ServiceName == "FRR" {
		atomic.AddInt64(&c.messagesIngested, 1)
	}
}

func (c *statsCounter) MessageSent(m *fspb.Message) {
	if m.Source.ServiceName == "FRR" {
		atomic.AddInt64(&c.messagesSent, 1)
	}
}

func (c *statsCounter) MessageSaved(forClient bool, m *fspb.Message, cd *db.ClientData) {
	if m.Destination.ServiceName == "FRR" {
		savedPayloadBytes := 0
		if m.Data != nil {
			savedPayloadBytes = len(m.Data.TypeUrl) + len(m.Data.Value)
		}
		atomic.AddInt64(&c.payloadBytesSaved, int64(savedPayloadBytes))
	}
}

func (c *statsCounter) MessageProcessed(start, end time.Time, m *fspb.Message, isFirstTry bool, cd *db.ClientData) {
	if m.Destination.ServiceName == "FRR" {
		atomic.AddInt64(&c.messagesProcessed, 1)
	}
}

func (c *statsCounter) MessageErrored(start, end time.Time, isTemp bool, m *fspb.Message, isFirstTry bool, cd *db.ClientData) {
	if m.Destination.ServiceName == "FRR" {
		atomic.AddInt64(&c.messagesErrored, 1)
	}
}

func (c *statsCounter) MessageDropped(m *fspb.Message, isFirstTry bool, cd *db.ClientData) {
	if m.Destination.ServiceName == "FRR" {
		atomic.AddInt64(&c.messagesDropped, 1)
	}
}

func (c *statsCounter) ClientPoll(stats.PollInfo) {
	atomic.AddInt64(&c.clientPolls, 1)
}

func (c *statsCounter) DatastoreOperation(start, end time.Time, operation string, err error) {
	atomic.AddInt64(&c.datastoreOperations, 1)
}

func (c *statsCounter) ResourceUsageDataReceived(cd *db.ClientData, rud *mpb.ResourceUsageData, v *fspb.ValidationInfo) {
}

func (c *statsCounter) KillNotificationReceived(cd *db.ClientData, kn *mpb.KillNotification) {
}

// FRRIntegrationTest spins up a small FRR installation, backed by the provided datastore
// and exercises it.
func FRRIntegrationTest(t *testing.T, ds db.Store, streaming bool) {
	fin := sertesting.SetServerRetryTime(func(_ uint32) time.Time {
		return db.Now().Add(time.Second)
	})
	defer fin()

	ctx := context.Background()

	if err := ds.StoreFile(ctx, "system", "RevokedCertificates", bytes.NewReader([]byte{})); err != nil {
		t.Errorf("Unable to store RevokedCertificate file: %v", err)
	}

	// Create a FRR master server and start it listening.
	gms := grpc.NewServer()
	masterServer := frr.NewMasterServer(nil)
	fgrpc.RegisterMasterServer(gms, masterServer)
	tl, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	go func() {
		err := gms.Serve(tl)
		log.Infof("Finished with MasterServer[%v]: %v", tl.Addr(), err)
	}()
	defer gms.Stop()

	// Create FS server certs and server communicator.
	cert, key, err := comtesting.ServerCert()
	if err != nil {
		t.Fatalf("comtesting.ServerCert(): %v", err)
	}

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	com, err := https.NewCommunicator(
		https.Params{
			Listener:           listener,
			Cert:               cert,
			Key:                key,
			Streaming:          true,
			StreamingLifespan:  10 * time.Second,
			StreamingCloseTime: 7 * time.Second,
		})
	if err != nil {
		t.Fatalf("https.NewCommunicator: %v", err)
	}
	log.Infof("Communicator listening to: %v", listener.Addr())

	// Create authorizer and signers.
	auth, signs := makeAuthorizerSigners(t)

	// Create and start a FS server.
	var stats statsCounter
	fsServer, err := server.MakeServer(
		&spb.ServerConfig{
			Services: []*spb.ServiceConfig{{
				Name:           "FRR",
				Factory:        "FRR",
				MaxParallelism: 5,
				Config: anypbtest.New(t, &fpb.Config{
					MasterServer: tl.Addr().String(),
				}),
			}},
			BroadcastPollTime: durationpb.New(time.Second),
		},
		server.Components{
			Datastore:        ds,
			ServiceFactories: map[string]service.Factory{"FRR": frr.ServerServiceFactory},
			Communicators:    []comms.Communicator{com},
			Stats:            &stats,
			Authorizer:       auth,
		})
	if err != nil {
		t.Fatal(err)
	}

	// Create a FS Admin interface, and start it listening.
	as := admin.NewServer(ds, nil)
	gas := grpc.NewServer()
	sgrpc.RegisterAdminServer(gas, as)

	asl, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	go func() {
		err := gas.Serve(asl)
		log.Infof("Finished with AdminServer[%v]: %v", asl.Addr(), err)
	}()
	defer gas.Stop()

	// Connect the FRR master to the resulting FS AdminInterface.
	conn, err := grpc.NewClient(asl.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("unable to connect to FS AdminInterface")
	}
	defer conn.Close()
	masterServer.SetAdminClient(sgrpc.NewAdminClient(conn))

	// Start watching for completed clients, create a broadcast to initiate
	// a hunt.  Because the number of messages coming at once is larger than
	// 3*MaxParallelism, messages tend to end up backlogged.
	completed := masterServer.WatchCompleted()
	trd := &fpb.TrafficRequestData{
		RequestId:      0,
		NumMessages:    20,
		MessageDelayMs: 20,
		Jitter:         1.0,
	}
	if err := masterServer.CreateBroadcastRequest(ctx, trd, numClients); err != nil {
		t.Errorf("unable to create hunt: %v", err)
	}

	// Prepare a general client config.
	conf := config.Configuration{
		TrustedCerts: x509.NewCertPool(),
		ClientLabels: []*fspb.Label{{
			ServiceName: "client",
			Label:       "integration_test",
		}},
		FixedServices: []*fspb.ClientServiceConfig{{
			Name:    "FRR",
			Factory: "FRR",
		}},
		Servers: []string{listener.Addr().String()},
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
	makeComm := func() ccomms.Communicator {
		return &chttps.Communicator{}
	}
	if streaming {
		makeComm = func() ccomms.Communicator {
			return &chttps.StreamingCommunicator{}
		}
	}
	// Create numClient clients.
	for i := range numClients {
		cl, err := client.New(
			conf,
			client.Components{
				ServiceFactories: map[string]cservice.Factory{"FRR": frr.ClientServiceFactory},
				Communicator:     makeComm(),
				Signers:          signs,
			})
		if err != nil {
			t.Errorf("Unable to start client %v: %v", i, err)
		}
		clients = append(clients, cl)
	}
	log.Infof("%v clients started", numClients)

	// We expect each client to create one response for the hunt.
	for i := range numClients {
		<-completed
		log.Infof("%v clients finished", i+1)
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
	for i := range numClients {
		<-completed
		log.Infof("%v clients finished download", i+1)
	}

	// Shut down everything before reading counters, to avoid even the
	// appearance of a race.
	for _, cl := range clients {
		cl.Stop()
	}
	fsServer.Stop()

	// Each client should have polled at least once.
	if stats.clientPolls < numClients {
		t.Errorf("Got %v messages processed, expected at least %v.", stats.clientPolls, numClients)
	}
	// Each client should have produced between 20 and 40 messages, each should be processed exactly once.
	if stats.messagesProcessed < 20*numClients || stats.messagesProcessed > 40*numClients {
		t.Errorf("Got %v messages processed, expected %v <= x <= %v", stats.messagesProcessed, 20*numClients+1, 40*numClients+1)
	}

	if stats.messagesSent < numClients || stats.messagesSent > 2*numClients {
		t.Errorf("Got %v messages sent, expected %v <= x <= %v", stats.messagesSent, numClients, 2*numClients)
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
