// Copyright 2019 Google LLC
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

// Package generic provides support methods to build a generic client, not bound
// to a particular installation. This means that critical parameters must be
// read from a configuration file.
//
// Serious users might consider building and packaging their own clients with
// parameters hardcoded in the client binary.
package generic

import (
	"crypto/x509"
	"errors"
	"runtime"
	"strings"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/client/config"

	gpb "github.com/google/fleetspeak/fleetspeak/src/client/generic/proto/fleetspeak_client_generic"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// MakeConfiguration returns a config.Configuration based on the provided gpb.Config.
func MakeConfiguration(cfg gpb.Config) (config.Configuration, error) {
	trustedCerts := x509.NewCertPool()
	if !trustedCerts.AppendCertsFromPEM([]byte(cfg.TrustedCerts)) {
		return config.Configuration{}, errors.New("unable to parse trusted_certs")
	}
	log.Infof("Read %d trusted certificates.", len(trustedCerts.Subjects()))

	if len(cfg.Server) == 0 {
		return config.Configuration{}, errors.New("no server provided")
	}

	labels := []*fspb.Label{
		{ServiceName: "client", Label: runtime.GOARCH},
		{ServiceName: "client", Label: runtime.GOOS}}

	for _, l := range cfg.ClientLabel {
		if strings.TrimSpace(l) == "" {
			continue
		}
		labels = append(labels, &fspb.Label{ServiceName: "client", Label: l})
	}

	ph, err := makePersistenceHandler(cfg)
	if err != nil {
		return config.Configuration{}, err
	}

	return config.Configuration{
		TrustedCerts:       trustedCerts,
		Servers:            cfg.Server,
		PersistenceHandler: ph,
		ClientLabels:       labels,
	}, nil
}
