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

// Package db defines the interface that fleetspeak expects from its persistence
// layer. Each installation will need to choose and configure a Store
// implementation. An example implementation meant for testing and small scale
// deployments is in the server/sqlite directory.
//
// It also includes some utility methods and types meant for use by Store
// implementations.
//
// SECURITY NOTE:
//
// The endpoints provide much of the data passed through this interface.
// Implementations are responsible for using safe coding practices to prevent
// SQL injection and similar attacks.
package db

import (
	"context"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/ids"

	"github.com/golang/protobuf/proto"

	tpb "github.com/golang/protobuf/ptypes/timestamp"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// A Store describes the full persistence mechanism required by the base
// fleetspeak system. These operations must be thread safe. These must also be
// all-or-nothing, fully committed on success, and are otherwise trusted to be
// individually transactional.
type Store interface {
	MessageStore
	ClientStore
	BroadcastStore
	FileStore

	// IsNotFound returns whether an error returned by the Datastore indicates that
	// a record was not found.
	IsNotFound(error) bool

	// Close shuts down the Store, releasing any held resources.
	Close() error
}

// ClientData contains basic data about a client.
type ClientData struct {
	Key    []byte        // The der encoded public key for the client.
	Labels []*fspb.Label // The client's labels.

	// Whether the client_id has been blacklisted. Once blacklisted any contact
	// from this client_id will result in an rekey request.
	Blacklisted bool
}

// Clone returns a deep copy.
func (d *ClientData) Clone() *ClientData {
	ret := &ClientData{
		Blacklisted: d.Blacklisted,
	}
	ret.Key = append(ret.Key, d.Key...)

	ret.Labels = make([]*fspb.Label, 0, len(d.Labels))
	for _, l := range d.Labels {
		ret.Labels = append(ret.Labels, proto.Clone(l).(*fspb.Label))
	}
	return ret
}

// A ContactID identifies a communication with a client. The form is determined
// by the Datastore implementation - it is treated as an opaque string by the
// rest of the FS system.
type ContactID string

// MessageStore provides methods to store and query messages.
//
// Notionally, a MessageStore is backed by a table where each row is a fspb.Message record,
// along with with one of the following:
//
// 1) due time and retry count - If the message is not processed or delivered
// before due time, it will be tried again. A count of the number of retries is
// maintained and used to compute the next due time.
//
// 2) completion time - When a record is processed or acknowledged by a client, the
// message is marked as completed by saving a completion time.
//
// Furthermore it is possible to register a MessageProcessor with each
// MessageStore which then receives notifications that server messages are ready
// for processing. In multi-server installations, the datastore should attempt
// to provide eactly one notification to some Fleetspeak server each time the
// message becomes overdue.
type MessageStore interface {
	// StoreMessages records msgs. If contact is not the empty string, it attaches
	// them to the associated contact.
	//
	// It is not an error for a message to already exist. In this case a success result
	// will overwrite any result already present, and a failed result will overwrite
	// an empty result. Otherwise the operation is silently dropped.
	//
	// A message is eligible to be returned by ClientMessagesForProcessing or the
	// registered MessageProcessor iff it does not yet have a Result. Also,
	// setting a Result will delete the message's Data field payload.
	StoreMessages(ctx context.Context, msgs []*fspb.Message, contact ContactID) error

	// ClientMessagesForProcessing returns up to lim messages that are due to be
	// processed by a client. It also increments the time at which the
	// messages will again become overdue using rp.
	//
	// Note that if an error occurs partway through the loading of messages,
	// the already loaded messages may be returned along with the error. In
	// particular, datastore implementations may want to do this if the ctx
	// times out before all messages are found and updated.
	ClientMessagesForProcessing(ctx context.Context, id common.ClientID, lim int) ([]*fspb.Message, error)

	// GetMessages retrieves specific messages.
	GetMessages(ctx context.Context, ids []common.MessageID, wantData bool) ([]*fspb.Message, error)

	// GetMessageResult retrieves the current status of a message.
	GetMessageResult(ctx context.Context, id common.MessageID) (*fspb.MessageResult, error)

	// SetMessageResult retrieves the current status of a message. The dest
	// parameter identifies the destination client, and must be the empty id for
	// messages addressed to the server.
	SetMessageResult(ctx context.Context, dest common.ClientID, id common.MessageID, res *fspb.MessageResult) error

	// RegisterMessageProcessor installs a MessageProcessor which will be
	// called when a message is overdue for processing.
	RegisterMessageProcessor(mp MessageProcessor)

	// StopMessageProcessor causes the datastore to stop making calls to the
	// registered MessageProcessor. It only returns once all existing calls
	// to MessageProcessor have completed.
	StopMessageProcessor()
}

// A MessageProcessor receives messages that are overdue and should be reprocessing.
type MessageProcessor interface {
	// ProcessMessage is called by the Datastore to indicate that the
	// provided message is overdue and that processing should be attempted
	// again.
	//
	// This call will be repeated until MarkMessage(Processed|Failed) is
	// successfully called on m.
	ProcessMessages(msgs []*fspb.Message)
}

// ContactData provides basic information about a client's contact with a FS
// server.
type ContactData struct {
	ClientID                 common.ClientID // ID of the client.
	NonceSent, NonceReceived uint64          // Nonce sent to the client and received from the client.
	Addr                     string          // Observed client network address.
	ClientClock              *tpb.Timestamp  // Client's report of its current clock setting.
}

// ClientStore provides methods to store and retrieve information about clients.
type ClientStore interface {
	// ListClients returns basic information about clients. If ids is empty, it
	// returns all clients.
	ListClients(ctx context.Context, ids []common.ClientID) ([]*spb.Client, error)

	// GetClientData retrieves the current data about the client identified
	// by id.
	GetClientData(ctx context.Context, id common.ClientID) (*ClientData, error)

	// AddClient creates a new client.
	AddClient(ctx context.Context, id common.ClientID, data *ClientData) error

	// AddClientLabel records that a client now has a label.
	AddClientLabel(ctx context.Context, id common.ClientID, l *fspb.Label) error

	// RemoveLabel records that a client no longer has a label.
	RemoveClientLabel(ctx context.Context, id common.ClientID, l *fspb.Label) error

	// BlacklistClient records that a client_id is no longer trusted and should be
	// recreated.
	BlacklistClient(ctx context.Context, id common.ClientID) error

	// RecordClientContact records an authenticated contact with a
	// client. On success provides a contact id - an opaque string which can
	// be used to link messages to a contact.
	RecordClientContact(ctx context.Context, data ContactData) (ContactID, error)

	// ListClientContacts lists all of the contacts in the database for a given
	// client.
	//
	// NOTE: This method is explicitly permitted to return data up to 30 seconds
	// stale. Also, it is normal (and expected) for a datastore to delete contact
	// older than a few weeks.
	ListClientContacts(ctx context.Context, id common.ClientID) ([]*spb.ClientContact, error)

	// LinkMessagesToContact associates messages with a contact - it records
	// that they were sent or received during the given contact.
	LinkMessagesToContact(ctx context.Context, contact ContactID, msgs []common.MessageID) error

	// Writes resource-usage data received from a client to the data-store.
	RecordResourceUsageData(ctx context.Context, id common.ClientID, rud mpb.ResourceUsageData) error

	// Fetches at most 'limit' resource-usage records for a given client from the data-store.
	// TODO: Add more complex queries.
	FetchResourceUsageRecords(ctx context.Context, id common.ClientID, limit int) ([]*spb.ClientResourceUsageRecord, error)
}

// Broadcast limits with special meaning.
const (
	BroadcastDisabled  = uint64(0)
	BroadcastUnlimited = uint64(math.MaxInt64) // The sqlite datastore's uint64 doesn't support full uint64 range.
)

// A BroadcastInfo describes a broadcast and contains the static broadcast
// information, plus the current limit and count of messages sent.
type BroadcastInfo struct {
	Broadcast *spb.Broadcast
	Sent      uint64
	Limit     uint64
}

// An AllocationInfo describes an allocation. An allocation is the right to send
// some broadcast to up to Limit machines before Expiry.
type AllocationInfo struct {
	ID     ids.AllocationID
	Limit  uint64
	Expiry time.Time
}

// ComputeBroadcastAllocation computes how large a new allocation should be. It
// is meant to be used by implementations of Broadcaststore.
//
// It takes the allocation's current message limit, also the number already
// sent, the number already allocated, and the target fraction of the allocation
// to claim. It returns the number of messages that should be allocated to a new
// allocation, also the new total number of messages allocated.
func ComputeBroadcastAllocation(messageLimit, allocated, sent uint64, frac float32) (toAllocate, newAllocated uint64) {
	// Allocations for unlimited broadcasts don't count; such allocations are only used
	// to keep tract of the number sent.
	if messageLimit == BroadcastUnlimited {
		return BroadcastUnlimited, allocated
	}
	a := allocated
	if sent > a {
		a = sent
	}
	if a > messageLimit {
		return 0, allocated
	}
	toAllocate = uint64(float32(messageLimit-a) * frac)
	if toAllocate == 0 {
		toAllocate = 1
	}
	if toAllocate > messageLimit-a {
		toAllocate = messageLimit - a
	}
	newAllocated = toAllocate + allocated
	return
}

// ComputeBroadcastAllocationCleanup computes the new number of messages
// allocated when cleaning up an allocation. It takes the number of messages
// that were allocated to the allocation, also the current total number of messages
// allocated from the broadcast.
func ComputeBroadcastAllocationCleanup(allocationLimit, allocated uint64) (uint64, error) {
	if allocationLimit == BroadcastUnlimited {
		return allocated, nil
	}
	if allocationLimit > allocated {
		return 0, fmt.Errorf("allocationLimit = %v, which is larger than allocated = %v", allocationLimit, allocated)
	}
	return allocated - allocationLimit, nil
}

// BroadcastStore provides methods to store and retrieve information about broadcasts.
type BroadcastStore interface {
	// CreateBroadcast stores a new broadcast message.
	CreateBroadcast(ctx context.Context, b *spb.Broadcast, limit uint64) error

	// SetBroadcastLimit adjusts the limit of an existing broadcast.
	SetBroadcastLimit(ctx context.Context, id ids.BroadcastID, limit uint64) error

	// SaveBroadcastMessage saves a new broadcast message.
	SaveBroadcastMessage(ctx context.Context, msg *fspb.Message, bid ids.BroadcastID, cid common.ClientID, aid ids.AllocationID) error

	// ListActiveBroadcasts lists broadcasts which could be sent to some
	// client.
	ListActiveBroadcasts(ctx context.Context) ([]*BroadcastInfo, error)

	// ListSentBroadcasts returns identifiers for those broadcasts which have already been sent to a client.
	ListSentBroadcasts(ctx context.Context, id common.ClientID) ([]ids.BroadcastID, error)

	// CreateAllocation creates an allocation for a given Broadcast,
	// reserving frac of the unallocated broadcast limit until
	// expiry. Return nil if there is no message allocation available.
	CreateAllocation(ctx context.Context, id ids.BroadcastID, frac float32, expiry time.Time) (*AllocationInfo, error)

	// CleanupAllocation deletes the identified allocation record and
	// updates the broadcast sent count according to the number that were
	// actually sent under the given allocation.
	CleanupAllocation(ctx context.Context, bid ids.BroadcastID, aid ids.AllocationID) error
}

// ReadSeekerCloser groups io.ReadSeeker and io.Closer.
type ReadSeekerCloser interface {
	io.ReadSeeker
	io.Closer
}

// NOOPCloser wraps an io.ReadSeeker to trivially turn it into a ReadSeekerCloser.
type NOOPCloser struct {
	io.ReadSeeker
}

// Close implements io.Closer.
func (c NOOPCloser) Close() error {
	return nil
}

// FileStore provides methods to store and retrieve files. Files are keyed by an associated
// service and name.
//
// SECURITY NOTES:
//
// Fleetspeak doesn't provide any ACL support for files - all files are readable
// by any client.
//
// Implementations are responsible for validating and/or sanitizing the
// identifiers provided. For example, an implementation backed by a filesystem
// would need to protect against path traversal vulnerabilities.
type FileStore interface {
	// StoreFile stores data into the Filestore, organized by service and name.
	StoreFile(ctx context.Context, service, name string, data io.Reader) error

	// StatFile returns the modification time of a file previously stored by
	// StoreFile. Returns ErrNotFound if not found.
	StatFile(ctx context.Context, servce, name string) (time.Time, error)

	// ReadFile returns the data and modification time of file previously
	// stored by StoreFile.  Caller is responsible for closing data.
	//
	// Note: Calls to data are permitted to fail if ctx is canceled or expired.
	ReadFile(ctx context.Context, service, name string) (data ReadSeekerCloser, modtime time.Time, err error)
}
