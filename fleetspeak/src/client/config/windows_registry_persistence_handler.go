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
	"errors"
	"fmt"
	"path/filepath"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	"github.com/google/fleetspeak/fleetspeak/src/windows/regutil"
)

// WindowsRegistryPersistenceHandler defines the Windows registry configuration storage strategy.
type WindowsRegistryPersistenceHandler struct {
	configurationPath string
	readonly          bool
}

// NewWindowsRegistryPersistenceHandler instantiates a WindowsRegistryPersistenceHandler.
//
// configurationPath is the registry key location to look for additional
// configuration values. Possible values include:
//
// /communicator.txt    - A text format clpb.CommunicatorConfig, used to tweak communicator behavior.
// /writeback           - Used to maintain state across restarts,
// /services/<service>  - A binary format SignedClientServiceConfig. One registry value for each configured service.
//
// All of these values are optional, though Fleetspeak will not be particularly
// useful without at least one configured service.
//
// If readonly is true, the client will not attempt to write to
// <ConfigurationPath>\writeback, in order to preserve client identity.
//
// readonly is intended for testing and specialized applications - should be
// hardcoded false in normal deployments.
func NewWindowsRegistryPersistenceHandler(configurationPath string, readonly bool) (*WindowsRegistryPersistenceHandler, error) {
	if err := regutil.VerifyPath(configurationPath); err != nil {
		return nil, fmt.Errorf("invalid configuration path: %v", err)
	}

	return &WindowsRegistryPersistenceHandler{
		configurationPath: configurationPath,
		readonly:          readonly,
	}, nil
}

// ReadState implements PersistenceHandler.
func (h *WindowsRegistryPersistenceHandler) ReadState() (*clpb.ClientState, error) {
	b, err := regutil.ReadBinaryValue(h.configurationPath, writebackFilename)
	if err != nil {
		return nil, fmt.Errorf("error while reading state from registry: %v", err)
	}

	ret := &clpb.ClientState{}
	if err := proto.Unmarshal(b, ret); err != nil {
		return nil, fmt.Errorf("unable to parse writeback registry value: %v", err)
	}

	return ret, nil
}

// WriteState implements PersistenceHandler.
func (h *WindowsRegistryPersistenceHandler) WriteState(s *clpb.ClientState) error {
	if h.readonly {
		return nil
	}

	b, err := proto.Marshal(s)
	if err != nil {
		log.Fatalf("Unable to serialize writeback: %v", err)
	}

	if err := regutil.WriteBinaryValue(h.configurationPath, writebackFilename, b); err != nil {
		return fmt.Errorf("unable to write new configuration: %v", err)
	}

	return nil
}

// ReadCommunicatorConfig implements PersistenceHandler.
func (h *WindowsRegistryPersistenceHandler) ReadCommunicatorConfig() (*clpb.CommunicatorConfig, error) {
	if h.configurationPath == "" {
		return nil, errors.New("configuration path not provided, can't read communicator config")
	}
	s, err := regutil.ReadStringValue(h.configurationPath, communicatorFilename)
	if err != nil {
		return nil, fmt.Errorf("can't read communicator config value [%s -> %s]: %v", h.configurationPath, communicatorFilename, err)
	}

	ret := &clpb.CommunicatorConfig{}
	if err := proto.UnmarshalText(s, ret); err != nil {
		return nil, fmt.Errorf("can't parse communicator config [%s -> %s]: %v", h.configurationPath, communicatorFilename, err)
	}

	return ret, nil
}

// ReadSignedServices implements PersistenceHandler.
func (h *WindowsRegistryPersistenceHandler) ReadSignedServices() ([]*fspb.SignedClientServiceConfig, error) {
	keyPath := filepath.Join(h.configurationPath, signedServicesDirname)
	if err := regutil.VerifyPath(keyPath); err != nil {
		return nil, fmt.Errorf("invalid signed services directory path: %v", err)
	}

	vs, err := regutil.Ls(keyPath)
	if err != nil {
		return nil, fmt.Errorf("unable to list values in signed services key path [%s]: %v", keyPath, err)
	}

	ret := make([]*fspb.SignedClientServiceConfig, 0)

	for _, v := range vs {
		b, err := regutil.ReadBinaryValue(keyPath, v)
		if err != nil {
			log.Errorf("Unable to read signed service registry value [%s -> %s], ignoring: %v", keyPath, v, err)
			continue
		}

		s := &fspb.SignedClientServiceConfig{}
		if err := proto.Unmarshal(b, s); err != nil {
			log.Errorf("Unable to parse signed service registry value [%s -> %s], ignoring: %v", keyPath, v, err)
			continue
		}

		ret = append(ret, s)
	}

	return ret, nil
}

// ReadServices implements PersistenceHandler.
func (h *WindowsRegistryPersistenceHandler) ReadServices() ([]*fspb.ClientServiceConfig, error) {
	keyPath := filepath.Join(h.configurationPath, servicesDirname)
	if err := regutil.VerifyPath(keyPath); err != nil {
		return nil, fmt.Errorf("invalid services directory path: %v", err)
	}

	vs, err := regutil.Ls(keyPath)
	if err != nil {
		return nil, fmt.Errorf("unable to list values in services key path [%s]: %v", keyPath, err)
	}

	ret := make([]*fspb.ClientServiceConfig, 0)

	for _, v := range vs {
		sv, err := regutil.ReadStringValue(keyPath, v)
		if err != nil {
			log.Errorf("Unable to read service registry value [%s -> %s], ignoring: %v", keyPath, v, err)
			continue
		}

		s := &fspb.ClientServiceConfig{}
		if err := proto.UnmarshalText(sv, s); err != nil {
			log.Errorf("Unable to parse service registry value [%s -> %s], ignoring: %v", keyPath, v, err)
			continue
		}

		ret = append(ret, s)
	}

	return ret, nil
}
