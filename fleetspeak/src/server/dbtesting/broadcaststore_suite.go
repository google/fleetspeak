package dbtesting

import (
	"bytes"
	"errors"
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

	ctx := t.Context()

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
		Destination: &fspb.Address{ServiceName: "clientService"},
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
				Destination:    &fspb.Address{ServiceName: "clientService"},
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
		var finalSent uint64
		if ids == allocs[0] || ids == allocs[4] {
			finalSent = 1
		}
		if err := ds.CleanupAllocation(ctx, ids.bID, ids.aID, finalSent); err != nil {
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

	// Now test DisableBroadcasts
	testDisableBroadcasts(t, ds, bid[0], clientID)
}

func testDisableBroadcasts(t *testing.T, ds db.Store, bid ids.BroadcastID, clientID common.ClientID) {
	ctx := t.Context()

	// 1. Create a fresh allocation
	a, err := ds.CreateAllocation(ctx, bid, 0.5, db.Now().Add(5*time.Minute))
	if err != nil {
		t.Fatalf("CreateAllocation failed: %v", err)
	}
	if a == nil {
		t.Fatalf("Expected non-nil allocation")
	}

	// 2. Disable the broadcast. This should delete the allocation we just made.
	if err := ds.DisableBroadcasts(ctx, []ids.BroadcastID{bid}); err != nil {
		t.Fatalf("DisableBroadcasts failed: %v", err)
	}

	// 3. Attempting to use the deleted allocation should fail with ErrBroadcastDisabled.
	mid, _ := common.RandomMessageID()
	err = ds.SaveBroadcastMessage(ctx, &fspb.Message{
		MessageId:    mid.Bytes(),
		Destination:  &fspb.Address{ClientId: clientID.Bytes(), ServiceName: "test"},
		CreationTime: db.NowProto(),
	}, bid, clientID, a.ID)

	if !errors.Is(err, db.ErrBroadcastDisabled) {
		t.Errorf("Expected ErrBroadcastDisabled, got: %v", err)
	}

	// 3b. SaveBroadcastMessage with empty AllocationID (unlimited) should also fail with ErrBroadcastDisabled.
	mid2, _ := common.RandomMessageID()
	err = ds.SaveBroadcastMessage(ctx, &fspb.Message{
		MessageId:    mid2.Bytes(),
		Destination:  &fspb.Address{ClientId: clientID.Bytes(), ServiceName: "test"},
		CreationTime: db.NowProto(),
	}, bid, clientID, ids.AllocationID{})

	if !errors.Is(err, db.ErrBroadcastDisabled) {
		t.Errorf("Expected ErrBroadcastDisabled for empty AllocationID, got: %v", err)
	}

	// 4. CleanupAllocation should ignore the missing allocation and succeed gracefully.
	if err := ds.CleanupAllocation(ctx, bid, a.ID, 0); err != nil {
		t.Errorf("CleanupAllocation failed for deleted allocation: %v", err)
	}
}

func isBroadcastActive(t *testing.T, ds db.Store, bID ids.BroadcastID) bool {
	t.Helper()
	bs, err := ds.ListActiveBroadcasts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bs {
		if bytes.Equal(b.Broadcast.BroadcastId, bID.Bytes()) {
			return true
		}
	}
	return false
}

func testBroadcastReplacement(t *testing.T, ds db.Store) {
	ctx := t.Context()

	createBC := func(grp, src, dst string, labels ...*fspb.Label) *spb.Broadcast {
		return &spb.Broadcast{
			GroupName:      grp,
			Source:         &fspb.Address{ServiceName: src},
			Destination:    &fspb.Address{ServiceName: dst},
			RequiredLabels: labels,
			MessageType:    "Empty",
		}
	}

	l1 := &fspb.Label{ServiceName: "client", Label: "label1"}
	l2 := &fspb.Label{ServiceName: "client", Label: "label2"}

	for _, tc := range []struct {
		desc        string
		b1          *spb.Broadcast
		b2          *spb.Broadcast
		wantReplace bool
	}{
		{
			desc:        "IdenticalMatchingProperties",
			b1:          createBC("g1", "src1", "dst1", l1),
			b2:          createBC("g1", "src1", "dst1", l1),
			wantReplace: true,
		},
		{
			desc:        "DifferentSource",
			b1:          createBC("g2", "src1", "dst1", l1),
			b2:          createBC("g2", "src2", "dst1", l1),
			wantReplace: false,
		},
		{
			desc:        "DifferentLabels",
			b1:          createBC("g3", "src1", "dst1", l1),
			b2:          createBC("g3", "src1", "dst1", l2),
			wantReplace: false,
		},
		{
			desc:        "DifferentDestination",
			b1:          createBC("g4", "src1", "dst1", l1),
			b2:          createBC("g4", "src1", "dst2", l1),
			wantReplace: false,
		},
		{
			desc:        "DifferentGroup",
			b1:          createBC("g5", "src1", "dst1", l1),
			b2:          createBC("g6", "src1", "dst1", l1),
			wantReplace: false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			id1, _ := ids.RandomBroadcastID()
			id2, _ := ids.RandomBroadcastID()
			tc.b1.BroadcastId = id1.Bytes()
			tc.b2.BroadcastId = id2.Bytes()

			if err := ds.CreateBroadcast(ctx, tc.b1, 10); err != nil {
				if errors.Is(err, db.ErrNotSupported) {
					t.Skip("Broadcast replacement feature not supported by this datastore")
				}
				t.Fatalf("Failed to create b1: %v", err)
			}
			if !isBroadcastActive(t, ds, id1) {
				t.Fatalf("b1 should be active upon creation")
			}

			if err := ds.CreateBroadcast(ctx, tc.b2, 10); err != nil {
				t.Fatalf("Failed to create b2: %v", err)
			}

			gotActive := isBroadcastActive(t, ds, id1)
			wantActive := !tc.wantReplace
			if gotActive != wantActive {
				t.Errorf("b1 got active status %v, want %v", gotActive, wantActive)
			}
			if !isBroadcastActive(t, ds, id2) {
				t.Errorf("b2 should always be active")
			}
		})
	}
}

func testUnlimitedBroadcast(t *testing.T, ds db.Store) {
	ctx := t.Context()

	bID, err := ids.RandomBroadcastID()
	if err != nil {
		t.Fatal(err)
	}

	br := &spb.Broadcast{
		BroadcastId: bID.Bytes(),
		Source:      &fspb.Address{ServiceName: "testService"},
		MessageType: "UnlimitedBroadcastNoAlloc",
	}

	if err := ds.CreateBroadcast(ctx, br, db.BroadcastUnlimited); err != nil {
		t.Fatalf("CreateBroadcast failed: %v", err)
	}

	clientID, _ := common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 9})
	if err := ds.AddClient(ctx, clientID, &db.ClientData{
		Key: []byte("client key"),
	}); err != nil {
		t.Fatal(err)
	}

	// 1. Save broadcast message with empty AllocationID (unlimited broadcast bypass).
	mID, _ := common.RandomMessageID()
	msg := &fspb.Message{
		MessageId:    mID.Bytes(),
		Source:       &fspb.Address{ServiceName: "testService"},
		Destination:  &fspb.Address{ClientId: clientID.Bytes(), ServiceName: "testService"},
		MessageType:  "UnlimitedBroadcastNoAlloc",
		CreationTime: db.NowProto(),
	}

	if err := ds.SaveBroadcastMessage(ctx, msg, bID, clientID, ids.AllocationID{}); err != nil {
		t.Fatalf("SaveBroadcastMessage with empty AllocationID failed: %v", err)
	}

	// 2. Sent count in DB should still be 0 before CleanupAllocation.
	bs, err := ds.ListActiveBroadcasts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found *db.BroadcastInfo
	for _, b := range bs {
		if bytes.Equal(b.Broadcast.BroadcastId, bID.Bytes()) {
			found = b
			break
		}
	}
	if found == nil {
		t.Fatalf("Broadcast %v not found in active broadcasts", bID)
	}
	if found.Sent != 0 {
		t.Errorf("Expected Sent count in DB to be 0 before cleanup, got %v", found.Sent)
	}

	// 3. CleanupAllocation with empty AllocationID and finalSent = 1.
	if err := ds.CleanupAllocation(ctx, bID, ids.AllocationID{}, 1); err != nil {
		t.Fatalf("CleanupAllocation with empty AllocationID failed: %v", err)
	}

	// 4. Sent count in DB should now be updated to 1.
	bs, err = ds.ListActiveBroadcasts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found = nil
	for _, b := range bs {
		if bytes.Equal(b.Broadcast.BroadcastId, bID.Bytes()) {
			found = b
			break
		}
	}
	if found == nil {
		t.Fatalf("Broadcast %v not found in active broadcasts after cleanup", bID)
	}
	if found.Sent != 1 {
		t.Errorf("Expected Sent count in DB to be 1 after cleanup, got %v", found.Sent)
	}
}

func broadcastStoreTestSuite(t *testing.T, env DbTestEnv) {
	t.Run("BroadcastStoreTestSuite", func(t *testing.T) {
		runTestSuite(t, env, map[string]func(*testing.T, db.Store){
			"BroadcastStoreTest":       broadcastStoreTest,
			"BroadcastReplacementTest": testBroadcastReplacement,
			"UnlimitedBroadcastTest":   testUnlimitedBroadcast,
		})
	})
}
