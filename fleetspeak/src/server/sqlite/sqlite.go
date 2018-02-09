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
	"sync"

	log "github.com/golang/glog"

	// We access the driver through sql.Open, but need to bring in the
	// dependency.
	_ "github.com/mattn/go-sqlite3"
)

// Datastore wraps an sqlite database and implements db.Store.
type Datastore struct {
	db *sql.DB

	// We serialize access to the database to eliminate database locked
	// errors when using this in a multithreaded context.
	l sync.Mutex

	looper *messageLooper
}

// MakeDatastore opens the given sqlite database file and creates any missing
// tables.
func MakeDatastore(fileName string) (*Datastore, error) {
	log.Infof("Opening sql database: %v", fileName)
	db, err := sql.Open("sqlite3", fileName)
	if err != nil {
		return nil, err
	}
	err = initDB(db)
	if err != nil {
		return nil, err
	}
	err = initSchema(db)
	if err != nil {
		return nil, err
	}
	return &Datastore{db: db}, nil
}

// Close closes the underlying database resources.
func (d *Datastore) Close() error {
	return d.db.Close()
}

// runInTx runs f, passing it a transaction. The transaction will be committed
// if f returns an error, otherwise rolled back.
func (d *Datastore) runInTx(f func(*sql.Tx) error) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	err = f(tx)
	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// IsNotFound implements db.Store.
func (d *Datastore) IsNotFound(err error) bool {
	return err == sql.ErrNoRows
}

func initDB(db *sql.DB) error {
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return err
	}
	return nil
}

func initSchema(db *sql.DB) error {
	for _, s := range []string{
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS clients(
client_id TEXT(16) PRIMARY KEY,
client_key BLOB,
blacklisted BOOLEAN NOT NULL,
last_contact_time INT8 NOT NULL,
last_contact_address TEXT(64),
last_clock_seconds INT8,
last_clock_nanos INT4)`,
		`CREATE TABLE IF NOT EXISTS client_labels(
client_id TEXT(16) NOT NULL,
service_name TEXT(128) NOT NULL,
label TEXT(128) NOT NULL,
PRIMARY KEY (client_id, service_name, label),
FOREIGN KEY (client_id) REFERENCES clients(client_id))
WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS client_contacts(
client_contact_id INTEGER NOT NULL,
client_id TEXT(16) NOT NULL,
time INT8 NOT NULL,
sent_nonce TEXT(16) NOT NULL,
received_nonce TEXT(16) NOT NULL,
address TEXT(64),
-- We want auto-increment functionality, and get it because an INTEGER primary key
-- becomes the ROWID.
PRIMARY KEY (client_contact_id),
FOREIGN KEY (client_id) REFERENCES clients(client_id))`,
		`CREATE TABLE IF NOT EXISTS client_resource_usage_records(
client_id TEXT(16) NOT NULL,
scope TEXT(128) NOT NULL,
pid INT8,
process_start_time INT8,
client_timestamp INT8,
server_timestamp INT8,
mean_user_cpu_rate REAL,
max_user_cpu_rate REAL,
mean_system_cpu_rate REAL,
max_system_cpu_rate REAL,
mean_resident_memory_mib INT4,
max_resident_memory_mib INT4,
FOREIGN KEY (client_id) REFERENCES clients(client_id))`,
		`CREATE TABLE IF NOT EXISTS messages(
message_id TEXT(64) NOT NULL,
source_client_id TEXT(16) NOT NULL,
source_service_name TEXT(128) NOT NULL,
source_message_id TEXT(32) NOT NULL,
destination_client_id TEXT(16),
destination_service_name TEXT(128) NOT NULL,
message_type TEXT(128),
creation_time_seconds INT8 NOT NULL,
creation_time_nanos INT4 NOT NULL,
processed_time_seconds INT8,
processed_time_nanos INT4,
validation_info BLOB,
failed INT1,
failed_reason TEXT,
PRIMARY KEY (message_id))`,
		`CREATE TABLE IF NOT EXISTS pending_messages(
message_id TEXT(64) NOT NULL,
retry_count INT4 NOT NULL,
scheduled_time INT8 NOT NULL,
data_type_url TEXT,
data_value BLOB,
PRIMARY KEY (message_id),
FOREIGN KEY (message_id) REFERENCES messages(message_id))`,
		`CREATE TABLE IF NOT EXISTS client_contact_messages(
client_contact_id INTEGER NOT NULL,
message_id TEXT(64) NOT NULL,
PRIMARY KEY (client_contact_id, message_id),
FOREIGN KEY (client_contact_id) REFERENCES client_contacts(client_contact_id),
FOREIGN KEY (message_id) REFERENCES messages(message_id))
WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS broadcasts(
broadcast_id TEXT(32) NOT NULL,
source_service_name TEXT(128) NOT NULL,
message_type TEXT(128) NOT NULL,
expiration_time_seconds INT8,
expiration_time_nanos INT4,
data_type_url TEXT,
data_value BLOB,
sent UINT8,
allocated UINT8,
message_limit UINT8,
PRIMARY KEY (broadcast_id))`,
		`CREATE TABLE IF NOT EXISTS broadcast_labels(
broadcast_id TEXT(32) NOT NULL,
service_name TEXT(128) NOT NULL,
label TEXT(128) NOT NULL,
PRIMARY KEY (broadcast_id, service_name, label),
FOREIGN KEY (broadcast_id) REFERENCES broadcasts(broadcast_id))
WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS broadcast_allocations(
broadcast_id TEXT(32) NOT NULL,
allocation_id TEXT(16) NOT NULL,
sent UINT8,
message_limit UINT8,
expiration_time_seconds INT8,
expiration_time_nanos INT4,
PRIMARY KEY (broadcast_id, allocation_id),
FOREIGN KEY (broadcast_id) REFERENCES broadcasts(broadcast_id))
WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS broadcast_sent(
broadcast_id TEXT(32) NOT NULL,
client_id TEXT(16) NOT NULL,
PRIMARY KEY (client_id, broadcast_id),
FOREIGN KEY (broadcast_id) REFERENCES broadcasts(broadcast_id),
FOREIGN KEY (client_id) REFERENCES clients(client_id))
WITHOUT ROWID`,
		`CREATE TABLE IF NOT EXISTS files(
service TEXT(128) NOT NULL,
name TEST(256) NOT NULL,
modified_time_nanos INT8 NOT NULL,
data BLOB,
PRIMARY KEY (service, name))
`,
	} {
		if _, err := db.Exec(s); err != nil {
			log.Errorf("Error [%v] creating table: \n%v", err, s)
			return err
		}
	}

	return nil
}
