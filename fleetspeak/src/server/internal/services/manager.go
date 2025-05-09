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

// Package services defines internal fleetspeak components relating to services.
package services

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/golang/glog"
	"golang.org/x/time/rate"
	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/cache"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/ftime"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

const MaxServiceFailureReasonLength = 900

// A Manager starts, remembers, and shuts down services.
type Manager struct {
	services        map[string]*liveService
	dataStore       db.Store
	serviceRegistry map[string]service.Factory // Used to look up the correct factory when configuring services.
	stats           stats.Collector
	cc              *cache.Clients
}

// NewManager creates a new manager using the provided components. Initially it only contains the 'system' service.
func NewManager(dataStore db.Store, serviceRegistry map[string]service.Factory, stats stats.Collector, clientCache *cache.Clients) *Manager {
	m := Manager{
		services:        make(map[string]*liveService),
		dataStore:       dataStore,
		serviceRegistry: serviceRegistry,
		stats:           stats,
		cc:              clientCache,
	}

	ssd := liveService{
		manager:        &m,
		name:           "system",
		maxParallelism: 100,
		pLogLimiter:    rate.NewLimiter(rate.Every(10*time.Second), 1),
	}
	ss := systemService{
		sctx:      &ssd,
		stats:     stats,
		datastore: dataStore,
		cc:        clientCache,
	}
	ssd.service = &ss
	m.services["system"] = &ssd
	ss.Start(&ssd)

	return &m
}

// clientData returns client data corresponding to client that is the source of the given message.
func (c *Manager) clientData(ctx context.Context, m *fspb.Message) (*db.ClientData, error) {
	cID, err := common.BytesToClientID(m.Source.ClientId)
	if err != nil || cID.IsNil() {
		return nil, fmt.Errorf("invalid source client id[%v]: %v", m.Source.ClientId, err)
	}

	cData, _, err := c.cc.GetOrRead(ctx, cID, c.dataStore)
	if err != nil {
		return nil, fmt.Errorf("can't get client data for id[%v]: %v", cID, err)
	}

	return cData, nil
}

// Install adds a service to the configuration, removing any existing service with
// the same name.
func (c *Manager) Install(cfg *spb.ServiceConfig) error {
	cfg = proto.Clone(cfg).(*spb.ServiceConfig)

	f := c.serviceRegistry[cfg.Factory]
	if f == nil {
		return fmt.Errorf("unable to find factory [%v]", cfg.Factory)
	}
	// "system" is a special service handling configuration and other
	// message passing for Fleetspeak itself. "client" is the service name
	// used for labels set by (and known by) the base Fleetspeak client
	// itself.
	if cfg.Name == "" || cfg.Name == "system" || cfg.Name == "client" {
		return fmt.Errorf("illegal service name [%v]", cfg.Name)
	}

	s, err := f(cfg)
	if err != nil {
		return err
	}

	if cfg.MaxParallelism == 0 {
		cfg.MaxParallelism = 100
	}

	d := liveService{
		manager: c,
		name:    cfg.Name,
		service: s,

		maxParallelism: cfg.MaxParallelism,
		pLogLimiter:    rate.NewLimiter(rate.Every(10*time.Second), 1),
	}

	if err = s.Start(&d); err != nil {
		return err
	}
	c.services[cfg.Name] = &d

	log.Infof("Installed %v service.", cfg.Name)
	return nil
}

// Stop closes and removes all services in the configuration.
func (c *Manager) Stop() {
	for _, d := range c.services {
		d.stop()
	}
	c.services = map[string]*liveService{}
}

// ShouldProcessMessageBatches returns true if the specified service is
// configured to process messages in batches.
func (c *Manager) ShouldProcessMessageBatches(serviceName string) bool {
	svc := c.services[serviceName]
	if svc == nil {
		return false
	}

	_, ok := svc.service.(service.BatchedService)
	return ok
}

// ProcessMessageBatch processes a batch of messages using the specified
// service.
func (c *Manager) ProcessMessageBatch(ctx context.Context, serviceName string, msgs []*fspb.Message) {
	svc := c.services[serviceName]
	if svc == nil {
		log.ErrorContextf(ctx, "No such service: %v", serviceName)
		return
	}

	batchedSvc, ok := svc.service.(service.BatchedService)
	if !ok {
		log.ErrorContextf(ctx, "Service %v does not implement BatchedService", serviceName)
	}

	if err := batchedSvc.ProcessMessageBatch(ctx, msgs); err != nil {
		log.ErrorContextf(ctx, "Process batched messages: %v", err)
	}
}

// ProcessMessages implements MessageProcessor and is called by the datastore on
// backlogged messages.
func (c *Manager) ProcessMessages(msgs []*fspb.Message) {
	ctx, fin := context.WithTimeout(context.Background(), 30*time.Second)

	hasResult := make([]bool, len(msgs))

	var working sync.WaitGroup
	working.Add(len(msgs))

	for idx, msg := range msgs {
		i, m := idx, msg
		go func() {
			defer working.Done()
			l := c.services[m.Destination.ServiceName]
			if l == nil {
				log.Errorf("Message in datastore [%v] is for unknown service [%s].", hex.EncodeToString(m.MessageId), m.Destination.ServiceName)
				return
			}
			cData, err := c.clientData(ctx, m)
			if err != nil {
				log.Warningf("Message in datastore [%v] for service [%s] is from unknown client: %v.", hex.EncodeToString(m.MessageId), m.Destination.ServiceName, err)
			}

			c.stats.MessageIngested(true, m, cData)
			res := l.processMessage(ctx, m, false)
			if res != nil {
				hasResult[i] = true
				m.Result = res
			}
		}()
	}
	working.Wait()
	fin()

	toSave := make([]*fspb.Message, 0, len(msgs))
	for i, m := range msgs {
		if hasResult[i] {
			toSave = append(toSave, m)
		}
	}
	if len(toSave) == 0 {
		return
	}
	ctx, fin = context.WithTimeout(context.Background(), 15*time.Second)
	defer fin()
	if err := c.dataStore.StoreMessages(ctx, toSave, ""); err != nil {
		log.Errorf("Error saving results for %d messages: %v", len(toSave), err)
	}
}

// processMessage attempts to processes m, returning a fspb.MessageResult. It
// also updates stats, calling exactly one of MessageDropped, MessageFailed,
// MessageProcessed.
func (s *liveService) processMessage(ctx context.Context, m *fspb.Message, isFirstTry bool) *fspb.MessageResult {
	cData, err := s.manager.clientData(ctx, m)
	if err != nil {
		log.Warningf("Couldn't fetch client data for the message: %v", err)
	}

	if cData == nil {
		log.Warningf("Can't annotate message with blocklisted status [service=%s] as client data couldn't be fetched.", s.name)
	} else {
		m.IsBlocklistedSource = cData.Blacklisted
	}

	p := atomic.AddUint32(&s.parallelism, 1)
	// Documented decrement operation.
	// https://golang.org/pkg/sync/atomic/#AddUint32
	defer atomic.AddUint32(&s.parallelism, ^uint32(0))
	if p > s.maxParallelism {
		if s.pLogLimiter.Allow() {
			log.Warningf("%s: Overloaded with %d concurrent messages, dropping excess, will retry.", s.name, s.maxParallelism)
		}
		s.manager.stats.MessageDropped(m, isFirstTry, cData)
		return nil
	}

	mid, err := common.BytesToMessageID(m.MessageId)
	if err != nil || mid.IsNil() {
		// message id should be validated before it gets to us.
		log.Fatalf("Invalid message id presented for processing: %v, %v", m.MessageId, err)
	}

	start := ftime.Now()
	e := s.service.ProcessMessage(ctx, m)
	switch {
	case e == nil:
		s.manager.stats.MessageProcessed(start, ftime.Now(), m, isFirstTry, cData)
		return &fspb.MessageResult{ProcessedTime: db.NowProto()}
	case service.IsTemporary(e):
		s.manager.stats.MessageErrored(start, ftime.Now(), true, m, isFirstTry, cData)
		log.Warningf("%s: Temporary error processing message %v, will retry: %v", s.name, mid, e)
		return nil
	case !service.IsTemporary(e):
		s.manager.stats.MessageErrored(start, ftime.Now(), false, m, isFirstTry, cData)
		log.Errorf("%s: Permanent error processing message %v, giving up: %v", s.name, mid, e)
		failedReason := e.Error()
		if len(failedReason) > MaxServiceFailureReasonLength {
			failedReason = failedReason[:MaxServiceFailureReasonLength-3] + "..."
		}
		return &fspb.MessageResult{
			ProcessedTime: db.NowProto(),
			Failed:        true,
			FailedReason:  failedReason,
		}
	}
	log.Fatal("Error is neither temporary or permanent.")
	return nil
}

// HandleNewMessages handles newly arrived messages that should be processed on
// the fleetspeak server. This handling includes validating that we recognize
// its ServiceNames, saving the messages to the datastore and attempting to
// process them.
func (c *Manager) HandleNewMessages(ctx context.Context, msgs []*fspb.Message, contact db.ContactID) error {
	now := db.NowProto()
	for _, m := range msgs {
		if m.Destination == nil || len(m.Destination.ClientId) != 0 {
			return fmt.Errorf("HandleNewMessage called with bad Destination: %v", m.Destination)
		}
		m.CreationTime = now
	}

	// Try to processes all the messages in parallel, with a 30 second timeout.
	ctx1, fin1 := context.WithTimeout(ctx, 30*time.Second)
	var wg sync.WaitGroup
	wg.Add(len(msgs))
	for _, msg := range msgs {
		m := msg
		go func() {
			defer wg.Done()
			l := c.services[m.Destination.ServiceName]
			if l == nil {
				log.Errorf("Received new message [%v] for unknown service [%s].", hex.EncodeToString(m.MessageId), m.Destination.ServiceName)
				return
			}

			cData, err := c.clientData(ctx1, m)
			if err != nil {
				log.Warningf("Can't get client data for message [%v] for service [%s] is from unknown client: %v.", hex.EncodeToString(m.MessageId), m.Destination.ServiceName, err)
			}
			c.stats.MessageIngested(false, m, cData)

			res := l.processMessage(ctx1, m, true)
			if res == nil {
				return
			}
			m.Result = res
			m.Data = nil
		}()
	}
	wg.Wait()
	fin1()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	ctx2, fin2 := context.WithTimeout(ctx, 30*time.Second)
	defer fin2()

	// Record that we are saving messages.
	for _, m := range msgs {
		cData, err := c.clientData(ctx2, m)
		if err != nil {
			log.Warningf("Can't get client data for message [%v] for service [%s] is from unknown client: %v.", hex.EncodeToString(m.MessageId), m.Destination.ServiceName, err)
		}

		c.stats.MessageSaved(false, m, cData)
	}

	return c.dataStore.StoreMessages(ctx2, msgs, contact)
}

// A liveService is a running Service, including implementation provided by the
// associated ServiceFactory and bookkeeping structures and methods.
type liveService struct {
	manager *Manager
	name    string
	service service.Service

	parallelism    uint32 // Current number of calls, used for load shedding. atomic access only.
	maxParallelism uint32
	pLogLimiter    *rate.Limiter
}

func (s *liveService) stop() {
	if err := s.service.Stop(); err != nil {
		log.Errorf("Error shutting down service [%v]: %v", s.name, err)
	}
}

// Send implements service.Context.
func (s *liveService) Send(ctx context.Context, m *fspb.Message) error {
	m.Source = &fspb.Address{ServiceName: s.name}
	if len(m.Destination.ClientId) == 0 {
		return s.manager.HandleNewMessages(ctx, []*fspb.Message{m}, "")
	}

	return s.manager.dataStore.StoreMessages(ctx, []*fspb.Message{m}, "")
}

// GetClientData implements service.Context.
func (s *liveService) GetClientData(ctx context.Context, id common.ClientID) (*db.ClientData, error) {
	cd, _, err := s.manager.cc.GetOrRead(ctx, id, s.manager.dataStore)
	if err != nil {
		return nil, err
	}
	return cd, nil
}
