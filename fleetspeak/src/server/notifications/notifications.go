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

// Package notifications defines the plugin interface for components which allow
// the fleetspeak servers within an installation to notify each other about
// interesting events.
package notifications

import (
	"context"

	"github.com/google/fleetspeak/fleetspeak/src/common"
)

type Listener interface {
	// Start causes the Listener to begin listening for notifications. Once
	// started, should write to ids every time it is notified that there may be
	// new messages for a client.
	Start() (<-chan common.ClientID, error)

	// Stop causes the Listener to stop listening for notifications and close the
	// ids channel.
	Stop()

	// Address returns the address of this listener. These addresses must be
	// suitable to pass as a target to any Notifier compatible with this Listener.
	// Will only be called after Start().
	Address() string
}

type Notifier interface {
	// NewMessageForClient indicates that one or more messages have been written
	// and notifies the provided target of this.
	NewMessageForClient(ctx context.Context, target string, id common.ClientID) error
}
