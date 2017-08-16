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

// Package signer defines an interface to add additional signatures to
// communications with the Fleetspeak server.
package signer

import "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"

// A Signer is given a chance to sign all data sent to the server.
type Signer interface {
	// SignContact attempts to produce a signature of data which
	// will be included when sending it to the server.
	//
	// SignContact may return nil if the intended certificate is
	// currently unavailable.
	SignContact(data []byte) *fleetspeak.Signature
}
