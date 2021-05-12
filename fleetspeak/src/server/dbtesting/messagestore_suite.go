package dbtesting

import (
	"context"
	"fmt"
	"testing"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"

	apb "github.com/golang/protobuf/ptypes/any"
	tpb "github.com/golang/protobuf/ptypes/timestamp"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

type idPair struct {
	cid common.ClientID
	mid common.MessageID
}

func storeGetMessagesTest(t *testing.T, ms db.Store) {
	fakeTime := sertesting.FakeNow(100000)
	defer fakeTime.Revert()

	ctx := context.Background()

	if err := ms.AddClient(ctx, clientID, &db.ClientData{Key: []byte("test key")}); err != nil {
		t.Fatalf("AddClient [%v] failed: %v", clientID, err)
	}

	msgs := []*fspb.Message{
		// A typical message to a client.
		{
			MessageId: []byte("01234567890123456789012345678902"),
			Source: &fspb.Address{
				ServiceName: "TestServiceName",
			},
			Destination: &fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestServiceName",
			},
			MessageType:  "Test message type 2",
			CreationTime: &tpb.Timestamp{Seconds: 42},
			Data: &apb.Any{
				TypeUrl: "test data proto urn 2",
				Value:   []byte("Test data proto 2")},
		},
		// Message with annotations.
		{
			MessageId: []byte("11234567890123456789012345678903"),
			Source: &fspb.Address{
				ServiceName: "TestServiceName",
			},
			Destination: &fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestServiceName",
			},
			MessageType:  "Test message type 2",
			CreationTime: &tpb.Timestamp{Seconds: 42},
			Data: &apb.Any{
				TypeUrl: "test data proto urn 2",
				Value:   []byte("Test data proto 2"),
			},
			Annotations: &fspb.Annotations{
				Entries: []*fspb.Annotations_Entry{
					{Key: "session_id", Value: "123"},
					{Key: "request_id", Value: "1"},
				},
			},
		},
	}
	// duplicate calls to StoreMessages shouldn't fail.
	if err := ms.StoreMessages(ctx, msgs, ""); err != nil {
		t.Fatal(err)
	}
	if err := ms.StoreMessages(ctx, msgs, ""); err != nil {
		t.Error(err)
	}

	var idPairs []idPair
	var messageIDs []common.MessageID
	idMap := make(map[common.MessageID]*fspb.Message)
	for _, m := range msgs {
		mid, err := common.BytesToMessageID(m.MessageId)
		if err != nil {
			t.Fatal(err)
		}
		cid, err := common.BytesToClientID(m.Destination.ClientId)
		if err != nil {
			t.Fatal(err)
		}
		messageIDs = append(messageIDs, mid)
		idPairs = append(idPairs, idPair{cid, mid})
		idMap[mid] = m
	}
	msgsRead, err := ms.GetMessages(ctx, messageIDs, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgsRead) != len(msgs) {
		t.Fatalf("Expected to read %v messages, got %v", len(messageIDs), msgsRead)
	}

	for _, m := range msgsRead {
		id, err := common.BytesToMessageID(m.MessageId)
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(idMap[id], m) {
			t.Errorf("Got %v but want %v when reading message id %v", m, idMap[id], id)
		}
	}

	stat, err := ms.GetMessageResult(ctx, messageIDs[0])
	if err != nil {
		t.Errorf("unexpected error while retrieving message status: %v", err)
	} else {
		if stat != nil {
			t.Errorf("GetMessageResult of unprocessed message: want [nil] got [%v]", stat)
		}
	}

	fakeTime.SetSeconds(84)

	for _, i := range idPairs {
		if err := ms.SetMessageResult(ctx, i.cid, i.mid, &fspb.MessageResult{ProcessedTime: db.NowProto()}); err != nil {
			t.Errorf("Unable to mark message %v as processed: %v", i, err)
		}
	}
	msgsRead, err = ms.GetMessages(ctx, messageIDs, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgsRead) != len(msgs) {
		t.Fatalf("Expected to read %v messages, got %v", len(messageIDs), msgsRead)
	}

	for _, m := range msgsRead {
		id, err := common.BytesToMessageID(m.MessageId)
		if err != nil {
			t.Fatal(err)
		}
		want := idMap[id]
		want.Data = nil
		want.Result = &fspb.MessageResult{
			ProcessedTime: &tpb.Timestamp{Seconds: 84},
		}
		if !proto.Equal(want, m) {
			t.Errorf("Got %v but want %v when reading message id %v", m, want, id)
		}
	}

	stat, err = ms.GetMessageResult(ctx, messageIDs[0])
	if err != nil {
		t.Errorf("unexpected error while retrieving message result: %v", err)
	} else {
		want := &fspb.MessageResult{
			ProcessedTime: &tpb.Timestamp{Seconds: 84},
			Failed:        false,
			FailedReason:  "",
		}
		if !proto.Equal(want, stat) {
			t.Errorf("GetMessageResult error: want [%v] got [%v]", want, stat)
		}
	}
}

func clientMessagesForProcessingTest(t *testing.T, ms db.Store) {
	fakeTime := sertesting.FakeNow(100000)
	defer fakeTime.Revert()

	ctx := context.Background()

	if err := ms.AddClient(ctx, clientID, &db.ClientData{Key: []byte("test key")}); err != nil {
		t.Fatalf("AddClient [%v] failed: %v", clientID, err)
	}

	mid2 := common.MakeMessageID(
		&fspb.Address{
			ClientId:    clientID.Bytes(),
			ServiceName: "TestServiceName",
		}, []byte("omid 2"))

	stored := fspb.Message{
		MessageId: mid2.Bytes(),
		Source: &fspb.Address{
			ServiceName: "TestServiceName",
		},
		SourceMessageId: []byte("omid 2"),
		Destination: &fspb.Address{
			ClientId:    clientID.Bytes(),
			ServiceName: "TestServiceName",
		},
		CreationTime:   db.NowProto(),
		ValidationInfo: &fspb.ValidationInfo{Tags: map[string]string{"result": "Valid"}},
	}
	err := ms.StoreMessages(ctx, []*fspb.Message{&stored}, "")
	if err != nil {
		t.Errorf("StoreMessages returned error: %v", err)
	}

	fakeTime.SetSeconds(300000)

	m, err := ms.ClientMessagesForProcessing(ctx, clientID, 10, nil)
	if err != nil {
		t.Fatalf("ClientMessagesForProcessing(%v) returned error: %v", clientID, err)
	}
	log.Infof("Retrieved: %v", m)
	if len(m) != 1 {
		t.Errorf("ClientMessageForProcessing(%v) didn't return one message: %v", clientID, m)
	}
	if !proto.Equal(m[0], &stored) {
		t.Errorf("ClientMessageForProcessing(%v) unexpected result, want: %v, got: %v", clientID, &stored, m[0])
	}
}

func clientMessagesForProcessingLimitTest(t *testing.T, ms db.Store) {
	ctx := context.Background()

	if err := ms.AddClient(ctx, clientID, &db.ClientData{Key: []byte("test key")}); err != nil {
		t.Fatalf("AddClient [%v] failed: %v", clientID, err)
	}

	// Create a backlog for 2 different services.
	var toStore []*fspb.Message
	for i := 0; i < 100; i++ {
		mid1 := common.MakeMessageID(
			&fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestService1",
			}, []byte(fmt.Sprintf("omit: %d", i)))
		toStore = append(toStore, &fspb.Message{
			MessageId: mid1.Bytes(),
			Source: &fspb.Address{
				ServiceName: "TestService1",
			},
			Destination: &fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestService1",
			},
			CreationTime: db.NowProto(),
		})
		mid2 := common.MakeMessageID(
			&fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestService2",
			}, []byte(fmt.Sprintf("omit: %d", i)))
		toStore = append(toStore, &fspb.Message{
			MessageId: mid2.Bytes(),
			Source: &fspb.Address{
				ServiceName: "TestService2",
			},
			Destination: &fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestService2",
			},
			CreationTime: db.NowProto(),
		})
	}

	if err := ms.StoreMessages(ctx, toStore, ""); err != nil {
		t.Errorf("StoreMessages returned error: %v", err)
		return
	}
	for _, s := range []string{"TestService1", "TestService2"} {
		m, err := ms.ClientMessagesForProcessing(ctx, clientID, 10, map[string]uint64{s: 5})
		if err != nil {
			t.Errorf("ClientMessagesForProcessing(10, %s=5) returned unexpected error: %v", s, err)
			continue
		}
		if len(m) != 5 {
			t.Errorf("ClientMessagesForProcessing(10, %s=5) returned %d messages, but expected 5.", s, len(m))
		}
		for _, v := range m {
			if v.Destination.ServiceName != s {
				t.Errorf("ClientMessagesForProcessing(10, %s=5) returned message with ServiceName=%s, but expected %s.", s, v.Destination.ServiceName, s)
			}
		}
	}
	m, err := ms.ClientMessagesForProcessing(ctx, clientID, 10, nil)
	if err != nil {
		t.Errorf("ClientMessagesForProcessing(10, nil) returned unexpected error: %v", err)
		return
	}
	if len(m) != 10 {
		t.Errorf("ClientMessagesForProcessing(10, nil) returned %d messages, but expected 5.", len(m))
	}

	// Get all messages remaining for processing, with limit.

	for i := 0; i < 20; i++ {
		_, err := ms.ClientMessagesForProcessing(ctx, clientID, 10, nil)
		if err != nil {
			t.Fatalf("ClientMessagesForProcessing(10, nil) returned unexpected error: %v", err)
		}
	}

	// There should be no messages left for processing.

	m, err = ms.ClientMessagesForProcessing(ctx, clientID, 10, nil)
	if len(m) != 0 {
		t.Fatalf("Exepcted 0 messages, got %v.", len(m))
	}
}

func checkMessageResults(t *testing.T, ms db.Store, statuses map[common.MessageID]*fspb.MessageResult) {
	for id, want := range statuses {
		got, err := ms.GetMessageResult(context.Background(), id)
		if err != nil {
			t.Errorf("GetMessageResult(%v) returned error: %v", id, err)
		} else {
			if !proto.Equal(got, want) {
				t.Errorf("GetMessageResult(%v)=[%v], but want [%v]", id, got, want)
			}
		}
	}
}

func storeMessagesTest(t *testing.T, ms db.Store) {
	fakeTime := sertesting.FakeNow(43)
	defer fakeTime.Revert()

	ctx := context.Background()

	if err := ms.AddClient(ctx, clientID, &db.ClientData{Key: []byte("test key")}); err != nil {
		t.Fatalf("AddClient [%v] failed: %v", clientID, err)
	}
	contact, err := ms.RecordClientContact(ctx, db.ContactData{
		ClientID:      clientID,
		NonceSent:     42,
		NonceReceived: 43,
		Addr:          "127.0.0.1"})
	if err != nil {
		t.Fatalf("RecordClientContact failed: %v", err)
	}

	// Create one message in each obvious state - new, processed, errored:
	newID, _ := common.BytesToMessageID([]byte("01234567890123456789012345678906"))
	processedID, _ := common.BytesToMessageID([]byte("01234567890123456789012345678907"))
	erroredID, _ := common.BytesToMessageID([]byte("01234567890123456789012345678908"))

	msgs := []*fspb.Message{
		{
			MessageId: newID.Bytes(),
			Source: &fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestServiceName",
			},
			Destination: &fspb.Address{
				ServiceName: "TestServiceName",
			},
			MessageType:  "Test message type",
			CreationTime: &tpb.Timestamp{Seconds: 42},
			Data: &apb.Any{
				TypeUrl: "test data proto urn 2",
				Value:   []byte("Test data proto 2")},
		},
		{
			MessageId: processedID.Bytes(),
			Source: &fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestServiceName",
			},
			Destination: &fspb.Address{
				ServiceName: "TestServiceName",
			},
			MessageType:  "Test message type",
			CreationTime: &tpb.Timestamp{Seconds: 42},
			Result: &fspb.MessageResult{
				ProcessedTime: &tpb.Timestamp{Seconds: 42, Nanos: 20000},
			},
		},
		// New message, will become errored.
		{
			MessageId: erroredID.Bytes(),
			Source: &fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestServiceName",
			},
			Destination: &fspb.Address{
				ServiceName: "TestServiceName",
			},
			MessageType:  "Test message type",
			CreationTime: &tpb.Timestamp{Seconds: 42},
			Result: &fspb.MessageResult{
				ProcessedTime: &tpb.Timestamp{Seconds: 42},
				Failed:        true,
				FailedReason:  "broken test message",
			},
		},
	}
	if err := ms.StoreMessages(ctx, msgs, contact); err != nil {
		t.Fatal(err)
	}
	checkMessageResults(t, ms,
		map[common.MessageID]*fspb.MessageResult{
			newID: nil,
			processedID: {
				ProcessedTime: &tpb.Timestamp{Seconds: 42, Nanos: 20000}},
			erroredID: {
				ProcessedTime: &tpb.Timestamp{Seconds: 42},
				Failed:        true,
				FailedReason:  "broken test message",
			},
		})

	// StoreMessages again, modeling that they were all resent, and that this time
	// all processing completed.
	for _, m := range msgs {
		m.CreationTime = &tpb.Timestamp{Seconds: 52}
		m.Result = &fspb.MessageResult{ProcessedTime: &tpb.Timestamp{Seconds: 52}}
		m.Data = nil
	}
	if err := ms.StoreMessages(ctx, msgs, contact); err != nil {
		t.Fatal(err)
	}

	checkMessageResults(t, ms,
		map[common.MessageID]*fspb.MessageResult{
			newID: {
				ProcessedTime: &tpb.Timestamp{Seconds: 52}},
			processedID: {
				ProcessedTime: &tpb.Timestamp{Seconds: 52}},
			erroredID: {
				ProcessedTime: &tpb.Timestamp{Seconds: 52}},
		})
}

func pendingMessagesTest(t *testing.T, ms db.Store) {
	fakeTime := sertesting.FakeNow(43)
	defer fakeTime.Revert()

	ctx := context.Background()

	if err := ms.AddClient(ctx, clientID, &db.ClientData{Key: []byte("test key")}); err != nil {
		t.Fatalf("AddClient [%v] failed: %v", clientID, err)
	}
	contact, err := ms.RecordClientContact(ctx, db.ContactData{
		ClientID:      clientID,
		NonceSent:     42,
		NonceReceived: 43,
		Addr:          "127.0.0.1"})
	if err != nil {
		t.Fatalf("RecordClientContact failed: %v", err)
	}

	newID0, _ := common.BytesToMessageID([]byte("91234567890123456789012345678900"))
	newID1, _ := common.BytesToMessageID([]byte("91234567890123456789012345678901"))
	newID2, _ := common.BytesToMessageID([]byte("91234567890123456789012345678902"))
	newID3, _ := common.BytesToMessageID([]byte("91234567890123456789012345678903"))
	newID4, _ := common.BytesToMessageID([]byte("91234567890123456789012345678904"))

	ids := []common.MessageID{
		newID0,
		newID1,
		newID2,
		newID3,
		newID4,
	}

	msgs := []*fspb.Message{
		{
			MessageId: newID0.Bytes(),
			Source: &fspb.Address{
				ServiceName: "TestSource",
			},
			Destination: &fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestServiceName",
			},
			MessageType:  "Test message type",
			CreationTime: &tpb.Timestamp{Seconds: 42},
			Data: &apb.Any{
				TypeUrl: "test data proto urn 0",
				Value:   []byte("Test data proto 0")},
		},
		{
			MessageId: newID1.Bytes(),
			Source: &fspb.Address{
				ServiceName: "TestSource",
			},
			Destination: &fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestServiceName",
			},
			MessageType:  "Test message type",
			CreationTime: &tpb.Timestamp{Seconds: 1},
			Data: &apb.Any{
				TypeUrl: "test data proto urn 1",
				Value:   []byte("Test data proto 1")},
		},
		{
			MessageId: newID2.Bytes(),
			Source: &fspb.Address{
				ServiceName: "TestSource",
			},
			Destination: &fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestServiceName",
			},
			MessageType:  "Test message type",
			CreationTime: &tpb.Timestamp{Seconds: 2},
			Data: &apb.Any{
				TypeUrl: "test data proto urn 2",
				Value:   []byte("Test data proto 2")},
		},
		{
			MessageId: newID3.Bytes(),
			Source: &fspb.Address{
				ServiceName: "TestSource",
			},
			Destination: &fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestServiceName",
			},
			MessageType:  "Test message type",
			CreationTime: &tpb.Timestamp{Seconds: 3},
			Data: &apb.Any{
				TypeUrl: "test data proto urn 3",
				Value:   []byte("Test data proto 3")},
		},
		{
			MessageId: newID4.Bytes(),
			Source: &fspb.Address{
				ServiceName: "TestSource",
			},
			Destination: &fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "TestServiceName",
			},
			MessageType:  "Test message type",
			CreationTime: &tpb.Timestamp{Seconds: 4},
			Data: &apb.Any{
				TypeUrl: "test data proto urn 4",
				Value:   []byte("Test data proto 4")},
		},
	}
	if err := ms.StoreMessages(ctx, msgs, contact); err != nil {
		t.Fatal(err)
	}

	mc, err := ms.GetMessages(ctx, ids, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(mc) != len(ids) {
		t.Fatalf("Written message should be present in the store, not %v", len(mc))
	}

	t.Run("GetPendingMessageCount", func(t *testing.T) {
		count, err := ms.GetPendingMessageCount(ctx, []common.ClientID{clientID})
		if err != nil {
			t.Fatal(err)
		}
		if count != uint64(len(msgs)) {
			t.Fatalf("Bad pending messages count. Expected: %v. Got: %v.", len(msgs), count)
		}
	})

	t.Run("GetPendingMessages/wantData=true", func(t *testing.T) {
		pendingMsgs, err := ms.GetPendingMessages(ctx, []common.ClientID{clientID}, 0, 0, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(pendingMsgs) != len(msgs) {
			t.Fatalf("Expected %v pending messages, got %v", len(msgs), len(pendingMsgs))
		}
		for i := range msgs {
			if !proto.Equal(msgs[i], pendingMsgs[i]) {
				t.Fatalf("Expected pending message: [%v]. Got [%v].", msgs[i], pendingMsgs[i])
			}
		}
	})

	t.Run("GetPendingMessages/wantData=true/offset/limit", func(t *testing.T) {
		pendingMsgs, err := ms.GetPendingMessages(ctx, []common.ClientID{clientID}, 1, 2, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(pendingMsgs) != 2 {
			t.Fatalf("Expected %v pending messages, got %v", 2, len(pendingMsgs))
		}
		for i := range pendingMsgs {
			if !proto.Equal(msgs[1+i], pendingMsgs[i]) {
				t.Fatalf("Expected pending message: [%v]. Got [%v].", msgs[1+i], pendingMsgs[i])
			}
		}
	})

	t.Run("GetPendingMessages/wantData=false", func(t *testing.T) {
		pendingMsgs, err := ms.GetPendingMessages(ctx, []common.ClientID{clientID}, 0, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(pendingMsgs) != len(msgs) {
			t.Fatalf("Expected %v pending message, got %v", len(msgs), len(pendingMsgs))
		}
		for i := range msgs {
			expectedMsg := proto.Clone(msgs[i]).(*fspb.Message)
			expectedMsg.Data = nil
			if !proto.Equal(expectedMsg, pendingMsgs[i]) {
				t.Fatalf("Expected pending message: [%v]. Got [%v].", expectedMsg, pendingMsgs[i])
			}
		}
	})

	t.Run("DeletePendingMessages", func(t *testing.T) {
		if err := ms.DeletePendingMessages(ctx, []common.ClientID{clientID}); err != nil {
			t.Fatal(err)
		}

		mc, err = ms.ClientMessagesForProcessing(ctx, clientID, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(mc) != 0 {
			t.Fatalf("No messages for processing were expected, found: %v", mc)
		}

		for _, id := range ids {
			mr, err := ms.GetMessageResult(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if mr == nil {
				t.Fatal("Message result must be in the store after pending message is deleted", mr)
			}
			if !mr.Failed {
				t.Errorf("Expected the message to have failed=true, got: %v", mr.Failed)
			}
			if mr.FailedReason != "Removed by admin action." {
				t.Errorf("Expected the message to have failure reason 'Removed by admin action', got: %v", mr.FailedReason)
			}
		}
	})
}

type fakeMessageProcessor struct {
	c chan *fspb.Message
}

func (p *fakeMessageProcessor) ProcessMessages(msgs []*fspb.Message) {
	ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	for _, m := range msgs {
		select {
		case p.c <- m:
		case <-ctx.Done():
		}
	}
}

func registerMessageProcessorTest(t *testing.T, ms db.Store) {
	fakeTime := sertesting.FakeNow(100000)
	defer fakeTime.Revert()

	ctx := context.Background()
	if err := ms.AddClient(ctx, clientID, &db.ClientData{Key: []byte("test key")}); err != nil {
		t.Fatalf("AddClient [%v] failed: %v", clientID, err)
	}

	p := fakeMessageProcessor{
		c: make(chan *fspb.Message, 1),
	}
	ms.RegisterMessageProcessor(&p)
	defer ms.StopMessageProcessor()

	msg := &fspb.Message{
		MessageId: []byte("01234567890123456789012345678903"),
		Source: &fspb.Address{
			ClientId:    clientID.Bytes(),
			ServiceName: "TestServiceName",
		},
		SourceMessageId: []byte("01234567"),
		Destination: &fspb.Address{
			ServiceName: "TestServiceName",
		},
		MessageType:  "Test message type 1",
		CreationTime: &tpb.Timestamp{Seconds: 42},
		Data: &apb.Any{
			TypeUrl: "test data proto urn 1",
			Value:   []byte("Test data proto 1"),
		},
	}
	if err := ms.StoreMessages(ctx, []*fspb.Message{msg}, ""); err != nil {
		t.Fatal(err)
	}

	// If we advance the clock 120 seconds (> than that added by
	// testRetryPolicy), the message should be processed. See the implementation
	// of ServerRetryTime (retry.go) for details.
	fakeTime.AddSeconds(121)

	select {
	case <-time.After(20 * time.Second):
		t.Errorf("Did not receive notification of message to process after 20 seconds")
	case m := <-p.c:
		if !proto.Equal(m, msg) {
			t.Errorf("ProcessMessage called with wrong message, got: [%v] want: [%v]", m, msg)
		}
		mid, err := common.BytesToMessageID(msg.MessageId)
		if err != nil {
			t.Fatal(err)
		}
		if err := ms.SetMessageResult(ctx, common.ClientID{}, mid, &fspb.MessageResult{ProcessedTime: db.NowProto()}); err != nil {
			t.Errorf("Unable to mark message as processed: %v", err)
		}
	}
}

func messageStoreTestSuite(t *testing.T, env DbTestEnv) {
	t.Run("MessageStoreTestSuite", func(t *testing.T) {
		runTestSuite(t, env, map[string]func(*testing.T, db.Store){
			"StoreGetMessagesTest":                 storeGetMessagesTest,
			"StoreMessagesTest":                    storeMessagesTest,
			"PendingMessagesTest":                  pendingMessagesTest,
			"ClientMessagesForProcessingTest":      clientMessagesForProcessingTest,
			"ClientMessagesForProcessingLimitTest": clientMessagesForProcessingLimitTest,
			"RegisterMessageProcessor":             registerMessageProcessorTest,
		})
	})
}
