package dbtesting

import (
	"context"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/ids"
	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"
	"google.golang.org/protobuf/proto"
	tspb "google.golang.org/protobuf/types/known/timestamppb"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

type idSet struct {
	bID ids.BroadcastID
	aID ids.AllocationID
}

func broadcastStoreTest(t *testing.T, ds db.Store) {
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

	future := tspb.New(time.Unix(200000, 0))

	b0 := &spb.Broadcast{
		BroadcastId: bid[0].Bytes(),
		Source:      &fspb.Address{ServiceName: "testService"},
		MessageType: "message type 1",
		Data: &anypb.Any{
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

func broadcastStoreTestSuite(t *testing.T, env DbTestEnv) {
	t.Run("BroadcastStoreTestSuite", func(t *testing.T) {
		runTestSuite(t, env, map[string]func(*testing.T, db.Store){
			"BroadcastStoreTest": broadcastStoreTest,
		})
	})
}
