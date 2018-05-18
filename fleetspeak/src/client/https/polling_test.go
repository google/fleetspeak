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

package https

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	common_util "github.com/google/fleetspeak/fleetspeak/src/comtesting"

	anypb "github.com/golang/protobuf/ptypes/any"
	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func TestCreate(t *testing.T) {
	var c Communicator
	conf := config.Configuration{
		Servers:       []string{"localhost"},
		FixedServices: []*fspb.ClientServiceConfig{{Name: "NOOPService", Factory: "NOOP"}},
	}

	cl, err := client.New(
		conf,
		client.Components{
			ServiceFactories: map[string]service.Factory{"NOOP": service.NOOPFactory},
			Communicator:     &c})
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}

	cl.Stop()
}

func filterMessages(msgs []*fspb.Message, fn func(*fspb.Message) bool) []*fspb.Message {
	var res []*fspb.Message
	for _, m := range msgs {
		if fn(m) {
			res = append(res, m)
		}
	}
	return res
}

func TestCommunicator(t *testing.T) {
	// Create a local https server for the client to talk to.
	pemCert, pemKey, err := common_util.ServerCert()
	if err != nil {
		t.Fatal(err)
	}
	cb, _ := pem.Decode(pemCert)
	if cb == nil || cb.Type != "CERTIFICATE" {
		t.Fatalf("Expected CERTIFICATE in parsed pem block, got: %v", cb)
	}

	cp, err := tls.X509KeyPair(pemCert, pemKey)
	if err != nil {
		t.Fatal(err)
	}
	ad, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	tl, err := net.ListenTCP("tcp", ad)
	if err != nil {
		t.Fatal(err)
	}
	addr := tl.Addr().String()

	// Dummy server just puts the ContactData records that we receive into a channel.
	mux := http.NewServeMux()
	received := make(chan *fspb.ContactData, 5)
	mux.HandleFunc("/message", func(res http.ResponseWriter, req *http.Request) {
		buf, err := ioutil.ReadAll(req.Body)
		if err != nil {
			t.Errorf("unable to read body in test server: %v", err)
			http.Error(res, "unable to read body", http.StatusBadRequest)
			return
		}

		var rec fspb.WrappedContactData
		if err := proto.Unmarshal(buf, &rec); err != nil {
			t.Errorf("unable to parse request body as WrappedContactData: %v", err)
			http.Error(res, "unable to read body", http.StatusBadRequest)
			return
		}
		var rcd fspb.ContactData
		if err := proto.Unmarshal(rec.ContactData, &rcd); err != nil {
			t.Errorf("unable to parse ContactData: %v", err)
			http.Error(res, "unable to read body", http.StatusBadRequest)
			return
		}
		received <- &rcd

		cd := fspb.ContactData{
			SequencingNonce: 42,
		}
		buf, err = proto.Marshal(&cd)
		if err != nil {
			t.Errorf("Unable to marshal ContactData: %v", err)
		}
		res.Header().Set("Content-Type", "application/octet-stream")
		res.WriteHeader(http.StatusOK)
		res.Write(buf)
	})

	server := http.Server{
		Addr:    addr,
		Handler: mux,
		TLSConfig: &tls.Config{
			ClientAuth:   tls.RequireAnyClientCert,
			Certificates: []tls.Certificate{cp},
			NextProtos:   []string{"h2"},
		},
	}
	l := tls.NewListener(tl, server.TLSConfig)
	go server.Serve(l)

	// Create a communicator, configured to talk to the local server.
	var c Communicator
	conf := config.Configuration{
		TrustedCerts:  x509.NewCertPool(),
		Servers:       []string{addr},
		FixedServices: []*fspb.ClientServiceConfig{{Name: "NOOPService", Factory: "NOOP"}},
		CommunicatorConfig: &clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    2,
			MaxBufferDelaySeconds:  1,
			MinFailureDelaySeconds: 1,
		},
	}
	if !conf.TrustedCerts.AppendCertsFromPEM(pemCert) {
		t.Fatal("unable to add server cert to pool")
	}

	cl, err := client.New(
		conf,
		client.Components{
			ServiceFactories: map[string]service.Factory{"NOOP": service.NOOPFactory},
			Communicator:     &c})
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}

	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{M: &fspb.Message{
			Destination: &fspb.Address{ServiceName: "DummyService"}},
		}); err != nil {
		t.Fatalf("unable to hand message to client: %v", err)
	}

	// There is typically one contact before the message works through the
	// system, and there may be more.  But we should eventually get a
	// contact with the single message that we tried to send to the server.
	for cb := range received {
		// filter out any system messages (first contact will also include a client info)
		cb.Messages = filterMessages(cb.Messages, func(m *fspb.Message) bool {
			return m.Destination.ServiceName != "system"
		})
		if len(cb.Messages) > 1 {
			t.Errorf("Expected at most one message in delivered ContactData, got: %v", cb.Messages)
			break
		}
		if len(cb.Messages) == 1 {
			want := &fspb.ContactData{
				SequencingNonce: 42,
				Messages: []*fspb.Message{
					{Destination: &fspb.Address{ServiceName: "DummyService"}},
				},
			}
			cb.ClientClock = nil
			if !proto.Equal(cb, want) {
				t.Errorf("Unexpected ContactData: want [%v], got [%v]", want, cb)
			}
			break
		}
	}

	// 105 small messages
	for i := 0; i < sendCountThreshold+5; i++ {
		if err := cl.ProcessMessage(context.Background(),
			service.AckMessage{M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: "DummyService"}}},
		); err != nil {
			t.Fatalf("unable to hand message to client: %v", err)
		}
	}

	// Should be split into to 2 contacts, 100 in the first, 5 in the second.
	// However, very occasionally things move slow enough in testing that the
	// first contact has fewer records.
	recTotalCount := 0
	recMaxCount := 0
	for recTotalCount < sendCountThreshold+5 {
		cd := <-received
		cnt := len(cd.Messages)
		recTotalCount += cnt
		if cnt > recMaxCount {
			recMaxCount = cnt
		}
	}
	if recTotalCount != sendCountThreshold+5 {
		t.Errorf("Expected %d messages, got: %d", sendCountThreshold, recTotalCount)
	}
	if recMaxCount < 50 {
		t.Errorf("Expected to see at least 50 messages in some contact, got: %v", recMaxCount)
	}
	if recMaxCount > 100 {
		t.Errorf("Expected to see at most 100 messages in each contact, got: %v", recMaxCount)
	}

	// 20 messages with 1MB payload.
	payload := make([]byte, 1024*1024)
	if _, err := rand.Read(payload); err != nil {
		t.Fatalf("Unable to read random bytes: %v", err)
	}
	for i := 0; i < 20; i++ {
		if err := cl.ProcessMessage(context.Background(),
			service.AckMessage{M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: "DummyService"},
				Data:        &anypb.Any{Value: payload},
			}},
		); err != nil {
			t.Fatalf("unable to hand message to client: %v", err)
		}
	}
	// Should be split into 2 contacts, 15 in the first, 5 in the second.
	// However, very occasionally things move slow enough in testing that the
	// first contact has fewer records.
	recTotalCount = 0
	recMaxCount = 0
	for recTotalCount < 20 {
		cd := <-received
		cnt := len(cd.Messages)
		recTotalCount += cnt
		if cnt > recMaxCount {
			recMaxCount = cnt
		}
	}
	if recTotalCount != 20 {
		t.Errorf("Expected %d messages, got: %d", 20, recTotalCount)
	}
	if recMaxCount < 10 {
		t.Errorf("Expected to see at least 10 messages in some contact, got: %v", recMaxCount)
	}
	if recMaxCount > 15 {
		t.Errorf("Expected to see at most 15 messages in each contact, got: %v", recMaxCount)
	}

	// TODO Test that messages go in the other direction, error cases, etc.

	// The most graceful way to shut down a http.Server is to close the associated listener.
	tl.Close()
	cl.Stop()
}

func TestErrorDelay(t *testing.T) {
	// Create a local https server for the client to talk to.
	pemCert, pemKey, err := common_util.ServerCert()
	if err != nil {
		t.Fatal(err)
	}
	cb, _ := pem.Decode(pemCert)
	if cb == nil || cb.Type != "CERTIFICATE" {
		t.Fatalf("Expected CERTIFICATE in parsed pem block, got: %v", cb)
	}

	cp, err := tls.X509KeyPair(pemCert, pemKey)
	if err != nil {
		t.Fatal(err)
	}
	ad, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	tl, err := net.ListenTCP("tcp", ad)
	if err != nil {
		t.Fatal(err)
	}
	addr := tl.Addr().String()

	// Dummy server just serves errors, recording the time of this event to a channel.
	mux := http.NewServeMux()
	errors := make(chan time.Time, 2)
	mux.HandleFunc("/message", func(res http.ResponseWriter, req *http.Request) {
		http.Error(res, "This server is broken.", http.StatusInternalServerError)
		errors <- time.Now()
		return
	})

	server := http.Server{
		Addr:    addr,
		Handler: mux,
		TLSConfig: &tls.Config{
			ClientAuth:   tls.RequireAnyClientCert,
			Certificates: []tls.Certificate{cp},
			NextProtos:   []string{"h2"},
		},
	}
	l := tls.NewListener(tl, server.TLSConfig)
	go server.Serve(l)

	// Create a communicator, configured to talk to the local server.
	var c Communicator
	conf := config.Configuration{
		Servers:       []string{addr},
		TrustedCerts:  x509.NewCertPool(),
		FixedServices: []*fspb.ClientServiceConfig{{Name: "NOOPService", Factory: "NOOP"}},
		CommunicatorConfig: &clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    2,
			MaxBufferDelaySeconds:  1,
			MinFailureDelaySeconds: 5,
		},
	}
	if !conf.TrustedCerts.AppendCertsFromPEM(pemCert) {
		t.Fatal("unable to add server cert to pool")
	}

	cl, err := client.New(
		conf,
		client.Components{
			ServiceFactories: map[string]service.Factory{"NOOP": service.NOOPFactory},
			Communicator:     &c})
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}

	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: "DummyService"}}},
	); err != nil {
		t.Fatalf("unable to hand message to client: %v", err)
	}

	first := <-errors
	second := <-errors
	delay := second.Sub(first)
	if delay < 5*time.Second || delay < 0 {
		t.Errorf("Client should wait at least 5 seconds, but waited: %v", delay)
	}
	third := <-errors
	delay = third.Sub(second)
	if delay < 5*time.Second || delay < 0 {
		t.Errorf("Client should wait at least 5 seconds, but waited: %v", delay)
	}

	// The most graceful way to shut down a http.Server is to close the associated listener.
	tl.Close()
	cl.Stop()
}

func TestCertificateRevoked(t *testing.T) {
	// Create a local https server for the client to talk to.
	pemCert, pemKey, err := common_util.ServerCert()
	if err != nil {
		t.Fatal(err)
	}
	cb, _ := pem.Decode(pemCert)
	if cb == nil || cb.Type != "CERTIFICATE" {
		t.Fatalf("Expected CERTIFICATE in parsed pem block, got: %v", cb)
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		t.Fatalf("Unable to parse certificate bytes: %v", err)
	}

	cp, err := tls.X509KeyPair(pemCert, pemKey)
	if err != nil {
		t.Fatal(err)
	}
	ad, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	tl, err := net.ListenTCP("tcp", ad)
	if err != nil {
		t.Fatal(err)
	}
	addr := tl.Addr().String()

	// Dummy server just puts the ContactData records that we receive into a channel.
	mux := http.NewServeMux()
	received := make(chan *fspb.ContactData)
	mux.HandleFunc("/message", func(res http.ResponseWriter, req *http.Request) {
		buf, err := ioutil.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("unable to read body in test server: %v", err)
		}

		var rec fspb.ContactData
		if err := proto.Unmarshal(buf, &rec); err != nil {
			t.Fatalf("unable to parse request body as ContactData: %v", err)
		}
		received <- &rec

		cd := fspb.ContactData{
			SequencingNonce: 42,
		}
		buf, err = proto.Marshal(&cd)
		if err != nil {
			t.Fatalf("unable to marshal ContactData in test server: %v", err)
		}
		res.Header().Set("Content-Type", "application/octet-stream")
		res.WriteHeader(http.StatusOK)
		res.Write(buf)
	})

	server := http.Server{
		Addr:    addr,
		Handler: mux,
		TLSConfig: &tls.Config{
			ClientAuth:   tls.RequireAnyClientCert,
			Certificates: []tls.Certificate{cp},
			NextProtos:   []string{"h2"},
		},
	}
	l := tls.NewListener(tl, server.TLSConfig)
	go server.Serve(l)

	// Create a communicator, configured to talk to the local server.
	var c Communicator
	conf := config.Configuration{
		Servers:       []string{addr},
		TrustedCerts:  x509.NewCertPool(),
		FixedServices: []*fspb.ClientServiceConfig{{Name: "NOOPService", Factory: "NOOP"}},
		CommunicatorConfig: &clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    2,
			MaxBufferDelaySeconds:  1,
			MinFailureDelaySeconds: 1,
		},
		RevokedCertSerials: [][]byte{cert.SerialNumber.Bytes()},
	}
	if !conf.TrustedCerts.AppendCertsFromPEM(pemCert) {
		t.Fatal("unable to add server cert to pool")
	}

	cl, err := client.New(
		conf,
		client.Components{
			ServiceFactories: map[string]service.Factory{"NOOP": service.NOOPFactory},
			Communicator:     &c})
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}

	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{M: &fspb.Message{
			Destination: &fspb.Address{ServiceName: "DummyService"}}},
	); err != nil {
		t.Fatalf("unable to hand message to client: %v", err)
	}

	// The only server is serving with a revoked cert, so we should never have a contact.
	// The client is set to retry once per second, and should try within a second.
	// Assume success if we see nothing for 5 seconds.

	select {
	case cb := <-received:
		t.Errorf("unexpected client contact: %v", cb)
	case <-time.After(5 * time.Second):

	}

	// The most graceful way to shut down a http.Server is to close the associated listener.
	tl.Close()
	cl.Stop()
}
