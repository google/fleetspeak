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

// Package comms defines the interface between the Fleetspeak client base
// library and the Communicator component used to talk to the server.
package comms

import (
	"context"
	"crypto"
	"crypto/x509"
	"io"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// A Communicator is a component which allows a client to communicate with a
// Fleetspeak server.
type Communicator interface {
	Setup(Context) error // Configure the communicator to work with Client.
	Start() error        // Tells the communicator to start sending and receiving messages.
	Stop()               // Tells the communicator to stop sending and receiving messages.

	// GetFileIfModified attempts to retrieve a file from a server, if it
	// has been modified since modSince. If it has not been modified, it
	// returns nil. Otherwise, it returns a ReadCloser for the file's data
	// and the last modified time.
	GetFileIfModified(ctx context.Context, service, name string, modSince time.Time) (data io.ReadCloser, mod time.Time, err error)
}

// A MessageInfo represents a message for the Communicator to send on to the FS
// server. Once taken from the Outbox, exactly one of Ack, Nack should be
// called.  If Nack is called the message may be delivered to another
// Communicator, or to this Communicator again.
type MessageInfo struct {
	M    *fspb.Message
	Ack  func()
	Nack func()
}

// A ClientIdentity contains what Communicator needs to know about a client's current identity.
type ClientIdentity struct {
	ID      common.ClientID
	Private crypto.PrivateKey
	Public  crypto.PublicKey
}

// A ServerInfo describes what a Communicator needs to know about the servers
// that it should communicate with.
type ServerInfo struct {
	// TrustedCerts contains the CA root certificates that the Communicator should
	// trust.
	TrustedCerts *x509.CertPool

	// The servers (in the form "<host>:<port>") that the Communicator should
	// attempt to communicate with.
	Servers []string
}

// A Context describes the view of the Fleetspeak client provided to a Communicator.
type Context interface {

	// Outbox returns a channel of MessageInfo records for the client to send to
	// the server. Once a MessageInfo is accepted, the Communicator commits to
	// calling exactly one of Ack, Nack.
	Outbox() <-chan MessageInfo

	// MakeContactData creates a WrappedContactData containing messages to be sent to the server.
	MakeContactData([]*fspb.Message) (*fspb.WrappedContactData, error)

	// ProcessContactData processes a ContactData recevied from the server.
	ProcessContactData(*fspb.ContactData) error

	// ChainRevoked takes an x509 certificate chain, and returns true if any link
	// of the chain has been revoked.
	ChainRevoked(chains []*x509.Certificate) bool

	// CurrentID returns the current client id.
	CurrentID() common.ClientID

	// CurrentIdentity returns the client's full identifying information.
	CurrentIdentity() (ClientIdentity, error)

	// ServerInfo returns the servers that the client should attempt to
	// communicate with.
	ServerInfo() (ServerInfo, error)

	// CommunicatorConfig returns the client's CommunicatorConfig.
	CommunicatorConfig() *clpb.CommunicatorConfig
}
