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

// Package authorizer defines interfaces and utility methods to validate and
// limit client communications.
package authorizer

import (
	"crypto/x509"
	"net"

	"github.com/google/fleetspeak/fleetspeak/src/common"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// ContactInfo contains information deducible from TLS negotiation and
// very basic analysis of the provided contact data.
type ContactInfo struct {
	ID           common.ClientID
	ContactSize  int      // The size of the contact, in bytes.
	ClientLabels []string // The client labels included in the contact.
}

// ClientInfo contains information known once the client has been
// looked up in the datastore.
type ClientInfo struct {
	New    bool          // Whether the client is new. True if the client is not yet in our datastore.
	Labels []*fspb.Label // The labels currently set for this client.
}

// SignatureInfo provides information about a signature included with
// the contact data.
type SignatureInfo struct {
	// The certificate chain provided with the signature.
	Certificate []*x509.Certificate

	// The signature algorithm used.
	Algorithm x509.SignatureAlgorithm

	// True if the signature provided with the message could be verified against
	// the first certificate in the chain.
	//
	// NOTE: The FS system only performs, and this bit only represents the result
	// of a simple signature check against Certificate[0].  The Authorizer
	// implementation is responsible for verifying every other aspect of chain. In
	// particular, an Authorizer will typically want to verify that provided
	// certificates really do chain together, that none of the certificates in it
	// have been revoked, and that the final certificate in the chain is signed by
	// a trusted authority.
	Valid bool
}

// An Authorizer regulates communication with fleetspeak
// clients. Specific installations can implement it to perform
// application layer DOS protection and to check signatures provided
// by clients.
//
// The Allow methods will be called in sequence as the contact is
// analyzed and processed. The sooner a contact can be blocked, the
// less server resources are wasted. So any particular DOS mitigation
// strategy should be implemented in the first Allow method which has
// sufficient data.
//
// Note that Fleetspeak intentionally does not produce log messages or
// store information about blocked contacts. This minimizes the effect
// of a blocked DOS attack on the rest of the system, but means that
// an Authorizer is also responsible for any monitoring or analysis of
// such attacks.
type Authorizer interface {
	// Allow1 is called when a network connection is opened.
	//
	// If it returns true, processing will continue, otherwise the
	// connection will be immediately closed.
	Allow1(net.Addr) bool

	// Allow2 is called after basic analysis of the contact has been
	// performed.
	//
	// If it returns true, processing will continue, otherwise
	// processing will end and the client will receive an http 503
	// or equivalent error code.
	Allow2(net.Addr, ContactInfo) bool

	// Allow3 is called after the datastore has been read to find
	// basic information on the client.
	//
	// If it returns true, processing will continue, otherwise
	// processing will end and the client will receive an http 503
	// or equivalent error code.
	Allow3(net.Addr, ContactInfo, ClientInfo) bool

	// Allow4 is called immediately before actually saving and
	// processing the messages provided by the client.
	//
	// When accept=true, all messages will have their
	// validation_info field set to the returned string, and then
	// they will be saved and processed. When accept=false,
	// processing will end, nothing will be saved, and the client
	// will receive an http 503 or equivalent error code.
	Allow4(net.Addr, ContactInfo, ClientInfo, []SignatureInfo) (accept bool, vi *fspb.ValidationInfo)
}

// PermissiveAuthorizer is a trival Authorizer which permits all operations.
type PermissiveAuthorizer struct{}

// Allow1 implements Authorizer.
func (PermissiveAuthorizer) Allow1(net.Addr) bool { return true }

// Allow2 implements Authorizer.
func (PermissiveAuthorizer) Allow2(net.Addr, ContactInfo) bool { return true }

// Allow3 implements Authorizer.
func (PermissiveAuthorizer) Allow3(net.Addr, ContactInfo, ClientInfo) bool { return true }

// Allow4 implements Authorizer.
func (PermissiveAuthorizer) Allow4(net.Addr, ContactInfo, ClientInfo, []SignatureInfo) (bool, *fspb.ValidationInfo) {
	return true, nil
}
