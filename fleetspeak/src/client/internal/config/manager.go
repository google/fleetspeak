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
	"io/ioutil"
	"os"
	"path"
	"sync"
	"time"

	"log"
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

	for _, l := range cfg.ClientLabels {
		if l.ServiceName != "client" || l.Label == "" {
			return nil, fmt.Errorf("invalid client label: %v", l)
		}
	}

	if cfg.ConfigurationPath == "" {
		if !cfg.Ephemeral {
			return nil, errors.New("no configuration path provided, but Ephemeral=false")
		}
		if cfg.FixedServices == nil {
			return nil, errors.New("no configuration path provided, but FixedServices=nil")
		}
	}

	r := Manager{
		cfg: cfg,
		cc:  cfg.CommunicatorConfig,

		state:           &clpb.ClientState{},
		revokedSerials:  make(map[string]bool),
		runningServices: make(map[string][]byte),

		configChanges: configChanges,
	}

	if r.cc == nil {
		r.cc = &clpb.CommunicatorConfig{}
	}

	if !cfg.Ephemeral {
		i, err := os.Stat(cfg.ConfigurationPath)
		if err != nil {
			return nil, fmt.Errorf("unable to stat config directory [%v]: %v", cfg.ConfigurationPath, err)
		}
		if !i.Mode().IsDir() {
			return nil, fmt.Errorf("config directory path [%v] is not a directory", cfg.ConfigurationPath)
		}
		r.writebackPath = path.Join(cfg.ConfigurationPath, "writeback")
		if err := r.readWriteback(); err != nil {
			log.Printf("initial load of writeback failed (continuing): %v", err)
			r.state = &clpb.ClientState{}
		}
	}

	r.AddRevokedSerials(cfg.RevokedCertSerials)

	if r.state.ClientKey == nil {
		if err := r.rekey(); err != nil {
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
		log.Printf("Using client id: %v (Escaped: %q, Octal: %o)", r.id, r.id.Bytes(), r.id.Bytes())
	}

	r.readCommunicatorConfig()

	if !r.cfg.Ephemeral {
		r.dirty = true
		if err := r.Sync(); err != nil {
			return nil, fmt.Errorf("unable to write initial Writeback[%v]: %v", r.writebackPath, err)
		}
		r.syncTicker = time.NewTicker(time.Minute)
		r.done = make(chan bool)
		go r.syncLoop()
	}
	return &r, nil
}

func (m *Manager) rekey() error {
	k, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
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

	log.Printf("Using new client id: %v (Escaped: %q, Octal: %o)", id, id.Bytes(), id.Bytes())

	return nil
}

// Sync writes the current dynamic state to the writeback file. This saves the
// current state, so changing data (e.g. the deduplication nonces) is persisted
// across restarts.
func (m *Manager) Sync() error {
	if m.cfg.Ephemeral {
		return nil
	}
	m.lock.RLock()
	p := m.writebackPath
	d := m.dirty
	m.lock.RUnlock()

	if p == "" || !d {
		return nil
	}

	tmp := p + ".new"
	os.RemoveAll(tmp)

	m.lock.Lock()
	defer m.lock.Unlock()
	bytes, err := proto.Marshal(m.state)
	if err != nil {
		log.Fatalf("Unable to serialize writeback: %v", err)
	}
	if err := ioutil.WriteFile(tmp, bytes, 0600); err != nil {
		return fmt.Errorf("unable to write new configuration: %v", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("unable to rename new confguration: %v", err)
	}
	m.dirty = false
	return nil
}

func (m *Manager) readWriteback() error {
	b, err := ioutil.ReadFile(m.writebackPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	m.state = &clpb.ClientState{}
	if err := proto.Unmarshal(b, m.state); err != nil {
		m.state = nil
		return fmt.Errorf("unable to parse writeback file: %v", err)
	}
	m.AddRevokedSerials(m.state.RevokedCertSerials)

	return nil
}

func (m *Manager) readCommunicatorConfig() {
	if m.cfg.ConfigurationPath == "" {
		return
	}
	p := path.Join(m.cfg.ConfigurationPath, "communicator.txt")
	b, err := ioutil.ReadFile(p)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Error reading communicator config [%s], ignoring: %v", p, err)
		}
		return
	}

	var cc clpb.CommunicatorConfig
	if err := proto.UnmarshalText(string(b), &cc); err != nil {
		log.Printf("Error parsing communicator config [%s], ignoring: %v", p, err)
		return
	}
	m.cc = &cc
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
