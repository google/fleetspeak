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

package client

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/common"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// A serviceConfiguration manages and communicates the services installed on a
// client. In normal use it is a singleton.
type serviceConfiguration struct {
	services  map[string]*serviceData
	lock      sync.RWMutex // Protects the structure of services.
	client    *Client
	factories map[string]service.Factory // Used to look up correct factory when configuring services.
}

func (c *serviceConfiguration) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	c.lock.RLock()
	target := c.services[m.Destination.ServiceName]
	c.lock.RUnlock()

	if target == nil {
		return fmt.Errorf("destination service not installed")
	}
	select {
	case target.inbox <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *serviceConfiguration) InstallSignedService(sd *fspb.SignedClientServiceConfig) error {
	if err := c.client.config.ValidateServiceConfig(sd); err != nil {
		return fmt.Errorf("Unable to verify signature of service config %v, ignoring: %v", sd.Signature, err)
	}

	var cfg fspb.ClientServiceConfig
	if err := proto.Unmarshal(sd.ServiceConfig, &cfg); err != nil {
		return fmt.Errorf("Unable to parse service config [%v], ignoring: %v", sd.Signature, err)
	}

ll:
	for _, l := range cfg.RequiredLabels {
		if l.ServiceName == "client" {
			for _, cl := range c.client.cfg.ClientLabels {
				if cl.Label == l.Label {
					continue ll
				}
			}
			return fmt.Errorf("service config requires label %v", l)
		}
	}

	return c.InstallService(&cfg, sd.Signature)
}

func (c *serviceConfiguration) InstallService(cfg *fspb.ClientServiceConfig, sig []byte) error {

	if cfg.Name == "" || cfg.Name == "system" || cfg.Name == "client" {
		return fmt.Errorf("illegal service name [%v]", cfg.Name)
	}

	f := c.factories[cfg.Factory]
	if f == nil {
		return fmt.Errorf("factory not found [%v]", cfg.Factory)
	}
	s, err := f(cfg)
	if err != nil {
		return fmt.Errorf("unable to create service: %v", err)
	}

	d := serviceData{
		config:  c,
		name:    cfg.Name,
		service: s,
		inbox:   make(chan *fspb.Message, 5),
	}
	if err := d.start(); err != nil {
		return fmt.Errorf("unable to start service: %v", err)
	}

	d.working.Add(1)
	go d.processingLoop()

	c.lock.Lock()
	old := c.services[cfg.Name]
	c.services[cfg.Name] = &d
	c.client.config.RecordRunningService(cfg.Name, sig)
	c.lock.Unlock()

	if old != nil {
		old.stop()
	}

	log.Infof("Started service %v with config:\n%v", cfg.Name, cfg)
	return nil
}

func (c *serviceConfiguration) Stop() {
	c.lock.Lock()
	defer c.lock.Unlock()
	for _, sd := range c.services {
		sd.stop()
	}
	c.services = make(map[string]*serviceData)
}

// A serviceData contains the data we have about a configured service, wrapping
// a Service interface and mediating communication between it and the rest of
// the Fleetspeak client.
type serviceData struct {
	config  *serviceConfiguration
	name    string
	working sync.WaitGroup
	service service.Service
	inbox   chan *fspb.Message
}

// Send implements service.Context.
func (d *serviceData) Send(ctx context.Context, am service.AckMessage) error {
	m := am.M
	id := d.config.client.config.ClientID().Bytes()

	m.Source = &fspb.Address{
		ClientId:    id,
		ServiceName: d.name,
	}

	if len(m.SourceMessageId) == 0 {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("unable to create random source message id: %v", err)
		}
		m.SourceMessageId = b
	}

	return d.config.client.ProcessMessage(ctx, am)
}

// GetLocalInfo implements service.Context.
func (d *serviceData) GetLocalInfo() *service.LocalInfo {
	ret := &service.LocalInfo{
		ClientID: d.config.client.config.ClientID(),
		Labels:   d.config.client.config.Labels(),
	}

	d.config.lock.RLock()
	defer d.config.lock.RUnlock()
	for s := range d.config.services {
		if s != "system" {
			ret.Services = append(ret.Services, s)
		}
	}
	return ret
}

// GetFileIfModified implements service.Context.
func (d *serviceData) GetFileIfModified(ctx context.Context, name string, modSince time.Time) (io.ReadCloser, time.Time, error) {
	if d.config.client.com == nil {
		// happens during tests
		return nil, time.Time{}, errors.New("file not found")
	}
	return d.config.client.com.GetFileIfModified(ctx, d.name, name, modSince)
}

func (d *serviceData) processingLoop() {
	// TODO: Kill misbehaving processes, limit processes'
	//       memory quota and niceness.

	for {
		m, ok := <-d.inbox
		if !ok {
			d.working.Done()
			return
		}
		id, err := common.BytesToMessageID(m.MessageId)
		if err != nil {
			log.Errorf("ignoring message with bad message id: [%v]", m.MessageId)
			continue
		}
		if err := d.service.ProcessMessage(context.TODO(), m); err != nil {
			d.config.client.errs <- &fspb.MessageErrorData{
				MessageId: id.Bytes(),
				Error:     err.Error(),
			}
		} else {
			d.config.client.acks <- id
		}
	}
}

func (d *serviceData) start() error {
	return d.service.Start(d)
}

func (d *serviceData) stop() {
	close(d.inbox)
	d.working.Wait()
	d.service.Stop()
}
