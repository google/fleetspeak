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
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/testserver"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func TestSystemServiceClientInfo(t *testing.T) {
	ctx := context.Background()

	ts := testserver.Make(t, "server", "SystemServiceClientInfo", nil)
	defer ts.S.Stop()

	id, err := ts.AddClient()
	if err != nil {
		t.Error(err)
		return
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
	}
	m.MessageId = common.MakeMessageID(m.Source, m.SourceMessageId).Bytes()
	m.Data, err = ptypes.MarshalAny(&fspb.ClientInfoData{
		Labels: []*fspb.Label{
			{ServiceName: "client", Label: "linux"},
			{ServiceName: "client", Label: "corp"},
		},
		Services: []*fspb.ClientInfoData_ServiceID{
			{Name: "TestService", Signature: []byte("signature")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.ProcessMessageFromClient(id, &m); err != nil {
		t.Fatal(err)
	}

	cd, err := ts.DS.GetClientData(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(cd.Labels) != 2 {
		t.Errorf("Expected 2 labels, got: %v", cd.Labels)
	}

	m.SourceMessageId = []byte("2")
	m.MessageId = common.MakeMessageID(m.Source, m.SourceMessageId).Bytes()
	m.Data, err = ptypes.MarshalAny(&fspb.ClientInfoData{
		Labels: []*fspb.Label{
			{ServiceName: "client", Label: "linux"},
		},
		Services: []*fspb.ClientInfoData_ServiceID{
			{Name: "TestService", Signature: []byte("signature")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.ProcessMessageFromClient(id, &m); err != nil {
		t.Fatal(err)
	}
	cd, err = ts.DS.GetClientData(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(cd.Labels) != 1 {
		t.Errorf("Expected 1 label, got: %v", cd.Labels)
	}
}

func TestSystemServiceMessageAck(t *testing.T) {
	ctx := context.Background()

	fin1 := sertesting.SetClientRetryTime(func() time.Time { return db.Now().Add(time.Minute) })
	defer fin1()
	fin2 := sertesting.SetServerRetryTime(func(_ uint32) time.Time { return db.Now().Add(time.Minute) })
	defer fin2()

	ts := testserver.Make(t, "server", "SystemServiceMessageAck", nil)

	cid, err := ts.AddClient()
	if err != nil {
		t.Error(err)
		return
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
	m.Data, err = ptypes.MarshalAny(&fspb.MessageAckData{
		MessageIds: [][]byte{mid.Bytes()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.ProcessMessageFromClient(cid, &m); err != nil {
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

	cid, err := ts.AddClient()
	if err != nil {
		t.Error(err)
		return
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
	m.Data, err = ptypes.MarshalAny(&fspb.MessageErrorData{
		MessageId: mid.Bytes(),
		Error:     "failed badly",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ts.ProcessMessageFromClient(cid, &m); err != nil {
		t.Fatal(err)
	}

	msg = ts.GetMessage(ctx, mid)
	if msg.Result == nil {
		t.Fatal("Added message should now be failed.")
	}
}
