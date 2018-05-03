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

// Package comms defines the interface used by the Fleetspeak modules which
// communicate with clients.
package comms

import (
	"crypto"
	"errors"
	"net"
	"time"

	"context"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// A Communicator can communicate with clients through some means (HTTP, etc).
type Communicator interface {
	Setup(Context) error // Configure the communicator to work with a Server.
	Start() error        // Tells the communicator to start sending and receiving messages.
	Stop()               // Tells the communicator to stop sending and receiving messages.
}

// A ClientInfo is the basic information that we have about a client.
type ClientInfo struct {
	ID          common.ClientID
	Key         crypto.PublicKey
	Labels      []*fspb.Label
	Blacklisted bool
	Cached      bool // Whether the data was retrieved from a cache.
}

// A ConnectionInfo is the static information that the FS system gathers about a
// particular connection to a client.
type ConnectionInfo struct {
	Addr                     net.Addr
	Client                   ClientInfo
	ContactID                db.ContactID
	NonceSent, NonceReceived uint64
	AuthClientInfo           authorizer.ClientInfo
}

// ErrNotAuthorized is returned by certain methods to indicate that the client
// is not authorized to communicate with this server.
var ErrNotAuthorized = errors.New("not authorized")

// A Context defines the view of the Fleetspeak server provided to a Communicator.
type Context interface {

	// InitializeConnection attempts to validate and configure a client
	// connection, and performs an initial exchange of messages.
	//
	// On success this effectively calls both HandleMessagesFromClient
	// and GetMessagesForClient.
	//
	// The returned ContactData should be sent back to the client unconditionally,
	// in order to update the client's de-duplication nonce.
	InitializeConnection(ctx context.Context, addr net.Addr, key crypto.PublicKey, wcd *fspb.WrappedContactData) (*ConnectionInfo, *fspb.ContactData, error)

	// HandleContactData processes the messags contined in a WrappedContactData
	// received from the client.  The ConnectionInfo parameter should have been created by
	// an InitializeConnection call made for this connection.
	HandleMessagesFromClient(ctx context.Context, info *ConnectionInfo, wcd *fspb.WrappedContactData) error

	// GetMessagesForClient finds unprocessed messages for a given client and
	// reserves them for processing. The ConnectionInfo parameter should have been
	// created by an InitializeConnection call made for this connection.
	//
	// Returns nil, nil when there are no outstanding messages for the client.
	GetMessagesForClient(ctx context.Context, info *ConnectionInfo) (*fspb.ContactData, error)

	// ReadFile returns the data and modification time of file. Caller is
	// responsible for closing data.
	//
	// Calls to data are permitted to fail if ctx is canceled or expired.
	ReadFile(ctx context.Context, service, name string) (data db.ReadSeekerCloser, modtime time.Time, err error)

	// IsNotFound returns whether an error returned by ReadFile indicates that the
	// file was not found.
	IsNotFound(err error) bool

	// StatsCollector returns the stats.Collector used by the Fleetspeak
	// system.
	StatsCollector() stats.Collector

	// Authorizer returns the authorizer.Authorizer used by the Fleetspeak
	// system.  The Communicator is responsible for calling Accept1.
	Authorizer() authorizer.Authorizer
}
