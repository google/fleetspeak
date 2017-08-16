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

package monitoring

import (
	"os/exec"
	"runtime"
	"testing"
	"time"

	"log"
	"github.com/golang/protobuf/ptypes"

	durpb "github.com/golang/protobuf/ptypes/duration"
)

func toDuration(t *testing.T, dur *durpb.Duration) time.Duration {
	d, err := ptypes.Duration(dur)
	if err != nil {
		t.Errorf("Error converting duration: %v", err)
		return 0
	}
	return d
}

func checkTime(t *testing.T, name string, value, min, max time.Duration) {
	if value < min || value > max {
		t.Errorf("Expected %s to be between %v and %v, got %v", name, min, max, value)
	}
}

func checkInt64(t *testing.T, name string, value, min, max int64) {
	if value < min || value > max {
		t.Errorf("Expected %s to be between %d and %d, got %d", name, min, max, value)
	}
}

func TestResourceUsageFromFinishedCmd(t *testing.T) {
	cmd := exec.Command("/bin/bash", "-c", `
    s=" "
    for i in {1..5000}
    do
      s="${s} ${i}"
    done
    `)
	start := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatalf("Unexpected error from Run(): %v", err)
	}
	runTime := time.Since(start)
	ru := ResourceUsageFromFinishedCmd(cmd)

	log.Printf("got resource usage: %#v", ru)

	checkTime(t, "UserTime", toDuration(t, ru.UserTime), 100*time.Millisecond, runTime)
	checkTime(t, "SystemTime", toDuration(t, ru.SystemTime), 10*time.Millisecond, runTime)

	if ru.Timestamp.Seconds < start.Unix() {
		t.Errorf("Expected timestamp to be at least %d (start, in unix-seconds), but it was %d", start.Unix(), ru.Timestamp.Seconds)
	}
	checkInt64(t, "resident memory", ru.ResidentMemory, 128*1024, 100*1024*1024)
	if ru.VerboseStatus == "" {
		t.Errorf("Expected VerboseStatus, got empty string.")
	}
}

func TestResourceUsageForPID(t *testing.T) {
	log.Printf("Testing resource usage on [%s, %s]", runtime.GOARCH, runtime.GOOS)
	cmd := exec.Command("/bin/bash", "-c", `
    s=" "
    for i in {1..5000}
    do
      s="${s} ${i}"
    done
    sleep infinity
    `)
	start := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatalf("Unexpected error from Run(): %v", err)
	}
	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Errorf("Error killing cmd: %v", err)
		}
		if err := cmd.Wait(); err != nil {
			log.Printf("Error waiting for cmd: %v", err)
		}
	}()
	time.Sleep(3 * time.Second)
	runTime := time.Since(start)
	ru, err := ResourceUsageForPID(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Error getting ResourceUsageForPid: %v", err)
	}
	log.Printf("got resource usage: %#v", ru)

	checkTime(t, "UserTime", toDuration(t, ru.UserTime), 100*time.Millisecond, runTime)
	checkTime(t, "SystemTime", toDuration(t, ru.SystemTime), 10*time.Millisecond, runTime)

	if ru.Timestamp.Seconds < start.Unix() {
		t.Errorf("Expected timestamp to be at least %d (start, in unix-seconds), but it was %d", start.Unix(), ru.Timestamp.Seconds)
	}
	checkInt64(t, "resident memory", ru.ResidentMemory, 128*1024, 100*1024*1024)
	if ru.VerboseStatus == "" {
		t.Errorf("Expected VerboseStatus, got empty string.")
	}
}
