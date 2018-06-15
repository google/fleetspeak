// Copyright 2018 Google Inc.
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

// Package main contains a plugin which provides a mysql based datastore.
package main

import (
	"database/sql"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/mysql"

	// We access the driver through sql.Open, but need to bring in the
	// dependency.
	_ "github.com/go-sql-driver/mysql"
)

// Factory is a plugins.DatabaseFactory which returns a mysql based
// database. Its configuration string should be a connection string as accepted
// by the mysql driver, e.g.: "<user>:<pass>@tcp(<host>)/<db>".
//
// See https://github.com/go-sql-driver/mysql#usage for more details.
func Factory(cs string) (db.Store, error) {
	c, err := sql.Open("mysql", cs)
	if err != nil {
		return nil, err
	}
	return mysql.MakeDatastore(c)
}

func main() {
	log.Exitf("unimplemented")
}
