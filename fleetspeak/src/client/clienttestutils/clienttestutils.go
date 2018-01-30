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

// Package clienttestutils contains utility functions for the client test, in part platform-specific.
package clienttestutils

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/golang/protobuf/proto"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// WriteSignedServiceConfig saves the given signed config to a test location.
func WriteSignedServiceConfig(dirpath, filename string, cfg *fspb.SignedClientServiceConfig) error {
	b, err := proto.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("unable to serialize signed service config: %v", err)
	}

	configPath := filepath.Join(dirpath, filename)
	if err := ioutil.WriteFile(configPath, b, 0644); err != nil {
		return fmt.Errorf("unable to write signed service config[%s]: %v", configPath, err)
	}

	return nil
}

// WriteServiceConfig saves the given config to a test location.
func WriteServiceConfig(dirpath, filename string, cfg *fspb.ClientServiceConfig) error {
	s := proto.MarshalTextString(cfg)

	configPath := filepath.Join(dirpath, filename)
	if err := ioutil.WriteFile(configPath, []byte(s), 0644); err != nil {
		return fmt.Errorf("unable to write service config[%s]: %v", configPath, err)
	}

	return nil
}
