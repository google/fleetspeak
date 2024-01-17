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

// Package integration_test runs integrationtest against the sqlite datastore.
package integration_test

import (
	"path/filepath"
	"testing"

	"github.com/google/fleetspeak/fleetspeak/src/inttesting/integrationtest"
	"github.com/google/fleetspeak/fleetspeak/src/server/sqlite"
)

func mustMakeDatastore(t *testing.T) *sqlite.Datastore {
	t.Helper()

	ds, err := sqlite.MakeDatastore(filepath.Join(t.TempDir(), "DataStore.sqlite"))
	if err != nil {
		t.Fatalf("sqlite.MakeDatastore: %v", err)
	}
	t.Cleanup(func() {
		err := ds.Close()
		if err != nil {
			t.Errorf("Closing SQLite datastore: %v", err)
		}
	})
	return ds
}

func TestFRRIntegration(t *testing.T) {
	ds := mustMakeDatastore(t)
	integrationtest.FRRIntegrationTest(t, ds, false)
}

func TestCloneHandling(t *testing.T) {
	ds := mustMakeDatastore(t)
	integrationtest.CloneHandlingTest(t, ds)
}

func TestFRRIntegrationStreaming(t *testing.T) {
	ds := mustMakeDatastore(t)
	integrationtest.FRRIntegrationTest(t, ds, true)
}
