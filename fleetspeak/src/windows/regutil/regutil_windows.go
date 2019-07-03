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
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const hkeyPrefix = `HKEY_LOCAL_MACHINE\`

// ErrKeyNotExist is returned if an attempt is made to access a key that does not exist.
var ErrKeyNotExist = errors.New("regutil: registry key does not exist")

// ErrValueNotExist is returned if an attempt is made to read a value that a key does not have.
var ErrValueNotExist = errors.New("regutil: registry value does not exist")

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

// Thin wrapper over registry.OpenKey() with better error-reporting.
func openKey(keypath string, access uint32) (registry.Key, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, keypath, access)
	if err != nil {
		if err == registry.ErrNotExist {
			return k, ErrKeyNotExist
		}
		return k, fmt.Errorf("unable to open registry keypath [%s]: %v", keypath, err)
	}
	return k, nil
}

// CreateKeyIfNotExist checks if a registry key exists, and if it does not, creates it (along with all its ancestors).
func CreateKeyIfNotExist(keypath string) error {
	p, err := stripHKey(keypath)
	if err != nil {
		return err
	}

	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, p, registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return nil
}

// WriteBinaryValue writes a REG_BINARY value to the given registry path.
func WriteBinaryValue(keypath, valuename string, content []byte) error {
	p, err := stripHKey(keypath)
	if err != nil {
		return err
	}

	k, err := openKey(p, registry.SET_VALUE)
	if err != nil {
		return err
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

	k, err := openKey(p, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()

	s, _, err := k.GetBinaryValue(valuename)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil, ErrValueNotExist
		}
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

	k, err := openKey(p, registry.SET_VALUE)
	if err != nil {
		return err
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

	k, err := openKey(p, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()

	s, _, err := k.GetStringValue(valuename)
	if err != nil {
		if err == registry.ErrNotExist {
			return "", ErrValueNotExist
		}
		return "", fmt.Errorf("unable to read string (REG_SZ) registry value [%s -> %s]: %v", p, valuename, err)
	}

	return s, nil
}

// ReadMultiStringValue reads a REG_MULTI_SZ value from the given registry path.
func ReadMultiStringValue(keypath, valuename string) ([]string, error) {
	p, err := stripHKey(keypath)
	if err != nil {
		return nil, err
	}

	k, err := openKey(p, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()

	vs, _, err := k.GetStringsValue(valuename)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil, ErrValueNotExist
		}
		return nil, fmt.Errorf("unable to read multi-string (REG_MULTI_SZ) registry value [%s -> %s]: %v", p, valuename, err)
	}

	return vs, nil
}

// Ls returns all the value names of the given key.
func Ls(keypath string) ([]string, error) {
	p, err := stripHKey(keypath)
	if err != nil {
		return nil, err
	}

	k, err := openKey(p, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()

	vs, err := k.ReadValueNames(0)
	if err != nil {
		return nil, fmt.Errorf("unable to list values in key path [%s]: %v", p, err)
	}

	return vs, nil
}
