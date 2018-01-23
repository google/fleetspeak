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

	"github.com/golang/protobuf/proto"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// WriteServiceConfig saves the given config to a test location.
func WriteServiceConfig(dirpath, filename string, cfg *fspb.SignedClientServiceConfig) error {
	b, err := proto.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("unable to serialize signed service config: %v", err)
	}

	return writeServiceConfigImpl(b, dirpath, filename, cfg)
}
