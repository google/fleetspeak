// Copyright 2024 Google Inc.
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

package stats

import fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"

// Note about this file:
// Contrary to other StatsCollectors being located within the package of their respective component,
// this file contains StatsCollectors related to the client itself in order to break a cycle in the
// package dependency graph (client -> stats -> client).

// ClientCollector gets notified about client operations.
// Implementations of this interface must be thread-safe.
type ClientCollector interface {
	// AfterMessageProcessed is called after msg has been processed by the client.
	// isLocal is set when a message is sent to a local service instead of the Fleetspeak server.
	AfterMessageProcessed(msg *fspb.Message, isLocal bool, err error)
}
