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

package config

import (
	"errors"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// NoopPersistenceHandler indicates that this client should not attempt
// to maintain state across restarts. If used, every execution will identify
// itself as a new client.
//
// Intended for testing and specialized applications.
type NoopPersistenceHandler struct{}

// NewNoopPersistenceHandler instantiates a NoopPersistenceHandler.
func NewNoopPersistenceHandler() *NoopPersistenceHandler {
	return &NoopPersistenceHandler{}
}

// ReadState implements PersistenceHandler.
func (*NoopPersistenceHandler) ReadState() (*clpb.ClientState, error) {
	return &clpb.ClientState{}, nil
}

// WriteState implements PersistenceHandler.
func (*NoopPersistenceHandler) WriteState(s *clpb.ClientState) error {
	return nil
}

// ReadCommunicatorConfig implements PersistenceHandler.
func (*NoopPersistenceHandler) ReadCommunicatorConfig() (*clpb.CommunicatorConfig, error) {
	return nil, nil
}

// ReadSignedServices implements PersistenceHandler.
func (*NoopPersistenceHandler) ReadSignedServices() ([]*fspb.SignedClientServiceConfig, error) {
	return nil, nil
}

// ReadServices implements PersistenceHandler.
func (*NoopPersistenceHandler) ReadServices() ([]*fspb.ClientServiceConfig, error) {
	return nil, nil
}

// SaveSignedService implements PersistenceHandler.
func (*NoopPersistenceHandler) SaveSignedService(*fspb.SignedClientServiceConfig) error {
	return errors.New("not implemented")
}
