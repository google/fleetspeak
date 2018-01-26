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

// Package regutil contains utility functions for Windows registry handling.
package regutil

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const hkeyPrefix = `HKEY_LOCAL_MACHINE\`

// VerifyPath performs basic verification on registry paths.
func VerifyPath(configurationPath string) error {
	if !strings.HasPrefix(configurationPath, hkeyPrefix) {
		return fmt.Errorf("invalid path [%s], only registry paths starting with %s are supported", configurationPath, hkeyPrefix)
	}

	return nil
}

func stripHKey(keypath string) (string, error) {
	if err := VerifyPath(keypath); err != nil {
		return "", err
	}

	return filepath.Clean(keypath[len(hkeyPrefix):]), nil
}

// WriteBinaryValue writes a REG_BINARY value to the given registry path.
func WriteBinaryValue(keypath, valuename string, content []byte) error {
	p, err := stripHKey(keypath)
	if err != nil {
		return err
	}

	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, p, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("unable to create registry keypath [%s]: %v", keypath, err)
	}
	defer k.Close()

	err = k.SetBinaryValue(valuename, content)
	if err != nil {
		return fmt.Errorf("unable to write binary (REG_BINARY) registry value [%s -> %s]: %v", keypath, valuename, err)
	}

	return nil
}

// ReadBinaryValue reads a REG_BINARY value from the given registry path.
func ReadBinaryValue(keypath, valuename string) ([]byte, error) {
	p, err := stripHKey(keypath)
	if err != nil {
		return nil, err
	}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, p, registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("unable to open registry keypath [%s]: %v", keypath, err)
	}
	defer k.Close()

	s, _, err := k.GetBinaryValue(valuename)
	if err != nil {
		return nil, fmt.Errorf("unable to read binary (REG_BINARY) registry value [%s -> %s]: %v", p, valuename, err)
	}

	return s, nil
}

// WriteStringValue writes a REG_SZ value to the given registry path.
func WriteStringValue(keypath, valuename string, content string) error {
	p, err := stripHKey(keypath)
	if err != nil {
		return err
	}

	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, p, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("unable to create registry keypath [%s]: %v", keypath, err)
	}
	defer k.Close()

	err = k.SetStringValue(valuename, content)
	if err != nil {
		return fmt.Errorf("unable to write string (REG_SZ) registry value [%s -> %s]: %v", keypath, valuename, err)
	}

	return nil
}

// ReadStringValue reads a REG_SZ value from the given registry path.
func ReadStringValue(keypath, valuename string) (string, error) {
	p, err := stripHKey(keypath)
	if err != nil {
		return "", err
	}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, p, registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("unable to open registry keypath [%s]: %v", keypath, err)
	}
	defer k.Close()

	s, _, err := k.GetStringValue(valuename)
	if err != nil {
		return "", fmt.Errorf("unable to read string (REG_SZ) registry value [%s -> %s]: %v", p, valuename, err)
	}

	return s, nil
}

// DeleteKey deletes a key from the given registry path.
func DeleteKey(keypath string) error {
	p, err := stripHKey(keypath)
	if err != nil {
		return err
	}

	return registry.DeleteKey(registry.LOCAL_MACHINE, p)
}

// Ls lists the given registry key path.
func Ls(keypath string) ([]string, error) {
	p, err := stripHKey(keypath)
	if err != nil {
		return nil, err
	}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, p, registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("unable to open services key path [%s]: %v", p, err)
	}
	defer k.Close()

	vs, err := k.ReadValueNames(0)
	if err != nil {
		return nil, fmt.Errorf("unable to list values in services key path [%s]: %v", p, err)
	}

	return vs, nil
}
