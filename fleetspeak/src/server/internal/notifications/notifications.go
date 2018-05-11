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

	"github.com/google/fleetspeak/fleetspeak/src/common"
)

// NoopListener implements notifications.Listener in a trivial way. It can be used
// as a listener when no listener is actually needed.
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
// used as a Notifier when no Notifier is actually needed.
type NoopNotifier struct{}

func (n NoopNotifier) NewMessageForClient(ctx context.Context, target string, id common.ClientID) error {
	return nil
}

// A Dispatcher connects dispatches incoming notifications according to the
// client that they are for.
type Dispatcher struct {
	l sync.RWMutex
	m map[common.ClientID]chan<- struct{}
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		m: make(map[common.ClientID]chan<- struct{}),
	}
}

// Dispatch sends a notification to the most recent registration for id. It is a
// no-op if there already a notification pending for the id.
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
	defer d.l.Unlock()

	c, ok := d.m[id]
	if ok {
		close(c)
	}
	d.m[id] = ch

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
