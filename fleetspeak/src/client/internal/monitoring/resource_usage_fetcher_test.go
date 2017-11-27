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
	"fmt"
	"os/exec"
	"runtime"
	"testing"
	"time"

	log "github.com/golang/glog"
)

func checkFloat64(name string, value, min, max float64) error {
	if value < min || value > max {
		return fmt.Errorf("Expected %s to be between %v and %v, got %v", name, min, max, value)
	}
	return nil
}

func checkInt64(name string, value, min, max int64) error {
	if value < min || value > max {
		return fmt.Errorf("Expected %s to be between %d and %d, got %d", name, min, max, value)
	}
	return nil
}

func TestResourceUsageFromFinishedCmd(t *testing.T) {
	ruf := ResourceUsageFetcher{}
	// Generate some system (os.listdir) and user (everything else) execution
	// time, consume some memory...
	cmd := exec.Command("python", "-c", `
import os
import time

s = ""
for i in xrange(5000):
  s += str(i)

t0 = time.time()
while time.time() - t0 < 1.:
  os.listdir(".")
	`)
	start := time.Now()
	if err := cmd.Run(); err != nil {
		t.Fatalf("Unexpected error from Run(): %v", err)
	}
	runTime := time.Since(start)
	ru := ruf.ResourceUsageFromFinishedCmd(cmd)

	log.Infof("got resource usage: %#v", ru)

	// Can CPU time be > observed wall-time if multiple cores are involved?
	if err := checkFloat64("UserCPUMillis", ru.UserCPUMillis, 0.0, runTime.Seconds()*1000.0); err != nil {
		t.Error(err)
	}
	if err := checkFloat64("SystemCPUMillis", ru.SystemCPUMillis, 0.0, runTime.Seconds()*1000.0); err != nil {
		t.Error(err)
	}

	if ru.Timestamp.Before(start) {
		t.Errorf("Expected timestamp to be at least %v, but it was %v", start, ru.Timestamp)
	}
}

func TestResourceUsageForPID(t *testing.T) {
	ruf := ResourceUsageFetcher{}
	log.Infof("Testing resource usage on [%s, %s]", runtime.GOARCH, runtime.GOOS)
	// Generate some system (os.listdir) and user (everything else) execution
	// time, consume some memory...
	cmd := exec.Command("python", "-c", `
import os
import time

s = ""
for i in xrange(5000):
  s += str(i)

t0 = time.time()
while time.time() - t0 < 60.:
  os.listdir(".")
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
			log.Errorf("Error waiting for cmd: %v", err)
		}
	}()
	time.Sleep(3 * time.Second)
	runTime := time.Since(start)
	ru, err := ruf.ResourceUsageForPID(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Error getting ResourceUsageForPid: %v", err)
	}
	log.Infof("got resource usage: %#v", ru)

	// Can CPU time be > observed wall-time if multiple cores are involved?
	if err := checkFloat64("UserCPUMillis", ru.UserCPUMillis, 0.0, runTime.Seconds()*1000.0); err != nil {
		t.Error(err)
	}
	if err := checkFloat64("SystemCPUMillis", ru.SystemCPUMillis, 0.0, runTime.Seconds()*1000.0); err != nil {
		t.Error(err)
	}

	if ru.Timestamp.Before(start) {
		t.Errorf("Expected timestamp to be at least %v, but it was %v", start, ru.Timestamp)
	}
	if err := checkInt64("resident memory", ru.ResidentMemory, 128*1024, 100*1024*1024); err != nil {
		t.Error(err)
	}

	debugStatus, err := ruf.DebugStatusForPID(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if debugStatus == "" {
		t.Error("Expected debug status to be non-empty.")
	}
}
