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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"testing"

	"github.com/golang/protobuf/proto"
	tpb "github.com/golang/protobuf/ptypes/timestamp"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/testserver"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func TestCommsContext(t *testing.T) {
	fakeTime := sertesting.FakeNow(50)
	defer fakeTime.Revert()

	ts := testserver.Make(t, "server", "CommsContext", nil)
	defer ts.S.Stop()
	ctx := context.Background()

	// Verify that we can add clients using different types of keys.
	privateKey1, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	privateKey2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	// For each client/key we go through a basic lifecyle - add the client
	// to the system, check for messages for the client, etc.
	for _, tc := range []struct {
		name string
		pub  crypto.PublicKey
	}{
		{
			name: "rsa",
			pub:  privateKey1.Public()},
		{
			name: "ecdsa",
			pub:  privateKey2.Public()},
	} {
		ci, cd, err := ts.CC.InitializeConnection(
			ctx,
			&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 123},
			tc.pub,
			&fspb.WrappedContactData{})
		if err != nil {
			t.Fatal(err)
		}
		id, err := common.MakeClientID(tc.pub)
		if err != nil {
			t.Fatal(err)
		}
		if ci.Addr.Network() != "tcp" || ci.Addr.String() != "127.0.0.1:123" {
			t.Errorf("%s: InitializeConnection returned ci.Addr of [%s,%v], but expected [tcp,127.0.0.1:123]", tc.name, ci.Addr.Network(), ci.Addr)
		}
		if ci.Client.ID != id {
			t.Errorf("%s: InitializeConnection returned client ID of %v, but expected %v", tc.name, ci.Client.ID, id)
		}
		if ci.Client.Key == nil {
			t.Errorf("%s: InitializeConnection returned empty ci.Client.Key", tc.name)
		}
		if ci.ContactID == "" {
			t.Errorf("%s: InitializeConnection returned empty ci.ContactID", tc.name)
		}
		if ci.NonceSent == 0 {
			t.Errorf("%s: InitializeConnection returned 0 NonceSent", tc.name)
		}
		if len(cd.Messages) != 0 {
			t.Fatalf("%s: Expected no messages, got: %v", tc.name, cd.Messages)
		}

		// If a client does provide messages, they should end up in the datastore.
		fakeTime.SetSeconds(1234)
		cd = &fspb.ContactData{
			SequencingNonce: 5,
			Messages: []*fspb.Message{
				{
					Source: &fspb.Address{
						ClientId:    id.Bytes(),
						ServiceName: "TestService",
					},
					Destination: &fspb.Address{
						ServiceName: "TestService",
					},
					SourceMessageId: []byte("AAABBBCCC"),
					MessageType:     "TestMessage",
				},
			},
		}
		bcd, err := proto.Marshal(cd)
		if err != nil {
			t.Fatalf("%s: Unable to marshal contact data: %v", tc.name, err)
		}
		if ci, cd, err = ts.CC.InitializeConnection(
			ctx,
			&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 123},
			tc.pub,
			&fspb.WrappedContactData{ContactData: bcd}); err != nil {
			t.Fatal(err)
		}
		fakeTime.SetSeconds(3000)

		mid := common.MakeMessageID(&fspb.Address{ClientId: id.Bytes(),
			ServiceName: "TestService"},
			[]byte("AAABBBCCC"))
		msgs, err := ts.DS.GetMessages(ctx, []common.MessageID{mid}, false)

		if err != nil {
			t.Fatal(err)
		}
		if len(msgs) != 1 {
			t.Fatalf("Expected 1 message, got: %v", msgs)
		}
		want := &fspb.Message{
			MessageId: mid.Bytes(),
			Source: &fspb.Address{
				ClientId:    id.Bytes(),
				ServiceName: "TestService",
			},
			Destination: &fspb.Address{
				ServiceName: "TestService",
			},
			SourceMessageId: []byte("AAABBBCCC"),
			MessageType:     "TestMessage",
			CreationTime:    &tpb.Timestamp{Seconds: 1234},
		}
		msgs[0].Result = nil
		if !proto.Equal(msgs[0], want) {
			t.Errorf("%s: InitializeConnection(%v)=%v, but want %v", tc.name, id, msgs[0], want)
		}
	}
}

func TestBlacklist(t *testing.T) {
	ts := testserver.Make(t, "server", "Blacklist", nil)
	defer ts.S.Stop()
	ctx := context.Background()

	k, err := ts.AddClient()
	if err != nil {
		t.Fatal(err)
	}
	id, err := common.MakeClientID(k)
	if err != nil {
		t.Fatal(err)
	}

	// Put a message in the database that would otherwise be ready for delivery.
	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("Unable to create message id: %v", err)
	}
	if err := ts.DS.StoreMessages(ctx, []*fspb.Message{
		{
			MessageId: mid.Bytes(),
			Source: &fspb.Address{
				ServiceName: "testService",
			},
			Destination: &fspb.Address{
				ServiceName: "testService",
				ClientId:    id.Bytes(),
			},
			MessageType:  "TestMessage",
			CreationTime: db.NowProto(),
		}}, ""); err != nil {
		t.Fatalf("Unable to store message: %v", err)
	}

	// Blacklist the client
	if err := ts.DS.BlacklistClient(ctx, id); err != nil {
		t.Fatalf("BlacklistClient returned error: %v", err)
	}

	msgs, err := ts.SimulateContactFromClient(ctx, k, nil)
	if err != nil {
		t.Error(err)
	}

	if len(msgs) != 1 {
		t.Fatalf("Expected 1 message, got: %+v", msgs)
	}
	msg := msgs[0]

	if msg.MessageType != "RekeyRequest" {
		t.Errorf("Expected RekeyRequest, got: %+v", msg)
	}

	// Verify that the RekeyRequest message is in the database.
	mid, err = common.BytesToMessageID(msg.MessageId)
	if err != nil {
		t.Fatalf("Unable to parse RekeyRequest message id: %v", err)
	}

	msgs, err = ts.DS.GetMessages(ctx, []common.MessageID{mid}, true)
	if err != nil {
		t.Fatalf("Error reading rekey message from datastore: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("GetMessages([%v]) returned %d messages, expected 1.", mid, len(msgs))
	}
	if !bytes.Equal(msgs[0].MessageId, msg.MessageId) || msgs[0].MessageType != "RekeyRequest" {
		t.Errorf("GetMessage([%v]) did not return expected RekeyRequest, want: %+v got: %+v", mid, msg, msgs[0])
	}
}
