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

// Package stats contains interfaces and utilities relating to the collection of
// statistics from a fleetspeak server.
package stats

import (
	"context"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

// PollType indicates the type of poll, primarily differentiating between the
// original bidirectional contact and the different events within a streaming
// connection.
type PollType int

// PollType values.
const (
	FullPoll         PollType = iota // Classic one-off bidirectional poll.
	StreamStart                      // Bidirectional poll that starts a streaming connection.
	StreamFromClient                 // 'Poll' that is really a client pushing data to the server.
	StreamToClient                   // 'Poll' that is really the server pushing data to the client.
)

// String implements fmt.Stringer.
func (t PollType) String() string {
	switch t {
	case FullPoll:
		return "POLL"
	case StreamStart:
		return "STREAM_START"
	case StreamFromClient:
		return "STREAM_FROM_CLIENT"
	case StreamToClient:
		return "STREAM_TO_CLIENT"
	default:
	}
	return "UNKNOWN"
}

// A PollInfo describes a client poll operation which has occurred.
type PollInfo struct {
	// A Context associated with the poll operation, if available.
	CTX context.Context

	// The ClientID of the polling client, if available.
	ID common.ClientID

	// When the operation started and ended.
	Start, End time.Time

	// Http status code returned to the client.
	Status int

	// Bytes read and written.
	ReadBytes, WriteBytes int

	// Time spent reading and writing messages.
	ReadTime, WriteTime time.Duration

	// Whether the client was in the client cache.
	CacheHit bool

	// The 'type of poll.
	Type PollType
}

// A Collector is a component which is notified when certain events occurred, to support
// performance monitoring of a fleetspeak installation.
type Collector interface {
	// MessageIngested is called when a message is received from a client, or as
	// a backlogged message from the datastore.
	MessageIngested(backlogged bool, m *fspb.Message, cd *db.ClientData)

	// MessageSaved is called when a message is first saved to the database.
	// m.Data will have been set to nil for fully processed messages.
	MessageSaved(forClient bool, m *fspb.Message, cd *db.ClientData)

	// MessageProcessed is called when a message is successfully processed by the
	// server.
	MessageProcessed(start, end time.Time, m *fspb.Message, cd *db.ClientData)

	// MessageErrored is called when a message processing returned an error
	// (temporary, or permanent).
	MessageErrored(start, end time.Time, permanent bool, m *fspb.Message, cd *db.ClientData)

	// MessageDropped is called when a message has been dropped because too many
	// messages for the services are being processed. Like a temporary error, the
	// message will be retried after some minutes.
	MessageDropped(m *fspb.Message, cd *db.ClientData)

	// ClientPoll is called every time a client polls the server.
	ClientPoll(info PollInfo)

	// DatastoreOperation is called every time a database operation completes.
	DatastoreOperation(start, end time.Time, operation string, result error)

	// ResourceUsageDataReceived is called every time a client-resource-usage proto is received.
	ResourceUsageDataReceived(cd *db.ClientData, rud *mpb.ResourceUsageData, v *fspb.ValidationInfo)

	// KillNotificationReceived is called when a kill notification is received from a client.
	KillNotificationReceived(cd *db.ClientData, kn *mpb.KillNotification)
}
