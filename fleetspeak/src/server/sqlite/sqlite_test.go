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
	"fmt"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"path"
	"testing"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/dbtesting"
)

type sqliteTestEnv struct {
	tempDirCleanup func()
	counter        int
}

func (e *sqliteTestEnv) Create() error {
	return nil
}

func (e *sqliteTestEnv) Clean() (db.Store, error) {
	dir, fn := comtesting.GetTempDir("sqlite_test")
	e.tempDirCleanup = fn

	p := path.Join(dir, fmt.Sprintf("%d.sqlite", e.counter))
	e.counter++

	log.Infof("Using database: %v", p)
	s, err := MakeDatastore(p)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (e *sqliteTestEnv) Destroy() error {
	e.tempDirCleanup()
	return nil
}

func TestSqliteStore(t *testing.T) {
	dbtesting.DataStoreTestSuite(t, &sqliteTestEnv{})
}
