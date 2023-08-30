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
	"reflect"
	"sort"
	"strings"
	"testing"

	"google.golang.org/grpc"

	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/admin"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/ids"
	"github.com/google/fleetspeak/fleetspeak/src/server/testserver"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	anypb "google.golang.org/protobuf/types/known/anypb"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
)

func TestBroadcastsAPI(t *testing.T) {
	ctx := context.Background()

	ts := testserver.Make(t, "server", "AdminServer", nil)
	defer ts.S.Stop()

	as := admin.NewServer(ts.DS, nil)

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

	as := admin.NewServer(ts.DS, nil)

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
		CreationTime: &tspb.Timestamp{Seconds: 42},
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
		CreationTime: &tspb.Timestamp{Seconds: 42},
	}
	if !proto.Equal(gmsRes, gmsWant) {
		t.Errorf("GetMessageStatus error: want [%v] got [%v]", gmsWant, gmsRes)
	}
}

type mockStreamClientIdsServer struct {
	grpc.ServerStream
	responses []*spb.StreamClientIdsResponse
}

func (m *mockStreamClientIdsServer) Send(response *spb.StreamClientIdsResponse) error {
	m.responses = append(m.responses, response)
	return nil
}

func (m *mockStreamClientIdsServer) Context() context.Context {
	return context.Background()
}

func TestListClientsAPI(t *testing.T) {
	ctx := context.Background()

	ts := testserver.Make(t, "server", "AdminServer", nil)
	defer ts.S.Stop()

	as := admin.NewServer(ts.DS, nil)

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

	t.Run("ListClients", func(t *testing.T) {
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
	})

	t.Run("StreamClientIds", func(t *testing.T) {
		var m mockStreamClientIdsServer
		req := &spb.StreamClientIdsRequest{}
		err := as.StreamClientIds(req, &m)
		if err != nil {
			t.Errorf("StreamClientIds returned an error: %v", err)
		}
		var ids [][]byte
		for _, response := range m.responses {
			ids = append(ids, response.ClientId)
		}
		sort.Slice(ids, func(i, j int) bool {
			return bytes.Compare(ids[i], ids[j]) < 0
		})
		expected := [][]byte{id0, id1}
		if !reflect.DeepEqual(ids, expected) {
			t.Errorf("StreamClientIds error: want [%v], got [%v].", expected, ids)
		}
	})
}

func TestInsertMessageAPI(t *testing.T) {
	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
	}
	ctx := context.Background()

	ts := testserver.Make(t, "server", "AdminServer", nil)
	defer ts.S.Stop()

	key, err := ts.AddClient()
	if err != nil {
		t.Fatalf("Unable to add client: %v", err)
	}
	id, err := common.MakeClientID(key)
	if err != nil {
		t.Fatalf("Unable to make ClientID: %v", err)
	}

	as := admin.NewServer(ts.DS, nil)

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
	msgs, err := ts.DS.ClientMessagesForProcessing(ctx, id, 10, nil)
	if err != nil {
		t.Fatalf("ClientMessagesForProcessing(%v) returned error: %v", id, err)
	}
	if len(msgs) != 1 || !proto.Equal(msgs[0], &m) {
		t.Errorf("ClientMessagesForProcessing(%v) returned unexpected value, got: %v, want [%v]", id, msgs, m.String())
	}
}

func TestInsertMessageAPI_LargeMessages(t *testing.T) {
	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
	}
	ctx := context.Background()

	server := testserver.Make(t, "server", "AdminServer", nil)
	defer server.S.Stop()

	key, err := server.AddClient()
	if err != nil {
		t.Fatalf("Unable to add client: %v", err)
	}
	id, err := common.MakeClientID(key)
	if err != nil {
		t.Fatalf("Unable to make ClientID: %v", err)
	}

	adminServer := admin.NewServer(server.DS, nil)

	dummyProto, err := anypb.New(&fspb.Signature{
		Signature: bytes.Repeat([]byte{0xa}, 2<<20+1),
	})
	if err != nil {
		t.Fatalf("Unable to marshal dummy proto: %v", err)
	}

	msg := fspb.Message{
		MessageId:    mid.Bytes(),
		Source:       &fspb.Address{ServiceName: "TestService"},
		Destination:  &fspb.Address{ServiceName: "TestService", ClientId: id.Bytes()},
		MessageType:  "DummyType",
		CreationTime: db.NowProto(),
		Data:         dummyProto,
	}

	if _, err := adminServer.InsertMessage(ctx, &msg); err == nil {
		t.Fatal("Expected InsertMessage to return an error.")
	} else if !strings.Contains(err.Error(), "exceeds the 2097152-byte limit") {
		t.Errorf("Unexpected error: [%v].", err)
	}
}

func TestPendingMessages(t *testing.T) {
	mid0, _ := common.BytesToMessageID([]byte("91234567890123456789012345678900"))
	mid1, _ := common.BytesToMessageID([]byte("91234567890123456789012345678901"))
	mid2, _ := common.BytesToMessageID([]byte("91234567890123456789012345678902"))
	mid3, _ := common.BytesToMessageID([]byte("91234567890123456789012345678903"))
	mid4, _ := common.BytesToMessageID([]byte("91234567890123456789012345678904"))

	ctx := context.Background()

	ts := testserver.Make(t, "server", "TestPendingMessages", nil)
	defer ts.S.Stop()

	key, err := ts.AddClient()
	if err != nil {
		t.Fatalf("Unable to add client: %v", err)
	}
	id, err := common.MakeClientID(key)
	if err != nil {
		t.Fatalf("Unable to make ClientID: %v", err)
	}

	as := admin.NewServer(ts.DS, nil)

	msgs := []*fspb.Message{
		{
			MessageId:    mid0.Bytes(),
			Source:       &fspb.Address{ServiceName: "TestService"},
			Destination:  &fspb.Address{ServiceName: "TestService", ClientId: id.Bytes()},
			MessageType:  "DummyType",
			CreationTime: db.NowProto(),
			Data: &anypb.Any{
				TypeUrl: "test data proto urn 0",
				Value:   []byte("Test data proto 0")},
		},
		{
			MessageId:    mid1.Bytes(),
			Source:       &fspb.Address{ServiceName: "TestService"},
			Destination:  &fspb.Address{ServiceName: "TestService", ClientId: id.Bytes()},
			MessageType:  "DummyType",
			CreationTime: db.NowProto(),
			Data: &anypb.Any{
				TypeUrl: "test data proto urn 1",
				Value:   []byte("Test data proto 1")},
		},
		{
			MessageId:    mid2.Bytes(),
			Source:       &fspb.Address{ServiceName: "TestService"},
			Destination:  &fspb.Address{ServiceName: "TestService", ClientId: id.Bytes()},
			MessageType:  "DummyType",
			CreationTime: db.NowProto(),
			Data: &anypb.Any{
				TypeUrl: "test data proto urn 2",
				Value:   []byte("Test data proto 2")},
		},
		{
			MessageId:    mid3.Bytes(),
			Source:       &fspb.Address{ServiceName: "TestService"},
			Destination:  &fspb.Address{ServiceName: "TestService", ClientId: id.Bytes()},
			MessageType:  "DummyType",
			CreationTime: db.NowProto(),
			Data: &anypb.Any{
				TypeUrl: "test data proto urn 3",
				Value:   []byte("Test data proto 3")},
		},
		{
			MessageId:    mid4.Bytes(),
			Source:       &fspb.Address{ServiceName: "TestService"},
			Destination:  &fspb.Address{ServiceName: "TestService", ClientId: id.Bytes()},
			MessageType:  "DummyType",
			CreationTime: db.NowProto(),
			Data: &anypb.Any{
				TypeUrl: "test data proto urn 4",
				Value:   []byte("Test data proto 4")},
		},
	}

	for _, m := range msgs {
		if _, err := as.InsertMessage(ctx, proto.Clone(m).(*fspb.Message)); err != nil {
			t.Fatalf("InsertMessage returned error: %v", err)
		}
	}

	// Get the message from the pending messages count

	t.Run("GetPendingMessageCount", func(t *testing.T) {
		greq := &spb.GetPendingMessageCountRequest{
			ClientIds: [][]byte{id.Bytes()},
		}
		gresp, err := as.GetPendingMessageCount(ctx, greq)
		if err != nil {
			t.Fatalf("GetPendingMessageCount returned error: %v", err)
		}
		if gresp.Count != uint64(len(msgs)) {
			t.Fatalf("Bad resul.t Expected: %v. Got: %v.", len(msgs), gresp.Count)
		}
	})

	// Get the message from the pending messages list, with data

	t.Run("GetPendingMessages/WantData=true", func(t *testing.T) {
		greq := &spb.GetPendingMessagesRequest{
			ClientIds: [][]byte{id.Bytes()},
			WantData:  true,
		}
		gresp, err := as.GetPendingMessages(ctx, greq)
		if err != nil {
			t.Fatalf("GetPendingMessages returned error: %v", err)
		}
		if len(gresp.Messages) != len(msgs) {
			t.Fatalf("Bad size of returned messages. Expected: %v. Got: %v.", len(msgs), len(gresp.Messages))
		}
		for i, _ := range msgs {
			if !proto.Equal(gresp.Messages[i], msgs[i]) {
				t.Fatalf("Got bad message. Expected: [%v]. Got: [%v].", msgs[i], gresp.Messages[i])
			}
		}
	})

	// Get the message from the pending messages list, with data, limit and offset

	t.Run("GetPendingMessages/WantData=true/limit/offset", func(t *testing.T) {
		greq := &spb.GetPendingMessagesRequest{
			ClientIds: [][]byte{id.Bytes()},
			WantData:  true,
			Offset:    1,
			Limit:     2,
		}
		gresp, err := as.GetPendingMessages(ctx, greq)
		if err != nil {
			t.Fatalf("GetPendingMessages returned error: %v", err)
		}
		if len(gresp.Messages) != 2 {
			t.Fatalf("Bad size of returned messages. Expected: %v. Got: %v.", len(msgs), len(gresp.Messages))
		}
		for i := 0; i < 2; i++ {
			if !proto.Equal(gresp.Messages[i], msgs[1+i]) {
				t.Fatalf("Got bad message. Expected: [%v]. Got: [%v].", msgs[1+i], gresp.Messages[i])
			}
		}
	})

	// Get the message from the pending messages list, without data

	t.Run("GetPendingMessages/WantData=false", func(t *testing.T) {
		greq := &spb.GetPendingMessagesRequest{
			ClientIds: [][]byte{id.Bytes()},
			WantData:  false,
		}
		gresp, err := as.GetPendingMessages(ctx, greq)

		if err != nil {
			t.Fatalf("GetPendingMessages returned error: %v", err)
		}
		if len(gresp.Messages) != len(msgs) {
			t.Fatalf("Bad size of returned messages. Expected: %v. Got: %v.", len(msgs), len(gresp.Messages))
		}
		for i, _ := range msgs {
			expectedMessage := proto.Clone(msgs[i]).(*fspb.Message)
			expectedMessage.Data = nil
			if !proto.Equal(gresp.Messages[i], expectedMessage) {
				t.Fatalf("Got bad message. Expected: [%v]. Got: [%v].", expectedMessage, gresp.Messages[i])
			}
		}
	})

	// Delete the message from the pending messages list.

	t.Run("DeletePendingMessages", func(t *testing.T) {
		req := &spb.DeletePendingMessagesRequest{
			ClientIds: [][]byte{id.Bytes()},
		}
		if _, err := as.DeletePendingMessages(ctx, req); err != nil {
			t.Fatalf("DeletePendingMessages returned error: %v", err)
		}

		// ClientMessagesForProcessing should return nothing, since the message is
		// supposed to be deleted from the pending mesages list.
		msgs, err := ts.DS.ClientMessagesForProcessing(ctx, id, 10, nil)
		if err != nil {
			t.Fatalf("ClientMessagesForProcessing(%v) returned error: %v", id, err)
		}
		if len(msgs) != 0 {
			t.Errorf("ClientMessagesForProcessing(%v) was expected to return 0 messages, got: %v", id, msgs)
		}
	})
}

type mockStreamClientContactsServer struct {
	grpc.ServerStream
	responses []*spb.StreamClientContactsResponse
}

func (m *mockStreamClientContactsServer) Send(response *spb.StreamClientContactsResponse) error {
	m.responses = append(m.responses, response)
	return nil
}

func (m *mockStreamClientContactsServer) Context() context.Context {
	return context.Background()
}

func TestClientContacts(t *testing.T) {
	ctx := context.Background()

	ts := testserver.Make(t, "server", "TestPendingMessages", nil)
	defer ts.S.Stop()

	as := admin.NewServer(ts.DS, nil)

	id := []byte{0, 0, 0, 0, 0, 0, 0, 0}
	cid, err := common.BytesToClientID(id)
	if err != nil {
		t.Fatalf("Failed to convert ID: %v.", err)
	}

	err = ts.DS.AddClient(ctx, cid, &db.ClientData{})
	if err != nil {
		t.Fatalf("Failed to add client: %v.", err)
	}

	for _, data := range []db.ContactData{
		{
			ClientID: cid,
			Addr:     "a1",
		},
		{
			ClientID: cid,
			Addr:     "a2",
		},
	} {
		_, err = ts.DS.RecordClientContact(ctx, data)
		if err != nil {
			t.Fatalf("Failed to record client contact: %v.", err)
		}
	}

	t.Run("StreamClientContacts", func(t *testing.T) {
		req := &spb.StreamClientContactsRequest{
			ClientId: id,
		}
		var m mockStreamClientContactsServer
		err := as.StreamClientContacts(req, &m)
		if err != nil {
			t.Fatalf("StreamClientContacts failed: %v.", err)
		}

		var addrs []string
		for _, response := range m.responses {
			addrs = append(addrs, response.Contact.ObservedAddress)
		}
		sort.Strings(addrs)

		expected := []string{"a1", "a2"}

		if !reflect.DeepEqual(addrs, expected) {
			t.Errorf("StreamClientContacts error: want [%v], got [%v].", expected, addrs)
		}
	})

}
