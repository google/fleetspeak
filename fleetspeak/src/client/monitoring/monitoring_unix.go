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

// Package monitoring contains utilities for gathering data about resource usage in
// order to monitor client-side resource usage.
package monitoring

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/golang/protobuf/ptypes"

	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

// TODO: Support monitoring on other platforms.

// ResourceUsageFromFinishedCmd populates a common.ResourceUsage proto with
// information from exec.Cmd.ProcessState . NOTE that this is only possible
// after the process has finished and has been waited for, and will most
// probably panic otherwise.
func ResourceUsageFromFinishedCmd(cmd *exec.Cmd) *mpb.ResourceUsage {
	timestamp := ptypes.TimestampNow()
	rusage := cmd.ProcessState.SysUsage().(*syscall.Rusage)
	residentMemory := rusage.Maxrss * 1024 // Conversion to bytes.

	verboseStatus := fmt.Sprint(
		"Timestamp:\t", timestamp, "\n",
		"UserTime:\t", cmd.ProcessState.UserTime(), "\n",
		"SystemTime:\t", cmd.ProcessState.SystemTime(), "\n",
		"ResidentMemory:\t", residentMemory, "B\n",
	)

	return &mpb.ResourceUsage{
		Timestamp:      timestamp,
		UserTime:       ptypes.DurationProto(cmd.ProcessState.UserTime()),
		SystemTime:     ptypes.DurationProto(cmd.ProcessState.SystemTime()),
		ResidentMemory: residentMemory,
		VerboseStatus:  verboseStatus,
	}
}

// In principle, pageSize and ticksPerSecond should be found through the system
// calls C.sysconf(C._SC_PAGE_SIZE), C.sysconf(C._SC_CLK_TCK). However, it seems
// to be fixed for all linux platforms that go supports so we hardcode it to
// avoid requiring cgo.
const (
	// 4 KiB
	pageSize = 4096

	// 100 Hz
	ticksPerSecond = 100
)

// ResourceUsageForPID populates a common.ResourceUsage proto with information
// from /proc/<PID>/stat{,m,us} . This is only possible with running processes,
// an error will be returned if the corresponding procfs entry is not present.
func ResourceUsageForPID(pid int) (*mpb.ResourceUsage, error) {
	timestamp := ptypes.TimestampNow()
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

	statusFilename := fmt.Sprintf("/proc/%d/status", pid)
	status, err := ioutil.ReadFile(statusFilename)
	if err != nil {
		return nil, fmt.Errorf("error while reading %s: %v", statusFilename, err)
	}

	return &mpb.ResourceUsage{
		Timestamp:      timestamp,
		UserTime:       ptypes.DurationProto(time.Duration(utime+cutime) * time.Second / ticksPerSecond),
		SystemTime:     ptypes.DurationProto(time.Duration(stime+cstime) * time.Second / ticksPerSecond),
		ResidentMemory: resident * pageSize,
		VerboseStatus:  string(status),
	}, nil
}
