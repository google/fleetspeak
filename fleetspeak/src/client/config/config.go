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
	"crypto/rsa"
	"crypto/x509"

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

	// DeploymentPublicKeys is used to verify service configuration records. To
	// configure fleetspeak to run a particular program, it is necessary to sign a
	// service configuration record with the private part of one of these keys.
	//
	// Hardcoding recommended.
	DeploymentPublicKeys []rsa.PublicKey

	// Servers lists the hosts that the client should attempt to connect to,
	// should be of the form <hostname>:<port>.
	//
	// Hardcoding recommended.
	Servers []string

	// ClientLabels should all be of the form "client:<label>" and will be
	// presented to the server as an initial set of labels for this client.
	ClientLabels []*fspb.Label

	// ConfigurationPath is the location to look for additional configuration
	// files. Possible files include:
	//
	// /communicator.txt    - A text format clpb.CommunicatorConfig, used to tweak communicator behavior.
	// /writeback           - Used to maintain state across restarts, ignored if Ephemeral is set.
	// /services/<service>  - A binary format SignedClientServiceConfig. One file for each configured service.
	//
	// All of these files are optional, though Fleetspeak will not be particularly
	// useful without at least one configured service.
	//
	// ConfigurationPath is required unless both Ephemeral and FixedServices are
	// set.
	ConfigurationPath string

	// Ephemeral indicates that this client should not attempt to maintain state
	// across restarts. If set, every executation will identify itself as a new
	// client. If not, the client will attempt to read from and write to
	// <ConfigurationPath>/writeback, in order to preserve client identity.
	//
	// Intended for testing and specialized applications - should be hardcoded
	// false in normal deployements.
	Ephemeral bool

	// FixedServices are installed and started during client startup without
	// checking the deployment key.
	//
	// Intended for testing and specialized applications - should be hardcoded nil
	// in normal deployments.
	FixedServices []*fspb.ClientServiceConfig

	// CommunicatorConfig sets default communication parameters, and is meant to
	// be hardcoded in order to set them for a particular deployment. This can be
	// overridden on individual machines by providing a communicator.txt in the
	// configuration directory.
	CommunicatorConfig *clpb.CommunicatorConfig

	// RevokedCertSerials is a list of certificate serial numbers which have been
	// revoked. Revoked serial numbers can also be provided by the server and will
	// stored to the writeback file, when Ephemeral is not set.
	RevokedCertSerials [][]byte
}
