package https

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/google/fleetspeak/fleetspeak/src/client"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

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

func TestStreamingCommunicator(t *testing.T) {
	// Create a local https server for the client to talk to.
	pemCert, pemKey, err := comtesting.ServerCert()
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

	// Dummy server just puts the ContactData records that we receive into a
	// channel, blindly returning responses.
	mux := http.NewServeMux()
	received := make(chan *fspb.ContactData, 5)
	toSend := make(chan *fspb.ContactData, 5)
	var rc int32
	mux.HandleFunc("/streaming-message", func(res http.ResponseWriter, req *http.Request) {
		cid, err := common.MakeClientID(req.TLS.PeerCertificates[0].PublicKey)
		if err != nil {
			t.Errorf("unable to make ClientID in test server: %v", err)
		}
		if reqs := atomic.AddInt32(&rc, 1); reqs != 1 {
			t.Errorf("Only expected 1 request, but this is request %d", reqs)
			http.Error(res, "only expected 1 request", http.StatusBadRequest)
		}
		body := bufio.NewReader(req.Body)
		b := make([]byte, 4)
		if _, err := io.ReadAtLeast(body, b, 4); err != nil {
			t.Errorf("Error reading magic number: %v", err)
			http.Error(res, "unable to read magic number", http.StatusBadRequest)
			return
		}
		m := binary.LittleEndian.Uint32(b)
		if m != magic {
			t.Errorf("Unexpected magic number, got %x expected %x", m, magic)
			http.Error(res, "bad magic number", http.StatusBadRequest)
			return
		}

		cnt := uint64(0)
		for {
			log.Error("starting read")
			size, err := binary.ReadUvarint(body)
			if err != nil {
				if err != io.EOF && err != io.ErrUnexpectedEOF {
					t.Errorf("Unable to read size: %v", err)
				}
				return
			}
			buf := make([]byte, size)
			_, err = io.ReadFull(body, buf)
			if err != nil {
				t.Errorf("Unable to read incoming messages: %v", err)
				return
			}
			var wcd fspb.WrappedContactData
			if err := proto.Unmarshal(buf, &wcd); err != nil {
				t.Errorf("Unable to parse incoming messages: %v", err)
				return
			}
			var rcd fspb.ContactData
			if err := proto.Unmarshal(wcd.ContactData, &rcd); err != nil {
				t.Errorf("Unable to parse ContactData: %v", err)
				return
			}
			log.Errorf("Read: %v", rcd)
			received <- &rcd

			cd := fspb.ContactData{
				AckIndex: cnt,
			}
			cnt++
			out := proto.NewBuffer(make([]byte, 0, 1024))
			if err := out.EncodeMessage(&cd); err != nil {
				t.Errorf("Unable to encode response: %v", err)
				return
			}
			if _, err := res.Write(out.Bytes()); err != nil {
				t.Errorf("Unable to write response: %v", err)
				return
			}
			res.(http.Flusher).Flush()
		F:
			for {
				select {
				case cd := <-toSend:
					for _, m := range cd.Messages {
						m.Destination.ClientId = cid.Bytes()
					}
					if err := out.EncodeMessage(cd); err != nil {
						t.Errorf("Unable to encode response: %v", err)
						return
					}
					if _, err := res.Write(out.Bytes()); err != nil {
						t.Errorf("Unable to write response: %v", err)
						return
					}
					res.(http.Flusher).Flush()
				default:
					break F
				}
			}
		}
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

	var c StreamingCommunicator
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
	if !conf.TrustedCerts.AppendCertsFromPEM(pemCert) {
		t.Fatal("unable to add server cert to pool")
	}
	cl, err := client.New(
		conf,
		client.Components{
			ServiceFactories: map[string]service.Factory{"NOOP": service.NOOPFactory},
			Communicator:     &c})

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

	toSend <- &fspb.ContactData{
		SequencingNonce: 44,
		Messages: []*fspb.Message{
			{Destination: &fspb.Address{ServiceName: "NOOPService"}, MessageType: "TestMessage"},
			{Destination: &fspb.Address{ServiceName: "NOOPService"}, MessageType: "TestMessage"},
			{Destination: &fspb.Address{ServiceName: "NOOPService"}, MessageType: "TestMessage"},
			{Destination: &fspb.Address{ServiceName: "NOOPService"}, MessageType: "TestMessage"},
			{Destination: &fspb.Address{ServiceName: "NOOPService"}, MessageType: "TestMessage"},
		},
	}
	// Send messages through until we get a contact datas giving 5 more capacity.
	var granted uint64
F:
	for {
		if err := cl.ProcessMessage(context.Background(),
			service.AckMessage{
				M: &fspb.Message{
					Destination: &fspb.Address{ServiceName: "DummyService"}},
			}); err != nil {
			t.Fatalf("unable to hand message to client: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	G:
		for {
			select {
			case rcb := <-received:
				log.Errorf("Received: %v", rcb)
				granted += rcb.AllowedMessages["NOOPService"]
				log.Errorf("Recieved AllowedMessages: %v", rcb.AllowedMessages)
				if granted >= 5 {
					break F
				}
			default:
				break G
			}
		}
	}
	if granted != 5 {
		t.Errorf("Only expected extra capacity, but got %d", granted)
	}
	tl.Close()
	cl.Stop()
}
