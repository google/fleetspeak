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
	"bytes"
	"context"
	"database/sql"
	"io"
	"io/ioutil"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/server/db"
)

func (d *Datastore) StoreFile(ctx context.Context, service, name string, data io.Reader) error {
	b, err := ioutil.ReadAll(data)
	if err != nil {
		return err
	}
	d.l.Lock()
	defer d.l.Unlock()
	return d.runInTx(func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO files (service, name, modified_time_nanos, data) VALUES(?, ?, ?, ?)",
			service, name, db.Now().UnixNano(), b)
		return err
	})
}

func (d *Datastore) StatFile(ctx context.Context, service, name string) (time.Time, error) {
	d.l.Lock()
	defer d.l.Unlock()

	var ts int64

	err := d.runInTx(func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, "SELECT modified_time_nanos FROM files WHERE service = ? AND name = ?", service, name)
		return row.Scan(&ts)
	})

	return time.Unix(0, ts).UTC(), err
}

func (d *Datastore) ReadFile(ctx context.Context, service, name string) (data db.ReadSeekerCloser, modtime time.Time, err error) {
	d.l.Lock()
	defer d.l.Unlock()

	var b []byte
	var ts int64

	err = d.runInTx(func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, "SELECT modified_time_nanos, data FROM files WHERE service = ? AND name = ?", service, name)
		if err := row.Scan(&ts, &b); err != nil {
			b = nil
			return err
		}
		return nil
	})

	if err != nil {
		return nil, time.Time{}, err
	}
	if b == nil {
		b = []byte{}
	}
	return db.NOOPCloser{bytes.NewReader(b)}, time.Unix(0, ts).UTC(), nil
}
