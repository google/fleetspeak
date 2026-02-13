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

// Package config defines structures and definitions relating to the client's configuration.
package config

import (
	"crypto/tls"
	"crypto/x509"
	"net/url"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// Configuration gathers the configuration parameters used to instantiate a
// Fleetspeak Client.
//
// When building a client binary that will be marked as trusted through binary
// signing, hash whitelisting, or similar, it is recommended that certain of
// these parameters be hardcoded when calling New(). This mitigates the risk
// that a trusted Fleetspeak binary will be misused.
type Configuration struct {
	// TrustedCerts is the root certificate pool used when verifying servers. All
	// servers will need to present a certificate chained back to this pool.
	//
	// Hardcoding recommended.
	TrustedCerts *x509.CertPool

	// Servers lists the hosts that the client should attempt to connect to,
	// should be of the form <hostname>:<port>.
	//
	// Hardcoding recommended.
	Servers []string

	// If set, used for SNI and certificate validation.
	ServerName string

	// ClientLabels should all be of the form "client:<label>" and will be
	// presented to the server as an initial set of labels for this client.
	ClientLabels []*fspb.Label

	// PersistenceHandler defines the configuration storage strategy to be used.
	// Typically it's files on Unix and registry keys on Windows.
	PersistenceHandler PersistenceHandler

	// FixedServices are installed and started during client startup without
	// checking the deployment key.
	FixedServices []*fspb.ClientServiceConfig

	// CommunicatorConfig sets default communication parameters, and is meant to
	// be hardcoded in order to set them for a particular deployment. This can be
	// overridden on individual machines by providing a communicator.txt in the
	// configuration directory.
	CommunicatorConfig *clpb.CommunicatorConfig

	// RevokedCertSerials is a list of certificate serial numbers which have been
	// revoked. Revoked serial numbers can also be provided by the server and will
	// stored to the writeback location, if NoopPersistenceHandler is not used.
	// Intended for testing and specialized applications - should be hardcoded nil
	// in normal deployments.
	RevokedCertSerials [][]byte

	// If non-nil, proxy used for connecting to the server.
	// See https://golang.org/pkg/net/http/#Transport.Proxy for details.
	Proxy *url.URL

	// If set, the communicator includes the client certificate in this header,
	// which the server will use to identify the client. This should be used if
	// TLS is terminated by e.g. a load balancer that can forward the certificate
	// to the server as another header, so that the server can check the
	// forwarded certificate against this header.
	// If the proxy can not forward the certificate to the server, there is no
	// guarantee that the certificate in this header is the actual client's
	// certificate, making identity spoofing possible.
	ClientCertificateHeader string

	// If set, used instead of the client certificate for authenticating with a
	// the server or a proxy. Requires ClientCertificateHeader to be set for the
	// server to still be able to identify the client.
	// Using this setting waives Fleetspeak's identity guarantees and offloads
	// the identity verification to the system that provides and verifies the
	// certificate.
	GetAuthCertificate func(*tls.CertificateRequestInfo) (*tls.Certificate, error)
}

// PersistenceHandler manages client's configuration storage.
type PersistenceHandler interface {
	ReadState() (*clpb.ClientState, error)
	WriteState(*clpb.ClientState) error
	ReadCommunicatorConfig() (*clpb.CommunicatorConfig, error)

	ReadSignedServices() ([]*fspb.SignedClientServiceConfig, error)
	ReadServices() ([]*fspb.ClientServiceConfig, error)
}

func unmarshalCompatible(b []byte, m proto.Message) error {
	return prototext.UnmarshalOptions{
		// Forward compatibility with future field names
		DiscardUnknown: true,
	}.Unmarshal(b, m)
}
