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
	"path"
	"testing"

	log "github.com/golang/glog"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/inttesting/integrationtest"
	"github.com/google/fleetspeak/fleetspeak/src/server/sqlite"
)

func TestFRRIntegration(t *testing.T) {
	tmpDir, tmpDirCleanup := comtesting.GetTempDir("sqlite_frr_integration")
	defer tmpDirCleanup()
	// Create an sqlite datastore.
	p := path.Join(tmpDir, "FRRIntegration.sqlite")
	ds, err := sqlite.MakeDatastore(p)
	if err != nil {
		t.Fatal(err)
	}
	log.Infof("Created database: %s", p)
	defer ds.Close()

	integrationtest.FRRIntegrationTest(t, ds, tmpDir)
}

func TestCloneHandling(t *testing.T) {
	tmpConfPath, tmpConfPathCleanup := comtesting.GetTempDir("sqlite_clone_handling")
	defer tmpConfPathCleanup()
	tmpDir, tmpDirCleanup := comtesting.GetTempDir("sqlite_clone_handling")
	defer tmpDirCleanup()
	// Create an sqlite datastore.
	p := path.Join(tmpDir, "CloneHandling.sqlite")
	ds, err := sqlite.MakeDatastore(p)
	if err != nil {
		t.Fatal(err)
	}
	log.Infof("Created database: %s", p)
	defer ds.Close()

	integrationtest.CloneHandlingTest(t, ds, tmpConfPath)
}
