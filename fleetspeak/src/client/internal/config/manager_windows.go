// Copyright 2018 Google Inc.
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

package config

import (
	"fmt"

	"github.com/google/fleetspeak/fleetspeak/src/windows/regutil"
)

func verifyConfigurationPath(configurationPath string) error {
	if err := regutil.VerifyPath(configurationPath); err != nil {
		return fmt.Errorf("invalid config path [%s]: %v", configurationPath, err)
	}

	return nil
}

func readWritebackImpl(writebackPath string) ([]byte, error) {
	b, err := regutil.ReadBinaryValue(writebackPath, writebackFilename)
	if err != nil {
		return nil, fmt.Errorf("unable to read writeback: %v", err)
	}

	return b, nil
}

func readCommunicatorConfigImpl(configPath string) ([]byte, error) {
	s, err := regutil.ReadStringValue(configPath, communicatorFilename)
	if err != nil {
		return nil, fmt.Errorf("unable to read communicator configuration: %v", err)
	}

	return []byte(s), nil
}

func syncImpl(path string, content []byte) error {
	if err := regutil.WriteBinaryValue(path, writebackFilename, content); err != nil {
		return fmt.Errorf("unable to write writeback: %v", err)
	}

	return nil
}
