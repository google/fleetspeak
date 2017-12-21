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

package frr

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"reflect"
	"sort"
	"testing"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"google.golang.org/grpc"

	cservice "github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	fgrpc "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	fpb "github.com/google/fleetspeak/fleetspeak/src/inttesting/frr/proto/fleetspeak_frr"
	srpb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

type fakeClientServiceContext struct {
	cservice.Context
	o chan *fspb.Message
}

func (f fakeClientServiceContext) Send(ctx context.Context, m cservice.AckMessage) error {
	select {
	case f.o <- m.M:
		if m.Ack != nil {
			m.Ack()
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f fakeClientServiceContext) GetLocalInfo() *cservice.LocalInfo {
	id, err := common.StringToClientID("0000000000000000")
	if err != nil {
		log.Fatal(err)
	}
	return &cservice.LocalInfo{
		ClientID: id,
	}
}

func (f fakeClientServiceContext) GetFileIfModified(ctx context.Context, name string, modSince time.Time) (io.ReadCloser, time.Time, error) {
	if name == "TestFile" {
		return ioutil.NopCloser(bytes.NewReader([]byte("Test file data."))), db.Now(), nil
	}
	return nil, time.Time{}, fmt.Errorf("File not found: %v", name)
}

func clearChannel(c chan *fspb.Message) {
	for {
		select {
		case <-c:
			continue
		default:
			return
		}
	}
}

func TestClientService(t *testing.T) {
	cs, err := ClientServiceFactory(nil)
	defer cs.Stop()
	if err != nil {
		t.Fatalf("Unable to create service: %v", err)
	}

	out := make(chan *fspb.Message, 10)
	if err := cs.Start(&fakeClientServiceContext{o: out}); err != nil {
		t.Fatalf("Unable to start service: %v", err)
	}

	for _, tc := range []struct {
		rd        fpb.TrafficRequestData
		wantCount int
		wantSize  int
	}{
		{
			rd:        fpb.TrafficRequestData{RequestId: 0},
			wantCount: 1,
			wantSize:  1024,
		},
		{
			rd:        fpb.TrafficRequestData{RequestId: 1, NumMessages: 5},
			wantCount: 5,
			wantSize:  1024,
		},
	} {
		m := fspb.Message{
			MessageType: "TrafficRequest",
		}
		m.Data, err = ptypes.MarshalAny(&tc.rd)
		if err != nil {
			t.Errorf("unable to marshal TrafficRequestData: %v", err)
			continue
		}
		if err := cs.ProcessMessage(context.Background(), &m); err != nil {
			t.Errorf("unable to process message [%v]: %v", tc.rd, err)
			continue
		}
		for i := 0; i < tc.wantCount; i++ {
			res := <-out
			var d fpb.TrafficResponseData
			if err := ptypes.UnmarshalAny(res.Data, &d); err != nil {
				t.Errorf("unable to unmarshal data")
				clearChannel(out)
				break
			}
			if d.RequestId != tc.rd.RequestId {
				t.Errorf("expected response for request %v got response for request %v", tc.rd.RequestId, d.RequestId)
				clearChannel(out)
				break
			}
			if len(d.Data) != tc.wantSize {
				t.Errorf("wanted data size of %v got size of %v", tc.wantSize, len(d.Data))
				clearChannel(out)
				break
			}
			// Last message should have the end marker.
			wantFin := i == tc.wantCount-1
			if d.Fin != wantFin {
				t.Errorf("wanted Fin: %v got Fin: %v", wantFin, d.Fin)
				clearChannel(out)
				break
			}
		}
	}

	frd := fpb.FileRequestData{MasterId: 42, Name: "TestFile"}
	d, err := ptypes.MarshalAny(&frd)
	if err != nil {
		t.Fatalf("Unable to marshal FileRequestData: %v", err)
	}
	if err := cs.ProcessMessage(context.Background(), &fspb.Message{
		MessageType: "FileRequest",
		Data:        d,
	}); err != nil {
		t.Fatalf("Unable to process FileRequest: %v", err)
	}
	res := <-out
	var got fpb.FileResponseData
	if err := ptypes.UnmarshalAny(res.Data, &got); err != nil {
		t.Errorf("Unable unmarshal FileResponse: %v", err)
	}
	want := &fpb.FileResponseData{
		MasterId: 42,
		Name:     "TestFile",
		Size:     15,
	}
	if !proto.Equal(want, &got) {
		t.Errorf("Unexpected FileResponse, want: %v, got %v", want, &got)
	}
}

func TestClientServiceEarlyShutdown(t *testing.T) {
	cs, err := ClientServiceFactory(nil)
	if err != nil {
		t.Fatalf("Unable to create service: %v", err)
	}
	// no buffering so Send will block.
	out := make(chan *fspb.Message)
	if err := cs.Start(&fakeClientServiceContext{o: out}); err != nil {
		t.Fatalf("Unable to start service: %v", err)
	}

	m := fspb.Message{
		MessageType: "TrafficRequest",
	}
	m.Data, err = ptypes.MarshalAny(&fpb.TrafficRequestData{RequestId: 1, NumMessages: 5})
	if err != nil {
		t.Fatalf("unable to marshal TrafficRequestData: %v", err)
	}
	if err := cs.ProcessMessage(context.Background(), &m); err != nil {
		t.Error("unable to process message")
	}

	// Service should be blocked trying to put a message into out. However, Stop should
	// shut everything down and return quickly.
	cs.Stop()
}

type fakeMasterServer struct {
	rec     chan<- *fpb.MessageInfo
	recFile chan<- *fpb.FileResponseInfo
}

func (s fakeMasterServer) RecordTrafficResponse(ctx context.Context, i *fpb.MessageInfo) (*fspb.EmptyMessage, error) {
	log.Infof("recording m: %v", i)
	s.rec <- i
	return &fspb.EmptyMessage{}, nil
}

func (s fakeMasterServer) RecordFileResponse(ctx context.Context, i *fpb.FileResponseInfo) (*fspb.EmptyMessage, error) {
	log.Infof("recording m: %v", i)
	s.recFile <- i
	return &fspb.EmptyMessage{}, nil
}

type fakeServiceContext struct {
	service.Context
}

func TestServerService(t *testing.T) {
	// A channel to collect messages received by the fake master server.
	rec := make(chan *fpb.MessageInfo, 10)

	// Create a fake master server as a real local grpc service.
	s := grpc.NewServer()
	fgrpc.RegisterMasterServer(s, fakeMasterServer{rec: rec})
	ad, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	tl, err := net.ListenTCP("tcp", ad)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()
	go func() {
		log.Infof("Finished with: %v", s.Serve(tl))
	}()

	// Directly create and start a FRR service.
	c := fpb.Config{MasterServer: tl.Addr().String()}
	a, err := ptypes.MarshalAny(&c)
	if err != nil {
		t.Fatal(err)
	}
	se, err := ServerServiceFactory(&srpb.ServiceConfig{Config: a})
	if err != nil {
		t.Fatal(err)
	}
	if err := se.Start(&fakeServiceContext{}); err != nil {
		t.Fatal(err)
	}
	defer se.Stop()

	// Build a message.
	rd := fpb.TrafficResponseData{
		RequestId:     42,
		ResponseIndex: 24,
		Data:          []byte("asdf"),
		Fin:           true,
	}
	d, err := ptypes.MarshalAny(&rd)
	if err != nil {
		t.Fatal(err)
	}
	id, err := common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 1})
	if err != nil {
		t.Fatal(err)
	}

	// Process it.
	if err := se.ProcessMessage(context.Background(), &fspb.Message{
		Source: &fspb.Address{
			ClientId:    id.Bytes(),
			ServiceName: "FRR",
		},
		Destination: &fspb.Address{
			ServiceName: "FRR",
		},
		MessageType: "TrafficResponse",
		Data:        d,
	}); err != nil {
		t.Fatal(err)
	}

	// Check that it was received.
	mi := <-rec
	if !bytes.Equal(mi.ClientId, id.Bytes()) {
		t.Errorf("Unexpected client id, got [%v], want [%v]", hex.EncodeToString(mi.ClientId), hex.EncodeToString(id.Bytes()))
	}

	rd.Data = nil
	if !proto.Equal(mi.Data, &rd) {
		t.Errorf("Unexpected TrafficRequestData, got [%v], want [%v]", mi.Data, rd)
	}
}

type Int64Slice []int64

func (p Int64Slice) Len() int           { return len(p) }
func (p Int64Slice) Less(i, j int) bool { return p[i] < p[j] }
func (p Int64Slice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

func TestMasterServer(t *testing.T) {
	ctx := context.Background()
	ms := NewMasterServer(nil)
	ch := ms.WatchCompleted()

	id, err := common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 1})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		mi        fpb.MessageInfo
		completed bool
	}{
		{
			mi: fpb.MessageInfo{
				ClientId: id.Bytes(),
				Data: &fpb.TrafficResponseData{
					MasterId:      ms.masterID,
					RequestId:     0,
					ResponseIndex: 0,
					Fin:           true,
				},
			},
			completed: true,
		},
		{
			mi: fpb.MessageInfo{
				ClientId: id.Bytes(),
				Data: &fpb.TrafficResponseData{
					MasterId:      ms.masterID,
					RequestId:     1,
					ResponseIndex: 0,
					Fin:           false,
				},
			},
			completed: false,
		},
		{
			mi: fpb.MessageInfo{
				ClientId: id.Bytes(),
				Data: &fpb.TrafficResponseData{
					MasterId:      ms.masterID,
					RequestId:     1,
					ResponseIndex: 1,
					Fin:           true,
				},
			},
			completed: true,
		},
		{
			mi: fpb.MessageInfo{
				ClientId: id.Bytes(),
				Data: &fpb.TrafficResponseData{
					MasterId:      ms.masterID,
					RequestId:     2,
					ResponseIndex: 1,
					Fin:           true,
				},
			},
			completed: false,
		},
		{
			mi: fpb.MessageInfo{
				ClientId: id.Bytes(),
				Data: &fpb.TrafficResponseData{
					MasterId:      ms.masterID,
					RequestId:     2,
					ResponseIndex: 0,
					Fin:           false,
				},
			},
			completed: true,
		},
		{
			mi: fpb.MessageInfo{
				ClientId: id.Bytes(),
				Data: &fpb.TrafficResponseData{
					MasterId:      ms.masterID,
					RequestId:     3,
					ResponseIndex: 3,
					Fin:           true,
				},
			},
			completed: false,
		},
		{
			mi: fpb.MessageInfo{
				ClientId: id.Bytes(),
				Data: &fpb.TrafficResponseData{
					MasterId:      ms.masterID,
					RequestId:     3,
					ResponseIndex: 2,
					Fin:           false,
				},
			},
			completed: false,
		},
		{
			mi: fpb.MessageInfo{
				ClientId: id.Bytes(),
				Data: &fpb.TrafficResponseData{
					MasterId:      ms.masterID,
					RequestId:     3,
					ResponseIndex: 2,
					Fin:           false,
				},
			},
			completed: false,
		},
		{
			mi: fpb.MessageInfo{
				ClientId: id.Bytes(),
				Data: &fpb.TrafficResponseData{
					MasterId:      ms.masterID,
					RequestId:     3,
					ResponseIndex: 1,
					Fin:           false,
				},
			},
			completed: false,
		},
		{
			mi: fpb.MessageInfo{
				ClientId: id.Bytes(),
				Data: &fpb.TrafficResponseData{
					MasterId:      ms.masterID,
					RequestId:     3,
					ResponseIndex: 0,
					Fin:           false,
				},
			},
			completed: true,
		},
		{
			mi: fpb.MessageInfo{
				ClientId: id.Bytes(),
				Data: &fpb.TrafficResponseData{
					MasterId:      ms.masterID,
					RequestId:     4,
					ResponseIndex: 4,
					Fin:           true,
				},
			},
			completed: false,
		},
	} {
		if _, err := ms.RecordTrafficResponse(ctx, &tc.mi); err != nil {
			t.Errorf("Unexpected error recording message [%v]: %v", tc.mi, err)
		}
		var gotComp bool
		select {
		case <-ch:
			gotComp = true
		default:
			gotComp = false
		}
		if tc.completed != gotComp {
			t.Errorf("For request %v, index %v, got completed=%v want %v", tc.mi.Data.RequestId, tc.mi.Data.ResponseIndex, gotComp, tc.completed)
		}
	}

	c := ms.CompletedRequests(id)
	sort.Sort(Int64Slice(c))
	wantCompleted := []int64{0, 1, 2, 3}
	if !reflect.DeepEqual(c, wantCompleted) {
		t.Errorf("Unexpected completed requests list: got %v, want %v", c, wantCompleted)
	}

	c = ms.AllRequests(id)
	sort.Sort(Int64Slice(c))
	wantAll := []int64{0, 1, 2, 3, 4}
	if !reflect.DeepEqual(c, wantAll) {
		t.Errorf("Unexpected all requests list: got %v, want %v", c, wantAll)
	}

}
