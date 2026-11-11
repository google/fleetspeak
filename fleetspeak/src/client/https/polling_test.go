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
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/golang/glog"
	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client"
	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	common_util "github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/https"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	anypb "google.golang.org/protobuf/types/known/anypb"
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

type blockingService struct {
	unblock  chan struct{}
	received chan *fspb.Message
}

func (s *blockingService) Start(_ service.Context) error { return nil }
func (s *blockingService) Stop() error                   { return nil }
func (s *blockingService) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.unblock:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.received <- m:
	}
	return nil
}

func testCommunicator(t *testing.T, proxy *url.URL, compression fspb.CompressionAlgorithm, clientCertHeader string) {
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

	received := make(chan *fspb.ContactData, 5)
	toSend := make(chan *fspb.ContactData, 5)

	// Dummy server just puts the ContactData records that we receive into a
	// channel, and looks into a channel for ContactData records to pass to
	// the client.
	var handle http.HandlerFunc = func(res http.ResponseWriter, req *http.Request) {
		if clientCertHeader != "" {
			rawHeader := req.Header.Get(clientCertHeader)
			if rawHeader == "" {
				t.Errorf("client certificate header is not set")
			} else if unescaped, err := url.PathUnescape(rawHeader); err != nil {
				t.Errorf("unable to unescape client certificate header: %v", err)
			} else if block, _ := pem.Decode([]byte(unescaped)); block == nil || block.Type != "CERTIFICATE" {
				t.Errorf("client certificate header = %q, want a CERTIFICATE PEM block", unescaped)
			} else if !bytes.Equal(block.Bytes, req.TLS.PeerCertificates[0].Raw) {
				t.Errorf("client certificate header DER mismatch: got %d bytes, want %d bytes", len(block.Bytes), len(req.TLS.PeerCertificates[0].Raw))
			}
		}
		cid, err := common.MakeClientID(req.TLS.PeerCertificates[0].PublicKey)
		if err != nil {
			t.Errorf("unable to make ClientID in test server: %v", err)
		}
		buf, err := io.ReadAll(req.Body)
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

		var cd *fspb.ContactData
		select {
		case cd = <-toSend:
		default:
			cd = &fspb.ContactData{
				SequencingNonce: 42,
			}
		}
		for _, m := range cd.Messages {
			m.Destination.ClientId = cid.Bytes()
		}
		buf, err = proto.Marshal(cd)
		if err != nil {
			t.Errorf("Unable to marshal ContactData: %v", err)
		}
		res.Header().Set("Content-Type", "application/octet-stream")
		res.WriteHeader(http.StatusOK)
		res.Write(buf)
	}

	mux := http.NewServeMux()
	mux.Handle("/message", &https.CompressionHandler{Wrapped: handle})

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
		TrustedCerts: x509.NewCertPool(),
		Servers:      []string{addr},
		FixedServices: []*fspb.ClientServiceConfig{{
			Name: "BlockingService", Factory: "Blocking"}},
		CommunicatorConfig: &clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    2,
			MaxBufferDelaySeconds:  1,
			MinFailureDelaySeconds: 1,
			Compression:            compression,
		},
		Proxy:                   proxy,
		ClientCertificateHeader: clientCertHeader,
	}
	if !conf.TrustedCerts.AppendCertsFromPEM(pemCert) {
		t.Fatal("unable to add server cert to pool")
	}

	bs := blockingService{
		unblock:  make(chan struct{}),
		received: make(chan *fspb.Message, 100),
	}
	cl, err := client.New(
		conf,
		client.Components{
			ServiceFactories: map[string]service.Factory{
				"Blocking": func(_ *fspb.ClientServiceConfig) (service.Service, error) {
					return &bs, nil
				},
			},
			Communicator: &c})
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
				AllowedMessages: map[string]uint64{
					"BlockingService": 100,
					"system":          100,
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
	for range sendCountThreshold + 5 {
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
	for range 20 {
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

	toSend <- &fspb.ContactData{
		SequencingNonce: 44,
		Messages: []*fspb.Message{
			{Destination: &fspb.Address{ServiceName: "BlockingService"}, MessageType: "TestMessage"},
			{Destination: &fspb.Address{ServiceName: "BlockingService"}, MessageType: "TestMessage"},
			{Destination: &fspb.Address{ServiceName: "BlockingService"}, MessageType: "TestMessage"},
			{Destination: &fspb.Address{ServiceName: "BlockingService"}, MessageType: "TestMessage"},
			{Destination: &fspb.Address{ServiceName: "BlockingService"}, MessageType: "TestMessage"},
		},
	}

	a := time.After(10 * time.Second)
F:
	for {
		select {
		case cd := <-received:
			// 5 messages in, buffer sized of 100, first message
			// blocked -> we should see free buffer space of 96.
			if cd.AllowedMessages["BlockingService"] == 96 {
				break F
			}
			log.Errorf("AllowedMessaged: %v", cd.AllowedMessages)
		case <-a:
			t.Errorf("Timed out waiting for reduced capacity.")
			break F
		}
	}

	close(bs.unblock)

G:
	for {
		select {
		case cd := <-received:
			if cd.AllowedMessages["BlockingService"] == 100 {
				break G
			}
		case <-a:
			t.Errorf("Timed out waiting for returned capacity.")
			break G
		}
	}

	// TODO error cases, etc.

	// The most graceful way to shut down a http.Server is to close the associated listener.
	tl.Close()
	cl.Stop()
}

func TestCommunicator(t *testing.T) {
	testCases := []struct {
		name        string
		compression fspb.CompressionAlgorithm
	}{
		{
			name:        "uncompressed",
			compression: fspb.CompressionAlgorithm_COMPRESSION_NONE,
		},
		{
			name:        "deflate",
			compression: fspb.CompressionAlgorithm_COMPRESSION_DEFLATE,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testCommunicator(t, nil, tc.compression, "")
		})
	}
}

// A simple HTTPS proxy.
type httpsProxy struct {
	tl net.Listener
	// Number of requests handled.
	atomicNumRequests uint32
}

// Sets up and starts a HTTPS proxy.
func newHTTPSProxy() (*httpsProxy, error) {
	hp := &httpsProxy{}
	ad, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}
	hp.tl, err = net.ListenTCP("tcp", ad)
	if err != nil {
		return nil, err
	}
	server := &http.Server{
		Addr:    hp.addr(),
		Handler: hp,
	}
	go server.Serve(hp.tl)
	return hp, nil
}

func (hp *httpsProxy) addr() string {
	return hp.tl.Addr().String()
}

func (hp *httpsProxy) numRequests() uint32 {
	return atomic.LoadUint32(&hp.atomicNumRequests)
}

func (hp *httpsProxy) close() {
	hp.tl.Close()
}

// Request handler for HTTPS proxy.
// Accepts only the HTTP connect method.
// Handles the request by hijacking the HTTP connection, opening a second connection to the host of
// the request and copying data between the two connections.
func (hp *httpsProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	returnError := func(err error) {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	d, err := httputil.DumpRequest(r, false)
	if err != nil {
		returnError(err)
		return
	}
	log.Infof("Got proxy request: %s.", string(d))

	if r.Method != http.MethodConnect {
		returnError(fmt.Errorf("Proxy received invalid method: %s", r.Method))
		return
	}
	conn, err := net.Dial("tcp", r.Host)
	if err != nil {
		returnError(err)
		return
	}
	defer conn.Close()
	w.WriteHeader(http.StatusOK)
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		log.Error("Failed to get hijacker for HTTP connection.")
		return
	}
	httpConn, _, err := hijacker.Hijack()
	if err != nil {
		log.Errorf("Failed to hijack HTTP connection: %s.", err)
		return
	}
	defer httpConn.Close()
	atomic.AddUint32(&hp.atomicNumRequests, 1)
	c := make(chan struct{})
	copyFromTo := func(from net.Conn, to net.Conn) {
		_, err := io.Copy(to, from)
		if err != nil {
			log.Errorf("Failed to copy to/from proxied connection: %s.", err)
		}
		c <- struct{}{}
	}
	go copyFromTo(conn, httpConn)
	go copyFromTo(httpConn, conn)
	<-c
	<-c
}

func TestCommunicatorWithProxyFromConfig(t *testing.T) {
	hp, err := newHTTPSProxy()
	if err != nil {
		t.Fatal(err)
	}
	defer hp.close()

	url := &url.URL{
		Host: hp.addr(),
	}

	testCommunicator(t, url, fspb.CompressionAlgorithm_COMPRESSION_NONE, "")

	if hp.numRequests() == 0 {
		t.Fatalf("Expected to receive proxy requests.")
	}
}

func TestClientCertHeader(t *testing.T) {
	testCommunicator(t, nil, fspb.CompressionAlgorithm_COMPRESSION_NONE, "Fleetspeak-Certificate")
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
		buf, err := io.ReadAll(req.Body)
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

func TestReset(t *testing.T) {
	var c Communicator
	conf := config.Configuration{
		Servers: []string{"localhost:1234"},
		CommunicatorConfig: &clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    10,
			MinFailureDelaySeconds: 10,
		},
	}
	dialCalls := make(chan struct{}, 10)
	c.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialCalls <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}

	cl, err := client.New(
		conf,
		client.Components{
			Communicator: &c,
		})
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}
	defer cl.Stop()

	// Wait for first dial attempt
	select {
	case <-dialCalls:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for first dial attempt")
	}

	// Call Reset
	time.Sleep(100 * time.Millisecond)
	c.Reset()

	// Wait for second dial attempt (should be immediate, not waiting 10s)
	select {
	case <-dialCalls:
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for second dial attempt after Reset")
	}
}

func TestFlush(t *testing.T) {
	// Create a local https server for the client to talk to.
	pemCert, pemKey, err := common_util.ServerCert()
	if err != nil {
		t.Fatal(err)
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

	pollCount := int32(0)
	mux := http.NewServeMux()
	mux.HandleFunc("/message", func(res http.ResponseWriter, req *http.Request) {
		atomic.AddInt32(&pollCount, 1)
		cd := fspb.ContactData{
			SequencingNonce: 42,
		}
		buf, err := proto.Marshal(&cd)
		if err != nil {
			t.Fatalf("unable to marshal ContactData: %v", err)
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
	defer tl.Close()

	var c Communicator
	conf := config.Configuration{
		TrustedCerts: x509.NewCertPool(),
		Servers:      []string{addr},
		CommunicatorConfig: &clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    10,
			MinFailureDelaySeconds: 10,
		},
	}
	if !conf.TrustedCerts.AppendCertsFromPEM(pemCert) {
		t.Fatal("unable to add server cert to pool")
	}

	cl, err := client.New(
		conf,
		client.Components{
			Communicator: &c,
		})
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}
	defer cl.Stop()

	// Wait for first poll
	for atomic.LoadInt32(&pollCount) == 0 {
		time.Sleep(100 * time.Millisecond)
	}

	// Call Flush in a goroutine
	flushDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		flushDone <- c.Flush(ctx)
	}()

	// Trigger a poll via wakeUp (indirectly via Reset)
	c.Reset()

	select {
	case err := <-flushDone:
		if err != nil {
			t.Errorf("Flush failed: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Timed out waiting for Flush")
	}
}

type pollingFlushTestContext struct {
	comms.Context
	id                 common.ClientID
	priv               *ecdsa.PrivateKey
	si                 comms.ServerInfo
	conf               *clpb.CommunicatorConfig
	outbox             chan comms.MessageInfo
	processContactData func(context.Context, *fspb.ContactData, bool) error
}

func (c *pollingFlushTestContext) Outbox() <-chan comms.MessageInfo { return c.outbox }
func (c *pollingFlushTestContext) CurrentID() common.ClientID       { return c.id }
func (c *pollingFlushTestContext) CurrentIdentity() (comms.ClientIdentity, error) {
	return comms.ClientIdentity{ID: c.id, Private: c.priv, Public: c.priv.Public()}, nil
}
func (c *pollingFlushTestContext) ServerInfo() (comms.ServerInfo, error)        { return c.si, nil }
func (c *pollingFlushTestContext) CommunicatorConfig() *clpb.CommunicatorConfig { return c.conf }
func (c *pollingFlushTestContext) ChainRevoked([]*x509.Certificate) bool        { return false }
func (c *pollingFlushTestContext) Stats() stats.Collector                       { return stats.NoopCollector{} }
func (c *pollingFlushTestContext) ProcessContactData(ctx context.Context, cd *fspb.ContactData, streaming bool) error {
	if c.processContactData != nil {
		return c.processContactData(ctx, cd, streaming)
	}
	return nil
}
func (c *pollingFlushTestContext) MakeContactData(msgs []*fspb.Message, _ map[string]uint64) (*fspb.WrappedContactData, map[string]uint64, error) {
	b, err := proto.Marshal(&fspb.ContactData{Messages: msgs})
	if err != nil {
		return nil, nil, err
	}
	return &fspb.WrappedContactData{ContactData: b}, nil, nil
}

// startPollingFlushTestServer serves handler at /message over TLS until the
// test ends. It returns a comms.Context for a client of that server whose
// poll, buffer and failure delays are all 60s, longer than any test waits.
func startPollingFlushTestServer(t *testing.T, handler http.HandlerFunc) *pollingFlushTestContext {
	t.Helper()
	pemCert, pemKey, err := common_util.ServerCert()
	if err != nil {
		t.Fatalf("ServerCert failed: %v", err)
	}
	cp, err := tls.X509KeyPair(pemCert, pemKey)
	if err != nil {
		t.Fatalf("X509KeyPair failed: %v", err)
	}
	ad, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("ResolveTCPAddr failed: %v", err)
	}
	tl, err := net.ListenTCP("tcp", ad)
	if err != nil {
		t.Fatalf("ListenTCP failed: %v", err)
	}
	addr := tl.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/message", handler)
	server := http.Server{
		Addr:    addr,
		Handler: mux,
		TLSConfig: &tls.Config{
			ClientAuth:   tls.RequireAnyClientCert,
			Certificates: []tls.Certificate{cp},
			NextProtos:   []string{"h2"},
		},
	}
	go server.Serve(tls.NewListener(tl, server.TLSConfig))
	t.Cleanup(func() { server.Close() })

	priv, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	id, err := common.MakeClientID(priv.Public())
	if err != nil {
		t.Fatalf("MakeClientID failed: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemCert) {
		t.Fatal("unable to add server cert to pool")
	}

	return &pollingFlushTestContext{
		id:   id,
		priv: priv,
		si: comms.ServerInfo{
			TrustedCerts: pool,
			Servers:      []string{addr},
		},
		conf: &clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    60,
			MaxBufferDelaySeconds:  60,
			MinFailureDelaySeconds: 60,
		},
		outbox: make(chan comms.MessageInfo),
	}
}

// writeEmptyContactData answers a poll with a ContactData that carries no
// messages.
func writeEmptyContactData(t *testing.T, res http.ResponseWriter) {
	t.Helper()
	buf, err := proto.Marshal(&fspb.ContactData{SequencingNonce: 42})
	if err != nil {
		t.Errorf("unable to marshal ContactData: %v", err)
		http.Error(res, "marshal error", http.StatusInternalServerError)
		return
	}
	res.Header().Set("Content-Type", "application/octet-stream")
	res.WriteHeader(http.StatusOK)
	res.Write(buf)
}

// TestPollDrainsReadyOutbox checks that a poll sends every message that is
// ready in Outbox when the poll starts.
func TestPollDrainsReadyOutbox(t *testing.T) {
	const numMessages = 3
	msgCounts := make(chan int, 10)
	cctx := startPollingFlushTestServer(t, func(res http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("unable to read request body: %v", err)
		}
		var wcd fspb.WrappedContactData
		var cd fspb.ContactData
		if err := proto.Unmarshal(body, &wcd); err != nil {
			t.Errorf("unable to unmarshal WrappedContactData: %v", err)
		} else if err := proto.Unmarshal(wcd.ContactData, &cd); err != nil {
			t.Errorf("unable to unmarshal ContactData: %v", err)
		}
		msgCounts <- len(cd.Messages)
		writeEmptyContactData(t, res)
	})
	cctx.outbox = make(chan comms.MessageInfo, numMessages)

	// Hold the first poll in ProcessContactData so that every message is
	// ready in Outbox before processingLoop looks at Outbox again.
	var poll1Once, releasePoll1Once sync.Once
	inPoll1Process := make(chan struct{})
	releasePoll1Process := make(chan struct{})
	closeReleasePoll1 := func() { releasePoll1Once.Do(func() { close(releasePoll1Process) }) }
	cctx.processContactData = func(context.Context, *fspb.ContactData, bool) error {
		poll1Once.Do(func() {
			close(inPoll1Process)
			<-releasePoll1Process
		})
		return nil
	}

	var c Communicator
	if err := c.Setup(cctx); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer c.Stop()
	defer closeReleasePoll1()

	select {
	case <-inPoll1Process:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for first poll to enter ProcessContactData")
	}

	for range numMessages {
		cctx.outbox <- comms.MessageInfo{
			M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: "DummyService"},
				Background:  true,
			},
			Ack:  func() {},
			Nack: func() {},
		}
	}
	// Reset makes processingLoop poll again without waiting
	// MaxBufferDelaySeconds.
	c.Reset()
	closeReleasePoll1()

	for i, want := range []int{0, numMessages} {
		select {
		case got := <-msgCounts:
			if got != want {
				t.Errorf("Poll %d carried %d messages, want %d", i+1, got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Timed out waiting for poll %d", i+1)
		}
	}
}

// waitForFirstPoll blocks until c has completed a poll, after which
// processingLoop waits MaxPollDelaySeconds unless something wakes it.
func waitForFirstPoll(t *testing.T, c *Communicator) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		c.mu.Lock()
		done := c.doneSeq >= 1
		notify := c.pollNotify
		c.mu.Unlock()
		if done {
			return
		}
		select {
		case <-notify:
		case <-timeout:
			t.Fatal("Timed out waiting for initial poll to complete")
		}
	}
}

func TestFlushWakesBufferedOutbox(t *testing.T) {
	cctx := startPollingFlushTestServer(t, func(res http.ResponseWriter, req *http.Request) {
		_, _ = io.ReadAll(req.Body)
		writeEmptyContactData(t, res)
	})

	var c Communicator
	if err := c.Setup(cctx); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer c.Stop()

	waitForFirstPoll(t, &c)

	// Enqueue a small background message on the unbuffered Outbox channel; once
	// the send completes, processingLoop has received it and will wait
	// MaxBufferDelaySeconds (60s) unless Flush wakes it.
	acked := make(chan struct{})
	cctx.outbox <- comms.MessageInfo{
		M: &fspb.Message{
			Destination: &fspb.Address{ServiceName: "DummyService"},
			Background:  true,
		},
		Ack:  func() { close(acked) },
		Nack: func() {},
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		buffered := c.reqSeq > c.doneSeq
		c.mu.Unlock()
		if buffered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Timed out waiting for Outbox message to advance reqSeq")
		}
		time.Sleep(time.Millisecond)
	}

	flushCtx, flushCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer flushCancel()
	if err := c.Flush(flushCtx); err != nil {
		t.Fatalf("Flush failed while message buffered: %v", err)
	}

	select {
	case <-acked:
	default:
		t.Fatal("Expected buffered message to be acked before Flush returned")
	}
}

// notifyOnDoneContext calls onDone the first time Done is called. Flush first
// calls Done when it is about to block waiting for a poll, so onDone reports
// that Flush has requested a poll and is waiting for it.
type notifyOnDoneContext struct {
	context.Context
	once   sync.Once
	onDone func()
}

func (c *notifyOnDoneContext) Done() <-chan struct{} {
	c.once.Do(c.onDone)
	return c.Context.Done()
}

func TestFlushAfterResetWaitsForPostResetPoll(t *testing.T) {
	var pollCount atomic.Int32
	var poll2StartedOnce, releasePoll2Once sync.Once
	poll2Started := make(chan struct{})
	releasePoll2 := make(chan struct{})
	closeReleasePoll2 := func() { releasePoll2Once.Do(func() { close(releasePoll2) }) }
	cctx := startPollingFlushTestServer(t, func(res http.ResponseWriter, req *http.Request) {
		_, _ = io.ReadAll(req.Body)
		if pollCount.Add(1) == 2 {
			poll2StartedOnce.Do(func() { close(poll2Started) })
			<-releasePoll2
		}
		writeEmptyContactData(t, res)
	})

	var poll1Once, releasePoll1Once sync.Once
	inPoll1Process := make(chan struct{})
	releasePoll1Process := make(chan struct{})
	closeReleasePoll1 := func() { releasePoll1Once.Do(func() { close(releasePoll1Process) }) }
	cctx.processContactData = func(context.Context, *fspb.ContactData, bool) error {
		poll1Once.Do(func() {
			close(inPoll1Process)
			<-releasePoll1Process
		})
		return nil
	}

	var dialCount atomic.Int32
	var c Communicator
	c.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialCount.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	if err := c.Setup(cctx); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer c.Stop()
	defer closeReleasePoll2()
	defer closeReleasePoll1()

	select {
	case <-inPoll1Process:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for first poll to enter ProcessContactData")
	}

	c.Reset()

	flushWaiting := make(chan struct{})
	baseCtx, flushCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer flushCancel()
	flushCtx := &notifyOnDoneContext{
		Context: baseCtx,
		onDone:  func() { close(flushWaiting) },
	}
	flushDone := make(chan error, 1)
	go func() {
		flushDone <- c.Flush(flushCtx)
	}()

	select {
	case <-flushWaiting:
	case err := <-flushDone:
		t.Fatalf("Flush returned %v before post-Reset poll started", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for Flush to wait on poll completion")
	}

	closeReleasePoll1()

	select {
	case <-poll2Started:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for post-Reset poll to start")
	}

	select {
	case err := <-flushDone:
		t.Fatalf("Flush returned %v before post-Reset poll completed", err)
	case <-time.After(50 * time.Millisecond):
	}

	closeReleasePoll2()

	select {
	case err := <-flushDone:
		if err != nil {
			t.Fatalf("Flush failed after post-Reset poll: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for Flush after post-Reset poll")
	}

	if got := pollCount.Load(); got < 2 {
		t.Errorf("Server received %d polls before Flush returned, want at least 2", got)
	}
	if got := dialCount.Load(); got < 2 {
		t.Errorf("Communicator dialed %d times, want at least 2 because Reset closes the idle connection", got)
	}
}

func TestFlushAfterResetBeforeFailureDelay(t *testing.T) {
	var failPolls atomic.Bool
	cctx := startPollingFlushTestServer(t, func(res http.ResponseWriter, req *http.Request) {
		_, _ = io.ReadAll(req.Body)
		if failPolls.Load() {
			http.Error(res, "injected failure", http.StatusInternalServerError)
			return
		}
		writeEmptyContactData(t, res)
	})

	var c Communicator
	if err := c.Setup(cctx); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer c.Stop()

	waitForFirstPoll(t, &c)

	// processingLoop Nacks the messages of a failed poll just before it starts
	// the MinFailureDelaySeconds wait, so a Nack that blocks holds
	// processingLoop at the point where a wakeUp token sent now is consumed by
	// that wait rather than by the wait for the next poll.
	nackEntered := make(chan struct{})
	releaseNack := make(chan struct{})
	var releaseNackOnce sync.Once
	closeReleaseNack := func() { releaseNackOnce.Do(func() { close(releaseNack) }) }
	defer closeReleaseNack()
	failPolls.Store(true)
	cctx.outbox <- comms.MessageInfo{
		M: &fspb.Message{
			Destination: &fspb.Address{ServiceName: "DummyService"},
			Background:  true,
		},
		Ack: func() {},
		Nack: func() {
			close(nackEntered)
			<-releaseNack
		},
	}
	c.Reset()

	select {
	case <-nackEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for the failed poll to Nack its message")
	}
	failPolls.Store(false)

	c.Reset()

	flushWaiting := make(chan struct{})
	baseCtx, flushCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer flushCancel()
	flushCtx := &notifyOnDoneContext{
		Context: baseCtx,
		onDone:  func() { close(flushWaiting) },
	}
	flushDone := make(chan error, 1)
	go func() {
		flushDone <- c.Flush(flushCtx)
	}()

	select {
	case <-flushWaiting:
	case err := <-flushDone:
		t.Fatalf("Flush returned %v while the failed poll was still Nacking", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for Flush to wait on poll completion")
	}

	closeReleaseNack()

	select {
	case err := <-flushDone:
		if err != nil {
			t.Errorf("Flush after Reset = %v, want nil from a poll made without waiting MaxPollDelaySeconds", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Timed out waiting for Flush to return")
	}
}
