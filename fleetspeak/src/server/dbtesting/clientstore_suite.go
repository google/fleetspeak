package dbtesting

import (
	"bytes"
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"

	"github.com/golang/protobuf/ptypes"
	apb "github.com/golang/protobuf/ptypes/any"
	tpb "github.com/golang/protobuf/ptypes/timestamp"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

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

func clientStoreTest(t *testing.T, ds db.Store) {
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
		StreamingTo:   "fs.servers.somewhere.com",
	})
	if err != nil {
		t.Errorf("unexpected error for RecordClientContact: %v", err)
	}

	if err := ds.StoreMessages(ctx, []*fspb.Message{
		{
			MessageId: []byte("01234567890123456789012345678901"),
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
		LastContactTime:        &tpb.Timestamp{Seconds: 84},
		LastContactStreamingTo: "fs.servers.somewhere.com",
		LastContactAddress:     longAddr,
		LastClock:              &tpb.Timestamp{Seconds: 21},
	}

	labelSorter{got.Labels}.Sort()
	labelSorter{want.Labels}.Sort()

	if !proto.Equal(want, got) {
		t.Errorf("ListClients error: want [%v] got [%v]", want, got)
	}

	checkClientContacts := func(t *testing.T, contacts []*spb.ClientContact) {
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
	}

	t.Run("ListClientContacts", func(t *testing.T) {
		contacts, err := ds.ListClientContacts(ctx, clientID)
		if err != nil {
			t.Errorf("ListClientContacts returned error: %v", err)
		}
		checkClientContacts(t, contacts)
	})

	t.Run("StreamClientContacts", func(t *testing.T) {
		var contacts []*spb.ClientContact
		callback := func(contact *spb.ClientContact) error {
			contacts = append(contacts, contact)
			return nil
		}
		err := ds.StreamClientContacts(ctx, clientID, callback)
		if err != nil {
			t.Errorf("StreamClientContacts returned error: %v", err)
		}
		checkClientContacts(t, contacts)
	})

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

func addClientsTest(t *testing.T, ds db.Store) {
	ctx := context.Background()
	key := []byte("A binary client key \x00\xff\x01\xfe")

	t.Run("add non-blacklisted client", func(t *testing.T) {
		if err := ds.AddClient(ctx, clientID, &db.ClientData{
			Key: key,
		}); err != nil {
			t.Errorf("Can't add client.")
		}

		got, err := ds.GetClientData(ctx, clientID)
		if err != nil {
			t.Errorf("Can't get client data.")
		}

		expected := &db.ClientData{
			Key: key,
		}
		if !reflect.DeepEqual(expected, got) {
			t.Errorf("Expected %v, got %v", expected, got)
		}
	})

	t.Run("add blacklisted client", func(t *testing.T) {
		if err := ds.AddClient(ctx, clientID2, &db.ClientData{
			Key:         key,
			Blacklisted: true,
		}); err != nil {
			t.Errorf("Can't add client.")
		}

		got, err := ds.GetClientData(ctx, clientID2)
		if err != nil {
			t.Errorf("Can't get client data.")
		}

		expected := &db.ClientData{
			Key:         key,
			Blacklisted: true,
		}
		if !reflect.DeepEqual(expected, got) {
			t.Errorf("Expected %v, got %v", expected, got)
		}
	})
}

func listClientsTest(t *testing.T, ds db.Store) {
	ctx := context.Background()

	for _, cid := range [...]common.ClientID{clientID, clientID2, clientID3} {
		if err := ds.AddClient(ctx, cid, &db.ClientData{Key: []byte("test key")}); err != nil {
			t.Fatalf("AddClient [%v] failed: %v", clientID, err)
		}
	}

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

func streamClientIdsTest(t *testing.T, ds db.Store) {
	ctx := context.Background()

	clientIds := []common.ClientID{clientID, clientID2, clientID3}
	contactTimes := []time.Time{}

	for idx, cid := range clientIds {
		contactTimes = append(contactTimes, db.Now())
		if err := ds.AddClient(ctx, cid, &db.ClientData{Key: []byte("test key"), Blacklisted: idx%2 != 0}); err != nil {
			t.Fatalf("AddClient [%v] failed: %v", clientID, err)
		}
	}

	t.Run("Stream all clients", func(t *testing.T) {
		var result []common.ClientID

		callback := func(id common.ClientID) error {
			result = append(result, id)
			return nil
		}

		err := ds.StreamClientIds(ctx, true, nil, callback)
		if err != nil {
			t.Fatalf("StreamClientIds failed: %v", err)
		}

		sort.Slice(result, func(i int, j int) bool {
			return bytes.Compare(result[i].Bytes(), result[j].Bytes()) < 0
		})

		if !reflect.DeepEqual(result, clientIds) {
			t.Errorf("StreamClientIds returned unexpected result. Got: [%v]. Want: [%v].", result, clientIds)
		}
	})

	t.Run("Stream non-blacklisted clients only", func(t *testing.T) {
		var result []common.ClientID

		callback := func(id common.ClientID) error {
			result = append(result, id)
			return nil
		}

		err := ds.StreamClientIds(ctx, false, nil, callback)
		if err != nil {
			t.Fatalf("StreamClientIds failed: %v", err)
		}

		sort.Slice(result, func(i int, j int) bool {
			return bytes.Compare(result[i].Bytes(), result[j].Bytes()) < 0
		})

		expected := []common.ClientID{clientID, clientID3}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("StreamClientIds returned unexpected result. Got: [%v]. Want: [%v].", result, expected)
		}
	})

	t.Run("Stream all clients with time filter", func(t *testing.T) {
		var result []common.ClientID

		callback := func(id common.ClientID) error {
			result = append(result, id)
			return nil
		}

		err := ds.StreamClientIds(ctx, true, &contactTimes[1], callback)
		if err != nil {
			t.Fatalf("StreamClientIds failed: %v", err)
		}

		sort.Slice(result, func(i int, j int) bool {
			return bytes.Compare(result[i].Bytes(), result[j].Bytes()) < 0
		})

		expected := []common.ClientID{clientID2, clientID3}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("StreamClientIds returned unexpected result. Got: [%v]. Want: [%v].", result, expected)
		}
	})
}

func fetchResourceUsageRecordsTest(t *testing.T, ds db.Store) {
	ctx := context.Background()
	key := []byte("Test key")
	err := ds.AddClient(ctx, clientID, &db.ClientData{
		Key: key})
	if err != nil {
		t.Errorf("add client: got unexpected error performing op: %v", err)
	}

	meanRAM, maxRAM := 190, 200
	rud := mpb.ResourceUsageData{
		Scope:             "test-scope",
		Pid:               1234,
		ProcessStartTime:  &tpb.Timestamp{Seconds: 1234567890, Nanos: 98765},
		DataTimestamp:     &tpb.Timestamp{Seconds: 1234567891, Nanos: 98765},
		ProcessTerminated: true,
		ResourceUsage: &mpb.AggregatedResourceUsage{
			MeanUserCpuRate:    50.0,
			MaxUserCpuRate:     60.0,
			MeanSystemCpuRate:  70.0,
			MaxSystemCpuRate:   80.0,
			MeanResidentMemory: float64(meanRAM) * 1024 * 1024,
			MaxResidentMemory:  int64(maxRAM) * 1024 * 1024,
			MeanNumFds:         13.4,
			MaxNumFds:          42,
		},
	}

	beforeRecordTime := db.Now()
	oneMinAfterRecordTime := beforeRecordTime.Add(time.Minute) // This isn't exactly 1 minute after record time.
	beforeRecordTimestamp, err := ptypes.TimestampProto(beforeRecordTime)
	if err != nil {
		t.Fatalf("Invalid time.Time object cannot be converted to tpb.Timestamp: %v", err)
	}
	afterRecordTimestamp, err := ptypes.TimestampProto(oneMinAfterRecordTime)
	if err != nil {
		t.Fatalf("Invalid time.Time object cannot be converted to tpb.Timestamp: %v", err)
	}

	err = ds.RecordResourceUsageData(ctx, clientID, &rud)
	if err != nil {
		t.Fatalf("Unexpected error when writing client resource-usage data: %v", err)
	}

	records, err := ds.FetchResourceUsageRecords(ctx, clientID, beforeRecordTimestamp, afterRecordTimestamp)
	if err != nil {
		t.Errorf("Unexpected error when trying to fetch resource-usage data for client: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("Unexpected number of records returned. Want %d, got %v", 1, len(records))
	}

	record := records[0]
	expected := &spb.ClientResourceUsageRecord{
		Scope:                 "test-scope",
		Pid:                   1234,
		ProcessStartTime:      &tpb.Timestamp{Seconds: 1234567890, Nanos: 98765},
		ClientTimestamp:       &tpb.Timestamp{Seconds: 1234567891, Nanos: 98765},
		ServerTimestamp:       record.ServerTimestamp,
		ProcessTerminated:     true,
		MeanUserCpuRate:       50.0,
		MaxUserCpuRate:        60.0,
		MeanSystemCpuRate:     70.0,
		MaxSystemCpuRate:      80.0,
		MeanResidentMemoryMib: int32(meanRAM),
		MaxResidentMemoryMib:  int32(maxRAM),
		MeanNumFds:            13,
		MaxNumFds:             42,
	}

	if got, want := record, expected; !proto.Equal(got, want) {
		t.Errorf("Resource-usage record returned is different from what we expect; got:\n%q\nwant:\n%q", got, want)
	}

	recordExactTime, err := ptypes.Timestamp(record.ServerTimestamp)
	if err != nil {
		t.Fatalf("Invalid tpb.Timestamp object cannot be converted to time.Time object: %v", err)
	}
	nanoBeforeRecord := recordExactTime.Add(time.Duration(-1) * time.Nanosecond)
	nanoBeforeRecordTS, err := ptypes.TimestampProto(nanoBeforeRecord)
	if err != nil {
		t.Fatalf("Invalid time.Time object cannot be converted to tpb.Timestamp: %v", err)
	}
	nanoAfterRecord := recordExactTime.Add(time.Nanosecond)
	nanoAfterRecordTS, err := ptypes.TimestampProto(nanoAfterRecord)
	if err != nil {
		t.Fatalf("Invalid time.Time object cannot be converted to tpb.Timestamp: %v", err)
	}

	for _, tr := range []struct {
		desc            string
		startTs         *tpb.Timestamp
		endTs           *tpb.Timestamp
		shouldErr       bool
		recordsExpected int
	}{
		{
			desc:            "record out of time range",
			startTs:         nanoBeforeRecordTS,
			endTs:           record.ServerTimestamp,
			recordsExpected: 0,
		},
		{
			desc:      "time range invalid",
			startTs:   record.ServerTimestamp,
			endTs:     nanoBeforeRecordTS,
			shouldErr: true,
		},
		{
			desc:            "record in time range",
			startTs:         record.ServerTimestamp,
			endTs:           nanoAfterRecordTS,
			recordsExpected: 1,
		},
	} {
		records, err = ds.FetchResourceUsageRecords(ctx, clientID, tr.startTs, tr.endTs)
		if tr.shouldErr {
			if err == nil {
				t.Errorf("%s: Should have errored when trying to fetch resource-usage data for client as time range is invalid, but didn't error.", tr.desc)
			}
		} else {
			if err != nil {
				t.Errorf("%s: Unexpected error when trying to fetch resource-usage data for client: %v", tr.desc, err)
			}
			if len(records) != tr.recordsExpected {
				t.Fatalf("%s: Unexpected number of records returned. Want %d, got %v", tr.desc, tr.recordsExpected, len(records))
			}
		}
	}
}

func clientStoreTestSuite(t *testing.T, env DbTestEnv) {
	t.Run("ClientStoreTestSuite", func(t *testing.T) {
		runTestSuite(t, env, map[string]func(*testing.T, db.Store){
			"AddClientsTest":                addClientsTest,
			"ClientStoreTest":               clientStoreTest,
			"ListClientsTest":               listClientsTest,
			"StreamClientIdsTest":           streamClientIdsTest,
			"FetchResourceUsageRecordsTest": fetchResourceUsageRecordsTest,
		})
	})
}
