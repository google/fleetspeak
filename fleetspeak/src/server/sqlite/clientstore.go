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

// Package sqlite implements the fleetspeak datastore interface using an sqlite
// database. It is meant for testing for small single-server deployments. In
// particular, having multiple servers using the same sqlite datastore is not
// supported.
package sqlite

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

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// ListClients implements db.ClientStore.
func (d *Datastore) ListClients(ctx context.Context) ([]*spb.Client, error) {
	d.l.Lock()
	defer d.l.Unlock()

	// Return value map, maps string client ids to the return values.
	retm := make(map[string]*spb.Client)

	err := d.runInTx(func(tx *sql.Tx) error {
		// Note all the client ids in the database.
		rows, err := tx.QueryContext(ctx, "SELECT client_id FROM clients ")
		if err != nil {
			return err
		}

		defer rows.Close()

		for rows.Next() {
			var sid string
			if err = rows.Scan(&sid); err != nil {
				return err
			}

			id, err := common.StringToClientID(sid)
			if err != nil {
				return err
			}

			retm[sid] = &spb.Client{
				ClientId: id.Bytes(),
			}
		}

		if err = rows.Err(); err != nil {
			return err
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

		lastRows, err := tx.QueryContext(ctx, "SELECT client_id, MAX(time) FROM client_contacts GROUP BY client_id")
		if err != nil {
			return err
		}

		defer lastRows.Close()
		for lastRows.Next() {
			var cid string
			var timeNS int64
			if err := lastRows.Scan(&cid, &timeNS); err != nil {
				return err
			}
			if timeNS != 0 {
				ts, err := ptypes.TimestampProto(time.Unix(0, timeNS))
				if err != nil {
					return err
				}
				retm[cid].LastContactTime = ts
			}
		}

		return labRows.Err() // This is normally nil.
	})

	var ret []*spb.Client
	for _, v := range retm {
		ret = append(ret, v)
	}

	return ret, err
}

// GetClientData implements db.ClientStore.
func (d *Datastore) GetClientData(ctx context.Context, id common.ClientID) (*db.ClientData, error) {
	d.l.Lock()
	defer d.l.Unlock()
	var cd *db.ClientData
	err := d.runInTx(func(tx *sql.Tx) error {
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
	d.l.Lock()
	defer d.l.Unlock()
	return d.runInTx(func(tx *sql.Tx) error {
		sid := id.String()
		if _, err := tx.ExecContext(ctx, "INSERT INTO clients(client_id, client_key) VALUES(?, ?)", sid, data.Key); err != nil {
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
	d.l.Lock()
	defer d.l.Unlock()
	_, err := d.db.ExecContext(ctx, "INSERT INTO client_labels(client_id, service_name, label) VALUES(?, ?, ?)", id.String(), l.ServiceName, l.Label)
	return err
}

// RemoveClientLabel implements db.ClientStore.
func (d *Datastore) RemoveClientLabel(ctx context.Context, id common.ClientID, l *fspb.Label) error {
	d.l.Lock()
	defer d.l.Unlock()
	_, err := d.db.ExecContext(ctx, "DELETE FROM client_labels WHERE client_id=? AND service_name=? AND label=?", id.String(), l.ServiceName, l.Label)
	return err
}

// RecordClientContact implements db.ClientStore.
func (d *Datastore) RecordClientContact(ctx context.Context, id common.ClientID, nonceSent, nonceReceived uint64, addr string) (db.ContactID, error) {
	d.l.Lock()
	defer d.l.Unlock()
	var res db.ContactID
	err := d.runInTx(func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, "INSERT INTO client_contacts(client_id, time, sent_nonce, received_nonce, address) VALUES(?, ?, ?, ?, ?)",
			id.String(), db.Now().UnixNano(), nonceSent, nonceReceived, addr)
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
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
	d.l.Lock()
	defer d.l.Unlock()
	return d.runInTx(func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, "INSERT INTO client_contact_messages(client_contact_id, message_id) VALUES (?, ?)", c, id.String()); err != nil {
				return err
			}
		}
		return nil
	})
}
