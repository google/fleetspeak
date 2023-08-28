// Copyright 2019 Google LLC
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

// Package serer
package server

import (
	"errors"
	"fmt"
	"os"

	"google.golang.org/protobuf/encoding/prototext"

	cpb "github.com/google/fleetspeak/fleetspeak/src/config/proto/fleetspeak_config"
)

// WriteConfig validates and then writes a server component configuration.
func WriteConfig(cfg *cpb.Config, certPEM, keyPEM []byte) error {
	if cfg.ServerComponentConfigurationFile == "" {
		return errors.New("server_component_configuration_file is required")
	}

	cc := cfg.ComponentsConfig
	if cc == nil {
		return errors.New("components_config is required")
	}
	if cc.HttpsConfig == nil {
		return errors.New("components_config.https_config is required")
	}
	if cc.HttpsConfig.ListenAddress == "" {
		return errors.New("components_config.https_config.listen_address is required")
	}
	cc.HttpsConfig.Certificates = string(certPEM)
	cc.HttpsConfig.Key = string(keyPEM)

	b, err := prototext.Marshal(cc)
	if err != nil {
		return fmt.Errorf("failed to marshal server component configuration: %v", err)
	}
	err = os.WriteFile(cfg.ServerComponentConfigurationFile, b, 0600)
	if err != nil {
		return fmt.Errorf("failed to write server component configuration file [%s]: %v", cfg.ServerComponentConfigurationFile, err)
	}

	return nil
}
