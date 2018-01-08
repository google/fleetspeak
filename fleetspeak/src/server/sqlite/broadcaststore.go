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

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/ids"

	apb "github.com/golang/protobuf/ptypes/any"
	tpb "github.com/golang/protobuf/ptypes/timestamp"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// dbBroadcast matches the schema of the broadcasts table.
type dbBroadcast struct {
	broadcastID           string
	sourceServiceName     string
	messageType           string
	expirationTimeSeconds sql.NullInt64
	expirationTimeNanos   sql.NullInt64
	dataTypeURL           sql.NullString
	dataValue             []byte
	sent                  uint64
	allocated             uint64
	messageLimit          uint64
}

func fromBroadcastProto(b *spb.Broadcast) (*dbBroadcast, error) {
	if b == nil {
		return nil, errors.New("cannot convert nil Broadcast")
	}
	id, err := ids.BytesToBroadcastID(b.BroadcastId)
	if err != nil {
		return nil, err
	}
	if b.Source == nil {
		return nil, fmt.Errorf("Broadcast must have Source. Get: %v", b)
	}

	res := dbBroadcast{
		broadcastID:       id.String(),
		sourceServiceName: b.Source.ServiceName,
		messageType:       b.MessageType,
	}
	if b.ExpirationTime != nil {
		res.expirationTimeSeconds = sql.NullInt64{Int64: b.ExpirationTime.Seconds, Valid: true}
		res.expirationTimeNanos = sql.NullInt64{Int64: int64(b.ExpirationTime.Nanos), Valid: true}
	}
	if b.Data != nil {
		res.dataTypeURL = sql.NullString{String: b.Data.TypeUrl, Valid: true}
		res.dataValue = b.Data.Value
	}
	return &res, nil
}

func toBroadcastProto(b *dbBroadcast) (*spb.Broadcast, error) {
	bid, err := ids.StringToBroadcastID(b.broadcastID)
	if err != nil {
		return nil, err
	}
	ret := &spb.Broadcast{
		BroadcastId: bid.Bytes(),
		Source:      &fspb.Address{ServiceName: b.sourceServiceName},
		MessageType: b.messageType,
	}
	if b.expirationTimeSeconds.Valid && b.expirationTimeNanos.Valid {
		ret.ExpirationTime = &tpb.Timestamp{
			Seconds: b.expirationTimeSeconds.Int64,
			Nanos:   int32(b.expirationTimeNanos.Int64),
		}
	}
	if b.dataTypeURL.Valid {
		ret.Data = &apb.Any{
			TypeUrl: b.dataTypeURL.String,
			Value:   b.dataValue,
		}
	}
	return ret, nil
}

func (d *Datastore) CreateBroadcast(ctx context.Context, b *spb.Broadcast, limit uint64) error {
	d.l.Lock()
	defer d.l.Unlock()
	dbB, err := fromBroadcastProto(b)
	if err != nil {
		return err
	}
	dbB.messageLimit = limit
	return d.runInTx(func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO broadcasts("+
			"broadcast_id, "+
			"source_service_name, "+
			"message_type, "+
			"expiration_time_seconds, "+
			"expiration_time_nanos, "+
			"data_type_url, "+
			"data_value, "+
			"sent, "+
			"allocated, "+
			"message_limit) "+
			"VALUES(?, ?, ?, ?, ?, ?, ?, 0, 0, ?)",
			dbB.broadcastID,
			dbB.sourceServiceName,
			dbB.messageType,
			dbB.expirationTimeSeconds,
			dbB.expirationTimeNanos,
			dbB.dataTypeURL,
			dbB.dataValue,
			dbB.messageLimit,
		); err != nil {
			return err
		}
		for _, l := range b.RequiredLabels {
			if _, err := tx.ExecContext(ctx, "INSERT INTO broadcast_labels(broadcast_id, service_name, label) VALUES(?,?,?)", dbB.broadcastID, l.ServiceName, l.Label); err != nil {
				return err
			}

		}
		return nil
	})
}

func (d *Datastore) SetBroadcastLimit(ctx context.Context, id ids.BroadcastID, limit uint64) error {
	d.l.Lock()
	defer d.l.Unlock()
	return d.runInTx(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE broadcasts(message_limit) VALUES(?) WHERE broadcast_id=?", limit, id.String())
		return err
	})
}

func (d *Datastore) SaveBroadcastMessage(ctx context.Context, msg *fspb.Message, bID ids.BroadcastID, cID common.ClientID, aID ids.AllocationID) error {
	d.l.Lock()
	defer d.l.Unlock()
	dbm, err := fromMessageProto(msg)
	if err != nil {
		return err
	}

	return d.runInTx(func(tx *sql.Tx) error {
		var as, al uint64
		var exp tpb.Timestamp
		r := tx.QueryRowContext(ctx, "SELECT sent, message_limit, expiration_time_seconds, expiration_time_nanos FROM broadcast_allocations WHERE broadcast_id = ? AND allocation_id = ?", bID.String(), aID.String())
		if err := r.Scan(&as, &al, &exp.Seconds, &exp.Nanos); err != nil {
			return err
		}
		if as >= al {
			return fmt.Errorf("SaveBroadcastMessage: broadcast allocation [%v, %v] is full: Sent: %v Limit: %v", aID, bID, as, al)
		}
		et, err := ptypes.Timestamp(&exp)
		if err != nil {
			return fmt.Errorf("SaveBroadcastMessage: unable to convert expiry to time: %v", err)
		}
		if db.Now().After(et) {
			return fmt.Errorf("SaveBroadcastMessage: broadcast allocation [%v, %v] is expired: %v", aID, bID, et)
		}

		if err := d.tryStoreMessage(ctx, tx, dbm, true); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, "UPDATE broadcast_allocations SET sent = ? WHERE broadcast_id = ? AND allocation_id = ?", as+1, bID.String(), aID.String()); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO broadcast_sent(broadcast_id, client_id) VALUES (?, ?)", bID.String(), cID.String())
		return err
	})
}

func (d *Datastore) ListActiveBroadcasts(ctx context.Context) ([]*db.BroadcastInfo, error) {
	d.l.Lock()
	defer d.l.Unlock()
	var ret []*db.BroadcastInfo
	err := d.runInTx(func(tx *sql.Tx) error {
		now := db.NowProto()
		rs, err := tx.QueryContext(ctx, "SELECT "+
			"broadcast_id, "+
			"source_service_name, "+
			"message_type, "+
			"expiration_time_seconds, "+
			"expiration_time_nanos, "+
			"data_type_url, "+
			"data_value, "+
			"sent, "+
			"allocated, "+
			"message_limit "+
			"FROM broadcasts "+
			"WHERE sent < message_limit "+
			"AND (expiration_time_seconds IS NULL OR (expiration_time_seconds > ?) "+
			"OR (expiration_time_seconds = ? "+
			"AND expiration_time_nanos > ?))",
			now.Seconds, now.Seconds, now.Nanos)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var b dbBroadcast
			if err := rs.Scan(
				&b.broadcastID,
				&b.sourceServiceName,
				&b.messageType,
				&b.expirationTimeSeconds,
				&b.expirationTimeNanos,
				&b.dataTypeURL,
				&b.dataValue,
				&b.sent,
				&b.allocated,
				&b.messageLimit,
			); err != nil {
				return err
			}
			bp, err := toBroadcastProto(&b)
			if err != nil {
				return err
			}
			ret = append(ret, &db.BroadcastInfo{
				Broadcast: bp,
				Sent:      b.sent,
				Limit:     b.messageLimit,
			})
		}
		if err := rs.Err(); err != nil {
			return err
		}
		rs.Close()
		stmt, err := tx.Prepare("SELECT service_name, label FROM broadcast_labels WHERE broadcast_id = ?")
		defer stmt.Close()
		if err != nil {
			return err
		}
		for _, i := range ret {
			id, err := ids.BytesToBroadcastID(i.Broadcast.BroadcastId)
			if err != nil {
				return err
			}
			r, err := stmt.QueryContext(ctx, id.String())
			if err != nil {
				return err
			}
			for r.Next() {
				l := &fspb.Label{}
				if err := r.Scan(&l.ServiceName, &l.Label); err != nil {
					return err
				}
				i.Broadcast.RequiredLabels = append(i.Broadcast.RequiredLabels, l)
			}
			if err := r.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	return ret, err
}

func (d *Datastore) ListSentBroadcasts(ctx context.Context, id common.ClientID) ([]ids.BroadcastID, error) {
	d.l.Lock()
	defer d.l.Unlock()
	rs, err := d.db.QueryContext(ctx, "SELECT broadcast_id FROM broadcast_sent WHERE client_id = ?", id.String())
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var res []ids.BroadcastID
	for rs.Next() {
		var b string
		err = rs.Scan(&b)
		if err != nil {
			return nil, err
		}
		bID, err := ids.StringToBroadcastID(b)
		if err != nil {
			return nil, fmt.Errorf("ListSentBroadcasts: bad broadcast id for client %v: %v", id, b)
		}
		res = append(res, bID)
	}
	if err := rs.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

func (d *Datastore) CreateAllocation(ctx context.Context, id ids.BroadcastID, frac float32, expiry time.Time) (*db.AllocationInfo, error) {
	d.l.Lock()
	defer d.l.Unlock()
	var ret *db.AllocationInfo
	err := d.runInTx(func(tx *sql.Tx) error {
		ep, err := ptypes.TimestampProto(expiry)
		if err != nil {
			return err
		}
		aid, err := ids.RandomAllocationID()
		if err != nil {
			return err
		}

		var b dbBroadcast
		r := tx.QueryRowContext(ctx, "SELECT sent, allocated, message_limit FROM broadcasts WHERE broadcast_id = ?", id.String())
		if err := r.Scan(&b.sent, &b.allocated, &b.messageLimit); err != nil {
			return err
		}
		toAllocate, newAllocated := db.ComputeBroadcastAllocation(b.messageLimit, b.allocated, b.sent, frac)
		if toAllocate == 0 {
			return nil
		}

		if _, err := tx.ExecContext(ctx, "UPDATE broadcasts SET allocated = ? WHERE broadcast_id = ?", newAllocated, id.String()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO broadcast_allocations("+
			"broadcast_id, "+
			"allocation_id, "+
			"sent, "+
			"message_limit, "+
			"expiration_time_seconds, "+
			"expiration_time_nanos) "+
			"VALUES (?, ?, 0, ?, ?, ?) ",
			id.String(), aid.String(), toAllocate, ep.Seconds, ep.Nanos); err != nil {
			return err
		}

		ret = &db.AllocationInfo{
			ID:     aid,
			Limit:  toAllocate,
			Expiry: expiry,
		}
		return nil
	})
	return ret, err
}

func (d *Datastore) CleanupAllocation(ctx context.Context, bID ids.BroadcastID, aID ids.AllocationID) error {
	d.l.Lock()
	defer d.l.Unlock()
	return d.runInTx(func(tx *sql.Tx) error {
		var b dbBroadcast
		r := tx.QueryRowContext(ctx, "SELECT sent, allocated, message_limit FROM broadcasts WHERE broadcast_id = ?", bID.String())
		if err := r.Scan(&b.sent, &b.allocated, &b.messageLimit); err != nil {
			return err
		}

		var as, al uint64
		r = tx.QueryRowContext(ctx, "SELECT sent, message_limit FROM broadcast_allocations WHERE broadcast_id = ? AND allocation_id = ?", bID.String(), aID.String())
		if err := r.Scan(&as, &al); err != nil {
			return err
		}
		newAllocated, err := db.ComputeBroadcastAllocationCleanup(al, b.allocated)
		if err != nil {
			return fmt.Errorf("unable to clear allocation [%v,%v]: %v", bID.String(), aID.String(), err)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE broadcasts SET sent = ?, allocated = ? WHERE broadcast_id = ?", b.sent+as, newAllocated, bID.String()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE from broadcast_allocations WHERE broadcast_id = ? AND allocation_id = ?", bID.String(), aID.String()); err != nil {
			return err
		}
		return nil
	})
}
