// Copyright 2018 Google Inc.
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

package integrationtest

import (
	"context"
	"crypto/x509"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	log "github.com/golang/glog"
	"google.golang.org/grpc"

	"github.com/google/fleetspeak/fleetspeak/src/client"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	chttps "github.com/google/fleetspeak/fleetspeak/src/client/https"
	cservice "github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/admin"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/https"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"

	dpb "github.com/golang/protobuf/ptypes/duration"
	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	sgrpc "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// CloneHandlingTest runs an integration test using ds in which cloned clients
// are dealt with.
func CloneHandlingTest(t *testing.T, ds db.Store, tmpConfPath string) {
	fin := sertesting.SetClientCacheMaxAge(time.Second)
	defer fin()

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
	log.Infof("Communicator listening to: %v", listener.Addr())

	// Create a FS server.
	server, err := server.MakeServer(
		&spb.ServerConfig{
			Services: []*spb.ServiceConfig{{
				Name:    "NOOP Service",
				Factory: "NOOP",
			}},
			BroadcastPollTime: &dpb.Duration{Seconds: 1},
		},
		server.Components{
			Datastore:        ds,
			ServiceFactories: map[string]service.Factory{"NOOP": service.NOOPFactory},
			Communicators:    []comms.Communicator{com}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Stop()

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
		log.Infof("Finished with AdminServer[%v]: %v", asl.Addr(), gas.Serve(asl))
	}()
	conn, err := grpc.Dial(asl.Addr().String(), grpc.WithInsecure())
	if err != nil {
		t.Fatalf("unable to connect to FS AdminInterface: %v", err)
	}
	admin := sgrpc.NewAdminClient(conn)

	// Prepare a general client config.
	configPath := filepath.Join(tmpConfPath, "client_1")
	if err := os.Mkdir(configPath, 0777); err != nil {
		t.Fatalf("Unable to create client directory [%v]: %v", configPath, err)
	}

	ph, err := config.NewFilesystemPersistenceHandler(configPath, filepath.Join(configPath, "writeback"))
	if err != nil {
		t.Fatal(err)
	}

	conf := config.Configuration{
		PersistenceHandler: ph,
		TrustedCerts:       x509.NewCertPool(),
		ClientLabels: []*fspb.Label{
			{ServiceName: "client", Label: "clone_test"},
		},
		FixedServices: []*fspb.ClientServiceConfig{{Name: "NOOP", Factory: "NOOP"}},
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

	cl, err := client.New(
		conf,
		client.Components{
			ServiceFactories: map[string]cservice.Factory{"NOOP": cservice.NOOPFactory},
			Communicator:     &chttps.Communicator{},
		})
	if err != nil {
		t.Fatalf("Unable to start initial client: %v", err)
	}
	cl.Stop()

	wb, err := ioutil.ReadFile(filepath.Join(configPath, "writeback"))
	if err != nil {
		t.Fatalf("Unable to read example writeback file: %v", err)
	}
	for _, pc := range []string{"client_2", "client_3", "client_4", "client_5"} {
		configPath := filepath.Join(tmpConfPath, pc)
		if err := os.Mkdir(configPath, 0777); err != nil {
			t.Fatalf("Unable to create client directory [%v]: %v", configPath, err)
		}
		ioutil.WriteFile(filepath.Join(configPath, "writeback"), wb, 0644)
	}

	// We have 5 config dirs with the same key. Start a client running against
	// each of them.

	for i, pc := range []string{"client_1", "client_2", "client_3", "client_4", "client_5"} {
		pcConfigPath := filepath.Join(tmpConfPath, pc)
		ph, err := config.NewFilesystemPersistenceHandler(pcConfigPath, path.Join(pcConfigPath, "writeback"))
		if err != nil {
			t.Fatal(err)
		}

		conf := config.Configuration{
			PersistenceHandler: ph,
			TrustedCerts:       x509.NewCertPool(),
			ClientLabels: []*fspb.Label{
				{ServiceName: "client", Label: "clone_test"},
			},
			FixedServices: []*fspb.ClientServiceConfig{{Name: "NOOP", Factory: "NOOP"}},
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
		cl, err := client.New(
			conf,
			client.Components{
				ServiceFactories: map[string]cservice.Factory{"NOOP": cservice.NOOPFactory},
				Communicator:     &chttps.Communicator{},
			})
		if err != nil {
			t.Fatalf("Unable to start client %d: %v", i+1, err)
		}
		defer cl.Stop()
	}

	var id common.ClientID

	// Wait for one client id to show up through the admin interface.
	ctx, fin := context.WithTimeout(context.Background(), 20*time.Second)
	for {
		res, err := admin.ListClients(ctx, &spb.ListClientsRequest{})
		if err != nil {
			t.Fatalf("Unable to list clients: %v", err)
		}
		if len(res.Clients) > 1 {
			t.Fatalf("Only expected 1 client, got %d", len(res.Clients))
		}
		if len(res.Clients) == 1 {
			cid, err := common.BytesToClientID(res.Clients[0].ClientId)
			if err != nil {
				t.Fatalf("Unable to parse listed client id: %v", err)
			}
			id = cid
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for client id: %v", ctx.Err())
		case <-time.After(200 * time.Millisecond):
			continue
		}
	}
	fin()

	// Wait for 20 contacts.
	ctx, fin = context.WithTimeout(context.Background(), 20*time.Second)
	for {
		res, err := admin.ListClientContacts(ctx, &spb.ListClientContactsRequest{ClientId: id.Bytes()})
		if err != nil {
			t.Fatalf("Unable to list client contacts: %v", err)
		}
		if len(res.Contacts) >= 20 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for at least 20 contacts: %v", ctx.Err())
		case <-time.After(200 * time.Millisecond):
			continue
		}
	}
	fin()

	// Blacklist the id.
	ctx, fin = context.WithTimeout(context.Background(), 5*time.Second)
	if _, err := admin.BlacklistClient(ctx, &spb.BlacklistClientRequest{ClientId: id.Bytes()}); err != nil {
		t.Fatalf("Unable to blacklist client: %v", err)
	}
	fin()

	// Wait for a total of 6 client ids to show up in the admin interface.
	ctx, fin = context.WithTimeout(context.Background(), 20*time.Second)
	for {
		res, err := admin.ListClients(ctx, &spb.ListClientsRequest{})
		if err != nil {
			t.Fatalf("Unable to list clients: %v", err)
		}
		if len(res.Clients) == 6 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Timed out waiting for clients to rekey: %v", ctx.Err())
		case <-time.After(200 * time.Millisecond):
			continue
		}
	}
	fin()
}
