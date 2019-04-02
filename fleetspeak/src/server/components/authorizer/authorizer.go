// Copyright 2019 Google Inc.
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

// Package authorizer provide generic implementations and utility methods
// for Fleetspeak's authorizer component type.
package authorizer

import (
	"net"

	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// LabelFilter is an authorizer.Authorizer which refuses connections from
// clients that do not have a specific label.
type LabelFilter struct {
	Label string
}

// Allow1 implements Authorizer.
func (f LabelFilter) Allow1(net.Addr) bool { return true }

// Allow2 implements Authorizer.
func (f LabelFilter) Allow2(_ net.Addr, i authorizer.ContactInfo) bool {
	if f.Label == "" {
		return true
	}
	for _, l := range i.ClientLabels {
		if l == f.Label {
			return true
		}
	}
	return false
}

// Allow3 implements Authorizer.
func (f LabelFilter) Allow3(net.Addr, authorizer.ContactInfo, authorizer.ClientInfo) bool { return true }

// Allow4 implements Authorizer.
func (f LabelFilter) Allow4(net.Addr, authorizer.ContactInfo, authorizer.ClientInfo, []authorizer.SignatureInfo) (bool, *fspb.ValidationInfo) {
	return true, nil
}
