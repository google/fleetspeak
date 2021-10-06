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

package process

import (
	"os"
)

// KillProcessByPid causes the Process to exit immediately. It does not
// wait until the Process has actually exited. This only kills the Process
// itself, not any other processes it may have started. If the process
// can't be found at the moment of the call, KillProcessByPid returns nil.
func KillProcessByPid(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	return p.Kill()
}
