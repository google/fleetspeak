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

// +build windows

package command

import (
	"fmt"
	"os"
	"syscall"
)

func (cmd *Command) softKill() error {
	return cmd.Kill()
}

func (cmd *Command) kill() error {
	return cmd.Cmd.Process.Kill()
}

func (cmd *Command) addInPipeFDImpl() (*os.File, int, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, 0, fmt.Errorf("error in os.Pipe: %v", err)
	}

	fd := pr.Fd()
	syscall.SetHandleInformation(syscall.Handle(fd), syscall.HANDLE_FLAG_INHERIT, 1)
	cmd.filesToClose = append(cmd.filesToClose, pr)

	return pw, int(fd), nil
}

func (cmd *Command) addOutPipeFDImpl() (*os.File, int, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, 0, fmt.Errorf("error in os.Pipe: %v", err)
	}

	fd := pw.Fd()
	syscall.SetHandleInformation(syscall.Handle(fd), syscall.HANDLE_FLAG_INHERIT, 1)
	cmd.filesToClose = append(cmd.filesToClose, pw)

	return pr, int(fd), nil
}
