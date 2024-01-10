// Copyright 2023 Google Inc.
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

// Package stats contains interfaces and utilities relating to the collection of
// statistics from a fleetspeak client.
package stats

import (
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// Collector is a component which is notified when certain events occur. It can be implemented with
// different metric backends to enable monitoring of a the Fleetspeak client.
// Implementations of this interface must be thread-safe.
type Collector interface {
	// AfterConfigSync is called after each config sync attempt by the config manager.
	// err is the result of the operation.
	AfterConfigSync(err error)
	// AfterMessageProcessed is called after a message is processed in the client and queued for
	// delivery to the server or a local service.
	// isLocal is set when a message is sent to a local service instead of the Fleetspeak server.
	AfterMessageProcessed(msg *fspb.Message, isLocal bool, err error)
	// BeforeMessageRetry is called when a message has been nacked and got readded to the outbound
	// message queue.
	BeforeMessageRetry(msg *fspb.Message)
}

// NoopCollector implements Collector by doing nothing.
type NoopCollector struct{}

// AfterConfigSync implements Collector by doing nothing.
func (c NoopCollector) AfterConfigSync(err error) {}

// AfterMessageProcessed implements Collector by doing nothing.
func (c NoopCollector) AfterMessageProcessed(msg *fspb.Message, isLocal bool, err error) {}

// BeforeMessageRetry implements Collector by doing nothing.
func (c NoopCollector) BeforeMessageRetry(msg *fspb.Message) {}
