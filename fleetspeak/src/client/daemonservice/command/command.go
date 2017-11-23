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

// Package command provides a relatively thin wrapper around exec.Cmd, adding support
// for communicating with the dependent process using a channel.Channel.
package command

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/google/fleetspeak/fleetspeak/src/client/channel"
)

// Command is a wrapper around exec.Cmd which adds needed operations which are
// os specific. Notably, it knows how to attach a channel.Channel.
type Command struct {
	exec.Cmd

	filesToClose []*os.File // Files to close after starting cmd.
}

// Start refines exec.Cmd.Start.
func (cmd *Command) Start() error {
	// Delegate to the wrapped struct.
	if err := cmd.Cmd.Start(); err != nil {
		return err
	}

	// Close our copy of the passed pipe file descrptors.
	for _, f := range cmd.filesToClose {
		f.Close()
	}

	return nil
}

// SoftKill sends SIGINT to cmd.
func (cmd *Command) SoftKill() error {
	return cmd.softKill()
}

// Kill sends SIGKILL to cmd.
func (cmd *Command) Kill() error {
	return cmd.kill()
}

// AddEnvVar passes the given environment variable to the process.
//
// The environment variables are passed when the process is spawned, so this
// method should only be called before .Start is called.
//
// The argument kv is a key value pair in the form "key=value".
func (cmd *Command) AddEnvVar(kv string) {
	if cmd.Env == nil {
		// Append to current environment instead of overriding it. That's also how
		// os/exec handles .Env == nil - see golang.org/src/os/exec/exec.go
		cmd.Env = os.Environ()
	}

	cmd.Env = append(cmd.Env, kv)
}

// addInPipeFD creates a pipe passed to the process as a file descriptor. The
// process receives the read end of the pipe.
//
// The file descriptor is passed when the process is spawned, so this method
// should only be called before .Start is called.
//
// Returns the write end of the pipe and the file descriptor number that the
// process will receive.
func (cmd *Command) addInPipeFD() (*os.File, int, error) {
	return cmd.addInPipeFDImpl()
}

// addOutPipeFD creates a pipe passed to the process as a file descriptor. The
// process receives the write end of the pipe.
//
// The file descriptor is passed when the process is spawned, so this method
// should only be called before .Start is called.
//
// Returns the read end of the pipe and the file descriptor number that the
// process will receive.
func (cmd *Command) addOutPipeFD() (*os.File, int, error) {
	return cmd.addOutPipeFDImpl()
}

// SetupCommsChannel prepares a daemonServiceCommand to communicate with a
// DaemonService locally over an interprocess pipe.
func (cmd *Command) SetupCommsChannel() (*channel.Channel, error) {
	pw, inFd, err := cmd.addInPipeFD()
	if err != nil {
		return nil, fmt.Errorf("failed to create an input pipe: %v", err)
	}

	pr, outFd, err := cmd.addOutPipeFD()
	if err != nil {
		return nil, fmt.Errorf("failed to create an output pipe: %v", err)
	}

	// Note the FD numbers in cmd's environment.
	cmd.AddEnvVar(fmt.Sprintf("FLEETSPEAK_COMMS_CHANNEL_INFD=%d", inFd))
	cmd.AddEnvVar(fmt.Sprintf("FLEETSPEAK_COMMS_CHANNEL_OUTFD=%d", outFd))

	return channel.New(pr, pw), nil
}
