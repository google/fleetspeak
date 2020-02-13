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

package https

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/watchdog"
	"github.com/google/fleetspeak/fleetspeak/src/common"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// Communicator implements comms.Communicator and communicates with a Fleetspeak
// server using https.
type Communicator struct {
	cctx        comms.Context
	conf        *clpb.CommunicatorConfig
	id          common.ClientID
	hc          *http.Client
	ctx         context.Context
	done        context.CancelFunc
	working     sync.WaitGroup
	hosts       []string
	hostLock    sync.RWMutex                                                      // Protects hosts
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error) // If set, will be used to initiate network connections to the server.

	// 1 hour watchdog for server communication attempts.
	wd *watchdog.Watchdog
}

func (c *Communicator) Setup(cl comms.Context) error {
	c.cctx = cl
	return c.configure()
}

func (c *Communicator) configure() error {
	id, tr, err := makeTransport(c.cctx, c.DialContext)
	if err != nil {
		return err
	}
	c.id = id

	si, err := c.cctx.ServerInfo()
	if err != nil {
		return fmt.Errorf("unable to configure communicator, could not get server information: %v", err)
	}
	c.hostLock.Lock()
	c.hosts = append([]string(nil), si.Servers...)
	c.hostLock.Unlock()

	if len(c.hosts) == 0 {
		return errors.New("no server_addresses in client configuration")
	}

	c.conf = c.cctx.CommunicatorConfig()
	if c.conf == nil {
		return errors.New("no communicator_config in client configuration")
	}
	if c.conf.MaxPollDelaySeconds == 0 {
		c.conf.MaxPollDelaySeconds = 60 * 5
	}
	if c.conf.MaxBufferDelaySeconds == 0 {
		c.conf.MaxBufferDelaySeconds = 5
	}
	if c.conf.MinFailureDelaySeconds == 0 {
		c.conf.MinFailureDelaySeconds = 60 * 5
	}
	if c.conf.FailureSuicideTimeSeconds == 0 {
		c.conf.FailureSuicideTimeSeconds = 60 * 60 * 24 * 7
	}

	c.hc = &http.Client{
		Transport: tr,
		Timeout:   5 * time.Minute,
	}
	c.ctx, c.done = context.WithCancel(context.Background())
	return nil
}

func (c *Communicator) Start() error {
	c.wd = watchdog.MakeWatchdog(watchdog.DefaultDir, "fleetspeak-polling-traces-", time.Hour, true)
	c.working.Add(1)
	go c.processingLoop()
	return nil
}

func (c *Communicator) Stop() {
	c.done()
	c.working.Wait()
	c.wd.Stop()
}

// processingLoop polls the server according to the configured policies while
// the communicator is active.
//
// It is run on an internal goroutine by c.Start, stops when c.ctx is canceled
// and indicates completion by decrementing c.working.
func (c *Communicator) processingLoop() {
	defer c.working.Done()

	// These are the variables that we need to keep tract of between polls.

	// Message we are trying to deliver. Nack anything leftover during shutdown.
	var toSend []comms.MessageInfo
	defer func() {
		for _, m := range toSend {
			m.Nack()
		}
	}()

	var toSendSize int // approximate size of toSend in bytes
	var lastPoll, oldestUnsent, lastActive time.Time

	// poll performs a poll (actually implemented by c.poll), records any errors
	// and updates the variables defined above. In case of failure it also sleeps
	// for the MinFailureDelay.
	poll := func() {
		c.wd.Reset()
		if c.cctx.CurrentID() != c.id {
			c.configure()
		}
		active, err := c.poll(toSend)
		if err != nil {
			log.Warningf("Failure during polling: %v", err)
			for _, m := range toSend {
				m.Nack()
			}
			toSend = nil
			toSendSize = 0

			if (lastPoll != time.Time{}) && (time.Since(lastPoll) > time.Duration(c.conf.FailureSuicideTimeSeconds)*time.Second) {
				// Die in the hopes that our replacement will be better configured, or otherwise have better luck.
				log.Fatalf("Too Lonely! Failed to contact server in %v.", time.Since(lastPoll))
			}

			t := time.NewTimer(jitter(c.conf.MinFailureDelaySeconds))
			select {
			case <-t.C:
			case <-c.ctx.Done():
				t.Stop()
			}
			return
		}
		for _, m := range toSend {
			m.Ack()
		}
		toSend = nil
		toSendSize = 0
		oldestUnsent = time.Time{}
		lastPoll = time.Now()
		if active {
			lastActive = time.Now()
		}
	}

	for {
		// Stop if we are shutting down, e.g. during the wait after a previous poll
		// failure.
		if c.ctx.Err() != nil {
			return
		}

		// Compute the time that we should next send (assuming we don't hit a send
		// threshold). This could be MaxPollDelay after the last successful send.
		deadline := lastPoll.Add(jitter(c.conf.MaxPollDelaySeconds))
		if log.V(2) {
			log.Infof("Base wait of %v", deadline.Sub(time.Now()))
		}

		// If we received something recently, we reduce it to 200ms + 1/10 of the
		// time since we last received a message. (Instructions often lead to more
		// instructions.)
		if !lastActive.IsZero() {
			fpd := lastPoll.Add(200*time.Millisecond + time.Since(lastActive)/10)
			if fpd.Before(deadline) {
				deadline = fpd
				if log.V(2) {
					log.Infof("Last active %v ago, reduced wait to %v.", time.Since(lastActive), deadline.Sub(time.Now()))
				}
			}
		}

		// If we already have something, we should wait at most MaxBufferDelay from
		// receipt of it before sending.
		if !oldestUnsent.IsZero() {
			bd := oldestUnsent.Add(jitter(c.conf.MaxBufferDelaySeconds))
			if bd.Before(deadline) {
				deadline = bd
			}
		}

		now := time.Now()
		if now.After(deadline) || toSendSize > sendBytesThreshold || len(toSend) >= sendCountThreshold {
			log.V(1).Info("Polling without delay.")
			poll()
			if c.ctx.Err() != nil {
				return
			}
			continue
		}

		delay := deadline.Sub(now)
		t := time.NewTimer(delay)

		if delay > closeWaitThreshold {
			// Our planned sleep is longer than the idle timeout, so go ahead and kill
			// any idle connection now.
			c.hc.Transport.(*http.Transport).CloseIdleConnections()
		}
		log.V(1).Infof("Waiting %v for next poll.", delay)

		select {
		case <-c.ctx.Done():
			t.Stop()
			if toSendSize > 0 {
				poll()
			}
			return
		case <-t.C:
			poll()
		case m := <-c.cctx.Outbox():
			t.Stop()
			toSend = append(toSend, m)
			toSendSize += 2 + proto.Size(m.M)
			if toSendSize >= sendBytesThreshold ||
				len(toSend) >= sendCountThreshold {
				poll()
			} else {
				if oldestUnsent.IsZero() {
					oldestUnsent = time.Now()
				}
			}
		}
	}
}

func (c *Communicator) poll(toSend []comms.MessageInfo) (bool, error) {
	var sent bool // records whether an interesting (non-LOW) priority message was sent.
	msgs := make([]*fspb.Message, 0, len(toSend))
	for _, m := range toSend {
		msgs = append(msgs, m.M)
		if !m.M.Background {
			if !sent && bool(log.V(2)) {
				log.Infof("Activity: %s - %s", m.M.Destination.ServiceName, m.M.MessageType)
			}
			sent = true
		}
	}
	d, _, err := c.cctx.MakeContactData(msgs, nil)
	if err != nil {
		return false, err
	}
	data, err := proto.Marshal(d)

	if err != nil {
		return false, fmt.Errorf("unable to marshal outgoing messages: %v", err)
	}

	for i, h := range c.hosts {
		u := url.URL{Scheme: "https", Host: h, Path: "/message"}

		resp, err := c.hc.Post(u.String(), "", bytes.NewReader(data))
		if err != nil {
			log.Warningf("POST to %v failed with error: %v", u, err)
			continue
		}

		if resp.StatusCode != 200 {
			log.Warningf("POST to %v failed with status: %v", u, resp.StatusCode)
			continue
		}

		var b bytes.Buffer
		if _, err := b.ReadFrom(resp.Body); err != nil {
			resp.Body.Close()
			log.Warning("Unable to read response body.")
			continue
		}
		resp.Body.Close()

		var r fspb.ContactData
		if err := proto.Unmarshal(b.Bytes(), &r); err != nil {
			log.Warningf("Unable to parse ContactData from server: %v", err)
			continue
		}

		if err := c.cctx.ProcessContactData(context.TODO(), &r, false); err != nil {
			log.Warningf("Error processing ContactData from server: %v", err)
			continue
		}

		if i != 0 {
			c.hostLock.Lock()
			// Swap, so we check this host first next time.
			c.hosts[0], c.hosts[i] = c.hosts[i], c.hosts[0]
			c.hostLock.Unlock()
		}
		return sent || (len(r.Messages) != 0), nil
	}
	return false, errors.New("unable to contact any server")
}

func (c *Communicator) GetFileIfModified(ctx context.Context, service, name string, modSince time.Time) (io.ReadCloser, time.Time, error) {
	c.hostLock.RLock()
	hosts := append([]string(nil), c.hosts...)
	c.hostLock.RUnlock()
	return getFileIfModified(ctx, hosts, c.hc, service, name, modSince)
}
