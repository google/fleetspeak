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
	"github.com/google/fleetspeak/fleetspeak/src/client/internal/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/internal/message"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// Collector is a component which is notified when certain events occur. It can be implemented with
// different metric backends to enable monitoring of a Fleetspeak client.
// Implementations of this interface must be thread-safe.
type Collector interface {
	message.RetryLoopStatsCollector
	config.ManagerStatsCollector
	ClientCollector
	CommsContextCollector
}

// NoopCollector implements Collector by doing nothing.
type NoopCollector struct{}

// AfterConfigSync implements Collector by doing nothing.
func (c NoopCollector) AfterConfigSync(err error) {}

// AfterMessageProcessed implements Collector by doing nothing.
func (c NoopCollector) AfterMessageProcessed(msg *fspb.Message, isLocal bool, err error) {}

// BeforeMessageRetry implements Collector by doing nothing.
func (c NoopCollector) BeforeMessageRetry(msg *fspb.Message) {}

// AfterRekey implements Collector by doing nothing.
func (c NoopCollector) AfterRekey(err error) {}

// ContactDataCreated implements Collector by doing nothing.
func (c NoopCollector) ContactDataCreated(wcd *fspb.WrappedContactData, err error) {}

// ContactDataProcessed implements Collector by doing nothing.
func (c NoopCollector) ContactDataProcessed(cd *fspb.ContactData, streaming bool, err error) {}
