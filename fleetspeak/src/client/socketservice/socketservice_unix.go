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

package socketservice

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/google/fleetspeak/fleetspeak/src/client/socketservice/checks"
)

func listen(path string) (net.Listener, error) {
	// We create the listener in this temporary location, chmod it, and then move
	// it. This prevents the client library from observing the listener before its
	// permissions are set.
	tmpPath := path + ".tmp"
	parent := filepath.Dir(path)

	// Although golang documentation does not explicitly mention this, there is a
	// limit for how long socket paths can be on different platforms. If you try
	// to bind to a socket path that is too long, you get a kernel error saying
	// 'invalid argument'. Also see http://pubs.opengroup.org/onlinepubs/009604499/basedefs/sys/un.h.html
	maxPathLen := len(unix.RawSockaddrUnix{}.Path) - 1 // C-strings are null-terminated
	if len(tmpPath) > maxPathLen {
		return nil, fmt.Errorf("socket path is too long (%d chars) - max allowed length is %d: %s", len(tmpPath), maxPathLen, tmpPath)
	}

	// Ensure that the parent directory exists and that the socket itself does not.
	if err := os.MkdirAll(parent, 0700); err != nil {
		return nil, fmt.Errorf("os.MkdirAll failed: %v", err)
	}
	for _, f := range []string{path, tmpPath} {
		if err := os.Remove(f); err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("os.Remove(%q): %v", f, err)
			}
		}
	}

	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: tmpPath, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("failed to create a Unix domain listener: %v", err)
	}

	if err := os.Chmod(tmpPath, 0600); err != nil {
		return nil, fmt.Errorf("failed to chmod a Unix domain listener: %v", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return nil, fmt.Errorf("failed to rename a Unix domain listener: %v", err)
	}

	if err := checks.CheckSocketFile(path); err != nil {
		return nil, fmt.Errorf("CheckSocketFile(...): %v", err)
	}

	return l, nil
}
