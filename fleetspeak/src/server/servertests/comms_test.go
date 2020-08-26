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
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	tpb "github.com/golang/protobuf/ptypes/timestamp"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/services"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"
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
		name      string
		pub       crypto.PublicKey
		streaming bool
	}{
		{
			name: "rsa",
			pub:  privateKey1.Public()},
		{
			name: "ecdsa",
			pub:  privateKey2.Public()},
		{
			name:      "rsa-streaming",
			pub:       privateKey1.Public(),
			streaming: true},
		{
			name:      "ecdsa-streaming",
			pub:       privateKey2.Public(),
			streaming: true},
	} {
		ci, cd, _, err := ts.CC.InitializeConnection(
			ctx,
			&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 123},
			tc.pub,
			&fspb.WrappedContactData{},
			false)
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
		if tc.streaming {
			if err := ts.CC.HandleMessagesFromClient(
				ctx,
				ci,
				&fspb.WrappedContactData{ContactData: bcd}); err != nil {
				t.Fatal(err)
			}
			cd, _, err := ts.CC.GetMessagesForClient(ctx, ci)
			if err != nil {
				t.Fatal(err)
			}
			if cd != nil {
				t.Errorf("%s: Expected nil ContactData, got: %v", tc.name, cd)
			}
		} else {
			if ci, cd, _, err = ts.CC.InitializeConnection(
				ctx,
				&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 123},
				tc.pub,
				&fspb.WrappedContactData{ContactData: bcd},
				false); err != nil {
				t.Fatal(err)
			}
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

func TestDie(t *testing.T) {
	ts := testserver.Make(t, "server", "Die", nil)
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

	// Create a Die message and a Foo message for the client

	midDie, err := common.RandomMessageID()
	if err != nil {
		t.Fatal(err)
	}
	midFoo, err := common.RandomMessageID()
	if err != nil {
		t.Fatal(err)
	}
	err = ts.DS.StoreMessages(ctx, []*fspb.Message{
		{
			MessageId: midDie.Bytes(),
			Source: &fspb.Address{
				ServiceName: "system",
			},
			Destination: &fspb.Address{
				ServiceName: "system",
				ClientId:    id.Bytes(),
			},
			MessageType:  "Die",
			CreationTime: db.NowProto(),
		},
		{
			MessageId: midFoo.Bytes(),
			Source: &fspb.Address{
				ServiceName: "foo",
			},
			Destination: &fspb.Address{
				ServiceName: "foo",
				ClientId:    id.Bytes(),
			},
			MessageType:  "Foo",
			CreationTime: db.NowProto(),
		},
	}, "")
	if err != nil {
		t.Fatalf("Unable to store message: %v", err)
	}

	// Simulate contact from client

	cd := fspb.ContactData{AllowedMessages: map[string]uint64{"foo": 20, "system": 20}}
	cdb, err := proto.Marshal(&cd)
	if err != nil {
		t.Error(err)
	}
	ci, rcd, _, err := ts.CC.InitializeConnection(
		ctx,
		&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 123},
		k,
		&fspb.WrappedContactData{ContactData: cdb},
		false)
	if err != nil {
		t.Error(err)
	}
	msgs := rcd.Messages

	if len(msgs) != 2 {
		t.Fatalf("Expected 2 messages, got: %+v", msgs)
	}

	// Check tokens
	// The Die message should not have consumed any token.

	if ci.MessageTokens()["foo"] != 19 {
		t.Fatalf("Service foo should have 19 tokens left.")
	}

	if ci.MessageTokens()["system"] != 20 {
		t.Fatalf("Service system should have all 20 tokens left.")
	}

	// The Die message should be acked automatically

	m := ts.GetMessage(ctx, midDie)
	if m.Result == nil || m.Result.Failed {
		t.Error("Expected result of Die message to be success.")
	}

	// The Foo message should not be acked

	m = ts.GetMessage(ctx, midFoo)
	if m.Result != nil {
		t.Error("Expected no result for Foo message.")
	}

	// The client sends a MessageAck for the Foo message

	m = &fspb.Message{
		Source: &fspb.Address{
			ClientId:    id.Bytes(),
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
		MessageIds: [][]byte{midFoo.Bytes()},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = ts.ProcessMessageFromClient(k, m)
	if err != nil {
		t.Fatal(err)
	}

	// Both the Foo and Die messages should be acked.

	m = ts.GetMessage(ctx, midDie)
	if m.Result == nil || m.Result.Failed {
		t.Error("Expected result of Die message to be success.")
	}
	m = ts.GetMessage(ctx, midFoo)
	if m.Result == nil || m.Result.Failed {
		t.Error("Expected result of Foo message to be success.")
	}
}

// errorService is a Fleetspeak service.Service that returns a specified
// error every time Service.ProcessMessage() is called.
type errorService struct {
	err error
}

func (s errorService) Start(sctx service.Context) error                          { return nil }
func (s errorService) ProcessMessage(ctx context.Context, m *fspb.Message) error { return s.err }
func (s errorService) Stop() error                                               { return nil }

func TestServiceError(t *testing.T) {
	ctx := context.Background()
	testService := errorService{errors.New(strings.Repeat("a", services.MaxServiceFailureReasonLength+1))}
	serverWrapper := testserver.MakeWithService(t, "server", "ServiceError", testService)
	defer serverWrapper.S.Stop()

	clientPrivateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientPublicKey := clientPrivateKey.Public()

	clientID, err := common.MakeClientID(clientPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	clientMessage := &fspb.Message{
		Source: &fspb.Address{
			ClientId:    clientID.Bytes(),
			ServiceName: "TestService",
		},
		Destination: &fspb.Address{
			ServiceName: "TestService",
		},
		SourceMessageId: []byte("AAABBBCCC"),
		MessageType:     "TestMessage",
	}
	contactData := &fspb.ContactData{
		SequencingNonce: 5,
		Messages:        []*fspb.Message{clientMessage},
	}
	serializedContactData, err := proto.Marshal(contactData)
	if err != nil {
		t.Fatalf("Unable to marshal contact data: %v", err)
	}

	if _, _, _, err = serverWrapper.CC.InitializeConnection(
		ctx,
		&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 123},
		clientPublicKey,
		&fspb.WrappedContactData{ContactData: serializedContactData},
		false); err != nil {
		t.Fatalf("InitializeConnection() failed: %v", err)
	}

	messageID := common.MakeMessageID(clientMessage.Source, clientMessage.SourceMessageId)
	messageResult, err := serverWrapper.DS.GetMessageResult(ctx, messageID)
	if err != nil {
		t.Fatalf("Failed to get message result: %v", err)
	}

	expectedFailedReason := strings.Repeat("a", services.MaxServiceFailureReasonLength-3) + "..."
	if messageResult.FailedReason != expectedFailedReason {
		t.Errorf("Unexpected failure reason: got [%v], want [%v]", messageResult.FailedReason, expectedFailedReason)
	}
}
