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
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"log"
	"context"
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

// ListClients implements db.ClientStore.
func (d *Datastore) ListClients(ctx context.Context, ids []common.ClientID) ([]*spb.Client, error) {
	// Return value map, maps string client ids to the return values.
	retm := make(map[string]*spb.Client)

	h := func(rows *sql.Rows, err error) error {
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sid string
			var timeNS int64
			var addr sql.NullString
			if err := rows.Scan(&sid, &timeNS, &addr); err != nil {
				return err
			}

			id, err := common.StringToClientID(sid)
			if err != nil {
				return err
			}

			ts, err := ptypes.TimestampProto(time.Unix(0, timeNS))
			if err != nil {
				return err
			}

			if !addr.Valid {
				addr.String = ""
			}

			retm[sid] = &spb.Client{
				ClientId:           id.Bytes(),
				LastContactTime:    ts,
				LastContactAddress: addr.String,
			}
		}
		return rows.Err()
	}

	err := d.runInTx(ctx, true, func(tx *sql.Tx) error {
		if len(ids) == 0 {
			if err := h(tx.QueryContext(ctx, "SELECT client_id, last_contact_time, last_contact_address FROM clients")); err != nil {
				return err
			}
		} else {
			for _, id := range ids {
				if err := h(tx.QueryContext(ctx, "SELECT client_id, last_contact_time, last_contact_address FROM clients WHERE client_id = ?", id.String())); err != nil {
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
			var sid string
			l := &fspb.Label{}
			if err := labRows.Scan(&sid, &l.ServiceName, &l.Label); err != nil {
				return err
			}

			retm[sid].Labels = append(retm[sid].Labels, l)
		}
		return nil
	})

	var ret []*spb.Client
	for _, v := range retm {
		ret = append(ret, v)
	}

	return ret, err
}

// GetClientData implements db.ClientStore.
func (d *Datastore) GetClientData(ctx context.Context, id common.ClientID) (*db.ClientData, error) {
	var cd *db.ClientData
	err := d.runInTx(ctx, true, func(tx *sql.Tx) error {
		sid := id.String()

		r := tx.QueryRowContext(ctx, "SELECT client_key FROM clients WHERE client_id=?", sid)
		var c db.ClientData

		err := r.Scan(&c.Key)
		if err != nil {
			return err
		}

		rs, err := tx.QueryContext(ctx, "SELECT service_name, label FROM client_labels WHERE client_id=?", sid)
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

// AddClient implements db.ClientStore.
func (d *Datastore) AddClient(ctx context.Context, id common.ClientID, data *db.ClientData) error {
	return d.runInTx(ctx, false, func(tx *sql.Tx) error {
		sid := id.String()
		if _, err := tx.ExecContext(ctx, "INSERT INTO clients(client_id, client_key, last_contact_time) VALUES(?, ?, ?)", sid, data.Key, db.Now().UnixNano()); err != nil {
			return err
		}
		for _, l := range data.Labels {
			if _, err := tx.ExecContext(ctx, "INSERT INTO client_labels(client_id, service_name, label) VALUES(?, ?, ?)", sid, l.ServiceName, l.Label); err != nil {
				return err
			}
		}
		return nil
	})
}

// AddClientLabel implements db.ClientStore.
func (d *Datastore) AddClientLabel(ctx context.Context, id common.ClientID, l *fspb.Label) error {
	_, err := d.db.ExecContext(ctx, "INSERT INTO client_labels(client_id, service_name, label) VALUES(?, ?, ?)", id.String(), l.ServiceName, l.Label)
	return err
}

// RemoveClientLabel implements db.ClientStore.
func (d *Datastore) RemoveClientLabel(ctx context.Context, id common.ClientID, l *fspb.Label) error {
	_, err := d.db.ExecContext(ctx, "DELETE FROM client_labels WHERE client_id=? AND service_name=? AND label=?", id.String(), l.ServiceName, l.Label)
	return err
}

// RecordClientContact implements db.ClientStore.
func (d *Datastore) RecordClientContact(ctx context.Context, cid common.ClientID, nonceSent, nonceReceived uint64, addr string) (db.ContactID, error) {
	var res db.ContactID
	err := d.runInTx(ctx, false, func(tx *sql.Tx) error {
		n := db.Now().UnixNano()
		r, err := tx.ExecContext(ctx, "INSERT INTO client_contacts(client_id, time, sent_nonce, received_nonce, address) VALUES(?, ?, ?, ?, ?)",
			cid.String(), n, nonceSent, nonceReceived, addr)
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE clients SET last_contact_time = ?, last_contact_address = ? WHERE client_id = ?", n, addr, cid.String()); err != nil {
			return err
		}
		res = db.ContactID(strconv.FormatUint(uint64(id), 16))
		return nil
	})
	return res, err
}

// LinkMessagesToContact implements db.ClientStore.
func (d *Datastore) LinkMessagesToContact(ctx context.Context, contact db.ContactID, ids []common.MessageID) error {
	c, err := strconv.ParseUint(string(contact), 16, 64)
	if err != nil {
		e := fmt.Errorf("unable to parse ContactID [%v]: %v", contact, err)
		log.Print(e)
		return e
	}
	return d.runInTx(ctx, false, func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, "INSERT INTO client_contact_messages(client_contact_id, message_id) VALUES (?, ?)", c, id.String()); err != nil {
				return err
			}
		}
		return nil
	})
}

// RecordResourceUsageData implements db.ClientStore.
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
			id.String(),
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

// FetchResourceUsageRecords implements db.ClientStore.
func (d *Datastore) FetchResourceUsageRecords(ctx context.Context, id common.ClientID, limit int) ([]*spb.ClientResourceUsageRecord, error) {
	var records []*spb.ClientResourceUsageRecord
	err := d.runInTx(ctx, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(
			ctx,
			"SELECT "+
				"scope, pid, process_start_time, client_timestamp, server_timestamp, "+
				"mean_user_cpu_rate, max_user_cpu_rate, mean_system_cpu_rate, "+
				"max_system_cpu_rate, mean_resident_memory_mib, max_resident_memory_mib "+
				"FROM client_resource_usage_records WHERE client_id=? LIMIT ?",
			id.String(),
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
