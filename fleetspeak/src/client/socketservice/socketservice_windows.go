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

package socketservice

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/hectane/go-acl"

	"github.com/google/fleetspeak/fleetspeak/src/client/socketservice/checks"
	"github.com/google/fleetspeak/fleetspeak/src/windows/wnixsocket"
)

func listen(socketPath string) (net.Listener, error) {
	parent := filepath.Dir(socketPath)

	// Ensure that the parent directory exists. wnixsocket.Listen ensures the
	// socket file exists and is truncated.
	if _, err := os.Lstat(parent); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("os.Lstat failed: %v", err)
		}
	}

	if err := os.MkdirAll(parent, 0); err != nil {
		return nil, fmt.Errorf("os.MkdirAll failed: %v", err)
	}

	// MkdirAll doesn't set mode as expected on Windows, so we make
	// sure with Chmod. Note that os.Chmod also doesn't work as expected, so
	// we use go-acl.
	if err := acl.Chmod(parent, 0700); err != nil {
		return nil, fmt.Errorf("failed to chmod a Wnix domain listener's parent directory: %v", err)
	}

	l, err := wnixsocket.Listen(socketPath, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to create a Wnix domain listener: %v", err)
	}

	if err := checks.CheckSocketFile(socketPath); err != nil {
		return nil, fmt.Errorf("CheckSocketFile(...): %v", err)
	}

	return l, nil
}
