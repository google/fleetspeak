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

// Package wnixsocket provides Unix-like sockets on Windows.
package wnixsocket

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/google/fleetspeak/fleetspeak/src/windows/hashpipe"
	"github.com/hectane/go-acl"

	"golang.org/x/sys/windows"
)

// TODO(ogaro): Remove this function after https://github.com/hectane/go-acl/pull/9/files
// is merged (go-acl's Chmod() is currently broken; the repo's Appveyor tests have been
// failing for a month now).
func Chmod(name string, mode os.FileMode) error {
	// https://support.microsoft.com/en-us/help/243330/well-known-security-identifiers-in-windows-operating-systems
	creatorOwnerSID, err := windows.StringToSid("S-1-3-0")
	if err != nil {
		return err
	}
	creatorGroupSID, err := windows.StringToSid("S-1-3-1")
	if err != nil {
		return err
	}
	everyoneSID, err := windows.StringToSid("S-1-1-0")
	if err != nil {
		return err
	}

	return acl.Apply(
		name,
		true,
		false,
		acl.GrantSid((uint32(mode)&0700)<<23, creatorOwnerSID),
		acl.GrantSid((uint32(mode)&0070)<<26, creatorGroupSID),
		acl.GrantSid((uint32(mode)&0007)<<29, everyoneSID),
	)
}

// Listen prepares a net.Listener bound to the given filesystem path.
func Listen(socketPath string) (net.Listener, error) {
	// Allow Administrators and SYSTEM. See:
	// - SDDL format:
	//   https://msdn.microsoft.com/en-us/library/windows/desktop/aa379570(v=vs.85).aspx
	// - ACE (rules in brackets) format:
	//   https://msdn.microsoft.com/en-us/library/windows/desktop/aa374928(v=vs.85).aspx
	// - SID string (user handles, `BA' and `SY') format:
	//   https://msdn.microsoft.com/en-us/library/windows/desktop/aa379602(v=vs.85).aspx
	// - Protecting Objects from the Effects of Inherited Rights:
	//   https://msdn.microsoft.com/en-us/library/ms677634(v=vs.85).aspx
	//
	// The above links are quite elaborate. Including the relevant items here for
	// easier lookup; note that the order is important:
	// - D stands for DACL
	// - P disables access perms inheritance
	// - In brackets ():
	// -- A means allow access to the specified user
	// -- GA means read, write and execute
	// -- BA means apply to `built-in administrators'
	// -- SY means apply to `local system'
	const sddl = "D:P(A;;GA;;;BA)(A;;GA;;;SY)"
	c := &winio.PipeConfig{
		SecurityDescriptor: sddl,
	}

	l, pipeFSPath, err := hashpipe.ListenPipe(c)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on a hashpipe: %v", err)
	}

	// Previous versions of Fleetspeak had a bug where on go1.14 the above
	// WriteFile would create a read-only file. Clear that attribute if
	// such a file has been left behind.
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		if err := windows.Chmod(socketPath, windows.S_IWRITE); err != nil {
			return nil, fmt.Errorf("clearing read-only bit on wnix socket: %w", err)
		}
	}

	// The socket file will be truncated if it exists.
	if err := ioutil.WriteFile(socketPath, []byte{}, 0600); err != nil {
		return nil, fmt.Errorf("error while truncating a Wnix socket file; ioutil.WriteFile(%q, ...): %v", socketPath, err)
	}

	// WriteFile doesn't set mode as expected on Windows, so we make
	// sure with Chmod. Note that os.Chmod also doesn't work as expected, so we
	// use go-acl.
	if err := Chmod(socketPath, 0600); err != nil {
		return nil, fmt.Errorf("failed to chmod a Wnix pipe: %v", err)
	}

	// Note that we only write the pipeFSPath to a file after we've reserved the
	// pipe name and chmoded it. The order is important for hardening purposes.
	if err := ioutil.WriteFile(socketPath, []byte(pipeFSPath), 0600); err != nil {
		return nil, fmt.Errorf("failed to initialize a Wnix socket: %v", err)
	}

	return l, nil
}

// Dial dials a Wnix socket bound to the given filesystem path.
func Dial(socketPath string, timeout time.Duration) (net.Conn, error) {
	bytePipeFSPath, err := ioutil.ReadFile(socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to dial to a Wnix socket (socketPath: [%v]): %v", socketPath, err)
	}

	pipeFSPath := string(bytePipeFSPath)

	if pipeFSPath == "" {
		return nil, fmt.Errorf("the dialed socket is not initialized (socketPath: [%v]): %v", socketPath, err)
	}

	conn, err := winio.DialPipe(pipeFSPath, &timeout)
	if err != nil {
		return nil, fmt.Errorf("winio.DialPipe() failed (pipeFSPath: [%q], socketPath: [%v]): %v", pipeFSPath, socketPath, err)
	}

	return conn, nil
}
