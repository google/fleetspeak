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

// +build linux

// Package monitoring contains utilities for gathering data about resource usage in
// order to monitor client-side resource usage.
package monitoring

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// TODO: Support monitoring on other platforms.

// ResourceUsage contains resource-usage data for a single process.
type ResourceUsage struct {
	// When the resource-usage data was retrieved.
	Timestamp time.Time

	// Amount of CPU time scheduled for a process so far in user mode.
	UserCPUMillis float64

	// Amount of CPU time scheduled for a process so far in kernel mode.
	SystemCPUMillis float64

	// Resident set size for a process.
	ResidentMemory int64
}

// ResourceUsageFetcher obtains resource-usage data for a process from the OS.
type ResourceUsageFetcher struct{}

// ResourceUsageFromFinishedCmd returns a ResourceUsage struct with
// information from exec.Cmd.ProcessState. NOTE that this is only possible
// after the process has finished and has been waited for, and will most
// probably panic otherwise.
// This function doesn't fill in ResourceUsage.ResidentMemory.
func (f ResourceUsageFetcher) ResourceUsageFromFinishedCmd(cmd *exec.Cmd) *ResourceUsage {
	return &ResourceUsage{
		Timestamp:       time.Now(),
		UserCPUMillis:   float64(cmd.ProcessState.UserTime().Nanoseconds()) / 1e6,
		SystemCPUMillis: float64(cmd.ProcessState.SystemTime().Nanoseconds()) / 1e6,
	}
}

// In principle, pageSize and ticksPerSecond should be found through the system
// calls C.sysconf(C._SC_PAGE_SIZE), C.sysconf(C._SC_CLK_TCK). However, it seems
// to be fixed for all linux platforms that go supports so we hardcode it to
// avoid requiring cgo.
const (
	// 4 KiB
	pageSize = 4096
)

// ResourceUsageForPID returns a ResourceUsage struct with information
// from /proc/<PID>/stat and /proc/<PID>/statm . This is only possible with running processes,
// an error will be returned if the corresponding procfs entry is not present.
func (f ResourceUsageFetcher) ResourceUsageForPID(pid int) (*ResourceUsage, error) {
	timestamp := time.Now()
	statFilename := fmt.Sprintf("/proc/%d/stat", pid)
	stat, err := ioutil.ReadFile(statFilename)
	if err != nil {
		return nil, fmt.Errorf("error while reading %s: %v", statFilename, err)
	}

	splitStat := strings.Split(string(stat), " ")

	const expectedStatLen = 17
	if len(splitStat) < expectedStatLen {
		return nil, fmt.Errorf("unexpected format in %s - expected at least %d fields", statFilename, expectedStatLen)
	}

	utime, err := strconv.ParseInt(splitStat[13], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error while parsing utime from %s: %v", statFilename, err)
	}

	stime, err := strconv.ParseInt(splitStat[14], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error while parsing stime from %s: %v", statFilename, err)
	}

	cutime, err := strconv.ParseInt(splitStat[15], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error while parsing cutime from %s: %v", statFilename, err)
	}

	cstime, err := strconv.ParseInt(splitStat[16], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error while parsing cstime from %s: %v", statFilename, err)
	}

	statmFilename := fmt.Sprintf("/proc/%d/statm", pid)
	statm, err := ioutil.ReadFile(statmFilename)
	if err != nil {
		return nil, fmt.Errorf("error while reading %s: %v", statmFilename, err)
	}

	splitStatm := strings.Split(string(statm), " ")

	const expectedStatmLen = 2
	if len(splitStatm) < expectedStatmLen {
		return nil, fmt.Errorf("unexpected format in %s - expected at least %d fields", statmFilename, expectedStatmLen)
	}

	resident, err := strconv.ParseInt(splitStatm[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("error while parsing resident from %s: %v", statmFilename, err)
	}

	return &ResourceUsage{
		Timestamp:       timestamp,
		UserCPUMillis:   float64((utime + cutime) * 10), // Assume rate of 100 ticks/second
		SystemCPUMillis: float64((stime + cstime) * 10), // Assume rate of 100 ticks/second
		ResidentMemory:  resident * pageSize,
	}, nil
}

// DebugStatusForPID returns a string containing extra debug info about resource-usage that may not be
// captured in ResourceUsage. This is only possible for running processes.
func (f ResourceUsageFetcher) DebugStatusForPID(pid int) (string, error) {
	statusFilename := fmt.Sprintf("/proc/%d/status", pid)
	status, err := ioutil.ReadFile(statusFilename)
	if err != nil {
		return "", fmt.Errorf("error while reading %s: %v", statusFilename, err)
	}
	return string(status), nil
}
