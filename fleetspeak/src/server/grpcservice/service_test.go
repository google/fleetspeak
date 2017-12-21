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

package grpcservice

import (
	"context"
	"net"
	"testing"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"

	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	ggrpc "github.com/google/fleetspeak/fleetspeak/src/server/grpcservice/proto/fleetspeak_grpcservice"
	gpb "github.com/google/fleetspeak/fleetspeak/src/server/grpcservice/proto/fleetspeak_grpcservice"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

type trivialProcessorServer struct {
	msgs chan *fspb.Message
}

func (s *trivialProcessorServer) Process(ctx context.Context, m *fspb.Message) (*fspb.EmptyMessage, error) {
	s.msgs <- m
	return &fspb.EmptyMessage{}, nil
}

func startServer() (msgs <-chan *fspb.Message, addr string, fin func()) {
	s := trivialProcessorServer{
		msgs: make(chan *fspb.Message, 1),
	}
	l, err := net.Listen("tcp", "localhost:")
	if err != nil {
		log.Fatal(err)
	}

	gs := grpc.NewServer()
	ggrpc.RegisterProcessorServer(gs, &s)
	go gs.Serve(l)

	return s.msgs, l.Addr().String(), gs.Stop
}

func TestNewUpdate(t *testing.T) {
	msgs1, addr1, fin1 := startServer()
	defer fin1()

	con1, err := grpc.DialContext(context.Background(), addr1, grpc.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	svc := NewGRPCService(con1)
	sctx := sertesting.FakeContext{}
	svc.Start(&sctx)
	defer svc.Stop()

	if err := svc.ProcessMessage(context.Background(), &fspb.Message{
		MessageType: "Test Message 1",
	}); err != nil {
		t.Errorf("ProcessMessages returned error: %v", err)
	}
	m := <-msgs1
	if m.MessageType != "Test Message 1" {
		t.Errorf("Expected Test Message 1, got: %v", m)
	}

	// setting a nil should cause temp errors
	svc.Update(nil)
	if err := svc.ProcessMessage(context.Background(), &fspb.Message{
		MessageType: "Test Message 2",
	}); err == nil || !service.IsTemporary(err) {
		t.Errorf("Expected TemporaryError from ProcessMessages, got: %v", err)
	}

	msgs2, addr2, fin2 := startServer()
	defer fin2()
	con2, err := grpc.DialContext(context.Background(), addr2, grpc.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	svc.Update(con2)

	if err := svc.ProcessMessage(context.Background(), &fspb.Message{
		MessageType: "Test Message 3",
	}); err != nil {
		t.Errorf("ProcessMessages returned error: %v", err)
	}
	m = <-msgs2
	if m.MessageType != "Test Message 3" {
		t.Errorf("Expected Test Message 3, got: %v", m)
	}

	// Should be fine to Stop when con is nil.
	svc.Update(nil)
}

func TestFactory(t *testing.T) {
	msgs, addr, fin := startServer()
	defer fin()

	cfg, err := ptypes.MarshalAny(&gpb.Config{
		Target:   addr,
		Insecure: true,
	})
	if err != nil {
		t.Fatalf("Unable to marshal config: %v", err)
	}

	svc, err := Factory(&spb.ServiceConfig{
		Config: cfg,
	})
	if err != nil {
		t.Fatalf("Unable to create service: %v", err)
	}

	sctx := sertesting.FakeContext{}
	svc.Start(&sctx)

	if err := svc.ProcessMessage(context.Background(), &fspb.Message{
		MessageType: "Test Message",
	}); err != nil {
		t.Errorf("ProcessMessages returned error: %v", err)
	}
	svc.Stop()

	m := <-msgs
	if m.MessageType != "Test Message" {
		t.Errorf("Expected Test Message, got: %v", m)
	}
}
