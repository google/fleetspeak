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

package servertests_test

import (
	"bytes"
	"context"
	"sort"
	"testing"

	"github.com/golang/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/admin"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/ids"
	"github.com/google/fleetspeak/fleetspeak/src/server/testserver"

	tpb "github.com/golang/protobuf/ptypes/timestamp"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

func TestBroadcastsAPI(t *testing.T) {
	ctx := context.Background()

	ts := testserver.Make(t, "server", "AdminServer", nil)
	defer ts.S.Stop()

	as := admin.NewServer(ts.DS)

	bid, err := ids.BytesToBroadcastID([]byte{0, 0, 0, 0, 0, 0, 0, 1})
	if err != nil {
		t.Fatal(err)
	}

	br := spb.Broadcast{
		BroadcastId: bid.Bytes(),
		Source: &fspb.Address{
			ServiceName: "TestService",
		},
		MessageType: "TestMessage",
	}

	if _, err := as.CreateBroadcast(ctx, &spb.CreateBroadcastRequest{Broadcast: &br, Limit: 100}); err != nil {
		t.Errorf("CreateBroadcast returned error: %v", err)
	}

	res, err := as.ListActiveBroadcasts(ctx, &spb.ListActiveBroadcastsRequest{ServiceName: "TestService"})
	if err != nil {
		t.Errorf("ListActiveBroadcasts returned error: %v", err)
	}

	wantResp := &spb.ListActiveBroadcastsResponse{Broadcasts: []*spb.Broadcast{&br}}
	if !proto.Equal(res, wantResp) {
		t.Errorf("ListActiveBroadcasts error: want [%v] got [%v]", wantResp, res)
	}
}

func TestMessageStatusAPI(t *testing.T) {
	ctx := context.Background()

	ts := testserver.Make(t, "server", "AdminServer", nil)
	defer ts.S.Stop()

	as := admin.NewServer(ts.DS)

	// A byte slice representing a message id, with 32 zeros.
	bmid0 := make([]byte, 32)

	gmsRes, err := as.GetMessageStatus(ctx, &spb.GetMessageStatusRequest{
		MessageId: bmid0,
	})
	if err != nil {
		t.Errorf("GetMessageStatus returned an error: %v", err)
	}

	gmsWant := &spb.GetMessageStatusResponse{}
	if !proto.Equal(gmsRes, gmsWant) {
		t.Errorf("GetMessageStatus error: want [%v] got [%v]", gmsWant, gmsRes)
	}

	addr := &fspb.Address{
		ServiceName: "TestService",
	}

	m := &fspb.Message{
		MessageId:    bmid0,
		Source:       addr,
		Destination:  addr,
		CreationTime: &tpb.Timestamp{Seconds: 42},
	}

	if err := ts.DS.StoreMessages(ctx, []*fspb.Message{m}, ""); err != nil {
		t.Errorf("StoreMessage (Message: [%v]) error: %v", m, err)
	}

	gmsRes, err = as.GetMessageStatus(ctx, &spb.GetMessageStatusRequest{
		MessageId: bmid0,
	})
	if err != nil {
		t.Errorf("GetMessageStatus returned an error: %v", err)
	}

	gmsWant = &spb.GetMessageStatusResponse{
		CreationTime: &tpb.Timestamp{Seconds: 42},
	}
	if !proto.Equal(gmsRes, gmsWant) {
		t.Errorf("GetMessageStatus error: want [%v] got [%v]", gmsWant, gmsRes)
	}
}

func TestListClientsAPI(t *testing.T) {
	ctx := context.Background()

	ts := testserver.Make(t, "server", "AdminServer", nil)
	defer ts.S.Stop()

	as := admin.NewServer(ts.DS)

	id0 := []byte{0, 0, 0, 0, 0, 0, 0, 0}
	cid0, err := common.BytesToClientID(id0)
	if err != nil {
		t.Errorf("BytesToClientID(%v) = %v, want nil", cid0, err)
	}

	if err = ts.DS.AddClient(ctx, cid0, &db.ClientData{}); err != nil {
		t.Fatalf("AddClient returned an error: %v", err)
	}

	id1 := []byte{0, 0, 0, 0, 0, 0, 0, 1}
	cid1, err := common.BytesToClientID(id1)
	if err != nil {
		t.Errorf("BytesToClientID(%v) = %v, want nil", cid1, err)
	}

	lab1 := []*fspb.Label{
		{
			ServiceName: "BarService",
			Label:       "BarLabel",
		},
		{
			ServiceName: "FooService",
			Label:       "FooLabel",
		},
	}

	if err = ts.DS.AddClient(ctx, cid1, &db.ClientData{
		Labels: lab1,
	}); err != nil {
		t.Errorf("AddClient returned an error: %v", err)
	}

	lcRes, err := as.ListClients(ctx, &spb.ListClientsRequest{})
	if err != nil {
		t.Errorf("ListClients returned an error: %v", err)
	}

	lcWant := &spb.ListClientsResponse{
		Clients: []*spb.Client{
			{
				ClientId: id0,
			},
			{
				ClientId: id1,
				Labels:   lab1,
			},
		},
	}

	// The result's order is arbitrary, so let's sort it.
	sort.Slice(lcRes.Clients, func(i, j int) bool {
		return bytes.Compare(lcRes.Clients[i].ClientId, lcRes.Clients[j].ClientId) < 0
	})

	for _, c := range lcRes.Clients {
		if c.LastContactTime == nil {
			t.Errorf("ListClients error: LastSeenTimestamp is nil")
		}
		c.LastContactTime = nil
	}

	if !proto.Equal(lcRes, lcWant) {
		t.Errorf("ListClients error: want [%v], got [%v]", lcWant, lcRes)
	}
}

func TestInsertMessageAPI(t *testing.T) {
	id, err := common.StringToClientID("0000000000000042")
	if err != nil {
		t.Fatalf("Unable to create client id: %v", err)
	}
	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
	}
	ctx := context.Background()

	ts := testserver.Make(t, "server", "AdminServer", nil)
	defer ts.S.Stop()

	as := admin.NewServer(ts.DS)

	m := fspb.Message{
		MessageId:    mid.Bytes(),
		Source:       &fspb.Address{ServiceName: "TestService"},
		Destination:  &fspb.Address{ServiceName: "TestService", ClientId: id.Bytes()},
		MessageType:  "DummyType",
		CreationTime: db.NowProto(),
	}

	if _, err := as.InsertMessage(ctx, proto.Clone(&m).(*fspb.Message)); err != nil {
		t.Fatalf("InsertMessage returned error: %v", err)
	}
	// m should now be available for processing:
	msgs, err := ts.DS.ClientMessagesForProcessing(ctx, id, 10)
	if err != nil {
		t.Fatalf("ClientMessagesForProcessing(%v) returned error: %v", id, err)
	}
	if len(msgs) != 1 || !proto.Equal(msgs[0], &m) {
		t.Errorf("ClientMessagesForProcessing(%v) returned unexpected value, got: %v, want [%v]", id, msgs, m)
	}
}
