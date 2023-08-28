package https

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
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
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"

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
	t        *testing.T
	received chan<- *fspb.ContactData
	toSend   <-chan *fspb.ContactData

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

	mux := http.NewServeMux()

	mux.HandleFunc("/streaming-message", func(res http.ResponseWriter, req *http.Request) {
		cid, err := common.MakeClientID(req.TLS.PeerCertificates[0].PublicKey)
		if err != nil {
			s.t.Errorf("unable to make ClientID in test server: %v", err)
		}
		if reqs := atomic.AddInt32(&s.rc, 1); reqs != 1 {
			s.t.Errorf("Only expected 1 request, but this is request %d", reqs)
			http.Error(res, "only expected 1 request", http.StatusBadRequest)
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
	})

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

func startStreamingClient(t *testing.T, addr string, cert []byte, out *rate.Limiter) *client.Client {
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
	received := make(chan *fspb.ContactData, 5)
	toSend := make(chan *fspb.ContactData, 5)

	server := streamingTestServer{
		t:        t,
		received: received,
		toSend:   toSend,
	}
	server.Start()
	defer server.Stop()

	cl := startStreamingClient(t, server.Addr(), server.pemCert, nil)
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
	for i := 0; i < 10; i++ {
		j := i
		if err := cl.ProcessMessage(context.Background(),
			service.AckMessage{
				M: &fspb.Message{
					Destination: &fspb.Address{ServiceName: "DummyService"}},
				Ack: func() { acks <- j },
			},
		); err != nil {
			t.Fatalf("unable to hand message to client: %v", err)
		}

	}

	rcb := <-received
	if len(rcb.Messages) != 10 {
		t.Errorf("Expected a ContactData with 10 records, got %d", len(rcb.Messages))
	}
	for i := 0; i < 10; i++ {
		j := <-acks
		if j != i {
			t.Errorf("Expected ack for %d, got ack for %d", i, j)
		}
	}

	scb := fspb.ContactData{
		SequencingNonce: 44,
	}
	for i := 0; i < 35; i++ {
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
	cl := startStreamingClient(t, server.Addr(), server.pemCert, rate.NewLimiter(100*1024, 10*1024))
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
	for i := 0; i < 6; i++ {
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
	cl := startStreamingClient(t, server.Addr(), server.pemCert, rate.NewLimiter(5*1024*1024, 10*1024))
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
	for i := 0; i < 10; i++ {
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
