// Copyright 2021 Google Inc.
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

// +build windows

package process

import (
	"os"
	"syscall"
)


// Kill causes the Process to exit immediately. Kill does not wait until
// the Process has actually exited. This only kills the Process itself,
// not any other processes it may have started.
//
// TODO(https://github.com/google/fleetspeak/issues/341): remove as soon
// as Go's own Process.Kill() works as expected.
func KillProcess(p *os.Process) error {
	// Using the original terminateProcess implementation from https://github.com/golang/go/commit/105c5b50e0098720b9e24aea5efa8e161c31db6d#
	// Calling cmd.Cmd.Process.Kill() would trigger 'DuplicateHandle: The handle is invalid.'
	h, e := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, uint32(p.Pid))
	if e != nil {
		return os.NewSyscallError("OpenProcess", e)
	}
	defer syscall.CloseHandle(h)
	e = syscall.TerminateProcess(h, uint32(1))
	return os.NewSyscallError("TerminateProcess", e)
}

