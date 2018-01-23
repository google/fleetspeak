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

// +build linux darwin

package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
)

func verifyConfigurationPath(configurationPath string) error {
	i, err := os.Stat(configurationPath)
	if err != nil {
		return fmt.Errorf("unable to stat config directory [%v]: %v", configurationPath, err)
	}
	if !i.Mode().IsDir() {
		return fmt.Errorf("config directory path [%v] is not a directory", configurationPath)
	}

	return nil
}

func readWritebackImpl(writebackDir string) ([]byte, error) {
	p := path.Join(writebackDir, writebackFilename)
	b, err := ioutil.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	return b, nil
}

func readCommunicatorConfigImpl(configPath string) ([]byte, error) {
	p := path.Join(configPath, communicatorFilename)
	return ioutil.ReadFile(p)
}

func syncImpl(writebackDir string, content []byte) error {
	p := path.Join(writebackDir, writebackFilename)
	tmp := p + ".new"
	os.RemoveAll(tmp)
	if err := ioutil.WriteFile(tmp, content, 0600); err != nil {
		return fmt.Errorf("unable to write new configuration: %v", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("unable to rename new confguration: %v", err)
	}

	return nil
}
