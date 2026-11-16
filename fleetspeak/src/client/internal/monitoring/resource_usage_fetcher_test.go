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
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	log "github.com/golang/glog"
)

const (
	workloadDurationEnvVar = "FLEETSPEAK_MONITORING_TEST_WORKLOAD_DURATION"
	workloadHelperTestName = "TestResourceUsageWorkloadHelper"
)

// TestResourceUsageWorkloadHelper is not a standalone test; it is invoked in a
// subprocess by workloadCmd to generate measurable user CPU, system CPU, and
// resident memory usage without relying on an external interpreter.
func TestResourceUsageWorkloadHelper(t *testing.T) {
	rawDuration := os.Getenv(workloadDurationEnvVar)
	if rawDuration == "" {
		return
	}
	d, err := time.ParseDuration(rawDuration)
	if err != nil {
		t.Fatalf("time.ParseDuration(%q): %v", rawDuration, err)
	}
	// Allocate a string and repeatedly list the current directory to generate
	// user CPU, system CPU, and resident memory usage.
	var b strings.Builder
	for i := range 5000 {
		fmt.Fprint(&b, i)
	}
	_ = b.String()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		_, _ = os.ReadDir(".")
		time.Sleep(20 * time.Millisecond)
	}
	os.Exit(0)
}

// workloadCmd returns an exec.Cmd that runs TestResourceUsageWorkloadHelper in
// a single-core subprocess for duration d.
func workloadCmd(t *testing.T, d time.Duration) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable(): %v", err)
	}
	cmd := exec.Command(exe, "-test.run=^"+workloadHelperTestName+"$")
	cmd.Env = append(os.Environ(),
		workloadDurationEnvVar+"="+d.String(),
		"GOMAXPROCS=1",
		"GOGC=20",
	)
	return cmd
}

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
	// Generate some system (os.ReadDir) and user execution time, consume some
	// memory.
	cmd := workloadCmd(t, time.Second)
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
	// Generate some system (os.ReadDir) and user execution time, consume some
	// memory.
	cmd := workloadCmd(t, 60*time.Second)
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

	if runtime.GOOS == "linux" {
		if ru.NumFDs == 0 {
			t.Errorf("Expected NumFDs to be non-0.")
		}
	} else {
		if ru.NumFDs != 0 {
			t.Errorf("Expected NumFDs to always be 0 on non-Linux systems.")
		}
	}

	debugStatus, err := ruf.DebugStatusForPID(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if debugStatus == "" {
		t.Error("Expected debug status to be non-empty.")
	}
}
