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
	"bytes"
	"context"
	"io"
	"time"

	"cloud.google.com/go/spanner"

	"github.com/google/fleetspeak/fleetspeak/src/server/db"

	tpb "google.golang.org/protobuf/types/known/timestamppb"
)

// StoreFile implements db.FileStore.
func (d *Datastore) StoreFile(ctx context.Context, service, name string, data io.Reader) error {
	b, err := io.ReadAll(data)
	if err != nil {
		return err
	}

	_, err = d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		d.tryStoreFile(txn, service, name, b)
		return nil
	})
	return err
}

func (d *Datastore) tryStoreFile(txn *spanner.ReadWriteTransaction, service, name string, data []byte) {
	m := spanner.InsertOrUpdate(d.files, []string{"Service", "Name", "ModifiedTime", "Data"}, []interface{}{service, name, db.NowProto(), data})
	txn.BufferWrite([]*spanner.Mutation{m})
}

// StatFile implements db.FileStore.
func (d *Datastore) StatFile(ctx context.Context, service, name string) (time.Time, error) {
	txn := d.dbClient.Single()
	defer txn.Close()
	row, err := txn.ReadRow(ctx, d.files, spanner.Key{service, name}, []string{"ModifiedTime"})
	if err != nil {
		return time.Time{}, err
	}
	var tp tpb.Timestamp
	err = row.Columns(&tp)
	if err != nil {
		return time.Time{}, err
	}
	err = (&tp).CheckValid()
	if err != nil {
		return time.Time{}, err
	}
	ts := (&tp).AsTime()

	return ts.UTC(), nil
}

// ReadFile implements db.FileStore.
func (d *Datastore) ReadFile(ctx context.Context, service, name string) (data db.ReadSeekerCloser, modtime time.Time, err error) {
	txn := d.dbClient.Single()
	defer txn.Close()
	row, err := txn.ReadRow(ctx, d.files, spanner.Key{service, name}, []string{"ModifiedTime", "Data"})
	if err != nil {
		return nil, time.Time{}, err
	}
	var tp tpb.Timestamp
	var bs []byte

	err = row.Columns(&tp, &bs)
	if err != nil {
		return nil, time.Time{}, err
	}
	err = (&tp).CheckValid()
	if err != nil {
		return nil, time.Time{}, err
	}
	ts := (&tp).AsTime()

	return db.NOOPCloser{ReadSeeker: bytes.NewReader(bs)}, ts.UTC(), nil
}