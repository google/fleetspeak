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

package watchdog

import (
	"io/ioutil"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
)

func TestCreate(t *testing.T) {
	w := MakeWatchdog("asdf", "asdf", time.Second)
	w.Stop()
}

func TestReset(t *testing.T) {
	w := MakeWatchdog("asdf", "asdf", time.Second)
	defer w.Stop()
	for i := 0; i < 5; i++ {
		time.Sleep(500 * time.Millisecond)
		w.Reset()
	}
}

func TestDump(t *testing.T) {
	dir, fin := comtesting.GetTempDir("TestDump")
	defer fin()

	w := MakeWatchdog(dir, "TestTimer", time.Second)
	w.skipExit = true
	defer w.Stop()

	time.Sleep(1500 * time.Millisecond)

	files, err := ioutil.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("Expected 1 file, but found %d", len(files))
	}
}
