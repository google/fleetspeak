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

// Package client contains the components and utilities that every Fleetspeak client should include.
package client

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/flow"
	intconfig "github.com/google/fleetspeak/fleetspeak/src/client/internal/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/internal/message"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/signer"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
	"github.com/google/fleetspeak/fleetspeak/src/common"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

const (
	// Maximum number of messages/bytes to buffer, per priority level.
	maxBufferCount = 100
	maxBufferBytes = 20 * 1024 * 1024
)

// Components gathers the plug-ins used to instantiate a Fleetspeak Client.
type Components struct {
	Communicator     comms.Communicator         // Required to communicate with a Fleetspeak server.
	ServiceFactories map[string]service.Factory // Required to instantiate any local services.
	Signers          []signer.Signer            // If set, will be given a chance to sign data before sending it to the server.
	Filter           *flow.Filter               // If set, will be used to filter messages to the server.
	Stats            stats.Collector
}

// A Client is an active fleetspeak client instance.
type Client struct {
	// Process id for the client.
	pid int
	// Time when the client was started (set by client.New())
	startTime time.Time

	cfg config.Configuration
	com comms.Communicator
	sc  *serviceConfiguration

	// outbox produces prioritized MessageInfo records from the FS client buffer.
	// These are drained by the Communicator component, which tries to send them
	// to the FS server.
	outbox chan comms.MessageInfo
	// outUnsorted produces unsorted buffered messages
	outUnsorted chan comms.MessageInfo
	// out(High|Medium|Low) feed buffers of different priorities.
	outHigh   chan service.AckMessage
	outMedium chan service.AckMessage
	outLow    chan service.AckMessage
	// used to wait until the retry and sort loop goroutines are done
	retryLoopsDone sync.WaitGroup
	sortLoopDone   sync.WaitGroup

	acks    chan common.MessageID
	errs    chan *fspb.MessageErrorData
	signers []signer.Signer
	config  *intconfig.Manager

	processingBeacon chan struct{}
	stats            stats.Collector
}

// New creates a new Client object based on the provided components.
//
// clientLabels becomes a list of hardcoded labels of the form "client:<label>",
// which is reported to the server as an initial set of labels for this client.
// In addition to those provided to NewClient, the client will also include
// labels indicating the CPU architecture and OS that the client was build for
// (based on runtime.GOARCH and runtime.GOOS).
//
// TODO: Add support for multiple Communicators.
func New(cfg config.Configuration, cmps Components) (*Client, error) {
	if cmps.Stats == nil {
		cmps.Stats = stats.NoopCollector{}
	}

	configChanges := make(chan *fspb.ClientInfoData)
	cm, err := intconfig.StartManager(&cfg, configChanges, cmps.Stats)
	if err != nil {
		return nil, fmt.Errorf("bad configuration: %v", err)
	}

	ret := &Client{
		pid:       os.Getpid(),
		startTime: time.Now(),

		cfg: cfg,
		com: cmps.Communicator,

		outbox:      make(chan comms.MessageInfo),
		outUnsorted: make(chan comms.MessageInfo),
		outLow:      make(chan service.AckMessage),
		outMedium:   make(chan service.AckMessage),
		outHigh:     make(chan service.AckMessage),

		sc: &serviceConfiguration{
			services:  make(map[string]*serviceData),
			factories: cmps.ServiceFactories,
		},
		config:  cm,
		acks:    make(chan common.MessageID, 500),
		errs:    make(chan *fspb.MessageErrorData, 50),
		signers: cmps.Signers,

		processingBeacon: make(chan struct{}, 1),
		stats:            cmps.Stats,
	}
	ret.sc.client = ret
	ret.retryLoopsDone.Add(3)
	go func() {
		message.RetryLoop(ret.outLow, ret.outUnsorted, cmps.Stats, maxBufferBytes, maxBufferCount)
		ret.retryLoopsDone.Done()
	}()
	go func() {
		message.RetryLoop(ret.outMedium, ret.outUnsorted, cmps.Stats, maxBufferBytes, maxBufferCount)
		ret.retryLoopsDone.Done()
	}()
	go func() {
		message.RetryLoop(ret.outHigh, ret.outUnsorted, cmps.Stats, maxBufferBytes, maxBufferCount)
		ret.retryLoopsDone.Done()
	}()
	f := cmps.Filter
	if f == nil {
		f = flow.NewFilter()
	}
	ret.sortLoopDone.Add(1)
	go func() {
		message.SortLoop(ret.outUnsorted, ret.outbox, f)
		ret.sortLoopDone.Done()
	}()

	ssd := &serviceData{
		config: ret.sc,
		name:   "system",
		service: &systemService{
			client:        ret,
			configChanges: configChanges,
		},
		inbox: make(chan *fspb.Message, 5),
	}
	ret.sc.services["system"] = ssd
	ssd.start()
	ssd.working.Add(1)
	go func() {
		defer ssd.working.Done()
		ssd.processingLoop(context.TODO())
	}()

	for _, s := range cfg.FixedServices {
		if err := ret.sc.InstallService(s, nil); err != nil {
			log.Errorf("Unable to install fixed service [%s]: %v", s.Name, err)
		}
	}

	if ss, err := ret.cfg.PersistenceHandler.ReadSignedServices(); err != nil {
		log.Warningf("No signed service configs could be read; continuing: %v", err)
	} else {
		for _, s := range ss {
			if err := ret.sc.InstallSignedService(s); err != nil {
				log.Warningf("Unable to install signed service, ignoring: %v", err)
			}
		}
	}

	if ss, err := ret.cfg.PersistenceHandler.ReadServices(); err != nil {
		log.Warningf("No unsigned service configs could be read; continuing: %v", err)
	} else {
		for _, s := range ss {
			if err := ret.sc.InstallService(s, nil); err != nil {
				log.Warningf("Unable to install service [%s], ignoring: %v", s.Name, err)
			}
		}
	}

	if ret.com != nil {
		cctx := commsContext{c: ret}
		if err := ret.com.Setup(cctx); err != nil {
			ssd.stop()
			return nil, fmt.Errorf("unable to configure communicator: %v", err)
		}
		ret.com.Start()
		ssd.service.(*systemService).pollRevokedCerts()
	}
	cm.Sync()
	cm.SendConfigUpdate()
	return ret, nil
}

// ProcessMessage accepts a message into the Fleetspeak system.
//
// If m is for a service on the local client it will ask the service to process
// it. If m for the a server component, it will queue up the message to be
// delivered to the server. Fleetspeak does not support direct messages from one
// client to another.
func (c *Client) ProcessMessage(ctx context.Context, am service.AckMessage) (err error) {
	m := am.M

	var isLocal bool
	defer func() {
		c.stats.AfterMessageProcessed(m, isLocal, err)
	}()

	if m.Destination == nil || m.Destination.ServiceName == "" {
		return fmt.Errorf("destination must have ServiceName, got: %v", m.Destination)
	}
	if clientID := m.Destination.ClientId; len(clientID) != 0 {
		// This is a local message. No need to send it to the server.
		isLocal = true
		if myID := c.config.ClientID().Bytes(); !bytes.Equal(clientID, myID) {
			return fmt.Errorf("cannot send directly to client %x from client %x", clientID, myID)
		}
		return c.sc.ProcessMessage(ctx, m)
	}

	var out chan service.AckMessage
	switch m.Priority {
	case fspb.Message_LOW:
		out = c.outLow
	case fspb.Message_MEDIUM:
		out = c.outMedium
	case fspb.Message_HIGH:
		out = c.outHigh
	default:
		log.Warningf("Received message with unknown priority %v, treating as Medium.", m.Priority)
		m.Priority = fspb.Message_MEDIUM
		out = c.outMedium
	}

	select {
	case out <- am:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop shuts the client down gracefully. This includes stopping all communicators and services.
func (c *Client) Stop() {
	log.Info("Stopping client...")
	if c.com != nil {
		c.com.Stop()
	}
	c.sc.Stop()
	c.config.Stop()
	log.Info("Components have been stopped.")

	// From here, shutdown is a little subtle:
	//
	// - At this point, the communicator is off, so nothing else should be
	//   draining outbox. We do this ourselves and Ack everything so that the
	//   RetryLoops are guaranteed to terminate.
	//
	// - The fake Acks in 1) are safe because the config manager is stopped.
	//   This means that client services are shut down and the Acks will not be
	//   reported outside of this process.

	close(c.outLow)
	close(c.outMedium)
	close(c.outHigh)
	c.retryLoopsDone.Wait()
	log.Info("Retry loops have terminated.")

	// - Now, no more messages enter outUnsorted.
	//
	// - We close outUnsorted and drain outbox, to make sure no messages are lost.
	//   Once these two things are complete, SortLoop will return, and the client
	//   can be shut down.

	done := make(chan struct{})
	close(c.outUnsorted)
	go func() {
		for {
			select {
			case m := <-c.outbox:
				m.Ack()
			case <-done:
				return
			}
		}
	}()
	c.sortLoopDone.Wait()
	done <- struct{}{}
	log.Info("Messages have been drained.")
}
