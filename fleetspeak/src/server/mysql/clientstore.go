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

package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"

	tspb "github.com/golang/protobuf/ptypes/timestamp"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

const (
	bytesToMIB = 1.0 / float64(1<<20)
)

func (d *Datastore) ListClients(ctx context.Context, ids []common.ClientID) ([]*spb.Client, error) {
	// Return value map, maps string client ids to the return values.
	var retm map[string]*spb.Client

	h := func(rows *sql.Rows, err error) error {
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id []byte
			var timeNS int64
			var addr sql.NullString
			var clockSecs, clockNanos sql.NullInt64
			var blacklisted bool
			if err := rows.Scan(&id, &timeNS, &addr, &clockSecs, &clockNanos, &blacklisted); err != nil {
				return err
			}

			ts, err := ptypes.TimestampProto(time.Unix(0, timeNS))
			if err != nil {
				return err
			}

			if !addr.Valid {
				addr.String = ""
			}

			var lastClock *tspb.Timestamp
			if clockSecs.Valid && clockNanos.Valid {
				lastClock = &tspb.Timestamp{
					Seconds: clockSecs.Int64,
					Nanos:   int32(clockNanos.Int64),
				}
			}
			retm[string(id)] = &spb.Client{
				ClientId:           id,
				LastContactTime:    ts,
				LastContactAddress: addr.String,
				LastClock:          lastClock,
				Blacklisted:        blacklisted,
			}
		}
		return rows.Err()
	}

	err := d.runInTx(ctx, true, func(tx *sql.Tx) error {
		retm = make(map[string]*spb.Client)
		if len(ids) == 0 {
			if err := h(tx.QueryContext(ctx, "SELECT client_id, last_contact_time, last_contact_address, last_clock_seconds, last_clock_nanos, blacklisted FROM clients")); err != nil {
				return err
			}
		} else {
			for _, id := range ids {
				if err := h(tx.QueryContext(ctx, "SELECT client_id, last_contact_time, last_contact_address, last_clock_seconds, last_clock_nanos, blacklisted FROM clients WHERE client_id = ?", id.Bytes())); err != nil {
					return err
				}
			}
		}

		// Match all the labels in the database with the client ids noted in the
		// previous step. Note that clients.client_id is a foreign key of
		// client_labels.
		labRows, err := tx.QueryContext(ctx, "SELECT client_id, service_name, label FROM client_labels")
		if err != nil {
			return err
		}

		defer labRows.Close()

		for labRows.Next() {
			var id []byte
			l := &fspb.Label{}
			if err := labRows.Scan(&id, &l.ServiceName, &l.Label); err != nil {
				return err
			}

			retm[string(id)].Labels = append(retm[string(id)].Labels, l)
		}
		return nil
	})

	var ret []*spb.Client
	for _, v := range retm {
		ret = append(ret, v)
	}

	return ret, err
}

func (d *Datastore) GetClientData(ctx context.Context, id common.ClientID) (*db.ClientData, error) {
	var cd *db.ClientData
	err := d.runInTx(ctx, true, func(tx *sql.Tx) error {
		iid := id.Bytes()

		r := tx.QueryRowContext(ctx, "SELECT client_key, blacklisted FROM clients WHERE client_id=?", iid)
		var c db.ClientData

		err := r.Scan(&c.Key, &c.Blacklisted)
		if err != nil {
			return err
		}

		rs, err := tx.QueryContext(ctx, "SELECT service_name, label FROM client_labels WHERE client_id=?", iid)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			l := &fspb.Label{}
			err = rs.Scan(&l.ServiceName, &l.Label)
			if err != nil {
				return err
			}
			c.Labels = append(c.Labels, l)
		}
		if err := rs.Err(); err != nil {
			return err
		}
		cd = &c
		return nil
	})
	return cd, err
}

func (d *Datastore) AddClient(ctx context.Context, id common.ClientID, data *db.ClientData) error {
	return d.runInTx(ctx, false, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO clients(client_id, client_key, blacklisted, last_contact_time) VALUES(?, ?, 'FALSE', ?)", id.Bytes(), data.Key, db.Now().UnixNano()); err != nil {
			return err
		}
		for _, l := range data.Labels {
			if _, err := tx.ExecContext(ctx, "INSERT INTO client_labels(client_id, service_name, label) VALUES(?, ?, ?)", id.Bytes(), l.ServiceName, l.Label); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *Datastore) AddClientLabel(ctx context.Context, id common.ClientID, l *fspb.Label) error {
	return d.runInTx(ctx, false, func(tx *sql.Tx) error {
		_, err := d.db.ExecContext(ctx, "INSERT INTO client_labels(client_id, service_name, label) VALUES(?, ?, ?)", id.Bytes(), l.ServiceName, l.Label)
		return err
	})
}

func (d *Datastore) RemoveClientLabel(ctx context.Context, id common.ClientID, l *fspb.Label) error {
	return d.runInTx(ctx, false, func(tx *sql.Tx) error {
		_, err := d.db.ExecContext(ctx, "DELETE FROM client_labels WHERE client_id=? AND service_name=? AND label=?", id.Bytes(), l.ServiceName, l.Label)
		return err
	})
}

func (d *Datastore) BlacklistClient(ctx context.Context, id common.ClientID) error {
	return d.runInTx(ctx, false, func(tx *sql.Tx) error {
		_, err := d.db.ExecContext(ctx, "UPDATE clients SET blacklisted=TRUE WHERE client_id=?", id.Bytes())
		return err
	})
}

func (d *Datastore) RecordClientContact(ctx context.Context, data db.ContactData) (db.ContactID, error) {
	var res db.ContactID
	err := d.runInTx(ctx, false, func(tx *sql.Tx) error {
		n := db.Now().UnixNano()
		r, err := tx.ExecContext(ctx, "INSERT INTO client_contacts(client_id, time, sent_nonce, received_nonce, address) VALUES(?, ?, ?, ?, ?)",
			data.ClientID.Bytes(), n, data.NonceSent, data.NonceReceived, data.Addr)
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		var lcs, lcn sql.NullInt64
		if data.ClientClock != nil {
			lcs.Int64, lcs.Valid = data.ClientClock.Seconds, true
			lcn.Int64, lcn.Valid = int64(data.ClientClock.Nanos), true
		}
		if _, err := tx.ExecContext(ctx, "UPDATE clients SET last_contact_time = ?, last_contact_address = ?, last_clock_seconds = ?, last_clock_nanos = ? WHERE client_id = ?", n, data.Addr, lcs, lcn, data.ClientID.Bytes()); err != nil {
			return err
		}
		res = db.ContactID(strconv.FormatUint(uint64(id), 16))
		return nil
	})
	return res, err
}

func (d *Datastore) ListClientContacts(ctx context.Context, id common.ClientID) ([]*spb.ClientContact, error) {
	var res []*spb.ClientContact
	if err := d.runInTx(ctx, true, func(tx *sql.Tx) error {
		res = nil
		rows, err := tx.QueryContext(
			ctx,
			"SELECT time, sent_nonce, received_nonce, address FROM client_contacts WHERE client_id = ?",
			id.Bytes())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var addr sql.NullString
			var timeNS int64
			c := &spb.ClientContact{}
			if err := rows.Scan(&timeNS, &c.SentNonce, &c.ReceivedNonce, &addr); err != nil {
				return err
			}

			if addr.Valid {
				c.ObservedAddress = addr.String
			}

			ts, err := ptypes.TimestampProto(time.Unix(0, timeNS))
			if err != nil {
				return err
			}
			c.Timestamp = ts

			res = append(res, c)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return res, nil
}

func (d *Datastore) LinkMessagesToContact(ctx context.Context, contact db.ContactID, ids []common.MessageID) error {
	c, err := strconv.ParseUint(string(contact), 16, 64)
	if err != nil {
		e := fmt.Errorf("unable to parse ContactID [%v]: %v", contact, err)
		log.Error(e)
		return e
	}
	return d.runInTx(ctx, false, func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, "INSERT INTO client_contact_messages(client_contact_id, message_id) VALUES (?, ?)", c, id.Bytes()); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *Datastore) RecordResourceUsageData(ctx context.Context, id common.ClientID, rud mpb.ResourceUsageData) error {
	processStartTime, err := ptypes.Timestamp(rud.ProcessStartTime)
	if err != nil {
		return fmt.Errorf("failed to parse process start time: %v", err)
	}
	clientTimestamp, err := ptypes.Timestamp(rud.DataTimestamp)
	if err != nil {
		return fmt.Errorf("failed to parse data timestamp: %v", err)
	}
	return d.runInTx(ctx, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(
			ctx,
			"INSERT INTO client_resource_usage_records VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			id.Bytes(),
			rud.Scope,
			rud.Pid,
			processStartTime.UnixNano(),
			clientTimestamp.UnixNano(),
			db.Now().UnixNano(),
			rud.ResourceUsage.MeanUserCpuRate,
			rud.ResourceUsage.MaxUserCpuRate,
			rud.ResourceUsage.MeanSystemCpuRate,
			rud.ResourceUsage.MaxSystemCpuRate,
			int32(rud.ResourceUsage.MeanResidentMemory*bytesToMIB),
			int32(float64(rud.ResourceUsage.MaxResidentMemory)*bytesToMIB))
		return err
	})
}

func (d *Datastore) FetchResourceUsageRecords(ctx context.Context, id common.ClientID, limit int) ([]*spb.ClientResourceUsageRecord, error) {
	var records []*spb.ClientResourceUsageRecord
	err := d.runInTx(ctx, true, func(tx *sql.Tx) error {
		records = nil
		rows, err := tx.QueryContext(
			ctx,
			"SELECT "+
				"scope, pid, process_start_time, client_timestamp, server_timestamp, "+
				"mean_user_cpu_rate, max_user_cpu_rate, mean_system_cpu_rate, "+
				"max_system_cpu_rate, mean_resident_memory_mib, max_resident_memory_mib "+
				"FROM client_resource_usage_records WHERE client_id=? LIMIT ?",
			id.Bytes(),
			limit)

		if err != nil {
			return err
		}

		defer rows.Close()

		for rows.Next() {
			record := &spb.ClientResourceUsageRecord{}
			var processStartTime, clientTimestamp, serverTimestamp int64
			err := rows.Scan(
				&record.Scope, &record.Pid, &processStartTime, &clientTimestamp, &serverTimestamp,
				&record.MeanUserCpuRate, &record.MaxUserCpuRate, &record.MeanSystemCpuRate,
				&record.MaxSystemCpuRate, &record.MeanResidentMemoryMib, &record.MaxResidentMemoryMib)

			if err != nil {
				return err
			}

			record.ProcessStartTime = timestampProto(processStartTime)
			record.ClientTimestamp = timestampProto(clientTimestamp)
			record.ServerTimestamp = timestampProto(serverTimestamp)
			records = append(records, record)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

func timestampProto(nanos int64) *tspb.Timestamp {
	return &tspb.Timestamp{
		Seconds: nanos / time.Second.Nanoseconds(),
		Nanos:   int32(nanos % time.Second.Nanoseconds()),
	}
}
