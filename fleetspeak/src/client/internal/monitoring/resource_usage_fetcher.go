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

// +build windows darwin

package monitoring

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/shirou/gopsutil/process"
)

// TODO

// ResourceUsage contains resource-usage data for a single process.
type ResourceUsage struct {
	// When the resource-usage data was retrieved.
	Timestamp time.Time

	// Amount of CPU time scheduled for a process so far in user mode.
	UserCPUMillis float64

	// Amount of CPU time scheduled for a process so far in kernel mode.
	SystemCPUMillis float64

	// Resident set size for a process, in bytes.
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

// ResourceUsageForPID returns a ResourceUsage struct with information
// from /proc/<PID>/stat and /proc/<PID>/statm . This is only possible with running processes,
// an error will be returned if the corresponding procfs entry is not present.
func (f ResourceUsageFetcher) ResourceUsageForPID(pid int) (*ResourceUsage, error) {
	timestamp := time.Now()

	process, err := process.NewProcess(int32(pid))
	if err != nil {
		return nil, err
	}

	memoryInfo, err := process.MemoryInfo()
	if err != nil {
		return nil, err
	}

	times, err := process.Times()
	if err != nil {
		return nil, err
	}

	return &ResourceUsage{
		Timestamp:       timestamp,
		UserCPUMillis:   times.User * 1e3,
		SystemCPUMillis: times.System * 1e3,
		ResidentMemory:  int64(memoryInfo.RSS),
	}, nil
}

// DebugStatusForPID returns a string containing extra debug info about resource-usage that may not be
// captured in ResourceUsage. This is only possible for running processes.
func (f ResourceUsageFetcher) DebugStatusForPID(pid int) (string, error) {
	process, err := process.NewProcess(int32(pid))
	if err != nil {
		return "", err
	}

	memoryInfo, err := process.MemoryInfo()
	if err != nil {
		return "", err
	}

	times, err := process.Times()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(`
Process:
%q

Times:
%q

MemoryInfo:
%q`, process, times, memoryInfo), nil
}
