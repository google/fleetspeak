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

// +build linux darwin

package command

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func (cmd *Command) softKill() error {
	return unix.Kill(cmd.Process.Pid, unix.SIGINT)
}

func (cmd *Command) kill() error {
	return unix.Kill(cmd.Process.Pid, unix.SIGKILL)
}

func (cmd *Command) addInPipeFDImpl() (*os.File, int, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, 0, fmt.Errorf("error in os.Pipe: %v", err)
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, pr)
	cmd.filesToClose = append(cmd.filesToClose, pr)

	// Starts with 3.
	// See: https://golang.org/pkg/os/exec/#Cmd
	fd := len(cmd.ExtraFiles) + 2

	return pw, fd, nil
}

func (cmd *Command) addOutPipeFDImpl() (*os.File, int, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, 0, fmt.Errorf("error in os.Pipe: %v", err)
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, pw)
	cmd.filesToClose = append(cmd.filesToClose, pw)

	// Starts with 3.
	// See: https://golang.org/pkg/os/exec/#Cmd
	fd := len(cmd.ExtraFiles) + 2

	return pr, fd, nil
}
