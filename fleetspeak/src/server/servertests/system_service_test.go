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

// Package servertests contains tests of the fleetspeak server.
package servertests_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/common/anypbtest"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/testserver"
)

func TestSystemServiceClientInfo(t *testing.T) {
	ctx := context.Background()

	ts := testserver.Make(t, "server", "SystemServiceClientInfo", nil)
	defer ts.S.Stop()

	k, err := ts.AddClient()
	if err != nil {
		t.Error(err)
		return
	}
	id, err := common.MakeClientID(k)
	if err != nil {
		t.Fatal(err)
	}

	checkCachedLabels := func(want []*fspb.Label) {
		t.Helper()
		info, _, _, err := ts.CC.InitializeConnection(
			ctx,
			&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 123},
			k,
			&fspb.WrappedContactData{},
			false,
		)
		if err != nil {
			t.Fatalf("InitializeConnection() failed: %v", err)
		}
		if !info.Client.Cached {
			t.Errorf("info.Client.Cached = false, want true")
		}
		if !db.LabelsEqual(info.Client.Labels, want) {
			t.Errorf("cached info.Client.Labels = %v, want %v", info.Client.Labels, want)
		}
	}

	// Seed a non-client service label; ClientInfo must preserve it in both the
	// datastore and the client cache.
	if err := ts.DS.AddClientLabel(ctx, id, &fspb.Label{ServiceName: "TestService", Label: "keep"}); err != nil {
		t.Fatal(err)
	}

	m := fspb.Message{
		Source: &fspb.Address{
			ClientId:    id.Bytes(),
			ServiceName: "system",
		},
		Destination: &fspb.Address{
			ServiceName: "system",
		},
		SourceMessageId: []byte("1"),
		MessageType:     "ClientInfo",
		Data: anypbtest.New(t, &fspb.ClientInfoData{
			Labels: []*fspb.Label{
				{ServiceName: "client", Label: "linux"},
				{ServiceName: "client", Label: "linux"}, // duplicate label should be skipped
				{ServiceName: "client", Label: "corp"},
			},
			Services: []*fspb.ClientInfoData_ServiceID{
				{Name: "TestService", Signature: []byte("signature")},
			},
		}),
	}
	m.MessageId = common.MakeMessageID(m.Source, m.SourceMessageId).Bytes()

	if err := ts.ProcessMessageFromClient(k, &m); err != nil {
		t.Fatal(err)
	}

	wantLabels1 := []*fspb.Label{
		{ServiceName: "TestService", Label: "keep"},
		{ServiceName: "client", Label: "linux"},
		{ServiceName: "client", Label: "corp"},
	}
	cd, err := ts.DS.GetClientData(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !db.LabelsEqual(cd.Labels, wantLabels1) {
		t.Errorf("datastore labels = %v, want %v", cd.Labels, wantLabels1)
	}
	checkCachedLabels(wantLabels1)

	// Remove client:corp and verify the cache is updated in place without losing
	// client:linux or TestService:keep.
	m.SourceMessageId = []byte("2")
	m.MessageId = common.MakeMessageID(m.Source, m.SourceMessageId).Bytes()
	m.Data = anypbtest.New(t, &fspb.ClientInfoData{
		Labels: []*fspb.Label{
			{ServiceName: "client", Label: "linux"},
		},
		Services: []*fspb.ClientInfoData_ServiceID{
			{Name: "TestService", Signature: []byte("signature")},
		},
	})
	if err := ts.ProcessMessageFromClient(k, &m); err != nil {
		t.Fatal(err)
	}
	wantLabels2 := []*fspb.Label{
		{ServiceName: "TestService", Label: "keep"},
		{ServiceName: "client", Label: "linux"},
	}
	cd, err = ts.DS.GetClientData(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !db.LabelsEqual(cd.Labels, wantLabels2) {
		t.Errorf("datastore labels = %v, want %v", cd.Labels, wantLabels2)
	}
	checkCachedLabels(wantLabels2)

	// Set an authorizer label in the datastore; ClientInfo must not remove it
	// and must ignore any client-spoofed non-client labels.
	if err := ts.DS.AddClientLabel(ctx, id, &fspb.Label{ServiceName: "authorizer", Label: "GECC"}); err != nil {
		t.Fatal(err)
	}

	m.SourceMessageId = []byte("3")
	m.MessageId = common.MakeMessageID(m.Source, m.SourceMessageId).Bytes()
	m.Data = anypbtest.New(t, &fspb.ClientInfoData{
		Labels: []*fspb.Label{
			{ServiceName: "client", Label: "linux"},
			{ServiceName: "authorizer", Label: "SPOOFED"},
		},
		Services: []*fspb.ClientInfoData_ServiceID{
			{Name: "TestService", Signature: []byte("signature")},
		},
	})
	if err := ts.ProcessMessageFromClient(k, &m); err != nil {
		t.Fatal(err)
	}
	wantLabels3 := []*fspb.Label{
		{ServiceName: "TestService", Label: "keep"},
		{ServiceName: "client", Label: "linux"},
		{ServiceName: "authorizer", Label: "GECC"},
	}
	cd, err = ts.DS.GetClientData(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !db.LabelsEqual(cd.Labels, wantLabels3) {
		t.Errorf("datastore labels = %v, want %v", cd.Labels, wantLabels3)
	}
}

func TestSystemServiceMessageAck(t *testing.T) {
	ctx := context.Background()

	fin1 := sertesting.SetClientRetryTime(func() time.Time { return db.Now().Add(time.Minute) })
	defer fin1()
	fin2 := sertesting.SetServerRetryTime(func(_ uint32) time.Time { return db.Now().Add(time.Minute) })
	defer fin2()

	ts := testserver.Make(t, "server", "SystemServiceMessageAck", nil)

	k, err := ts.AddClient()
	if err != nil {
		t.Error(err)
		return
	}
	cid, err := common.MakeClientID(k)
	if err != nil {
		t.Fatal(err)
	}

	mid, err := common.StringToMessageID("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatal(err)
	}

	if err := ts.DS.StoreMessages(ctx, []*fspb.Message{
		{
			MessageId: mid.Bytes(),
			Source: &fspb.Address{
				ServiceName: "TestService"},
			Destination: &fspb.Address{
				ClientId:    cid.Bytes(),
				ServiceName: "TestService"},
			CreationTime: db.NowProto(),
		}}, ""); err != nil {
		t.Fatal(err)
	}

	msg := ts.GetMessage(ctx, mid)
	if msg.Result != nil {
		t.Fatal("Added message should not be processed.")
	}

	m := fspb.Message{
		Source: &fspb.Address{
			ClientId:    cid.Bytes(),
			ServiceName: "system",
		},
		Destination: &fspb.Address{
			ServiceName: "system",
		},
		SourceMessageId: []byte("1"),
		MessageType:     "MessageAck",
	}
	m.MessageId = common.MakeMessageID(m.Source, m.SourceMessageId).Bytes()
	m.Data = anypbtest.New(t, &fspb.MessageAckData{
		MessageIds: [][]byte{mid.Bytes()},
	})
	if err := ts.ProcessMessageFromClient(k, &m); err != nil {
		t.Fatal(err)
	}

	msg = ts.GetMessage(ctx, mid)
	if msg.Result == nil {
		t.Fatal("Added message should now be processed.")
	}
}

func TestSystemServiceMessageError(t *testing.T) {
	ctx := context.Background()

	fin1 := sertesting.SetClientRetryTime(func() time.Time { return db.Now().Add(time.Minute) })
	defer fin1()
	fin2 := sertesting.SetServerRetryTime(func(_ uint32) time.Time { return db.Now().Add(time.Minute) })
	defer fin2()

	ts := testserver.Make(t, "server", "SystemServiceMessageError", nil)
	defer ts.S.Stop()

	k, err := ts.AddClient()
	if err != nil {
		t.Error(err)
		return
	}
	cid, err := common.MakeClientID(k)
	if err != nil {
		t.Fatal(err)
	}

	mid, err := common.StringToMessageID("000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f")
	if err != nil {
		t.Fatal(err)
	}

	if err := ts.DS.StoreMessages(ctx, []*fspb.Message{
		{
			MessageId: mid.Bytes(),
			Source: &fspb.Address{
				ServiceName: "TestService"},
			Destination: &fspb.Address{
				ClientId:    cid.Bytes(),
				ServiceName: "TestService"},
			CreationTime: db.NowProto(),
		}}, ""); err != nil {
		t.Fatal(err)
	}

	msg := ts.GetMessage(ctx, mid)
	if msg.Result != nil {
		t.Fatal("Added message should not be processed.")
	}

	m := fspb.Message{
		Source: &fspb.Address{
			ClientId:    cid.Bytes(),
			ServiceName: "system",
		},
		Destination: &fspb.Address{
			ServiceName: "system",
		},
		SourceMessageId: []byte("1"),
		MessageType:     "MessageError",
	}
	m.MessageId = common.MakeMessageID(m.Source, m.SourceMessageId).Bytes()
	m.Data = anypbtest.New(t, &fspb.MessageErrorData{
		MessageId: mid.Bytes(),
		Error:     "failed badly",
	})
	if err := ts.ProcessMessageFromClient(k, &m); err != nil {
		t.Fatal(err)
	}

	msg = ts.GetMessage(ctx, mid)
	if msg.Result == nil {
		t.Fatal("Added message should now be failed.")
	}
}

func TestUnknownService(t *testing.T) {
	ctx := context.Background()

	ts := testserver.Make(t, "server", "UnknownService", nil)
	defer ts.S.Stop()

	k, err := ts.AddClient()
	if err != nil {
		t.Error(err)
		return
	}
	id, err := common.MakeClientID(k)
	if err != nil {
		t.Fatal(err)
	}

	m := fspb.Message{
		Source: &fspb.Address{
			ClientId:    id.Bytes(),
			ServiceName: "system",
		},
		Destination: &fspb.Address{
			ServiceName: "UnknownService",
		},
		SourceMessageId: []byte("1"),
		MessageType:     "TestMessage",
	}
	m.MessageId = common.MakeMessageID(m.Source, m.SourceMessageId).Bytes()

	if err := ts.ProcessMessageFromClient(k, &m); err != nil {
		t.Fatal(err)
	}

	msgResult, err := ts.DS.GetMessageResult(ctx, common.MakeMessageID(m.Source, m.SourceMessageId))
	if err != nil {
		t.Fatalf("Failed to get message result: %v", err)
	}
	if msgResult == nil {
		t.Fatal("Expected message to have a result.")
	}
	if !msgResult.Failed {
		t.Errorf("Got Failed = false, want true")
	}
}
