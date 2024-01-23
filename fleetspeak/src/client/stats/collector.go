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

// RetryLoopCollector gets notified about messages currently kept in memory by the RetryLoop.
// Implementations of this interface must be thread-safe.
type RetryLoopCollector interface {
	// BeforeMessageRetry is called when a message has been nacked and got readded to the outbound
	// message queue.
	BeforeMessageRetry(msg *fspb.Message)
	// MessagePending is called before a new message is being placed into the output channel.
	// A message is considered pending until it got Acked by the server. In case the message gets
	// Nacked, the RetryLoop will retry and the message is still considered pending.
	// size is the serialized message's size in bytes.
	MessagePending(msg *fspb.Message, size int)
	// MessageAcknowledged is called after a pending message has been acknowledged.
	// size is the serialized message's size in bytes.
	MessageAcknowledged(msg *fspb.Message, size int)
}

// ConfigManagerCollector gets notified about config manager operations.
// Implementations of this interface must be thread-safe.
type ConfigManagerCollector interface {
	// AfterConfigSync is called after each config sync attempt by the config manager.
	// err is the result of the operation.
	AfterConfigSync(err error)
	// AfterRekey is called after each rekey attempt by the config manager.
	AfterRekey(err error)
}

// ClientCollector gets notified about client operations.
// Implementations of this interface must be thread-safe.
type ClientCollector interface {
	// AfterMessageProcessed is called after msg has been processed by the client.
	// isLocal is set when a message is sent to a local service instead of the Fleetspeak server.
	AfterMessageProcessed(msg *fspb.Message, isLocal bool, err error)
}

// CommsContextCollector gets notified when the Communicator makes use of its comms.Context.
// Implementations of this interface must be thread-safe.
type CommsContextCollector interface {
	// ContactDataCreated is called by the comms.Context when the Communicator uses it to create a
	// ContactData to be sent to the server.
	// wcd can be nil if an error occurred.
	ContactDataCreated(wcd *fspb.WrappedContactData, err error)

	// ContactDataProcessed is called by the comms.Context when the Communicator retrieves cd from the
	// server and passes it to the comms.Context for processing.
	ContactDataProcessed(cd *fspb.ContactData, streaming bool, err error)
}

// CommunicatorCollector gets notified about operations and traffic of communicators.
// Implementations of this interface must be thread-safe.
type CommunicatorCollector interface {
	// OutboundContactData gets called after an attempt to send a ContactData to the host.
	// bytesSent is the amount of bytes that were sent during the operation. err is an error
	// that occurred during the operation, if any.
	OutboundContactData(host string, bytesSent int, err error)
	// InboundContactData gets called after an attempt to receive a ContactData from the host.
	// bytesReceived is the amount of bytes that were received during the operation. err is an error
	// that occurred during the operation, if any.
	InboundContactData(host string, bytesReceived int, err error)
	// AfterGetFileRequest gets called when a communicator attempts to make a request for a file on
	// behalf of the client (see comms.Communicator documentation for more details about this
	// functionality).
	// didFetch indicates whether or not the requested file has been fetched from the server,
	// depending on whether it has been modified since we last fetched it.
	AfterGetFileRequest(host, service, name string, didFetch bool, err error)
}

// DaemonServiceCollector gets notified about operations of daemonservice.Services.
// Implementations of this interface must be thread-safe.
type DaemonServiceCollector interface {
	// DaemonServiceSubprocessFinished gets called when a service's subprocess terminates.
	// If the subprocess finished for a reason other than the service shutting down, the cause should
	// be passed as err.
	DaemonServiceSubprocessFinished(service string, err error)
}

// Collector is a component which is notified when certain events occur. It can be implemented with
// different metric backends to enable monitoring of a Fleetspeak client.
// Implementations of this interface must be thread-safe.
type Collector interface {
	RetryLoopCollector
	ConfigManagerCollector
	ClientCollector
	CommsContextCollector
	CommunicatorCollector
	DaemonServiceCollector
}

// NoopCollector implements Collector by doing nothing.
type NoopCollector struct{}

// BeforeMessageRetry implements Collector by doing nothing.
func (c NoopCollector) BeforeMessageRetry(msg *fspb.Message) {}

// MessagePending implements Collector by doing nothing.
func (c NoopCollector) MessagePending(msg *fspb.Message, size int) {}

// MessageAcknowledged implements Collector by doing nothing.
func (c NoopCollector) MessageAcknowledged(msg *fspb.Message, size int) {}

// AfterConfigSync implements Collector by doing nothing.
func (c NoopCollector) AfterConfigSync(err error) {}

// AfterRekey implements Collector by doing nothing.
func (c NoopCollector) AfterRekey(err error) {}

// AfterMessageProcessed implements Collector by doing nothing.
func (c NoopCollector) AfterMessageProcessed(msg *fspb.Message, isLocal bool, err error) {}

// ContactDataCreated implements Collector by doing nothing.
func (c NoopCollector) ContactDataCreated(wcd *fspb.WrappedContactData, err error) {}

// ContactDataProcessed implements Collector by doing nothing.
func (c NoopCollector) ContactDataProcessed(cd *fspb.ContactData, streaming bool, err error) {}

// OutboundContactData implements Collector by doing nothing.
func (c NoopCollector) OutboundContactData(host string, bytesSent int, err error) {}

// InboundContactData implements Collector by doing nothing.
func (c NoopCollector) InboundContactData(host string, bytesReceived int, err error) {}

// AfterGetFileRequest implements Collector by doing nothing.
func (c NoopCollector) AfterGetFileRequest(host, service, name string, didFetch bool, err error) {}

// DaemonServiceSubprocessFinished implements Collector by doing nothing.
func (c NoopCollector) DaemonServiceSubprocessFinished(service string, err error) {}
