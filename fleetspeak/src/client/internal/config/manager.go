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

// Package config contains internal structures and methods relating to managing
// a client's configuration.
package config

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/common"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// A Manager stores and manages the current configuration of the client.
// It exports a view of the current configuration and is safe for concurrent
// access.
type Manager struct {
	cfg *config.Configuration // does not change
	cc  *clpb.CommunicatorConfig

	lock            sync.RWMutex // protects the state variables below
	state           *clpb.ClientState
	id              common.ClientID
	revokedSerials  map[string]bool // key is raw bytes of serial
	dirty           bool            // indicates that state has changed and should be written to disk
	writebackPath   string
	runningServices map[string][]byte // map from service name to signature of installed service configuration

	syncTicker    *time.Ticker
	configChanges chan<- *fspb.ClientInfoData
	done          chan bool
}

// StartManager attempts to create a Manager from the provided client
// configuration.
//
// The labels parameter defines what client labels the client should
// report to the server.
func StartManager(cfg *config.Configuration, configChanges chan<- *fspb.ClientInfoData) (*Manager, error) {
	if cfg == nil {
		return nil, errors.New("configuration must be provided")
	}

	if cfg.PersistenceHandler == nil {
		log.Warning("PersistenceHandler not provided. Using NoopPersistenceHandler.")
		cfg.PersistenceHandler = config.NewNoopPersistenceHandler()
	}

	for _, l := range cfg.ClientLabels {
		if l.ServiceName != "client" || l.Label == "" {
			return nil, fmt.Errorf("invalid client label: %v", l)
		}
	}

	r := Manager{
		cfg: cfg,
		cc:  cfg.CommunicatorConfig,

		state:           &clpb.ClientState{},
		revokedSerials:  make(map[string]bool),
		dirty:           true,
		runningServices: make(map[string][]byte),

		configChanges: configChanges,
	}

	if r.cc == nil {
		r.cc = &clpb.CommunicatorConfig{}
	}

	var err error
	if r.state, err = cfg.PersistenceHandler.ReadState(); err != nil {
		log.Errorf("initial load of writeback failed (continuing): %v", err)
		r.state = &clpb.ClientState{}
	}
	r.AddRevokedSerials(r.state.RevokedCertSerials)
	r.AddRevokedSerials(cfg.RevokedCertSerials)

	if r.state.ClientKey == nil {
		if err := r.Rekey(); err != nil {
			return nil, fmt.Errorf("no key present, and %v", err)
		}
	} else {
		k, err := x509.ParseECPrivateKey(r.state.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("unable to parse client key: %v", err)
		}
		r.id, err = common.MakeClientID(k.Public())
		if err != nil {
			return nil, fmt.Errorf("unable to create clientID: %v", err)
		}
		log.Infof("Using client id: %v (Escaped: %q, Octal: %o)", r.id, r.id.Bytes(), r.id.Bytes())
	}

	if cc, err := cfg.PersistenceHandler.ReadCommunicatorConfig(); err == nil {
		if cc != nil {
			r.cc = cc
		}
	} else {
		log.Errorf("Error reading communicator config, ignoring: %v", err)
	}

	if err := r.Sync(); err != nil {
		return nil, fmt.Errorf("unable to write initial writeback: %v", err)
	}
	r.syncTicker = time.NewTicker(time.Minute)
	r.done = make(chan bool)
	go r.syncLoop()

	return &r, nil
}

// Rekey creates a new private key and identity for the client.
func (m *Manager) Rekey() error {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("unable to generate new key: %v", err)
	}
	bytes, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		return fmt.Errorf("unable to marshal new key: %v", err)
	}
	id, err := common.MakeClientID(k.Public())
	if err != nil {
		return fmt.Errorf("unable to create client id: %v", err)
	}

	m.lock.Lock()
	m.state.ClientKey = bytes
	m.id = id
	m.dirty = true
	m.lock.Unlock()

	log.Infof("Using new client id: %v (Escaped: %q, Octal: %o)", id, id.Bytes(), id.Bytes())

	return nil
}

// Sync writes the current dynamic state to the writeback location. This saves the current state, so changing data (e.g. the deduplication nonces) is persisted across restarts.
func (m *Manager) Sync() error {
	m.lock.RLock()
	d := m.dirty
	m.lock.RUnlock()
	if !d {
		return nil
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	if err := m.cfg.PersistenceHandler.WriteState(m.state); err != nil {
		return fmt.Errorf("Failed to sync state to writeback: %v", err)
	}

	m.dirty = false
	return nil
}

// AddRevokedSerials takes a list of revoked certificate serial numbers and adds
// them to the configuration's list.
func (m *Manager) AddRevokedSerials(revoked [][]byte) {
	if len(revoked) == 0 {
		return
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	for _, serial := range revoked {
		ss := string(serial)
		if !m.revokedSerials[ss] {
			m.revokedSerials[ss] = true
			m.state.RevokedCertSerials = append(m.state.RevokedCertSerials, serial)
			m.dirty = true
		}
	}
}

// Stop shuts down the Manager, in particular it will stop sychronizing to the
// writeback file.
func (m *Manager) Stop() {
	if m.syncTicker != nil {
		m.syncTicker.Stop()
		close(m.done)
	}
}

func (m *Manager) syncLoop() {
	for {
		select {
		case <-m.syncTicker.C:
			m.Sync()
		case <-m.done:
			return
		}
	}
}

// ClientID returns the current client identifier.
func (m *Manager) ClientID() common.ClientID {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.id
}

// ValidateServiceConfig checks that a config is correctly signed according to
// some configured deployment key.
func (m *Manager) ValidateServiceConfig(sd *fspb.SignedClientServiceConfig) error {
	var err error
	for _, k := range m.cfg.DeploymentPublicKeys {
		hashed := sha256.Sum256(sd.ServiceConfig)
		err = rsa.VerifyPSS(&k, crypto.SHA256, hashed[:], sd.Signature, nil)
		if err == nil {
			return nil
		}
	}
	return err
}

// RecordRunningService adds name to the list of services which this client is
// currently running. This list will be included when sending a ClientInfo
// record to the server. The optional parameter sig should be set when the
// configuration was signed to emake it clear to the server which instance of
// the service is running.
func (m *Manager) RecordRunningService(name string, sig []byte) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.runningServices[name] = sig
	m.dirty = true
}

// SequencingNonce returns the most recent sequencing nonce known to the client.
func (m *Manager) SequencingNonce() uint64 {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.state.SequencingNonce
}

// SetSequencingNonce sets the sequencing nonce received from the server.
func (m *Manager) SetSequencingNonce(n uint64) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.state.SequencingNonce = n
	m.dirty = true
}

// SendConfigUpdate causes a ClientInfo message capturing the current configuration
// to be sent to the server.
func (m *Manager) SendConfigUpdate() {
	m.lock.RLock()
	defer m.lock.RUnlock()

	info := fspb.ClientInfoData{
		Labels: m.cfg.ClientLabels,
	}
	for s, sig := range m.runningServices {
		info.Services = append(info.Services,
			&fspb.ClientInfoData_ServiceID{
				Name:      s,
				Signature: sig,
			})
	}
	m.configChanges <- &info
}

// CurrentState returns the client's current dynamic state.
func (m *Manager) CurrentState() *clpb.ClientState {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return proto.Clone(m.state).(*clpb.ClientState)
}

// Configuration returns the configuration struct used to create this client.
func (m *Manager) Configuration() *config.Configuration {
	return m.cfg
}

// CommunicatorConfig returns the communicator configuration that the client
// is configured to use.
func (m *Manager) CommunicatorConfig() *clpb.CommunicatorConfig {
	return m.cc
}

// ChainRevoked returns true if any certificate in provided slice has been revoked.
func (m *Manager) ChainRevoked(chain []*x509.Certificate) bool {
	m.lock.RLock()
	defer m.lock.RUnlock()

	for _, c := range chain {
		if m.revokedSerials[string(c.SerialNumber.Bytes())] {
			return true
		}
	}
	return false
}

// Labels returns the labels built into this client.
func (m *Manager) Labels() []*fspb.Label {
	return m.cfg.ClientLabels
}
