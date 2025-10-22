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
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

const inboxSize = 100

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

		target.countLock.Lock()
		target.acceptCount++
		target.countLock.Unlock()

		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// InstallSignedService installs a service provided in signed service
// configuration format. Note however that this is meant for backwards
// compatibility and the signature is not checked.
//
// Currently, we assume that service configurations are kept in a secured
// location.
func (c *serviceConfiguration) InstallSignedService(sd *fspb.SignedClientServiceConfig) error {
	var cfg fspb.ClientServiceConfig
	if err := proto.Unmarshal(sd.ServiceConfig, &cfg); err != nil {
		return fmt.Errorf("Unable to parse service config [%v], ignoring: %v", sd.Signature, err)
	}

	if err := c.InstallService(&cfg, sd.Signature); err != nil {
		return fmt.Errorf("installing [%s]: %w", cfg.Name, err)
	}
	return nil
}

func validateServiceName(sname string) error {
	if sname == "" || sname == "system" || sname == "client" {
		return fmt.Errorf("illegal service name [%v]", sname)
	}
	return nil
}

func (c *serviceConfiguration) InstallService(cfg *fspb.ClientServiceConfig, sig []byte) error {
	if err := validateServiceName(cfg.Name); err != nil {
		return fmt.Errorf("can't install service: %v", err)
	}

ll:
	for _, l := range cfg.RequiredLabels {
		if l.ServiceName == "client" {
			for _, cl := range c.client.cfg.ClientLabels {
				if cl.Label == l.Label {
					continue ll
				}
			}
			return fmt.Errorf("service config [%s] requires label %v", cfg.Name, l)
		}
	}

	f := c.factories[cfg.Factory]
	if f == nil {
		return fmt.Errorf("factory not found: %q", cfg.Factory)
	}
	s, err := f(cfg)
	if err != nil {
		return fmt.Errorf("unable to create service with factory %q: %v", cfg.Factory, err)
	}

	d := serviceData{
		config:        c,
		name:          cfg.Name,
		serviceConfig: cfg,
		service:       s,
		inbox:         make(chan *fspb.Message, inboxSize),
	}
	if err := d.start(); err != nil {
		return fmt.Errorf("unable to start service: %v", err)
	}

	ctx := context.TODO()
	d.working.Add(1)
	go func() {
		defer d.working.Done()
		ctx = context.WithoutCancel(ctx)
		d.processingLoop(ctx)
	}()

	c.lock.Lock()
	old := c.services[cfg.Name]
	c.services[cfg.Name] = &d
	c.client.config.RecordRunningService(cfg.Name, sig)
	c.lock.Unlock()

	if old != nil {
		old.stop()
	}

	b, err := prototext.Marshal(cfg)
	if err != nil {
		return err
	}
	log.Infof("Started service %v with config:\n%s", cfg.Name, string(b))
	return nil
}

func (c *serviceConfiguration) removeService(sname string) (*serviceData, error) {
	c.lock.Lock()
	defer c.lock.Unlock()
	srv := c.services[sname]
	if srv == nil {
		return nil, fmt.Errorf("falied to remove non-existent service: %v", sname)
	}
	delete(c.services, sname)
	return srv, nil
}

func (c *serviceConfiguration) RestartService(sname string) error {
	if err := validateServiceName(sname); err != nil {
		return fmt.Errorf("can't restart service: %v", err)
	}

	srv, err := c.removeService(sname)
	if err != nil {
		return err
	}

	srv.stop()

	if err := c.InstallService(srv.serviceConfig, nil); err != nil {
		return fmt.Errorf("can't reinstall service '%s' on restart: %v", sname, err)
	}

	return nil
}

// Counts returns the number of accepted and processed messages for each
// service.
func (c *serviceConfiguration) Counts() (accepted, processed map[string]uint64) {
	am := make(map[string]uint64)
	pm := make(map[string]uint64)
	c.lock.RLock()
	defer c.lock.RUnlock()

	for _, sd := range c.services {
		sd.countLock.Lock()
		a, p := sd.acceptCount, sd.processedCount
		sd.countLock.Unlock()
		am[sd.name] = a
		pm[sd.name] = p
	}
	return am, pm
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
	config        *serviceConfiguration
	name          string
	serviceConfig *fspb.ClientServiceConfig
	working       sync.WaitGroup
	service       service.Service
	inbox         chan *fspb.Message

	countLock                   sync.Mutex // Protects acceptCount, processCount
	acceptCount, processedCount uint64
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

// Stats implements service.Context.
func (d *serviceData) Stats() stats.Collector {
	return d.config.client.stats
}

// processingLoop reads Fleetspeak message from d.inbox
// and terminates once this channel is closed.
func (d *serviceData) processingLoop(ctx context.Context) {
	for {
		m, ok := <-d.inbox

		d.countLock.Lock()
		d.processedCount++
		cnt := d.processedCount
		d.countLock.Unlock()

		if cnt&0x1f == 0 {
			select {
			case d.config.client.processingBeacon <- struct{}{}:
			default:
			}
		}

		if !ok {
			return
		}
		id, err := common.BytesToMessageID(m.MessageId)
		if err != nil {
			log.Errorf("ignoring message with bad message id: [%v]", m.MessageId)
			continue
		}
		err = d.service.ProcessMessage(ctx, m)
		if m.MessageType == "Die" && m.Destination != nil && m.Destination.ServiceName == "system" {
			// Die messages are special and pre-acked on the server side.
			// Don't send another ack or error report.
			continue
		}
		if err != nil {
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
