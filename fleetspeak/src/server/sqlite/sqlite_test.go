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
	"path"
	"testing"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/dbtesting"
)

func setup(t *testing.T, caseName string) *Datastore {
	dir, tmpDirCleanup := comtesting.GetTempDir("sqlite_test")
	defer tmpDirCleanup()
	p := path.Join(dir, caseName+".sqlite")
	log.Infof("Using database: %v", p)
	s, err := MakeDatastore(p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestClientStore(t *testing.T) {
	ds := setup(t, "TestClientStore")

	dbtesting.ClientStoreTest(t, ds)
	ds.Close()
}

func TestMessageStore(t *testing.T) {
	ms := setup(t, "TestMessageStore")
	dbtesting.MessageStoreTest(t, ms)
	ms.Close()
}

func TestBroadcastStore(t *testing.T) {
	ms := setup(t, "TestBroadcastStore")
	dbtesting.BroadcastStoreTest(t, ms)
	ms.Close()
}

func TestFileStore(t *testing.T) {
	ms := setup(t, "TestFileStore")
	dbtesting.FileStoreTest(t, ms)
	ms.Close()
}
