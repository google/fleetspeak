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

package server

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sort"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/signatures"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

const maxMessagesPerContact = 100
const processingChunkSize = 10

type commsContext struct {
	s *Server
}

// GetClientInfo loads basic information about a client. Returns nil if the client does
// not exist in the datastore.
func (c commsContext) GetClientInfo(ctx context.Context, id common.ClientID) (*comms.ClientInfo, error) {
	cld, cacheHit, err := c.s.clientCache.GetOrRead(ctx, id, c.s.dataStore)
	if err != nil {
		if c.s.dataStore.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	k, err := x509.ParsePKIXPublicKey(cld.Key)
	if err != nil {
		return nil, err
	}
	return &comms.ClientInfo{
		ID:          id,
		Key:         k,
		Labels:      cld.Labels,
		Blacklisted: cld.Blacklisted,
		Cached:      cacheHit}, nil
}

// AddClient adds a new client to the system.
func (c commsContext) AddClient(ctx context.Context, id common.ClientID, key crypto.PublicKey) (*comms.ClientInfo, error) {
	k, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return nil, err
	}
	if err := c.s.dataStore.AddClient(ctx, id, &db.ClientData{Key: k}); err != nil {
		return nil, err
	}
	return &comms.ClientInfo{ID: id, Key: key}, nil
}

func (c commsContext) HandleClientContact(ctx context.Context, info *comms.ClientInfo, addr net.Addr, wcd *fspb.WrappedContactData) (*fspb.ContactData, error) {
	sigs, err := signatures.ValidateWrappedContactData(info.ID, wcd)
	if err != nil {
		return nil, err
	}
	accept, validationInfo := c.s.authorizer.Allow4(
		addr,
		authorizer.ContactInfo{
			ID:           info.ID,
			ContactSize:  len(wcd.ContactData),
			ClientLabels: wcd.ClientLabels,
		},
		authorizer.ClientInfo{
			Labels: info.Labels,
		},
		sigs)
	if !accept {
		return nil, errors.New("contact not authorized")
	}
	var cd fspb.ContactData
	if err = proto.Unmarshal(wcd.ContactData, &cd); err != nil {
		return nil, fmt.Errorf("unable to parse contact_data: %v", err)
	}
	if len(cd.Messages) > maxMessagesPerContact {
		return nil, fmt.Errorf("contact_data contains %d messages, only %d allowed", len(cd.Messages), maxMessagesPerContact)
	}
	toSend := fspb.ContactData{SequencingNonce: uint64(rand.Int63())}
	ct, err := c.s.dataStore.RecordClientContact(ctx,
		db.ContactData{
			ClientID:      info.ID,
			NonceSent:     toSend.SequencingNonce,
			NonceReceived: cd.SequencingNonce,
			Addr:          addr.String(),
			ClientClock:   cd.ClientClock,
		})
	if err != nil {
		return nil, err
	}

	err = c.handleMessagesFromClient(ctx, info, ct, &cd, validationInfo)
	if err != nil {
		return nil, err
	}

	toSend.Messages, err = c.FindMessagesForClient(ctx, info, ct, 100)
	if err != nil {
		return nil, err
	}
	return &toSend, nil
}

// FindMessagesForClient finds unprocessed messages for a given client and
// reserves them for processing.
func (c commsContext) FindMessagesForClient(ctx context.Context, info *comms.ClientInfo, contactID db.ContactID, maxMessages int) ([]*fspb.Message, error) {
	if info.Blacklisted {
		log.Warningf("Contact from blacklisted id [%v], creating RekeyRequest.", info.ID)
		m, err := c.MakeBlacklistMessage(ctx, info, contactID)
		return []*fspb.Message{m}, err
	}
	msgs, err := c.s.dataStore.ClientMessagesForProcessing(ctx, info.ID, maxMessages)
	if err != nil {
		if len(msgs) == 0 {
			return nil, err
		}
		log.Warning("Got %v messages along with error, continuing: %v", len(msgs), err)
	}

	// If the client recently contacted us, the broadcast situation is unlikely to
	// have changed, so we skip checking for broadcasts. To keep this from delaying
	// broadcast distribution, the broadcast manager clears the client cache when it
	// finds more broadcasts.
	if !info.Cached {
		bms, err := c.s.broadcastManager.MakeBroadcastMessagesForClient(ctx, info.ID, info.Labels)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, bms...)
	}

	if len(msgs) == 0 {
		return msgs, nil
	}

	mids := make([]common.MessageID, 0, len(msgs))
	for _, m := range msgs {
		id, err := common.BytesToMessageID(m.MessageId)
		if err != nil {
			return nil, err
		}
		mids = append(mids, id)
	}
	err = c.s.dataStore.LinkMessagesToContact(ctx, contactID, mids)
	if err != nil {
		return nil, err
	}
	return msgs, nil
}

func (c commsContext) MakeBlacklistMessage(ctx context.Context, info *comms.ClientInfo, contactID db.ContactID) (*fspb.Message, error) {
	mid, err := common.RandomMessageID()
	if err != nil {
		return nil, fmt.Errorf("unable to create message id: %v", err)
	}
	msg := &fspb.Message{
		MessageId: mid.Bytes(),
		Source: &fspb.Address{
			ServiceName: "system",
		},
		Destination: &fspb.Address{
			ServiceName: "system",
			ClientId:    info.ID.Bytes(),
		},
		MessageType:  "RekeyRequest",
		CreationTime: db.NowProto(),
	}
	if err = c.s.dataStore.StoreMessages(ctx, []*fspb.Message{msg}, contactID); err != nil {
		return nil, fmt.Errorf("unable to store RekeyRequest: %v", err)
	}
	return msg, nil
}

func (c commsContext) validateMessageFromClient(id common.ClientID, m *fspb.Message, validationInfo *fspb.ValidationInfo) error {
	if m.Destination == nil {
		return fmt.Errorf("message must have Destination")
	}
	if m.Destination.ClientId != nil {
		return fmt.Errorf("cannot send a message directly to another client [%v]", m.Destination.ClientId)
	}
	if m.Source == nil || m.Source.ServiceName == "" {
		return fmt.Errorf("message must have a source with a ServiceName, got: %v", m.Source)
	}
	if m.SourceMessageId == nil {
		return fmt.Errorf("source message id cannot be empty")
	}

	m.Source.ClientId = id.Bytes()
	m.ValidationInfo = validationInfo
	m.MessageId = common.MakeMessageID(m.Source, m.SourceMessageId).Bytes()
	return nil
}

// handleMessagesFromClient processes a block of messages from a particular
// client. It saves them to the database, associates them with the contact
// identified by contactTime, and processes them.
func (c commsContext) handleMessagesFromClient(ctx context.Context, info *comms.ClientInfo, contactID db.ContactID, received *fspb.ContactData, validationInfo *fspb.ValidationInfo) error {
	msgs := make([]*fspb.Message, 0, len(received.Messages))
	for _, m := range received.Messages {
		err := c.validateMessageFromClient(info.ID, m, validationInfo)
		if err != nil {
			log.Errorf("Dropping invalid message from [%v]: %v", info.ID, err)
			continue
		}
		msgs = append(msgs, m)
	}
	if len(msgs) == 0 {
		return nil
	}

	sort.Slice(msgs, func(a, b int) bool {
		return bytes.Compare(msgs[a].MessageId, msgs[b].MessageId) == -1
	})

	for {
		if len(msgs) <= processingChunkSize {
			return c.s.serviceConfig.HandleNewMessages(ctx, msgs, contactID)
		}

		if err := c.s.serviceConfig.HandleNewMessages(ctx, msgs[:processingChunkSize], contactID); err != nil {
			return err
		}
		msgs = msgs[processingChunkSize:]
	}
}

// ReadFile returns the data and modification time of file. Caller is
// responsible for closing data.
//
// Calls to data are permitted to fail if ctx is canceled or expired.
func (c commsContext) ReadFile(ctx context.Context, service, name string) (data db.ReadSeekerCloser, modtime time.Time, err error) {
	return c.s.dataStore.ReadFile(ctx, service, name)
}

// IsNotFound returns whether an error returned by ReadFile indicates that the
// file was not found.
func (c commsContext) IsNotFound(err error) bool {
	return c.s.dataStore.IsNotFound(err)
}

// StatsCollector returns the stats.Collector used by the Fleetspeak
// system. Access is provided to allow collection of stats relating to the
// client communication.
func (c commsContext) StatsCollector() stats.Collector {
	return c.s.statsCollector
}

func (c commsContext) Authorizer() authorizer.Authorizer {
	return c.s.authorizer
}
