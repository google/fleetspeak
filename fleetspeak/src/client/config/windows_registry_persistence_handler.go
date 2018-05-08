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
	"io/ioutil"
	"path/filepath"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	"github.com/google/fleetspeak/fleetspeak/src/windows/regutil"
)

const (
	communicatorValuename = "communicator"
	writebackValuename    = "writeback"
	signedServicesKeyname = "services"
	textServicesKeyname   = "textservices"
)

// WindowsRegistryPersistenceHandler defines the Windows registry configuration storage strategy.
type WindowsRegistryPersistenceHandler struct {
	configurationPath string
	readonly          bool
}

// NewWindowsRegistryPersistenceHandler initializes registry keys used to store
// state and configuration info for Fleetspeak, and instantiates a WindowsRegistryPersistenceHandler.
//
// configurationPath is the registry key location to look for additional
// configuration values. Possible values include:
//
// \communicator           - Path to a text-format clpb.CommunicatorConfig, used to tweak communicator behavior.
// \writeback              - REG_BINARY value that is used to maintain state across restarts.
// \services\<service>     - Path to a binary format SignedClientServiceConfig. One registry value for each configured service.
// \textservices\<service> - Path to a text-format fspb.ClientServiceConfig. One registry value for each configured service.
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

	signedServicesKey := filepath.Join(configurationPath, signedServicesKeyname)
	textServicesKey := filepath.Join(configurationPath, textServicesKeyname)
	if err := regutil.CreateKeyIfNotExist(signedServicesKey); err != nil {
		return nil, err
	}
	if err := regutil.CreateKeyIfNotExist(textServicesKey); err != nil {
		return nil, err
	}

	return &WindowsRegistryPersistenceHandler{
		configurationPath: configurationPath,
		readonly:          readonly,
	}, nil
}

// ReadState implements PersistenceHandler.
func (h *WindowsRegistryPersistenceHandler) ReadState() (*clpb.ClientState, error) {
	b, err := regutil.ReadBinaryValue(h.configurationPath, writebackValuename)
	if err != nil {
		if err == regutil.ErrValueNotExist {
			// Clean state, writeback regvalue doesn't exist yet.
			return nil, nil
		}
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

	if err := regutil.WriteBinaryValue(h.configurationPath, writebackValuename, b); err != nil {
		return fmt.Errorf("unable to write new configuration: %v", err)
	}

	return nil
}

// ReadCommunicatorConfig implements PersistenceHandler.
func (h *WindowsRegistryPersistenceHandler) ReadCommunicatorConfig() (*clpb.CommunicatorConfig, error) {
	fpath, err := regutil.ReadStringValue(h.configurationPath, communicatorValuename)
	if err != nil {
		if err == regutil.ErrValueNotExist {
			// No communicator config specified.
			return nil, nil
		}
		return nil, fmt.Errorf("can't read communicator file path [%s -> %s]: %v", h.configurationPath, communicatorValuename, err)
	}

	fbytes, err := ioutil.ReadFile(fpath)
	if err != nil {
		return nil, fmt.Errorf("can't read communicator config file [%s]: %v", fpath, err)
	}

	ret := &clpb.CommunicatorConfig{}
	if err := proto.UnmarshalText(string(fbytes), ret); err != nil {
		return nil, fmt.Errorf("can't parse communicator config file [%s]: %v", fpath, err)
	}

	return ret, nil
}

// ReadSignedServices implements PersistenceHandler.
func (h *WindowsRegistryPersistenceHandler) ReadSignedServices() ([]*fspb.SignedClientServiceConfig, error) {
	keyPath := filepath.Join(h.configurationPath, signedServicesKeyname)

	regValues, err := regutil.Ls(keyPath)
	if err != nil {
		return nil, fmt.Errorf("unable to list values in signed services key path [%s]: %v", keyPath, err)
	}

	services := make([]*fspb.SignedClientServiceConfig, 0)

	for _, regValue := range regValues {
		fpath, err := regutil.ReadStringValue(keyPath, regValue)
		if err != nil {
			log.Errorf("Unable to read signed service registry value [%s -> %s], ignoring: %v", keyPath, regValue, err)
			continue
		}

		fbytes, err := ioutil.ReadFile(fpath)
		if err != nil {
			log.Errorf("Unable to read signed service file [%s], ignoring: %v", fpath, err)
			continue
		}

		service := &fspb.SignedClientServiceConfig{}
		if err := proto.Unmarshal(fbytes, service); err != nil {
			log.Errorf("Unable to parse signed service registry file [%s], ignoring: %v", fpath, err)
			continue
		}

		services = append(services, service)
	}

	return services, nil
}

// ReadServices implements PersistenceHandler.
func (h *WindowsRegistryPersistenceHandler) ReadServices() ([]*fspb.ClientServiceConfig, error) {
	keyPath := filepath.Join(h.configurationPath, textServicesKeyname)

	regValues, err := regutil.Ls(keyPath)
	if err != nil {
		return nil, fmt.Errorf("unable to list values in services key path [%s]: %v", keyPath, err)
	}

	services := make([]*fspb.ClientServiceConfig, 0)

	for _, regValue := range regValues {
		fpath, err := regutil.ReadStringValue(keyPath, regValue)
		if err != nil {
			log.Errorf("Unable to read service registry value [%s -> %s], ignoring: %v", keyPath, regValue, err)
			continue
		}

		fbytes, err := ioutil.ReadFile(fpath)
		if err != nil {
			log.Errorf("Unable to read service file [%s], ignoring: %v", fpath, err)
			continue
		}

		service := &fspb.ClientServiceConfig{}
		if err := proto.UnmarshalText(string(fbytes), service); err != nil {
			log.Errorf("Unable to parse service file [%s], ignoring: %v", fpath, err)
			continue
		}

		services = append(services, service)
	}

	return services, nil
}
