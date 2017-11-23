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

package checks

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/hectane/go-acl/api"
	"golang.org/x/sys/windows" 
)

// CheckSocketFile ensures the naming, filetype and ownership
// (owner, group) of a Wnix socket match what we create in a Fleetspeak socket
// service. This gives us some extra security. Note that using os.Lstat here
// prevents confusing FS with symlink tricks.
func CheckSocketFile(socketPath string) error {
	if !strings.HasPrefix(socketPath[1:], `:\`) {
		return fmt.Errorf(`expected a Wnix domain socket path starting with a drive letter plus ":\", got %q`, socketPath)
	}

	parent := filepath.Dir(socketPath)

	// Lstat prevents using a symlink as the Wnix socket or the parent dir.
	parentFI, err := os.Lstat(parent)
	if err != nil {
		return fmt.Errorf("can't stat the given socketPath's (%q) parent directory: %v", socketPath, err)
	}

	if !parentFI.IsDir() {
		return fmt.Errorf("the given socketPath's (%q) parent directory is not a directory", socketPath)
	}

	// Reading file access perms on Windows is difficult, but we don't need to do
	// it - if the directory is root-owned, we can assume it was created by
	// Fleetspeak with the correct perms. See for example:
	// https://stackoverflow.com/questions/33445727/how-to-control-file-access-in-windows
	// https://msdn.microsoft.com/en-us/library/windows/desktop/aa446645.aspx

	if err := checkOwnership(parent); err != nil {
		return fmt.Errorf("unexpected ownership of socketPath's (%q) parent directory: %v", socketPath, err)
	}

	fi, err := os.Lstat(socketPath)
	if err != nil {
		return fmt.Errorf("can't stat the given socketPath (%q): %v", socketPath, err)
	}

	// If any bits higher than 0777 are set, this is not a regular file.
	if fi.Mode() > 0777 {
		return fmt.Errorf("the given socketPath (%q) is not a regular file", socketPath)
	}

	if err := checkOwnership(socketPath); err != nil {
		return fmt.Errorf("unexpected ownership of socketPath (%q): %v", socketPath, err)
	}

	bytePipeFSPath, err := ioutil.ReadFile(socketPath)
	if err != nil {
		return fmt.Errorf("ioutil.ReadFile(%v): %v", socketPath, err)
	}

	return CheckSocketPipe(string(bytePipeFSPath))
}

// CheckSocketPipe ensures the naming and ownership (owner, group) of a Wnix
// socket's underlying pipe matches what we create in a Fleetspeak socket
// service. This gives us some extra security.
//
// Public only to support unittesting.
func CheckSocketPipe(pipeFSPath string) error {
	const prefix = `\\.\pipe\`
	if !strings.HasPrefix(pipeFSPath, prefix) {
		return fmt.Errorf(`expected a Wnix domain socket pipe path starting with %q, got %q`, prefix, pipeFSPath)
	}

	suffix := pipeFSPath[len(prefix):]
	errSuspiciousPipe := fmt.Errorf(`the given hash-named-pipe doesn't seem to be created by Fleetspeak; got %q`, pipeFSPath)
	if len(suffix) != 128 {
		return errSuspiciousPipe
	}
	if _, err := base64.URLEncoding.Strict().DecodeString(suffix); err != nil {
		return fmt.Errorf("%v\n.DecodeString(...): %v", errSuspiciousPipe, err)
	}

	return nil
}

func checkOwnership(filepath string) error {
	c, err := user.Current()
	if err != nil {
		return err
	}

	cg := c.Gid

	_, fg, err := getOwnership(filepath)
	if err != nil {
		return err
	}

	// Owner SID of a created directory is not the same as returned by
	// os/user.Current(), so we can't check against that. This can be worked
	// around by creating a directory and recording what owner and group it
	// gets assigned; it's not clear whether we actually want to do this.

	if fg != cg {
		return errors.New("unexpected group SID")
	}

	return nil
}

// getOwnership returns file owner SID and file group SID.
func getOwnership(filepath string) (string, string, error) {
	var psidOwner, psidGroup *windows.SID
	var securityDescriptor windows.Handle
	api.GetNamedSecurityInfo(
		filepath,
		api.SE_FILE_OBJECT,
		api.OWNER_SECURITY_INFORMATION|api.GROUP_SECURITY_INFORMATION,
		&psidOwner,
		&psidGroup,
		nil,
		nil,
		&securityDescriptor,
	)
	windows.LocalFree(securityDescriptor)

	o, err := psidOwner.String()
	if err != nil {
		return "", "", err
	}

	g, err := psidGroup.String()
	if err != nil {
		return "", "", err
	}

	return o, g, nil
}
