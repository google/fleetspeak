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

// Package checks implements code which checks permissions of unix sockets to
// mitigate the possibility of a non-root attacker messing with socketservice
// communications channel.
package checks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// CheckUnixSocket ensures the naming, mode (perms, filetype) and ownership
// (uid, gid) of a Unix socket match what we create in a Fleetspeak daemon
// service. This gives us some extra security. Note that using os.Lstat here
// prevents confusing FS with symlink tricks.
func CheckUnixSocket(unixAddr string) error {
	if !strings.HasPrefix(unixAddr, "/") {
		return fmt.Errorf("expected a Unix domain socket address starting with \"/\", got %q", unixAddr)
	}

	parent := filepath.Dir(unixAddr)

	// Lstat prevents using a symlink as the Unix socket or the parent dir.
	parentFI, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("can't stat the given unixAddr's (%q) parent directory: %v", unixAddr, err)
	}

	// Note that providing such specific mode is deliberately much stricter than
	// just checking the permission bits and os.FileMode.IsDir .
	//
	// It's also beneficial to have an enclosing directory with a strict mode
	// because some implementations of Unix domain sockets apparently ignore
	// the permissions entirely.
	if parentFI.Mode() != 0700|os.ModeDir {
		return fmt.Errorf("the given unixAddr's (%q) parent directory has unexpected mode", unixAddr)
	}

	if err := checkUnixOwnership(parentFI); err != nil {
		return fmt.Errorf("unexpected ownership of unixAddr's (%q) parent directory: %v", unixAddr, err)
	}

	fi, err := os.Lstat(unixAddr)
	if err != nil {
		return fmt.Errorf("can't stat the given unixAddr (%q): %v", unixAddr, err)
	}

	if fi.Mode() != 0600|os.ModeSocket {
		return fmt.Errorf("the given unixAddr (%q) has unexpected mode", unixAddr)
	}

	if err := checkUnixOwnership(fi); err != nil {
		return fmt.Errorf("unexpected ownership of unixAddr (%q): %v", unixAddr, err)
	}

	return nil
}

func checkUnixOwnership(fi os.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("failed to cast os.FileInfo.Sys() to syscall.Stat_t; is this a non-Unix system?")
	}

	uid := os.Getuid()
	if uid != int(st.Uid) {
		return errors.New("unexpected uid")
	}

	gid := os.Getgid()
	if gid != int(st.Gid) {
		return errors.New("unexpected gid")
	}

	return nil
}
