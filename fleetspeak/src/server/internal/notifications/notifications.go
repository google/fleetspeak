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

// Package internal contains miscellaneous small components used internally by
// the Fleetspeak server.
package notifications

import (
	"context"
	"sync"

	log "github.com/golang/glog"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"golang.org/x/time/rate"
)

// Limit to 50 bulk notification calls per second.
const bulkNotificationMaxRate = rate.Limit(50.0)

// NoopListener implements notifications.Listener in a trivial way. It can be used
// as a listener when no listener is actually needed. i.e., when streaming connections
// are not being used.
type NoopListener struct {
	c chan common.ClientID
}

func (l *NoopListener) Start() (<-chan common.ClientID, error) {
	l.c = make(chan common.ClientID)
	return l.c, nil
}
func (l *NoopListener) Stop() {
	close(l.c)
}
func (l *NoopListener) Address() string {
	return ""
}

// NoopNotifier implements notifications.Listener in a trivial way. It can be
// used as a Notifier when no Notifier is actually needed. i.e., when streaming
// connections are not being used.
type NoopNotifier struct{}

func (n NoopNotifier) NewMessageForClient(ctx context.Context, target string, id common.ClientID) error {
	return nil
}

// LocalListenerNotifier is both a Listener and a Notifier. It self notifies to
// support streaming connections in a single server installation.
type LocalListenerNotifier struct {
	c chan common.ClientID
	l sync.RWMutex
}

func (n *LocalListenerNotifier) Start() (<-chan common.ClientID, error) {
	n.c = make(chan common.ClientID)
	return n.c, nil
}

func (n *LocalListenerNotifier) Stop() {
	n.l.Lock()
	close(n.c)
	n.c = nil
	n.l.Unlock()
}

func (n *LocalListenerNotifier) Address() string {
	return "local"
}

func (n *LocalListenerNotifier) NewMessageForClient(ctx context.Context, target string, id common.ClientID) error {
	if target != "local" {
		log.Warningf("Attempt to send non-local notification. Igoring.")
		return nil
	}
	n.l.RLock()
	defer n.l.RUnlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case n.c <- id:
	}
	return nil
}

// A Dispatcher connects dispatches incoming notifications according to the
// client that they are for.
type Dispatcher struct {
	l   sync.RWMutex
	m   map[common.ClientID]chan<- struct{}
	lim *rate.Limiter
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		m:   make(map[common.ClientID]chan<- struct{}),
		lim: rate.NewLimiter(bulkNotificationMaxRate, 20),
	}
}

// Dispatch sends a notification to the most recent registration for id. It is a
// no-op if there is already a notification pending for the id.
func (d *Dispatcher) Dispatch(id common.ClientID) {
	d.l.RLock()
	defer d.l.RUnlock()

	c, ok := d.m[id]
	if ok {
		select {
		case c <- struct{}{}:
		default:
			// channel is already pending - no need to add another signal to it.
		}
	}
}

// Register creates a registration for id. Once called, any call to Dispatch for
// id will cause a notification to passed through notice.
//
// The registration will be cleared and noticed will be closed when fin is
// called, or if another registration for id is created.
func (d *Dispatcher) Register(id common.ClientID) (notice <-chan struct{}, fin func()) {
	// Buffered with length 1 - combined with non-blocking write, expected
	// behavior that a notification can be buffered until the connection is ready
	// to read it, with no real blocking possible of the Dispatch method.
	ch := make(chan struct{}, 1)

	d.l.Lock()
	c, ok := d.m[id]
	if ok {
		close(c)
	}
	d.m[id] = ch
	d.l.Unlock()

	return ch, func() {
		d.l.Lock()
		defer d.l.Unlock()

		c, ok := d.m[id]
		if ok && c == ch {
			close(c)
			delete(d.m, id)
		}
	}
}

// NotifyAll effectively dispatches to every client currently registered.
func (d *Dispatcher) NotifyAll(ctx context.Context) {
	d.l.RLock()
	ids := make([]common.ClientID, 0, len(d.m))
	for k, _ := range d.m {
		ids = append(ids, k)
	}
	d.l.RUnlock()

	for _, id := range ids {
		if err := d.lim.Wait(ctx); err != nil {
			// We are probably just out of time, trust any remaining clients to notice
			// eventually, e.g. on reconnect or similar.
			return
		}
		d.Dispatch(id)
	}
}
