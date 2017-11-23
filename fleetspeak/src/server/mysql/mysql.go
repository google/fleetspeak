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

// Package mysql implements the fleetspeak datastore interface using a mysql
// database.
//
// NOTE: Currently this is a fairly direct port of the sqlite datastore and not
// at all performant. TODO: Optimize and load test this.
package mysql

import (
	"database/sql"

	"log"
	"context"
)

// Datastore wraps a mysql backed sql.DB and implements db.Store.
type Datastore struct {
	db     *sql.DB
	looper *messageLooper
}

// MakeDatastore creates any missing tables and returns a Datastore. The db
// parameter must be connected to a mysql database, e.g. using the mymysql
// driver.
func MakeDatastore(db *sql.DB) (*Datastore, error) {
	err := initDB(db)
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
func (d *Datastore) runInTx(ctx context.Context, readOnly bool, f func(*sql.Tx) error) error {
	// TODO: Pass along the readOnly flag, once some mysql driver
	// supports it.
	tx, err := d.db.BeginTx(ctx, nil)
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
	db.SetMaxIdleConns(100)
	db.SetMaxOpenConns(100)
	return nil
}

func initSchema(db *sql.DB) error {
	for _, s := range []string{
		`CREATE TABLE IF NOT EXISTS clients(
client_id CHAR(16) PRIMARY KEY,
client_key BLOB,
last_contact_time BIGINT NOT NULL,
last_contact_address TEXT(64))`,
		`CREATE TABLE IF NOT EXISTS client_labels(
client_id CHAR(16) NOT NULL,
service_name VARCHAR(128) NOT NULL,
label VARCHAR(128) NOT NULL,
PRIMARY KEY (client_id, service_name, label),
FOREIGN KEY (client_id) REFERENCES clients(client_id))`,
		`CREATE TABLE IF NOT EXISTS client_contacts(
client_contact_id INTEGER NOT NULL AUTO_INCREMENT,
client_id CHAR(16) NOT NULL,
time BIGINT NOT NULL,
sent_nonce VARCHAR(16) NOT NULL,
received_nonce VARCHAR(16) NOT NULL,
address VARCHAR(64),
PRIMARY KEY (client_contact_id),
FOREIGN KEY (client_id) REFERENCES clients(client_id))`,
		`CREATE TABLE IF NOT EXISTS client_resource_usage_records(
client_id CHAR(16) NOT NULL,
scope VARCHAR(128) NOT NULL,
pid BIGINT,
process_start_time BIGINT,
client_timestamp BIGINT,
server_timestamp BIGINT,
mean_user_cpu_rate REAL,
max_user_cpu_rate REAL,
mean_system_cpu_rate REAL,
max_system_cpu_rate REAL,
mean_resident_memory_mib INT4,
max_resident_memory_mib INT4,
FOREIGN KEY (client_id) REFERENCES clients(client_id))`,
		`CREATE TABLE IF NOT EXISTS messages(
message_id CHAR(64) NOT NULL,
source_client_id CHAR(16) NOT NULL,
source_service_name VARCHAR(128) NOT NULL,
source_message_id VARCHAR(32) NOT NULL,
destination_client_id CHAR(16),
destination_service_name VARCHAR(128) NOT NULL,
message_type VARCHAR(128),
creation_time_seconds BIGINT NOT NULL,
creation_time_nanos INT NOT NULL,
processed_time_seconds BIGINT,
processed_time_nanos INT,
validation_info VARCHAR(256),
failed INT1,
failed_reason TEXT,
PRIMARY KEY (message_id))`,
		`CREATE TABLE IF NOT EXISTS pending_messages(
for_server BOOL NOT NULL,
message_id CHAR(64) NOT NULL,
retry_count INT NOT NULL,
scheduled_time BIGINT NOT NULL,
data_type_url TEXT,
data_value BLOB,
PRIMARY KEY (for_server, message_id),
FOREIGN KEY (message_id) REFERENCES messages(message_id))`,
		`CREATE TABLE IF NOT EXISTS client_contact_messages(
client_contact_id INTEGER NOT NULL,
message_id CHAR(64) NOT NULL,
PRIMARY KEY (client_contact_id, message_id),
FOREIGN KEY (client_contact_id) REFERENCES client_contacts(client_contact_id),
FOREIGN KEY (message_id) REFERENCES messages(message_id))`,
		`CREATE TABLE IF NOT EXISTS broadcasts(
broadcast_id CHAR(32) NOT NULL,
source_service_name VARCHAR(128) NOT NULL,
message_type VARCHAR(128) NOT NULL,
expiration_time_seconds BIGINT,
expiration_time_nanos INT,
data_type_url TEXT,
data_value BLOB,
sent BIGINT UNSIGNED,
allocated BIGINT UNSIGNED,
message_limit BIGINT UNSIGNED,
PRIMARY KEY (broadcast_id))`,
		`CREATE TABLE IF NOT EXISTS broadcast_labels(
broadcast_id VARCHAR(32) NOT NULL,
service_name VARCHAR(128) NOT NULL,
label VARCHAR(128) NOT NULL,
PRIMARY KEY (broadcast_id, service_name, label),
FOREIGN KEY (broadcast_id) REFERENCES broadcasts(broadcast_id))`,
		`CREATE TABLE IF NOT EXISTS broadcast_allocations(
broadcast_id VARCHAR(32) NOT NULL,
allocation_id VARCHAR(16) NOT NULL,
sent BIGINT UNSIGNED,
message_limit BIGINT UNSIGNED,
expiration_time_seconds BIGINT,
expiration_time_nanos INT,
PRIMARY KEY (broadcast_id, allocation_id),
FOREIGN KEY (broadcast_id) REFERENCES broadcasts(broadcast_id))`,
		`CREATE TABLE IF NOT EXISTS broadcast_sent(
broadcast_id VARCHAR(32) NOT NULL,
client_id CHAR(16) NOT NULL,
PRIMARY KEY (client_id, broadcast_id),
FOREIGN KEY (broadcast_id) REFERENCES broadcasts(broadcast_id),
FOREIGN KEY (client_id) REFERENCES clients(client_id))`,
		`CREATE TABLE IF NOT EXISTS files(
service VARCHAR(128) NOT NULL,
name VARCHAR(256) NOT NULL,
modified_time_nanos BIGINT NOT NULL,
data BLOB,
PRIMARY KEY (service, name))
`,
	} {
		if _, err := db.Exec(s); err != nil {
			log.Printf("Error [%v] creating table: \n%v", err, s)
			return err
		}
	}

	return nil
}
