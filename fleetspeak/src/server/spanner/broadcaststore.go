// Copyright 2025 Google Inc.
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

package spanner

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/spanner"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/ids"

	"google.golang.org/api/iterator"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
)

type broadcast struct {
	PrimaryKey   *spanner.Key
	Broadcast    *spb.Broadcast
	Sent         int64
	Allocated    int64
	MessageLimit int64
}

// CreateBroadcast implements db.BroadcastStore.
func (d *Datastore) CreateBroadcast(ctx context.Context, b *spb.Broadcast, limit uint64) error {
	br := broadcast{
		Broadcast:    b,
		Sent:         0,
		Allocated:    0,
		MessageLimit: int64(limit),
	}
	_, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		return d.tryCreateBroadcast(txn, &br)
	})
	return err
}

func (d *Datastore) tryCreateBroadcast(txn *spanner.ReadWriteTransaction, b *broadcast) error {
	ms := []*spanner.Mutation{spanner.InsertOrUpdate(d.broadcasts,
		[]string{"Broadcast", "Sent", "Allocated", "MessageLimit"},
		[]interface{}{b.Broadcast, b.Sent, b.Allocated, b.MessageLimit})}
	txn.BufferWrite(ms)
	return nil
}

// SetBroadcastLimit implements db.BroadcastStore.
func (d *Datastore) SetBroadcastLimit(ctx context.Context, id ids.BroadcastID, limit uint64) error {
	_, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		return d.trySetBroadcastLimit(txn, id, limit)
	})
	return err
}

func (d *Datastore) trySetBroadcastLimit(txn *spanner.ReadWriteTransaction, id ids.BroadcastID, limit uint64) error {
	ms := []*spanner.Mutation{spanner.Update(d.broadcasts, []string{"BroadcastID", "MessageLimit"}, []interface{}{id.Bytes(), int(limit)})}
	txn.BufferWrite(ms)
	return nil
}

// SaveBroadcastMessage implements db.BroadcastStore.
func (d *Datastore) SaveBroadcastMessage(ctx context.Context, msg *fspb.Message, bid ids.BroadcastID, cid common.ClientID, aid ids.AllocationID) error {
	_, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		return d.trySaveBroadcastMessage(ctx, txn, msg, bid, cid, aid)
	})
	return err
}

func (d *Datastore) trySaveBroadcastMessage(ctx context.Context, txn *spanner.ReadWriteTransaction, msg *fspb.Message, bid ids.BroadcastID, cid common.ClientID, aid ids.AllocationID) error {
	if err := d.tryStoreMessage(ctx, txn, msg, true); err != nil {
		return err
	}

	row, err := txn.ReadRow(ctx, d.broadcastAllocations, spanner.Key{bid.Bytes(), aid.Bytes()}, []string{"Sent", "MessageLimit", "ExpiresTime"})
	if err != nil {
		return err
	}

	var sent int64
	var messageLimit int64
	var expiresTime tspb.Timestamp
    if err := row.Columns(&sent, &messageLimit, &expiresTime); err != nil {
		return err
	}

	if sent >= messageLimit {
		return fmt.Errorf("SaveBroadcastMessage: broadcast allocation [%v, %v] is full: Sent: %v Limit: %v", aid, bid, sent, messageLimit)
	}
	err = expiresTime.CheckValid()
	if err != nil {
		return fmt.Errorf("SaveBroadcastMessage: unable to convert ExpiresTime for broadcast allocation %v: %v", bid, err)
	}
	exp := expiresTime.AsTime()
	if db.Now().After(exp) {
		return fmt.Errorf("SaveBroadcastMessage: broadcast allocation %v is expired: %v", bid, exp)
	}

	allocationCols := []string{"BroadcastID", "AllocationID", "Sent"}
	sentCols := []string{"BroadcastID", "ClientID"}
	ms := []*spanner.Mutation{spanner.Update(d.broadcastAllocations, allocationCols, []interface{}{bid.Bytes(), aid.Bytes(), sent+1})}
    ms = append(ms, spanner.InsertOrUpdate(d.broadcastSent, sentCols, []interface{}{bid.Bytes(), cid.Bytes()}))
	txn.BufferWrite(ms)
	return nil
}

// ListActiveBroadcasts implements db.BroadcastStore.
func (d *Datastore) ListActiveBroadcasts(ctx context.Context) ([]*db.BroadcastInfo, error) {
	//log.Error("+++ broadcaststore: ListActiveBroadcasts() called")
	now := db.NowProto()
	stmt := spanner.Statement{
		SQL: "SELECT " +
			"b.Broadcast, " +
			"b.Sent, " +
			"b.MessageLimit " +
			"FROM Broadcasts AS b " +
			"WHERE b.Sent < b.MessageLimit " +
			"AND (b.Broadcast.expiration_time IS NULL OR (b.Broadcast.expiration_time.seconds > @nowSec OR (b.Broadcast.expiration_time.seconds = @nowSec AND b.Broadcast.expiration_time.nanos > @nowNano)))",
		Params: map[string]interface{}{
			"nowSec":  int64(now.Seconds),
			"nowNano": int64(now.Nanos),
		},
	}

	var ret []*db.BroadcastInfo
	txn := d.dbClient.Single()
	defer txn.Close()
	iter := txn.Query(ctx, stmt)
	defer iter.Stop()

	for {
		row, err := iter.Next()
		if err == iterator.Done {
			return ret, nil
		}
		if err != nil {
			return ret, err
		}
		var broadcast *spb.Broadcast
		var sent, messageLimit int64
		if err := row.Columns(&broadcast, &sent, &messageLimit); err != nil {
			return ret, err
		}
		ret = append(ret, &db.BroadcastInfo{
			Broadcast: broadcast,
			Sent:      uint64(sent),
			Limit:     uint64(messageLimit),
		})
	}
}

// ListSentBroadcasts implements db.BroadcastStore.
func (d *Datastore) ListSentBroadcasts(ctx context.Context, id common.ClientID) ([]ids.BroadcastID, error) {
	var res []ids.BroadcastID
	ro := d.dbClient.ReadOnlyTransaction()
	defer ro.Close()
	krp := spanner.Key{id.Bytes()}.AsPrefix()
	iter := ro.Read(ctx, d.broadcastSent, krp, []string{"BroadcastID"})
	defer iter.Stop()
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return res, err
		}
		var b []byte
		if err := row.Columns(&b); err != nil {
			return res, err
		}
		bid, err := ids.BytesToBroadcastID(b)
		if err != nil {
			return res, err
		}
		res = append(res, bid)
	}
	return res, nil
}

// CreateAllocation implements db.BroadcastStore.
func (d *Datastore) CreateAllocation(ctx context.Context, id ids.BroadcastID, frac float32, expiry time.Time) (*db.AllocationInfo, error) {
	var ret *db.AllocationInfo
	_, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		var err error
		ret, err = d.tryCreateAllocation(ctx, txn, id, frac, expiry)
		return err
	})
	return ret, err
}

func (d *Datastore) tryCreateAllocation(ctx context.Context, txn *spanner.ReadWriteTransaction, id ids.BroadcastID, frac float32, expiry time.Time) (*db.AllocationInfo, error) {
	ep := tspb.New(expiry)
	err := ep.CheckValid()
	if err != nil {
		return nil, err
	}

	aid, err := ids.RandomAllocationID()
	if err != nil {
		return nil, err
	}

	row, err := txn.ReadRow(ctx, d.broadcasts, spanner.Key{id.Bytes()}, []string{"Sent", "Allocated", "MessageLimit"})
	if err != nil {
		return nil, err
	}

	var sent int64
	var allocated int64
	var messageLimit int64
    if err := row.Columns(&sent, &allocated, &messageLimit); err != nil {
		return nil, err
	}

	toAllocate, newAllocated := db.ComputeBroadcastAllocation(uint64(messageLimit), uint64(allocated), uint64(sent), frac)

	if toAllocate == 0 {
		return nil, nil
	}

	ai := &db.AllocationInfo{
		ID:     aid,
		Limit:  toAllocate,
		Expiry: expiry,
	}

	broadcastCols := []string{"BroadcastID", "Allocated"}
	allocationCols := []string{"BroadcastID", "AllocationID", "Sent", "MessageLimit", "ExpiresTime"}
	ms := []*spanner.Mutation{spanner.Update(d.broadcasts, broadcastCols, []interface{}{id.Bytes(), int64(newAllocated)})}
    ms = append(ms, spanner.InsertOrUpdate(d.broadcastAllocations, allocationCols, []interface{}{id.Bytes(), aid.Bytes(), 0, int64(toAllocate), ep}))
	txn.BufferWrite(ms)

	return ai, nil
}

// CleanupAllocation implements db.BroadcastStore.
func (d *Datastore) CleanupAllocation(ctx context.Context, bid ids.BroadcastID, aid ids.AllocationID) error {
	_, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		return d.tryCleanupAllocation(ctx, txn, bid, aid)
	})
	return err
}

func (d *Datastore) tryCleanupAllocation(ctx context.Context, txn *spanner.ReadWriteTransaction, bid ids.BroadcastID, aid ids.AllocationID) error {

	row, err := txn.ReadRow(ctx, d.broadcasts, spanner.Key{bid.Bytes()}, []string{"Allocated", "Sent"})
	if err != nil {
		return err
	}
	var allocated, bSent int64
	if err := row.Columns(&allocated, &bSent); err != nil {
		return err
	}

	row, err = txn.ReadRow(ctx, d.broadcastAllocations, spanner.Key{bid.Bytes(), aid.Bytes()}, []string{"MessageLimit", "Sent"})
	if err != nil {
		return err
	}
	var messageLimit, baSent int64
	if err := row.Columns(&messageLimit, &baSent); err != nil {
		return err
	}

	newAllocated, err := db.ComputeBroadcastAllocationCleanup(uint64(messageLimit), uint64(allocated))
	if err != nil {
		return fmt.Errorf("unable to clear allocation [%v,%v]: %v", bid, aid, err)
	}

	broadcastCols := []string{"BroadcastID", "Allocated", "Sent"}
	ms := []*spanner.Mutation{spanner.Update(d.broadcasts, broadcastCols, []interface{}{bid.Bytes(), int64(newAllocated), bSent+baSent})}
    ms = append(ms, spanner.Delete(d.broadcastAllocations, spanner.Key{bid.Bytes(), aid.Bytes()}))
	txn.BufferWrite(ms)

	return nil
}