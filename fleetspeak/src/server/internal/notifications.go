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
package internal

import (
	"context"

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
