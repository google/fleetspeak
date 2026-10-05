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
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/golang/glog"
	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client"
	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
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
			if req.Header.Get(clientCertHeader) == "" {
				t.Errorf("client certificate header is not set")
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
	t.Parallel()
	dialCalls := make(chan struct{}, 10)
	c := Communicator{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialCalls <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	cctx := newTestCommsContext(
		t,
		comms.ServerInfo{Servers: []string{"localhost:1234"}},
		&clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    10,
			MinFailureDelaySeconds: 10,
		},
	)
	startCommunicator(t, &c, cctx)

	select {
	case <-dialCalls:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for first dial attempt")
	}

	select {
	case <-dialCalls:
		t.Fatal("DialContext() called a second time before Reset()")
	case <-time.After(100 * time.Millisecond):
	}

	c.Reset()

	// Reset cancels the in-flight poll and retries without waiting 10s.
	select {
	case <-dialCalls:
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for second dial attempt after Reset()")
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
			MaxPollDelaySeconds:    60,
			MaxBufferDelaySeconds:  60,
			MinFailureDelaySeconds: 60,
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

	// Wait for first poll.
	for atomic.LoadInt32(&pollCount) == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	waitForBufferedOutbox := func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			c.mu.Lock()
			idle := c.pollCancel == nil
			done := c.pollDone
			c.mu.Unlock()
			select {
			case <-done:
			default:
				if idle {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatal("Timed out waiting for buffered Outbox message to open pollDone")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	// 1. Flush called before Reset while an Outbox message is buffered: Reset
	// must keep the open pollDone channel rather than orphaning the Flush waiter.
	acked := make(chan struct{})
	if err := cl.ProcessMessage(t.Context(), service.AckMessage{
		M:   &fspb.Message{Destination: &fspb.Address{ServiceName: "DummyService"}},
		Ack: func() { close(acked) },
	}); err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}
	waitForBufferedOutbox()

	flushDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		flushDone <- c.Flush(ctx)
	}()

	select {
	case err := <-flushDone:
		t.Fatalf("Flush returned before Reset flushed buffered message: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	c.Reset()

	select {
	case err := <-flushDone:
		if err != nil {
			t.Fatalf("Flush failed: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Timed out waiting for Flush")
	}
	select {
	case <-acked:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for message Ack")
	}

	// 2. Standard Reset followed by Flush when idle: Flush must wait for the
	// Reset-triggered poll to complete.
	before := atomic.LoadInt32(&pollCount)
	c.Reset()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := c.Flush(ctx); err != nil {
		t.Fatalf("Flush after Reset failed: %v", err)
	}
	if got := atomic.LoadInt32(&pollCount); got <= before {
		t.Errorf("pollCount after Reset+Flush = %d, want > %d", got, before)
	}

	// 3. Repeated Reset while an in-flight poll has not yet exited: the first
	// Reset allocates a new pollDone for the post-Reset poll, and the second
	// Reset must not orphan a Flush caller waiting on that channel.
	inAck := make(chan struct{})
	releaseAck := make(chan struct{})
	if err := cl.ProcessMessage(t.Context(), service.AckMessage{
		M: &fspb.Message{Destination: &fspb.Address{ServiceName: "DummyService"}},
		Ack: func() {
			close(inAck)
			<-releaseAck
		},
	}); err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}
	waitForBufferedOutbox()
	c.Reset()
	select {
	case <-inAck:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for in-flight poll Ack")
	}

	before = atomic.LoadInt32(&pollCount)
	c.Reset()
	flushDone2 := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		flushDone2 <- c.Flush(ctx)
	}()
	select {
	case err := <-flushDone2:
		t.Fatalf("Flush returned before in-flight poll finished: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	c.Reset()
	close(releaseAck)

	select {
	case err := <-flushDone2:
		if err != nil {
			t.Fatalf("Flush after repeated Reset failed: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Timed out waiting for Flush after repeated Reset")
	}
	if got := atomic.LoadInt32(&pollCount); got <= before {
		t.Errorf("pollCount after repeated Reset+Flush = %d, want > %d", got, before)
	}
}

func TestPollingBackoffResetsAfterSuccess(t *testing.T) {
	var shouldFail atomic.Bool
	shouldFail.Store(true)
	failed := make(chan time.Time, 16)
	succeeded := make(chan struct{}, 16)

	okBody, err := proto.Marshal(&fspb.ContactData{SequencingNonce: 42})
	if err != nil {
		t.Fatalf("proto.Marshal() failed: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/message", func(res http.ResponseWriter, req *http.Request) {
		if shouldFail.Load() {
			http.Error(res, "server error", http.StatusInternalServerError)
			failed <- time.Now()
			return
		}
		res.Header().Set("Content-Type", "application/octet-stream")
		res.WriteHeader(http.StatusOK)
		res.Write(okBody)
		succeeded <- struct{}{}
	})
	addr, trusted := startStreamingTLSServer(t, mux, "h2")

	var c Communicator
	cctx := newTestCommsContext(
		t,
		comms.ServerInfo{
			Servers:      []string{addr},
			TrustedCerts: trusted,
		},
		&clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    20,
			MaxBufferDelaySeconds:  1,
			MinFailureDelaySeconds: 1,
		},
	)
	startCommunicator(t, &c, cctx)

	waitFail := func(what string) time.Time {
		t.Helper()
		select {
		case ts := <-failed:
			return ts
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for %s", what)
			return time.Time{}
		}
	}

	// Failure 1 sleeps [1.0s, 1.5s], failure 2 sleeps [2.0s, 3.0s], and
	// failure 3 leaves consecutiveFailures at 3 (next delay would be [8s, 12s]).
	t1 := waitFail("first failed poll")
	t2 := waitFail("second failed poll")
	t3 := waitFail("third failed poll")

	if d := t2.Sub(t1); d < 900*time.Millisecond {
		t.Errorf("delay after 1st failure = %v, want >= 900ms", d)
	}
	if d := t3.Sub(t2); d < 1900*time.Millisecond {
		t.Errorf("delay after 2nd failure = %v, want >= 1.9s (exponential backoff)", d)
	}

	// Allow the next poll to succeed and wake up out of backoff sleep.
	shouldFail.Store(false)
	time.Sleep(100 * time.Millisecond)
	c.Reset()

	select {
	case <-succeeded:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for successful poll")
	}
	flushCtx, flushCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer flushCancel()
	if err := c.Flush(flushCtx); err != nil {
		t.Fatalf("Flush() = %v, want nil", err)
	}

	// Trigger a new failure; poll() sleeps for the backoff duration before
	// returning and completing Flush. Because the successful poll reset backoff,
	// that sleep should be back to [1.0s, 1.5s] rather than >= 4s.
	shouldFail.Store(true)
	c.Reset()

	tAfterReset := waitFail("first failed poll after reset")
	flushCtx2, flushCancel2 := context.WithTimeout(t.Context(), 10*time.Second)
	defer flushCancel2()
	if err := c.Flush(flushCtx2); err == nil {
		t.Fatalf("Flush() after failed poll = nil, want error")
	}
	if d := time.Since(tAfterReset); d < 900*time.Millisecond || d >= 3500*time.Millisecond {
		t.Errorf("backoff sleep after successful poll = %v, want in [900ms, 3.5s)", d)
	}
}
