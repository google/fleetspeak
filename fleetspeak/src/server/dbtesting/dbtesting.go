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

// Package dbtesting contains utilities to help test implementations of
// db.Store.
package dbtesting

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"reflect"
	"sort"
	"testing"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/ids"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"

	apb "github.com/golang/protobuf/ptypes/any"
	tpb "github.com/golang/protobuf/ptypes/timestamp"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

var clientID, _ = common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 1})
var clientID2, _ = common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 2})
var clientID3, _ = common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 3})

// MessageStoreTest tests a MessageStore.
func MessageStoreTest(t *testing.T, ms db.Store) {
	fin1 := sertesting.SetClientRetryTime(func() time.Time { return db.Now().Add(time.Minute) })
	defer fin1()
	fin2 := sertesting.SetServerRetryTime(func(_ uint32) time.Time { return db.Now().Add(time.Minute) })
	defer fin2()

	storeGetMessagesTest(t, ms)
	storeMessagesTest(t, ms)
	clientMessagesForProcessingTest(t, ms)
	registerMessageProcessorTest(t, ms)
	listClientsTest(t, ms)
}

type idPair struct {
	cid common.ClientID
	mid common.MessageID
}

func storeGetMessagesTest(t *testing.T, ms db.Store) {
	fakeTime := sertesting.FakeNow(100000)
	defer fakeTime.Revert()

	ctx := context.Background()

	if err := ms.AddClient(ctx, clientID2, &db.ClientData{Key: []byte("test key")}); err != nil {
		t.Fatalf("AddClient [%v] failed: %v", clientID2, err)
	}

	msgs := []*fspb.Message{
		// A typical message to a client.
		{
			MessageId: []byte("01234567890123456789012345678902"),
			Source: &fspb.Address{
				ServiceName: "TestServiceName",
			},
			Destination: &fspb.Address{
				ClientId:    clientID2.Bytes(),
				ServiceName: "TestServiceName",
			},
			MessageType:  "Test message type 2",
			CreationTime: &tpb.Timestamp{Seconds: 42},
			Data: &apb.Any{
				TypeUrl: "test data proto urn 2",
				Value:   []byte("Test data proto 2")},
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

	m, err := ms.ClientMessagesForProcessing(ctx, clientID, 10)
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

func checkResults(t *testing.T, ms db.Store, statuses map[common.MessageID]*fspb.MessageResult) {
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

	if err := ms.AddClient(ctx, clientID3, &db.ClientData{Key: []byte("test key")}); err != nil {
		t.Fatalf("AddClient [%v] failed: %v", clientID3, err)
	}
	contact, err := ms.RecordClientContact(ctx, db.ContactData{
		ClientID:      clientID3,
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
				ClientId:    clientID3.Bytes(),
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
				ClientId:    clientID3.Bytes(),
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
				ClientId:    clientID3.Bytes(),
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
	checkResults(t, ms,
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

	checkResults(t, ms,
		map[common.MessageID]*fspb.MessageResult{
			newID: {
				ProcessedTime: &tpb.Timestamp{Seconds: 52}},
			processedID: {
				ProcessedTime: &tpb.Timestamp{Seconds: 52}},
			erroredID: {
				ProcessedTime: &tpb.Timestamp{Seconds: 52}},
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

func registerMessageProcessorTest(t *testing.T, ms db.MessageStore) {
	ctx := context.Background()
	fakeTime := sertesting.FakeNow(100000)
	defer fakeTime.Revert()

	p := fakeMessageProcessor{
		c: make(chan *fspb.Message, 1),
	}
	ms.RegisterMessageProcessor(&p)
	defer ms.StopMessageProcessor()

	msg := &fspb.Message{
		MessageId: []byte("01234567890123456789012345678903"),
		Source: &fspb.Address{
			ClientId:    []byte{0, 0, 0, 0, 0, 0, 0, 1},
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

	// If we advance the clock 70 seconds (> than that added by
	// testRetryPolicy), the message should be processed.
	fakeTime.AddSeconds(70)

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

type labelSorter struct {
	l []*fspb.Label
}

func (l labelSorter) Sort() {
	sort.Sort(l)
}

func (l labelSorter) Len() int {
	return len(l.l)
}

func (l labelSorter) Less(i, j int) bool {
	switch {
	case l.l[i].ServiceName < l.l[j].ServiceName:
		return true
	case l.l[i].ServiceName > l.l[j].ServiceName:
		return false
	}
	return l.l[i].Label < l.l[j].Label
}

func (l labelSorter) Swap(i, j int) {
	t := l.l[j]
	l.l[j] = l.l[i]
	l.l[i] = t
}

func clientDataEqual(a, b *db.ClientData) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a != nil && b == nil:
		return false
	case a == nil && b != nil:
		return false
	case a != nil && b != nil:
		if !bytes.Equal(a.Key, b.Key) {
			return false
		}
		if len(a.Labels) != len(b.Labels) {
			return false
		}
		labelSorter{a.Labels}.Sort()
		labelSorter{b.Labels}.Sort()

		l := len(a.Labels)
		if l != len(b.Labels) {
			return false
		}
		for i := 0; i < l; i++ {
			if !proto.Equal(a.Labels[i], b.Labels[i]) {
				return false
			}
		}

		return true
	}
	return false
}

// ClientStoreTest tests a ClientStore.
func ClientStoreTest(t *testing.T, ds db.Store) {
	fakeTime := sertesting.FakeNow(84)
	defer fakeTime.Revert()

	fin1 := sertesting.SetClientRetryTime(func() time.Time { return db.Now().Add(time.Minute) })
	defer fin1()
	fin2 := sertesting.SetServerRetryTime(func(_ uint32) time.Time { return db.Now().Add(time.Minute) })
	defer fin2()

	ctx := context.Background()
	key := []byte("A binary client key \x00\xff\x01\xfe")

	for _, tc := range []struct {
		desc         string
		op           func() error
		cd           *db.ClientData
		wantNotFound bool
	}{
		{
			desc:         "missing client",
			op:           func() error { return nil },
			cd:           nil,
			wantNotFound: true,
		},
		{
			desc: "client added",
			op: func() error {
				return ds.AddClient(ctx, clientID, &db.ClientData{
					Key: key,
					Labels: []*fspb.Label{
						{ServiceName: "system", Label: "Windows"},
						{ServiceName: "system", Label: "client-version-0.01"}}})
			},
			cd: &db.ClientData{
				Key: key,
				Labels: []*fspb.Label{
					{ServiceName: "system", Label: "Windows"},
					{ServiceName: "system", Label: "client-version-0.01"}},
			},
		},
		{
			desc: "label added",
			op: func() error {
				return ds.AddClientLabel(ctx, clientID, &fspb.Label{ServiceName: "system", Label: "new label"})
			},
			cd: &db.ClientData{
				Key: key,
				Labels: []*fspb.Label{
					{ServiceName: "system", Label: "new label"},
					{ServiceName: "system", Label: "Windows"},
					{ServiceName: "system", Label: "client-version-0.01"}},
			},
		},
		{
			desc: "label removed",
			op: func() error {
				return ds.RemoveClientLabel(ctx, clientID, &fspb.Label{ServiceName: "system", Label: "client-version-0.01"})
			},
			cd: &db.ClientData{
				Key: key,
				Labels: []*fspb.Label{
					{ServiceName: "system", Label: "new label"},
					{ServiceName: "system", Label: "Windows"}},
			},
		},
	} {
		err := tc.op()
		if err != nil {
			t.Errorf("%s: got unexpected error performing op: %v", tc.desc, err)
		}
		cd, err := ds.GetClientData(ctx, clientID)
		if tc.wantNotFound {
			if !ds.IsNotFound(err) {
				t.Errorf("%s: got %v but want not found error after performing op", tc.desc, err)
			}
		} else {
			if !clientDataEqual(cd, tc.cd) {
				t.Errorf("%s: got %v want %v after performing op", tc.desc, cd, tc.cd)
			}
		}
	}
	longAddr := "[ABCD:ABCD:ABCD:ABCD:ABCD:ABCD:192.168.123.123]:65535"
	contactID, err := ds.RecordClientContact(ctx, db.ContactData{
		ClientID:      clientID,
		NonceSent:     42,
		NonceReceived: 54,
		Addr:          longAddr,
		ClientClock:   &tpb.Timestamp{Seconds: 21},
	})
	if err != nil {
		t.Errorf("unexpected error for RecordClientContact: %v", err)
	}

	if err := ds.StoreMessages(ctx, []*fspb.Message{
		{
			MessageId: []byte("01234567890123456789012345678901"),
			Source: &fspb.Address{
				ClientId:    []byte{0, 0, 0, 0, 0, 0, 0, 1},
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
			}},
	}, ""); err != nil {
		t.Errorf("unexpected error for StoreMessage: %v", err)
	}

	mid, err := common.BytesToMessageID([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("unexpected error for BytesToMessageID: %v", err)
	}

	if err := ds.LinkMessagesToContact(ctx, contactID, []common.MessageID{mid}); err != nil {
		t.Errorf("unexpected error linking message to contact: %v", err)
	}

	clients, err := ds.ListClients(ctx, nil)
	if err != nil {
		t.Errorf("unexpected error while listing client ids: %v", err)
		return
	}
	if len(clients) != 1 {
		t.Errorf("expected ListClients to return 1 entry, got %v", len(clients))
		return
	}
	got := clients[0]
	// Some datastores might not respect db.Now for LastContactTime.  If it seems
	// to be a current timestamp (2017-2030) assume it is fine, and adjust to the
	// expected value.
	adjustDbTimestamp := func(timestamp *tpb.Timestamp) {
		if timestamp.Seconds > 1483228800 && timestamp.Seconds < 1893456000 {
			*timestamp = tpb.Timestamp{Seconds: 84}
		}
	}
	adjustDbTimestamp(got.LastContactTime)
	want := &spb.Client{
		ClientId: clientID.Bytes(),
		Labels: []*fspb.Label{
			{
				ServiceName: "system",
				Label:       "Windows",
			},
			{
				ServiceName: "system",
				Label:       "new label",
			},
		},
		LastContactTime:    &tpb.Timestamp{Seconds: 84},
		LastContactAddress: longAddr,
		LastClock:          &tpb.Timestamp{Seconds: 21},
	}

	labelSorter{got.Labels}.Sort()
	labelSorter{want.Labels}.Sort()

	if !proto.Equal(want, got) {
		t.Errorf("ListClients error: want [%v] got [%v]", want, got)
	}

	contacts, err := ds.ListClientContacts(ctx, clientID)
	if err != nil {
		t.Errorf("ListClientContacts returned error: %v", err)
	}
	if len(contacts) != 1 {
		t.Errorf("ListClientContacts returned %d results, expected 1.", len(contacts))
	} else {
		if contacts[0].SentNonce != 42 || contacts[0].ReceivedNonce != 54 {
			t.Errorf("ListClientContact[0] should return nonces (42, 54), got (%d, %d)",
				contacts[0].SentNonce, contacts[0].ReceivedNonce)
		}
		if contacts[0].ObservedAddress != longAddr {
			t.Errorf("ListClientContact[0] should return address %s, got %s",
				longAddr, contacts[0].ObservedAddress)
		}
	}

	meanRAM, maxRAM := 190, 200
	rud := mpb.ResourceUsageData{
		Scope:            "test-scope",
		Pid:              1234,
		ProcessStartTime: &tpb.Timestamp{Seconds: 1234567890, Nanos: 98765},
		DataTimestamp:    &tpb.Timestamp{Seconds: 1234567891, Nanos: 98765},
		ResourceUsage: &mpb.AggregatedResourceUsage{
			MeanUserCpuRate:    50.0,
			MaxUserCpuRate:     60.0,
			MeanSystemCpuRate:  70.0,
			MaxSystemCpuRate:   80.0,
			MeanResidentMemory: float64(meanRAM) * 1024 * 1024,
			MaxResidentMemory:  int64(maxRAM) * 1024 * 1024,
		},
	}
	err = ds.RecordResourceUsageData(ctx, clientID, rud)
	if err != nil {
		t.Fatalf("Unexpected error when writing client resource-usage data: %v", err)
	}
	records, err := ds.FetchResourceUsageRecords(ctx, clientID, 100)
	if err != nil {
		t.Errorf("Unexpected error when trying to fetch resource-usage data for client: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("Unexpected number of records returned. Want %d, got %v", 1, len(records))
	}
	expected := &spb.ClientResourceUsageRecord{
		Scope:                 "test-scope",
		Pid:                   1234,
		ProcessStartTime:      &tpb.Timestamp{Seconds: 1234567890, Nanos: 98765},
		ClientTimestamp:       &tpb.Timestamp{Seconds: 1234567891, Nanos: 98765},
		ServerTimestamp:       &tpb.Timestamp{Seconds: 84},
		MeanUserCpuRate:       50.0,
		MaxUserCpuRate:        60.0,
		MeanSystemCpuRate:     70.0,
		MaxSystemCpuRate:      80.0,
		MeanResidentMemoryMib: int32(meanRAM),
		MaxResidentMemoryMib:  int32(maxRAM),
	}
	record := records[0]
	adjustDbTimestamp(record.ServerTimestamp)

	// Adjust for floating-point rounding discrepancies.
	adjustForRounding := func(actual *int32, expected int) {
		if *actual == int32(expected-1) || *actual == int32(expected+1) {
			*actual = int32(expected)
		}
	}
	adjustForRounding(&record.MeanResidentMemoryMib, meanRAM)
	adjustForRounding(&record.MaxResidentMemoryMib, maxRAM)

	if got, want := record, expected; !proto.Equal(got, want) {
		t.Errorf("Resource-usage record returned is different from what we expect; got:\n%q\nwant:\n%q", got, want)
	}

	if err := ds.BlacklistClient(ctx, clientID); err != nil {
		t.Errorf("Error blacklisting client: %v", err)
	}
	g, err := ds.GetClientData(ctx, clientID)
	if err != nil {
		t.Errorf("Error getting client data after blacklisting: %v", err)
	}
	w := &db.ClientData{
		Key: key,
		Labels: []*fspb.Label{
			{ServiceName: "system", Label: "Windows"},
			{ServiceName: "system", Label: "new label"}},
		Blacklisted: true,
	}
	if !clientDataEqual(g, w) {
		t.Errorf("Got %+v want %+v after blacklisting client.", g, w)
	}
}

type idSet struct {
	bID ids.BroadcastID
	aID ids.AllocationID
}

// BroadcastStoreTest tests a BroadcastStore.
func BroadcastStoreTest(t *testing.T, ds db.Store) {
	fakeTime := sertesting.FakeNow(10000)
	defer fakeTime.Revert()

	fin1 := sertesting.SetClientRetryTime(func() time.Time { return db.Now().Add(time.Minute) })
	defer fin1()
	fin2 := sertesting.SetServerRetryTime(func(_ uint32) time.Time { return db.Now().Add(time.Minute) })
	defer fin2()

	ctx := context.Background()

	var bid []ids.BroadcastID

	for _, s := range []string{"0000000000000000", "0000000000000001", "0000000000000002", "0000000000000003", "0000000000000004"} {
		b, err := ids.StringToBroadcastID(s)
		if err != nil {
			t.Fatalf("BroadcastID(%v) failed: %v", s, err)
		}
		bid = append(bid, b)
	}

	future, err := ptypes.TimestampProto(time.Unix(200000, 0))
	if err != nil {
		t.Fatal(err)
	}

	b0 := &spb.Broadcast{
		BroadcastId: bid[0].Bytes(),
		Source:      &fspb.Address{ServiceName: "testService"},
		MessageType: "message type 1",
		Data: &apb.Any{
			TypeUrl: "message proto name 1",
			Value:   []byte("message data 1"),
		},
	}

	for i, tc := range []struct {
		br  *spb.Broadcast
		lim uint64
	}{
		{
			br:  b0,
			lim: 8,
		},
		{
			br: &spb.Broadcast{
				BroadcastId:    bid[1].Bytes(),
				Source:         &fspb.Address{ServiceName: "testService"},
				ExpirationTime: future},
			lim: 8,
		},
		{
			br: &spb.Broadcast{
				BroadcastId: bid[2].Bytes(),
				Source:      &fspb.Address{ServiceName: "testService"}},
			lim: 0, // (inactive)
		},
		{
			br: &spb.Broadcast{
				BroadcastId:    bid[3].Bytes(),
				Source:         &fspb.Address{ServiceName: "testService"},
				ExpirationTime: db.NowProto(), // just expired (inactive)
			},
			lim: 100,
		},
		{
			br: &spb.Broadcast{
				BroadcastId: bid[4].Bytes(),
				Source:      &fspb.Address{ServiceName: "testService"},
			},
			lim: db.BroadcastUnlimited,
		},
	} {
		if err := ds.CreateBroadcast(ctx, tc.br, tc.lim); err != nil {
			t.Fatalf("%v: Unable to CreateBroadcast(%v): %v", i, tc.br, err)
		}
	}

	bs, err := ds.ListActiveBroadcasts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 3 {
		t.Errorf("Expected 2 active broadcasts, got: %v", bs)
	}

	// Advance the fake time past the expiration of the second broadcast.
	fakeTime.SetSeconds(200001)
	bs, err = ds.ListActiveBroadcasts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 2 {
		t.Errorf("Expected 2 active broadcasts, got: %v", bs)
	} else {
		if !(proto.Equal(bs[0].Broadcast, b0) || proto.Equal(bs[1].Broadcast, b0)) {
			t.Errorf("ListActiveBroadcast=%v but want %v", bs[0], b0)
		}
	}

	// Check that allocations are allocated expected number messages to send.
	var allocs []idSet
	for i, tc := range []struct {
		id   ids.BroadcastID
		frac float32
		want uint64
	}{
		{id: bid[0], frac: 0.5, want: 4},
		{id: bid[0], frac: 0.5, want: 2},
		{id: bid[0], frac: 0.1, want: 1},
		{id: bid[0], frac: 2.0, want: 1},
		{id: bid[0], frac: 2.0, want: 0},
		{id: bid[4], frac: 0.1, want: db.BroadcastUnlimited},
	} {
		a, err := ds.CreateAllocation(ctx, tc.id, tc.frac, db.Now().Add(5*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if tc.want == 0 {
			if a != nil {
				t.Errorf("%v: Allocation(%v): wanted nil but got: %v", i, tc.id, a)
				break
			}
			continue
		}
		if a == nil || a.Limit != tc.want {
			t.Errorf("%v: Allocation(%v): wanted limit of %v but got: %v", i, tc.id, tc.want, a)
		}
		if a != nil {
			allocs = append(allocs, idSet{tc.id, a.ID})
		}
	}

	var clientID, _ = common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 1})
	if err := ds.AddClient(ctx, clientID, &db.ClientData{
		Key: []byte("a client key"),
	}); err != nil {
		t.Fatal(err)
	}

	for _, ids := range []idSet{allocs[0], allocs[4]} {
		mid, err := common.RandomMessageID()
		if err != nil {
			t.Fatal(err)
		}
		if err := ds.SaveBroadcastMessage(ctx, &fspb.Message{
			MessageId: mid.Bytes(),
			Destination: &fspb.Address{
				ClientId:    clientID.Bytes(),
				ServiceName: "testService",
			},
			Source: &fspb.Address{
				ServiceName: "testService",
			},
			CreationTime: db.NowProto(),
		}, ids.bID, clientID, ids.aID); err != nil {
			t.Fatal(err)
		}
	}

	// Clean them all up.
	for _, ids := range allocs {
		if err := ds.CleanupAllocation(ctx, ids.bID, ids.aID); err != nil {
			t.Errorf("Unable to cleanup allocation %v: %v", ids, err)
		}
	}

	// Fetch the active broadcasts again.
	bs, err = ds.ListActiveBroadcasts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 2 {
		t.Errorf("Expected 2 active broadcasts, got: %v", bs)
		t.FailNow()
	}

	if bs[0].Sent != 1 || bs[1].Sent != 1 {
		t.Errorf("Expected broadcasts to show 1 sent message, got: %v", bs)
	}

	sb, err := ds.ListSentBroadcasts(ctx, clientID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sb) != 2 {
		t.Errorf("Expected 2 sent broadcast, got: %v", sb)
		t.FailNow()
	}
	if sb[0] != bid[0] {
		t.Errorf("Expected sent broadcast to be %v got: %v", bid[0], sb[0])
	}
}

func listClientsTest(t *testing.T, ds db.Store) {
	ctx := context.Background()
	if err := ds.BlacklistClient(ctx, clientID3); err != nil {
		t.Errorf("Unable to blacklist client: %v", err)
	}
Cases:
	for _, tc := range []struct {
		name            string
		ids             []common.ClientID
		want            map[common.ClientID]bool
		wantBlacklisted map[common.ClientID]bool
	}{
		{
			ids:             nil,
			want:            map[common.ClientID]bool{clientID: true, clientID2: true, clientID3: true},
			wantBlacklisted: map[common.ClientID]bool{clientID3: true},
		},
		{
			ids:             []common.ClientID{clientID},
			want:            map[common.ClientID]bool{clientID: true},
			wantBlacklisted: map[common.ClientID]bool{},
		},
		{
			ids:             []common.ClientID{clientID, clientID2},
			want:            map[common.ClientID]bool{clientID: true, clientID2: true},
			wantBlacklisted: map[common.ClientID]bool{},
		},
	} {
		clients, err := ds.ListClients(ctx, tc.ids)
		if err != nil {
			t.Errorf("unexpected error while listing client ids [%v]: %v", tc.ids, err)
			continue Cases
		}
		got := make(map[common.ClientID]bool)
		gotBlacklisted := make(map[common.ClientID]bool)
		for _, c := range clients {
			id, err := common.BytesToClientID(c.ClientId)
			if err != nil {
				t.Errorf("ListClients(%v) returned invalid client_id: %v", tc.ids, err)
			}
			if c.LastContactTime == nil {
				t.Errorf("ListClients(%v) returned nil LastContactTime.", tc.ids)
			}
			got[id] = true
			if c.Blacklisted {
				gotBlacklisted[id] = true
			}
		}
		if !reflect.DeepEqual(tc.want, got) {
			t.Errorf("ListClients(%v) returned unexpected set of clients, want [%v], got[%v]", tc.ids, tc.want, got)
		}
		if !reflect.DeepEqual(tc.wantBlacklisted, gotBlacklisted) {
			t.Errorf("ListClients(%v) returned unexpected set of blacklisted clients, want [%v], got[%v]", tc.ids, tc.wantBlacklisted, gotBlacklisted)
		}
	}
}

// FileStoreTest tests a FileStore.
func FileStoreTest(t *testing.T, fs db.Store) {
	ctx := context.Background()

	fakeTime := sertesting.FakeNow(84)
	defer fakeTime.Revert()

	data := []byte("The quick sly fox jumped over the lazy dogs.")

	if err := fs.StoreFile(ctx, "testService", "testFile", bytes.NewReader(data)); err != nil {
		t.Errorf("Error from StoreFile(testService, testFile): %v", err)
	}

	ts, err := fs.StatFile(ctx, "testService", "testFile")
	if err != nil {
		t.Errorf("Error from StatFile(testService, testFile): %v", err)
	}
	if ts != fakeTime.Get() {
		t.Errorf("Wrong result of StatfileFile(testService, testFile), want %v got %v:", fakeTime.Get(), ts)
	}

	res, ts, err := fs.ReadFile(ctx, "testService", "testFile")
	if err != nil {
		t.Fatalf("Error from ReadFile(testService, testFile): %v", err)
	}
	rb, err := ioutil.ReadAll(res)
	if err != nil {
		t.Errorf("Error reading result of ReadFile(testService, testFile): %v", err)
	}
	if c, ok := res.(io.Closer); ok {
		c.Close()
	}
	if !bytes.Equal(rb, data) || ts != fakeTime.Get() {
		t.Errorf("Wrong result of ReadFile(testService, testFile), want (%v, %v) got (%v, %v):",
			fakeTime.Get(), data, ts, rb)
	}

	if _, err := fs.StatFile(ctx, "testService", "missingFile"); err == nil || !fs.IsNotFound(err) {
		t.Errorf("Wrong error for ReadFile(testService, missingFile), want IsNotFound(err)=true, got %v", err)
	}
	if _, _, err := fs.ReadFile(ctx, "testService", "missingFile"); err == nil || !fs.IsNotFound(err) {
		t.Errorf("Wrong error for ReadFile(testService, missingFile), want IsNotFound(err)=true, got %v", err)
	}
}
