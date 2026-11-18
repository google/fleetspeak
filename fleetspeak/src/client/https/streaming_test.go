package https

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/golang/glog"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client"
	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/https"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

func randBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func TestStreamingCreate(t *testing.T) {
	var c StreamingCommunicator
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

type streamingTestServer struct {
	// These should be populated by the creator.
	t                     *testing.T
	received              chan<- *fspb.ContactData
	toSend                <-chan *fspb.ContactData
	allowMultipleRequests bool

	// These are populated by Start.
	pemCert, pemKey []byte
	tl              net.Listener
	rc              int32
}

func (s *streamingTestServer) Start() {
	var err error
	s.pemCert, s.pemKey, err = comtesting.ServerCert()
	if err != nil {
		s.t.Fatal(err)
	}

	cb, _ := pem.Decode(s.pemCert)
	if cb == nil || cb.Type != "CERTIFICATE" {
		s.t.Fatalf("Expected CERTIFICATE in parsed pem block, got: %v", cb)
	}

	cp, err := tls.X509KeyPair(s.pemCert, s.pemKey)
	if err != nil {
		s.t.Fatal(err)
	}
	ad, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		s.t.Fatal(err)
	}
	s.tl, err = net.ListenTCP("tcp", ad)
	if err != nil {
		s.t.Fatal(err)
	}

	var handler http.HandlerFunc = func(res http.ResponseWriter, req *http.Request) {
		cid, err := common.MakeClientID(req.TLS.PeerCertificates[0].PublicKey)
		if err != nil {
			s.t.Errorf("unable to make ClientID in test server: %v", err)
		}
		reqs := atomic.AddInt32(&s.rc, 1)
		if !s.allowMultipleRequests {
			if reqs != 1 {
				s.t.Errorf("Only expected 1 request, but this is request %d", reqs)
				http.Error(res, "only expected 1 request", http.StatusBadRequest)
			}
		}
		body := bufio.NewReader(req.Body)
		b := make([]byte, 4)
		if _, err := io.ReadAtLeast(body, b, 4); err != nil {
			s.t.Errorf("Error reading magic number: %v", err)
			http.Error(res, "unable to read magic number", http.StatusBadRequest)
			return
		}
		m := binary.LittleEndian.Uint32(b)
		if m != magic {
			s.t.Errorf("Unexpected magic number, got %x expected %x", m, magic)
			http.Error(res, "bad magic number", http.StatusBadRequest)
			return
		}

		cnt := uint64(0)
		var writerStarted bool
		var writeLock sync.Mutex
		for {
			size, err := binary.ReadUvarint(body)
			if err != nil {
				if !(err == io.EOF || err == io.ErrUnexpectedEOF || strings.Contains(err.Error(), "connection reset by peer")) {
					s.t.Errorf("Unable to read size: %v", err)
				}
				return
			}
			buf := make([]byte, size)
			_, err = io.ReadFull(body, buf)
			if err != nil {
				s.t.Errorf("Unable to read incoming messages: %v", err)
				return
			}
			var wcd fspb.WrappedContactData
			if err := proto.Unmarshal(buf, &wcd); err != nil {
				s.t.Errorf("Unable to parse incoming messages: %v", err)
				return
			}
			var rcd fspb.ContactData
			if err := proto.Unmarshal(wcd.ContactData, &rcd); err != nil {
				s.t.Errorf("Unable to parse ContactData: %v", err)
				return
			}
			s.received <- &rcd
			cd := &fspb.ContactData{
				AckIndex: cnt,
			}
			cnt++

			outBuf, err := proto.Marshal(cd)
			if err != nil {
				s.t.Errorf("Unable to encode response: %v", err)
				return
			}
			sizeBuf := make([]byte, 0, 16)
			sizeBuf = binary.AppendUvarint(sizeBuf, uint64(len(outBuf)))

			writeLock.Lock()
			if _, err := res.Write(sizeBuf); err != nil {
				s.t.Errorf("Unable to write response: %v", err)
				return
			}
			if _, err := res.Write(outBuf); err != nil {
				s.t.Errorf("Unable to write response: %v", err)
				return
			}
			res.(http.Flusher).Flush()
			writeLock.Unlock()

			if !writerStarted {
				go func() {
					for cd := range s.toSend {
						for _, m := range cd.Messages {
							m.Destination.ClientId = cid.Bytes()
						}

						outBuf, err := proto.Marshal(cd)
						if err != nil {
							s.t.Errorf("Unable to encode response: %v", err)
							return
						}
						sizeBuf := make([]byte, 0, 16)
						sizeBuf = binary.AppendUvarint(sizeBuf, uint64(len(outBuf)))

						writeLock.Lock()
						if _, err := res.Write(sizeBuf); err != nil {
							s.t.Errorf("Unable to write response: %v", err)
							return
						}
						if _, err := res.Write(outBuf); err != nil {
							s.t.Errorf("Unable to write response: %v", err)
							return
						}
						res.(http.Flusher).Flush()
						writeLock.Unlock()
					}
				}()
				writerStarted = true
			}
		}
	}

	mux := http.NewServeMux()
	mux.Handle("/streaming-message", &https.CompressionHandler{Wrapped: handler})

	server := http.Server{
		Addr:    s.Addr(),
		Handler: mux,
		TLSConfig: &tls.Config{
			ClientAuth:   tls.RequireAnyClientCert,
			Certificates: []tls.Certificate{cp},
			NextProtos:   []string{"h2"},
		},
	}
	l := tls.NewListener(s.tl, server.TLSConfig)
	go server.Serve(l)
}

func (s *streamingTestServer) Addr() string {
	return s.tl.Addr().String()
}
func (s *streamingTestServer) Stop() {
	if err := s.tl.Close(); err != nil {
		log.Errorf("Error closing listener: %v", err)
	}
}

type slowConn struct {
	net.Conn
	out *rate.Limiter
}

func (c slowConn) Write(b []byte) (int, error) {
	var s int
	for len(b) > 0 {
		l := len(b)
		if l > c.out.Burst() {
			l = c.out.Burst()
		}
		h := b[:l]
		b = b[l:]
		c.out.WaitN(context.Background(), len(h))
		r, err := c.Conn.Write(h)
		s += r
		if err != nil {
			return s, err
		}
	}
	return s, nil
}

func startStreamingClient(t *testing.T, addr string, cert []byte, out *rate.Limiter, compression fspb.CompressionAlgorithm) *client.Client {
	var dial func(ctx context.Context, network, addr string) (net.Conn, error)
	if out != nil {
		dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
			return slowConn{c, out}, err
		}
	}
	c := StreamingCommunicator{
		DialContext: dial,
	}
	conf := config.Configuration{
		Servers:       []string{addr},
		TrustedCerts:  x509.NewCertPool(),
		FixedServices: []*fspb.ClientServiceConfig{{Name: "NOOPService", Factory: "NOOP"}},
		CommunicatorConfig: &clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    2,
			MaxBufferDelaySeconds:  1,
			MinFailureDelaySeconds: 1,
			Compression:            compression,
		},
	}
	if !conf.TrustedCerts.AppendCertsFromPEM(cert) {
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
	return cl
}

func TestStreamingCommunicator(t *testing.T) {
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
			testStreamingCommunicator(t, tc.compression)
		})
	}
}

func testStreamingCommunicator(t *testing.T, compression fspb.CompressionAlgorithm) {
	t.Helper()
	received := make(chan *fspb.ContactData, 5)
	toSend := make(chan *fspb.ContactData, 5)

	server := streamingTestServer{
		t:        t,
		received: received,
		toSend:   toSend,
	}
	server.Start()
	defer server.Stop()

	cl := startStreamingClient(t, server.Addr(), server.pemCert, nil, compression)
	defer cl.Stop()

	acks := make(chan int, 1000)
	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: "DummyService"}},
			Ack: func() { acks <- 0 },
		}); err != nil {
		t.Fatalf("unable to hand message to client: %v", err)
	}

	// The message not might work through the system in time for the initial
	// exchange, but it should eventually work through the system and come
	// out by itself in a ContactData.
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
				Messages: []*fspb.Message{
					{Destination: &fspb.Address{ServiceName: "DummyService"}},
				},
				AllowedMessages: map[string]uint64{
					"NOOPService": 100,
					"system":      100,
				},
			}
			cb.ClientClock = nil
			if !proto.Equal(cb, want) {
				t.Errorf("Unexpected ContactData: want [%v], got [%v]", want, cb)
			}
			break
		}
	}

	i := <-acks
	if i != 0 {
		t.Errorf("Expected ack for msg 0, got: %d", i)
	}

	// 10 small messages - should be grouped together into 1 contact data
	for i := range 10 {
		if err := cl.ProcessMessage(context.Background(),
			service.AckMessage{
				M: &fspb.Message{
					Destination: &fspb.Address{ServiceName: "DummyService"}},
				Ack: func() { acks <- i },
			},
		); err != nil {
			t.Fatalf("unable to hand message to client: %v", err)
		}

	}

	rcb := <-received
	if len(rcb.Messages) != 10 {
		t.Errorf("Expected a ContactData with 10 records, got %d", len(rcb.Messages))
	}
	for i := range 10 {
		j := <-acks
		if j != i {
			t.Errorf("Expected ack for %d, got ack for %d", i, j)
		}
	}

	scb := fspb.ContactData{
		SequencingNonce: 44,
	}
	for range 35 {
		scb.Messages = append(scb.Messages, &fspb.Message{
			Destination: &fspb.Address{ServiceName: "NOOPService"}, MessageType: "TestMessage"})
	}
	toSend <- &scb
	// Send messages through until we get a contact datas giving 5 more capacity.
	var granted uint64
F:
	for {
		rcb := <-received
		granted += rcb.AllowedMessages["NOOPService"]
		if granted >= 32 {
			break F
		}
	}
	if granted > 35 {
		t.Errorf("Expected to be granted at most 35, but got %d", granted)
	}
	close(toSend)
}

func TestStreamingCommunicatorBulkSlow(t *testing.T) {
	received := make(chan *fspb.ContactData, 5)
	toSend := make(chan *fspb.ContactData, 20)

	server := streamingTestServer{
		t:        t,
		received: received,
		toSend:   toSend,
	}
	server.Start()
	defer server.Stop()

	// Limit write rate to 100KB/sec in 1KB chunks
	cl := startStreamingClient(t, server.Addr(), server.pemCert, rate.NewLimiter(100*1024, 10*1024), fspb.CompressionAlgorithm_COMPRESSION_NONE)
	defer cl.Stop()

	// Send an initial message to make sure a streaming connection is started.
	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: "DummyService"},
				Data: &anypb.Any{
					TypeUrl: "Some proto",
				},
			},
		}); err != nil {
		t.Fatalf("unable to hand message to client: %v", err)
	}

	// Wait for it to work through the system, filter the initial client info mesage.
	for cb := range received {
		cb.Messages = filterMessages(cb.Messages, func(m *fspb.Message) bool {
			return m.Destination.ServiceName != "system"
		})
		if len(cb.Messages) > 1 {
			t.Errorf("Expected at most one message in delivered ContactData, got: %v", cb.Messages)
			break
		}
		if len(cb.Messages) == 1 {
			break
		}
	}

	// Send a large-ish message, sized to make rate measurement easy (0.5MB)
	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: "DummyService"},
				Data: &anypb.Any{
					TypeUrl: "Some proto",
					Value:   randBytes(512 * 1024),
				},
			},
		}); err != nil {
		t.Fatalf("unable to hand message to client: %v", err)
	}
	var rcb *fspb.ContactData
	select {
	case <-time.After(10 * time.Second):
		t.Fatalf("Failed to read expected contact data.")
	case rcb = <-received:
	}
	if len(rcb.Messages) != 1 {
		t.Errorf("Expected a ContactData with 1 record, got %d", len(rcb.Messages))
	}

	// 6 medium (300KB) messages. The client should be flushing when it has more than 10 sec (~1MB) worth of data, so
	// we expect 4 to arrive together, then 2 more.
	for range 6 {
		if err := cl.ProcessMessage(context.Background(),
			service.AckMessage{
				M: &fspb.Message{
					Destination: &fspb.Address{ServiceName: "DummyService"},
					Data: &anypb.Any{
						TypeUrl: "Some proto",
						Value:   randBytes(300 * 1024),
					},
				},
			},
		); err != nil {
			t.Fatalf("unable to hand message to client: %v", err)
		}

	}

	select {
	case <-time.After(15 * time.Second):
		t.Fatalf("Failed to read expected contact data.")
	case rcb = <-received:
	}
	if len(rcb.Messages) != 4 {
		t.Errorf("Expected a ContactData with 4 records, got %d", len(rcb.Messages))
	}

	select {
	case <-time.After(15 * time.Second):
		t.Fatalf("Failed to read expected contact data.")
	case rcb = <-received:
	}
	if len(rcb.Messages) != 2 {
		t.Errorf("Expected a ContactData with 2 records, got %d", len(rcb.Messages))
	}
	close(toSend)
}

func TestStreamingCommunicatorBulkFast(t *testing.T) {
	received := make(chan *fspb.ContactData, 5)
	toSend := make(chan *fspb.ContactData, 20)

	server := streamingTestServer{
		t:        t,
		received: received,
		toSend:   toSend,
	}
	server.Start()
	defer server.Stop()

	// 5MB/sec in 10KB chunks
	cl := startStreamingClient(t, server.Addr(), server.pemCert, rate.NewLimiter(5*1024*1024, 10*1024), fspb.CompressionAlgorithm_COMPRESSION_NONE)
	defer cl.Stop()

	// Send an initial message to make sure a streaming connection is started.
	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: "DummyService"},
				Data: &anypb.Any{
					TypeUrl: "Some proto",
				},
			},
		}); err != nil {
		t.Fatalf("unable to hand message to client: %v", err)
	}

	// Wait for it to work through the system, filter the initial client info mesage.
	for cb := range received {
		cb.Messages = filterMessages(cb.Messages, func(m *fspb.Message) bool {
			return m.Destination.ServiceName != "system"
		})
		if len(cb.Messages) > 1 {
			t.Errorf("Expected at most one message in delivered ContactData, got: %v", cb.Messages)
			break
		}
		if len(cb.Messages) == 1 {
			break
		}
	}

	// Send an initial streaming message, sized to make rate measurement easy (0.5MB)
	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: "DummyService"},
				Data: &anypb.Any{
					TypeUrl: "Some proto",
					Value:   randBytes(512 * 1024),
				},
			},
		}); err != nil {
		t.Fatalf("unable to hand message to client: %v", err)
	}
	var rcb *fspb.ContactData
	select {
	case <-time.After(10 * time.Second):
		t.Fatalf("Failed to read expected contact data.")
	case rcb = <-received:
	}
	if len(rcb.Messages) != 1 {
		t.Errorf("Expected a ContactData with 1 record, got %d", len(rcb.Messages))
	}

	// 10 large (1MB) messages. The client should be flushing when it has more than 7.5MB of data, so
	// we expect 8 to arrive together, then 2 more.
	for range 10 {
		if err := cl.ProcessMessage(context.Background(),
			service.AckMessage{
				M: &fspb.Message{
					Destination: &fspb.Address{ServiceName: "DummyService"},
					Data: &anypb.Any{
						TypeUrl: "Some proto",
						Value:   randBytes(1024 * 1024),
					},
				},
			},
		); err != nil {
			t.Fatalf("unable to hand message to client: %v", err)
		}

	}

	select {
	case <-time.After(10 * time.Second):
		t.Fatalf("Failed to read expected contact data.")
	case rcb = <-received:
	}
	if len(rcb.Messages) != 8 {
		t.Errorf("Expected a ContactData with 10 records, got %d", len(rcb.Messages))
	}

	select {
	case <-time.After(10 * time.Second):
		t.Fatalf("Failed to read expected contact data.")
	case rcb = <-received:
	}
	if len(rcb.Messages) != 2 {
		t.Errorf("Expected a ContactData with 2 records, got %d", len(rcb.Messages))
	}
	close(toSend)
}

func TestStreamingReset(t *testing.T) {
	var c StreamingCommunicator
	conf := config.Configuration{
		Servers: []string{"localhost:1234"},
		CommunicatorConfig: &clpb.CommunicatorConfig{
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

func TestStreamingFlushAfterReset(t *testing.T) {
	received := make(chan *fspb.ContactData, 5)
	toSend := make(chan *fspb.ContactData, 5)

	server := streamingTestServer{
		t:                     t,
		received:              received,
		toSend:                toSend,
		allowMultipleRequests: true,
	}
	server.Start()
	defer server.Stop()

	var c StreamingCommunicator
	conf := config.Configuration{
		Servers:       []string{server.Addr()},
		TrustedCerts:  x509.NewCertPool(),
		FixedServices: []*fspb.ClientServiceConfig{{Name: "NOOPService", Factory: "NOOP"}},
		CommunicatorConfig: &clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    2,
			MaxBufferDelaySeconds:  1,
			MinFailureDelaySeconds: 1,
			Compression:            fspb.CompressionAlgorithm_COMPRESSION_NONE,
		},
	}
	if !conf.TrustedCerts.AppendCertsFromPEM(server.pemCert) {
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
	defer cl.Stop()

	// Wait for initial connection to be established.
	acks := make(chan int, 1)
	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: "DummyService"}},
			Ack: func() { acks <- 0 },
		}); err != nil {
		t.Fatalf("unable to hand message to client: %v", err)
	}

	<-received
	<-acks

	// Connection is idle.

	// Call Reset to break it.
	c.Reset()

	// Call Flush in a goroutine.
	flushDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		flushDone <- c.Flush(ctx)
	}()

	select {
	case err := <-flushDone:
		if err != nil {
			t.Errorf("Flush failed: %v", err)
		}
		if atomic.LoadInt32(&server.rc) < 2 {
			t.Errorf("Flush returned before reconnect. Expected at least 2 requests, got %d", server.rc)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Timed out waiting for Flush to complete")
	}
}

// streamingTestContext is a minimal comms.Context for tests that run a
// StreamingCommunicator without a client.Client.
type streamingTestContext struct {
	comms.Context
	id                 common.ClientID
	priv               *ecdsa.PrivateKey
	si                 comms.ServerInfo
	conf               *clpb.CommunicatorConfig
	outbox             <-chan comms.MessageInfo
	beacon             <-chan struct{}
	stats              stats.Collector
	processContactData func(ctx context.Context, cd *fspb.ContactData, streaming bool) error
}

func (c *streamingTestContext) Outbox() <-chan comms.MessageInfo  { return c.outbox }
func (c *streamingTestContext) ProcessingBeacon() <-chan struct{} { return c.beacon }
func (c *streamingTestContext) CurrentID() common.ClientID        { return c.id }
func (c *streamingTestContext) CurrentIdentity() (comms.ClientIdentity, error) {
	return comms.ClientIdentity{ID: c.id, Private: c.priv, Public: c.priv.Public()}, nil
}
func (c *streamingTestContext) ServerInfo() (comms.ServerInfo, error)        { return c.si, nil }
func (c *streamingTestContext) CommunicatorConfig() *clpb.CommunicatorConfig { return c.conf }
func (c *streamingTestContext) ChainRevoked([]*x509.Certificate) bool        { return false }
func (c *streamingTestContext) Stats() stats.Collector {
	if c.stats != nil {
		return c.stats
	}
	return stats.NoopCollector{}
}
func (c *streamingTestContext) MakeContactData(msgs []*fspb.Message, _ map[string]uint64) (*fspb.WrappedContactData, map[string]uint64, error) {
	b, err := proto.Marshal(&fspb.ContactData{Messages: msgs})
	if err != nil {
		return nil, nil, err
	}
	return &fspb.WrappedContactData{ContactData: b}, nil, nil
}
func (c *streamingTestContext) ProcessContactData(ctx context.Context, cd *fspb.ContactData, streaming bool) error {
	if c.processContactData != nil {
		return c.processContactData(ctx, cd, streaming)
	}
	return nil
}

// TestStreamingConfigureFailureKeepsID checks that a configure() that fails
// leaves c.id unchanged. connectLoop calls configure() while c.id differs
// from the current client ID, so this is what makes it try again.
func TestStreamingConfigureFailureKeepsID(t *testing.T) {
	newIdentity := func() (common.ClientID, *ecdsa.PrivateKey) {
		t.Helper()
		priv, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
		if err != nil {
			t.Fatalf("GenerateKey failed: %v", err)
		}
		id, err := common.MakeClientID(priv.Public())
		if err != nil {
			t.Fatalf("MakeClientID failed: %v", err)
		}
		return id, priv
	}
	servers := []string{"localhost:1234"}

	oldID, oldPriv := newIdentity()
	cctx := &streamingTestContext{
		id:   oldID,
		priv: oldPriv,
		si:   comms.ServerInfo{Servers: servers},
		conf: &clpb.CommunicatorConfig{},
	}
	var c StreamingCommunicator
	if err := c.Setup(cctx); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	defer c.fin()

	newID, newPriv := newIdentity()
	cctx.id, cctx.priv = newID, newPriv
	cctx.si.Servers = nil
	if err := c.configure(); err == nil {
		t.Fatal("configure() with no server addresses = nil, want an error")
	}
	if c.id != oldID {
		t.Errorf("c.id = %v after a failed configure(), want the previous ID %v", c.id, oldID)
	}

	cctx.si.Servers = servers
	if err := c.configure(); err != nil {
		t.Fatalf("configure() = %v, want nil", err)
	}
	if c.id != newID {
		t.Errorf("c.id = %v after a successful configure(), want the new ID %v", c.id, newID)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestStreamingConnectClosesRequestPipeOnError checks that connect closes the
// request body pipe before returning an error, whether the attempt is canceled,
// fails in RoundTrip, receives a non-200 status, or fails reading the initial
// ContactData.
func TestStreamingConnectClosesRequestPipeOnError(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	id, err := common.MakeClientID(priv.Public())
	if err != nil {
		t.Fatalf("MakeClientID failed: %v", err)
	}
	beacon := make(chan struct{})
	close(beacon)

	emptyContactPayload, err := proto.Marshal(&fspb.ContactData{})
	if err != nil {
		t.Fatalf("proto.Marshal() failed: %v", err)
	}
	emptyContactFrame := append(binary.AppendUvarint(nil, uint64(len(emptyContactPayload))), emptyContactPayload...)

	errDial := errors.New("dial failed")
	for _, tc := range []struct {
		name               string
		roundTrip          func(cancel context.CancelFunc, req *http.Request) (*http.Response, error)
		processContactData func(cancel context.CancelFunc, reqCtx context.Context) error
		wantErr            error
	}{
		{
			name: "ContextCanceled",
			roundTrip: func(cancel context.CancelFunc, req *http.Request) (*http.Response, error) {
				cancel()
				<-req.Context().Done()
				return nil, req.Context().Err()
			},
			wantErr: context.Canceled,
		},
		{
			name: "RoundTripError",
			roundTrip: func(_ context.CancelFunc, _ *http.Request) (*http.Response, error) {
				return nil, errDial
			},
			wantErr: errDial,
		},
		{
			name: "NonOKStatus",
			roundTrip: func(_ context.CancelFunc, _ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Body:       http.NoBody,
				}, nil
			},
		},
		{
			name: "ReadContactError",
			roundTrip: func(_ context.CancelFunc, _ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       http.NoBody,
				}, nil
			},
			wantErr: io.EOF,
		},
		{
			name: "CanceledDuringProcessContactData",
			roundTrip: func(_ context.CancelFunc, _ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(emptyContactFrame)),
				}, nil
			},
			processContactData: func(cancel context.CancelFunc, reqCtx context.Context) error {
				cancel()
				<-reqCtx.Done()
				return nil
			},
			wantErr: context.Canceled,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)

			var reqCtx context.Context
			var reqBody io.ReadCloser
			cctx := &streamingTestContext{
				id:     id,
				priv:   priv,
				si:     comms.ServerInfo{Servers: []string{"localhost:1234"}},
				conf:   &clpb.CommunicatorConfig{Compression: fspb.CompressionAlgorithm_COMPRESSION_DEFLATE},
				beacon: beacon,
			}
			if tc.processContactData != nil {
				cctx.processContactData = func(context.Context, *fspb.ContactData, bool) error {
					return tc.processContactData(cancel, reqCtx)
				}
			}
			var c StreamingCommunicator
			if err := c.Setup(cctx); err != nil {
				t.Fatalf("Setup() unexpected error: %v", err)
			}
			t.Cleanup(c.fin)

			c.hc = &http.Client{
				Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					reqCtx = req.Context()
					reqBody = req.Body
					t.Cleanup(func() { _ = req.Body.Close() })
					return tc.roundTrip(cancel, req)
				}),
			}

			con, err := c.connect(ctx, "localhost:1234", time.Minute)
			if con != nil {
				con.stop()
				t.Errorf("connect() = %v, want nil", con)
			}
			if err == nil {
				t.Fatal("connect() error = nil, want non-nil")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("connect() error = %v, want %v", err, tc.wantErr)
			}
			if reqBody == nil {
				t.Fatal("RoundTrip was not called")
			}
			if n, readErr := reqBody.Read(make([]byte, 1)); n != 0 || !errors.Is(readErr, io.ErrClosedPipe) {
				t.Errorf("req.Body.Read() after failed connect = (%d, %v), want (0, %v)", n, readErr, io.ErrClosedPipe)
			}
		})
	}
}

// TestStreamingStopDuringDialWithDeflate stops the communicator while connect
// waits for a connection to be dialed, which is before anything reads the
// request body that connect writes through the DEFLATE writer.
func TestStreamingStopDuringDialWithDeflate(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	id, err := common.MakeClientID(priv.Public())
	if err != nil {
		t.Fatalf("MakeClientID failed: %v", err)
	}

	dialing := make(chan struct{}, 1)
	c := StreamingCommunicator{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			select {
			case dialing <- struct{}{}:
			default:
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	cctx := &streamingTestContext{
		id:   id,
		priv: priv,
		si:   comms.ServerInfo{Servers: []string{"localhost:1234"}},
		conf: &clpb.CommunicatorConfig{
			Compression: fspb.CompressionAlgorithm_COMPRESSION_DEFLATE,
		},
	}
	if err := c.Setup(cctx); err != nil {
		t.Fatalf("Setup failed: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	select {
	case <-dialing:
	case <-time.After(10 * time.Second):
		t.Fatal("Timed out waiting for the communicator to dial")
	}

	stopped := make(chan struct{})
	go func() {
		c.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Timed out waiting for Stop to return")
	}
}

func TestStreamingResetBeforeConnectionPublished(t *testing.T) {
	t.Parallel()
	received := make(chan *fspb.ContactData, 5)
	toSend := make(chan *fspb.ContactData, 5)
	defer close(toSend)

	server := streamingTestServer{
		t:                     t,
		received:              received,
		toSend:                toSend,
		allowMultipleRequests: true,
	}
	server.Start()
	defer server.Stop()

	var c StreamingCommunicator
	conf := config.Configuration{
		Servers:       []string{server.Addr()},
		TrustedCerts:  x509.NewCertPool(),
		FixedServices: []*fspb.ClientServiceConfig{{Name: "NOOPService", Factory: "NOOP"}},
		CommunicatorConfig: &clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    2,
			MaxBufferDelaySeconds:  1,
			MinFailureDelaySeconds: 1,
			Compression:            fspb.CompressionAlgorithm_COMPRESSION_NONE,
		},
	}
	if !conf.TrustedCerts.AppendCertsFromPEM(server.pemCert) {
		t.Fatal("AppendCertsFromPEM() = false, want true")
	}
	cl, err := client.New(
		conf,
		client.Components{
			ServiceFactories: map[string]service.Factory{"NOOP": service.NOOPFactory},
			Communicator:     &c})
	if err != nil {
		t.Fatalf("client.New() failed: %v", err)
	}
	defer cl.Stop()

	// Pause inside Ack during connect(), before connectLoop publishes c.curCon.
	ackEntered := make(chan struct{})
	releaseAck := make(chan struct{})
	var ackOnce sync.Once
	if err := cl.ProcessMessage(t.Context(),
		service.AckMessage{
			M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: "DummyService"}},
			Ack: func() {
				ackOnce.Do(func() {
					close(ackEntered)
					<-releaseAck
				})
			},
		}); err != nil {
		t.Fatalf("ProcessMessage() failed: %v", err)
	}

	<-received
	<-ackEntered
	c.mu.Lock()
	published := c.curCon != nil
	c.mu.Unlock()
	if published {
		close(releaseAck)
		t.Fatal("Ack ran after the connection was published, want it to run during connect(): the message missed the initial exchange")
	}

	// Trigger Reset and Flush while c.curCon is still nil inside connect().
	c.Reset()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	flushDone := make(chan error, 1)
	go func() {
		flushDone <- c.Flush(ctx)
	}()
	close(releaseAck)

	select {
	case err := <-flushDone:
		if err != nil {
			t.Errorf("Flush failed: %v", err)
		}
		if got := atomic.LoadInt32(&server.rc); got < 2 {
			t.Errorf("Flush returned before post-Reset reconnect: got %d requests, want at least 2", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Timed out waiting for Flush to complete")
	}
}

// TestStreamingMultiHostFailover checks that connectLoop tries the next
// configured server immediately when the first server fails without Reset, and
// promotes the working server to the front of c.hosts.
func TestStreamingMultiHostFailover(t *testing.T) {
	t.Parallel()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() failed: %v", err)
	}
	id, err := common.MakeClientID(priv.Public())
	if err != nil {
		t.Fatalf("MakeClientID() failed: %v", err)
	}

	received := make(chan *fspb.ContactData, 5)
	toSend := make(chan *fspb.ContactData, 5)
	defer close(toSend)

	server := streamingTestServer{
		t:                     t,
		received:              received,
		toSend:                toSend,
		allowMultipleRequests: true,
	}
	server.Start()
	defer server.Stop()

	trusted := x509.NewCertPool()
	if !trusted.AppendCertsFromPEM(server.pemCert) {
		t.Fatal("AppendCertsFromPEM() = false, want true")
	}

	const unreachableAddr = "unreachable.invalid:443"
	var unreachableDials atomic.Int32
	c := StreamingCommunicator{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == unreachableAddr {
				unreachableDials.Add(1)
				return nil, errors.New("connection refused")
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	beacon := make(chan struct{}, 2)
	beacon <- struct{}{}
	beacon <- struct{}{}
	cctx := &streamingTestContext{
		id:   id,
		priv: priv,
		si: comms.ServerInfo{
			Servers:      []string{unreachableAddr, server.Addr()},
			TrustedCerts: trusted,
		},
		conf: &clpb.CommunicatorConfig{
			MinFailureDelaySeconds: 10,
			Compression:            fspb.CompressionAlgorithm_COMPRESSION_NONE,
		},
		beacon: beacon,
	}
	if err := c.Setup(cctx); err != nil {
		t.Fatalf("Setup() failed: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer c.Stop()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := c.Flush(ctx); err != nil {
		t.Fatalf("Flush() = %v, want nil", err)
	}
	if got := unreachableDials.Load(); got != 1 {
		t.Errorf("unreachable host dial count = %d, want 1", got)
	}

	// Reconnecting after Reset starts with the promoted working server rather
	// than dialing unreachableAddr again.
	c.Reset()
	reconnectCtx, reconnectCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer reconnectCancel()
	if err := c.Flush(reconnectCtx); err != nil {
		t.Fatalf("Flush() after Reset = %v, want nil", err)
	}
	if got := atomic.LoadInt32(&server.rc); got < 2 {
		t.Errorf("server request count after Reset = %d, want at least 2", got)
	}
	if got := unreachableDials.Load(); got != 1 {
		t.Errorf("unreachable host dial count after Reset = %d, want 1", got)
	}
}

// TestStreamingResetSkipsSuicideCheck checks that an attempt abandoned by Reset
// skips the FailureSuicideTimeSeconds check.
func TestStreamingResetSkipsSuicideCheck(t *testing.T) {
	t.Parallel()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() failed: %v", err)
	}
	id, err := common.MakeClientID(priv.Public())
	if err != nil {
		t.Fatalf("MakeClientID() failed: %v", err)
	}

	dialCalls := make(chan struct{}, 4)
	c := StreamingCommunicator{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialCalls <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	beacon := make(chan struct{})
	close(beacon)
	cctx := &streamingTestContext{
		id:   id,
		priv: priv,
		si:   comms.ServerInfo{Servers: []string{"localhost:1234"}},
		conf: &clpb.CommunicatorConfig{
			MinFailureDelaySeconds:    10,
			FailureSuicideTimeSeconds: 1,
		},
		beacon: beacon,
	}
	if err := c.Setup(cctx); err != nil {
		t.Fatalf("Setup() failed: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer c.Stop()

	select {
	case <-dialCalls:
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for first dial attempt")
	}

	time.Sleep(1100 * time.Millisecond)
	c.Reset()

	select {
	case <-dialCalls:
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for second dial attempt after Reset")
	}
}

// TestStreamingFailureDelayAfterReset checks that a wakeUp token left by an
// earlier Reset does not cut short the retry delay of a later unrelated
// connection failure, while a Reset during that retry delay wakes connectLoop
// immediately.
func TestStreamingFailureDelayAfterReset(t *testing.T) {
	t.Parallel()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() failed: %v", err)
	}
	id, err := common.MakeClientID(priv.Public())
	if err != nil {
		t.Fatalf("MakeClientID() failed: %v", err)
	}

	var attempts atomic.Int32
	dialCalls := make(chan int32, 8)
	c := StreamingCommunicator{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			n := attempts.Add(1)
			dialCalls <- n
			if n == 2 {
				return nil, errors.New("connection refused")
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	beacon := make(chan struct{})
	close(beacon)
	cctx := &streamingTestContext{
		id:     id,
		priv:   priv,
		si:     comms.ServerInfo{Servers: []string{"localhost:1234"}},
		conf:   &clpb.CommunicatorConfig{MinFailureDelaySeconds: 10},
		beacon: beacon,
	}
	if err := c.Setup(cctx); err != nil {
		t.Fatalf("Setup() failed: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer c.Stop()

	select {
	case got := <-dialCalls:
		if got != 1 {
			t.Fatalf("first dial attempt = %d, want 1", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for first dial attempt")
	}

	// Reset cancels attempt 1 and buffers a wakeUp token. Attempt 2 drains
	// that token on start and then fails without Reset.
	c.Reset()

	select {
	case got := <-dialCalls:
		if got != 2 {
			t.Fatalf("second dial attempt = %d, want 2", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timed out waiting for second dial attempt")
	}

	select {
	case got := <-dialCalls:
		t.Fatalf("DialContext() called for attempt %d during MinFailureDelaySeconds wait", got)
	case <-time.After(200 * time.Millisecond):
	}

	// A Reset during the MinFailureDelaySeconds wait wakes connectLoop at once.
	c.Reset()

	select {
	case got := <-dialCalls:
		if got != 3 {
			t.Fatalf("third dial attempt = %d, want 3", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for third dial attempt after Reset")
	}
}

type inboundErrorDelayStats struct {
	stats.NoopCollector
	releaseEarly <-chan struct{}
	onErrorDone  func()
}

func (s *inboundErrorDelayStats) InboundContactData(_ string, _ int, err error) {
	if err != nil {
		select {
		case <-s.releaseEarly:
		case <-time.After(200 * time.Millisecond):
		}
		s.onErrorDone()
	}
}

// TestStreamingDiscardWaitsForConnectionStop checks that when connectLoop
// discards a connection whose attempt started before Reset, it waits for that
// connection's goroutines to finish before starting the replacement attempt.
func TestStreamingDiscardWaitsForConnectionStop(t *testing.T) {
	t.Parallel()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() failed: %v", err)
	}
	id, err := common.MakeClientID(priv.Public())
	if err != nil {
		t.Fatalf("MakeClientID() failed: %v", err)
	}

	received := make(chan *fspb.ContactData, 5)
	toSend := make(chan *fspb.ContactData, 5)
	defer close(toSend)

	server := streamingTestServer{
		t:                     t,
		received:              received,
		toSend:                toSend,
		allowMultipleRequests: true,
	}
	server.Start()
	defer server.Stop()

	trusted := x509.NewCertPool()
	if !trusted.AppendCertsFromPEM(server.pemCert) {
		t.Fatal("AppendCertsFromPEM() = false, want true")
	}

	var dialCount atomic.Int32
	var firstReadStopped atomic.Bool
	var startedBeforeFirstStopped atomic.Bool
	secondDialStarted := make(chan struct{})
	var secondDialOnce sync.Once

	c := StreamingCommunicator{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if dialCount.Add(1) == 2 {
				startedBeforeFirstStopped.Store(!firstReadStopped.Load())
				secondDialOnce.Do(func() { close(secondDialStarted) })
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}

	// Closing beacon inside Ack makes the replacement connect() return from
	// groupMessages without waiting 1s, so DialContext runs immediately if
	// connectLoop skips con.working.Wait().
	beacon := make(chan struct{})
	outbox := make(chan comms.MessageInfo, 1)
	var ackOnce sync.Once
	outbox <- comms.MessageInfo{
		M: &fspb.Message{Destination: &fspb.Address{ServiceName: "DummyService"}},
		Ack: func() {
			ackOnce.Do(func() {
				close(beacon)
				c.Reset()
			})
		},
		Nack: func() {},
	}

	cctx := &streamingTestContext{
		id:     id,
		priv:   priv,
		si:     comms.ServerInfo{Servers: []string{server.Addr()}, TrustedCerts: trusted},
		conf:   &clpb.CommunicatorConfig{MinFailureDelaySeconds: 10, Compression: fspb.CompressionAlgorithm_COMPRESSION_NONE},
		outbox: outbox,
		beacon: beacon,
		stats: &inboundErrorDelayStats{
			releaseEarly: secondDialStarted,
			onErrorDone:  func() { firstReadStopped.Store(true) },
		},
	}
	if err := c.Setup(cctx); err != nil {
		t.Fatalf("Setup() failed: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer c.Stop()

	select {
	case <-secondDialStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("Timed out waiting for replacement dial attempt after Reset")
	}
	if startedBeforeFirstStopped.Load() {
		t.Error("connectLoop() started replacement attempt before discarded connection finished stopping")
	}
}

type outboundContactObserver struct {
	stats.NoopCollector
	onSuccess func()
}

func (o *outboundContactObserver) OutboundContactData(_ string, _ int, err error) {
	if err == nil && o.onSuccess != nil {
		o.onSuccess()
	}
}

// TestStreamingResetDuringConnect checks that Reset abandons a connection
// attempt that the server has stalled, so that the Flush that follows
// completes on a new connection.
func TestStreamingResetDuringConnect(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// headersBeforeStall is whether the server sends the response headers
		// before it stalls. If it does, the client is reading the initial
		// ContactData when Reset is called; if not, it is waiting for the
		// response.
		headersBeforeStall bool
	}{
		{name: "StalledBeforeResponseHeaders"},
		{name: "StalledAfterResponseHeaders", headersBeforeStall: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			testStreamingResetDuringConnect(t, tc.headersBeforeStall)
		})
	}
}

func testStreamingResetDuringConnect(t *testing.T, headersBeforeStall bool) {
	pemCert, pemKey, err := comtesting.ServerCert()
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

	var reqCount atomic.Int32
	req1Stalled := make(chan struct{})
	req1Aborted := make(chan struct{})
	testDone := make(chan struct{})

	handler := http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		reqNum := reqCount.Add(1)
		body := bufio.NewReader(req.Body)
		b := make([]byte, 4)
		if _, err := io.ReadAtLeast(body, b, 4); err != nil {
			return
		}
		size, err := binary.ReadUvarint(body)
		if err != nil {
			return
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(body, buf); err != nil {
			return
		}

		if reqNum == 1 {
			defer close(req1Aborted)
			if headersBeforeStall {
				res.WriteHeader(http.StatusOK)
				res.(http.Flusher).Flush()
			}
			close(req1Stalled)
			// Reading from body blocks until Reset closes the client stream.
			_, _ = io.Copy(io.Discard, body)
			return
		}

		outBuf, err := proto.Marshal(&fspb.ContactData{AckIndex: 0})
		if err != nil {
			t.Errorf("Unable to encode response: %v", err)
			return
		}
		sizeBuf := binary.AppendUvarint(nil, uint64(len(outBuf)))
		if _, err := res.Write(sizeBuf); err != nil {
			return
		}
		if _, err := res.Write(outBuf); err != nil {
			return
		}
		res.(http.Flusher).Flush()
		select {
		case <-req.Context().Done():
		case <-testDone:
		}
	})

	mux := http.NewServeMux()
	mux.Handle("/streaming-message", &https.CompressionHandler{Wrapped: handler})
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
	defer server.Close()
	defer close(testDone)

	var c StreamingCommunicator
	conf := config.Configuration{
		Servers:       []string{addr},
		TrustedCerts:  x509.NewCertPool(),
		FixedServices: []*fspb.ClientServiceConfig{{Name: "NOOPService", Factory: "NOOP"}},
		CommunicatorConfig: &clpb.CommunicatorConfig{
			MaxPollDelaySeconds:    2,
			MaxBufferDelaySeconds:  1,
			MinFailureDelaySeconds: 1,
			Compression:            fspb.CompressionAlgorithm_COMPRESSION_DEFLATE,
		},
	}
	if !conf.TrustedCerts.AppendCertsFromPEM(pemCert) {
		t.Fatal("AppendCertsFromPEM() = false, want true")
	}
	headersReceived := make(chan struct{})
	var headersOnce sync.Once
	cl, err := client.New(
		conf,
		client.Components{
			ServiceFactories: map[string]service.Factory{"NOOP": service.NOOPFactory},
			Communicator:     &c,
			Stats: &outboundContactObserver{
				onSuccess: func() {
					headersOnce.Do(func() { close(headersReceived) })
				},
			},
		})
	if err != nil {
		t.Fatalf("client.New() failed: %v", err)
	}
	defer cl.Stop()

	select {
	case <-req1Stalled:
	case <-time.After(10 * time.Second):
		t.Fatal("Timed out waiting for the server to stall the first connection attempt")
	}
	if headersBeforeStall {
		select {
		case <-headersReceived:
		case <-time.After(10 * time.Second):
			t.Fatal("Timed out waiting for the client to receive the stalled response headers")
		}
	}

	c.Reset()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := c.Flush(ctx); err != nil {
		t.Fatalf("Flush after Reset = %v, want nil", err)
	}
	if got := reqCount.Load(); got < 2 {
		t.Errorf("Server received %d requests before Flush returned, want at least 2", got)
	}
	select {
	case <-req1Aborted:
	case <-time.After(5 * time.Second):
		t.Error("Timed out waiting for Reset to abort the stalled first request")
	}
}
