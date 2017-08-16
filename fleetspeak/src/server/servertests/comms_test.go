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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"testing"

	"context"
	"github.com/golang/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/testserver"

	tpb "github.com/golang/protobuf/ptypes/timestamp"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func TestCommsContext(t *testing.T) {
	fakeTime := sertesting.FakeNow(50)
	defer fakeTime.Revert()

	ts := testserver.Make(t, "server", "CommsMethods", nil)
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
		id, err := common.MakeClientID(tc.pub)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ts.CC.AddClient(ctx, id, tc.pub)
		if err != nil {
			t.Fatal(err)
		}

		info, err := ts.CC.GetClientInfo(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if info.ID != id {
			t.Fatalf("%s: Expected id=info.ID, got %v = %v", tc.name, id, info.ID)
		}
		cd, err := ts.CC.HandleClientContact(ctx, info,
			&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 123},
			&fspb.WrappedContactData{})
		if err != nil {
			t.Fatal(err)
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
		if _, err = ts.CC.HandleClientContact(ctx, info,
			&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 123},
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
			t.Errorf("%s: GetMessages(%v)=%v, but want %v", tc.name, id, msgs[0], want)
		}
	}
}
