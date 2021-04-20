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
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"fmt"
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

func randUint64() uint64 {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Random numbers are required for the correct operation of a FS server.
		panic(fmt.Errorf("unable to read random bytes: %v", err))
	}
	return binary.LittleEndian.Uint64(b)
}

func (c commsContext) InitializeConnection(ctx context.Context, addr net.Addr, key crypto.PublicKey, wcd *fspb.WrappedContactData, streaming bool) (*comms.ConnectionInfo, *fspb.ContactData, bool, error) {
	id, err := common.MakeClientID(key)
	if err != nil {
		return nil, nil, false, err
	}

	contactInfo := authorizer.ContactInfo{
		ID:           id,
		ContactSize:  len(wcd.ContactData),
		ClientLabels: wcd.ClientLabels,
	}
	if !c.s.authorizer.Allow2(addr, contactInfo) {
		log.Warningf("Allow2 failed for %v", addr)
		return nil, nil, false, comms.ErrNotAuthorized
	}

	ci, err := c.getClientInfo(ctx, id)
	if err != nil {
		return nil, nil, false, err
	}

	res := comms.ConnectionInfo{
		Addr: addr,
	}
	if ci == nil {
		res.AuthClientInfo.New = true
	} else {
		res.Client = *ci
		res.AuthClientInfo.Labels = ci.Labels
	}

	if !c.s.authorizer.Allow3(addr, contactInfo, res.AuthClientInfo) {
		log.Warningf("Allow3 failed for %v", addr)
		return nil, nil, false, comms.ErrNotAuthorized
	}

	if ci == nil {
		if err := c.addClient(ctx, id, key); err != nil {
			return nil, nil, false, err
		}
		res.Client.ID = id
		res.Client.Key = key
		// Set initial labels for the client according to the contact data. Going
		// forward, labels will be adjusted when the client sends a ClientInfo
		// message (see system_service.go).
		for _, l := range wcd.ClientLabels {
			cl := &fspb.Label{ServiceName: "client", Label: l}

			// Ignore errors - if this fails, the first ClientInfo message will try again
			// in a context where we can retry easily.
			c.s.dataStore.AddClientLabel(ctx, id, cl)

			res.Client.Labels = append(res.Client.Labels, cl)
		}
		res.AuthClientInfo.Labels = res.Client.Labels
	}

	sigs, err := signatures.ValidateWrappedContactData(id, wcd)
	if err != nil {
		return nil, nil, false, err
	}
	accept, validationInfo := c.s.authorizer.Allow4(
		addr,
		contactInfo,
		res.AuthClientInfo,
		sigs)
	if !accept {
		log.Warningf("Allow4 failed for %v", addr)
		return nil, nil, false, comms.ErrNotAuthorized
	}

	var cd fspb.ContactData
	if err = proto.Unmarshal(wcd.ContactData, &cd); err != nil {
		return nil, nil, false, fmt.Errorf("unable to parse contact_data: %v", err)
	}
	if len(cd.Messages) > maxMessagesPerContact {
		return nil, nil, false, fmt.Errorf("contact_data contains %d messages, only %d allowed", len(cd.Messages), maxMessagesPerContact)
	}

	res.NonceReceived = cd.SequencingNonce
	toSend := fspb.ContactData{SequencingNonce: randUint64()}
	res.NonceSent = toSend.SequencingNonce
	if len(cd.AllowedMessages) > 0 {
		res.AddMessageTokens(cd.AllowedMessages)
	}

	var streamingTo string
	if streaming {
		streamingTo = c.s.listener.Address()
	}
	res.ContactID, err = c.s.dataStore.RecordClientContact(ctx,
		db.ContactData{
			ClientID:      id,
			NonceSent:     toSend.SequencingNonce,
			NonceReceived: cd.SequencingNonce,
			Addr:          addr.String(),
			ClientClock:   cd.ClientClock,
			StreamingTo:   streamingTo,
		})
	if err != nil {
		return nil, nil, false, err
	}

	err = c.handleMessagesFromClient(ctx, &res.Client, res.ContactID, &cd, validationInfo)
	if err != nil {
		return nil, nil, false, err
	}

	// Complete registering for notifications before checking messages.
	if streaming {
		res.Notices, res.Fin = c.s.dispatcher.Register(id)
	}
	toSend.Messages, err = c.findMessagesForClient(ctx, &res)
	if err != nil {
		if res.Fin != nil {
			res.Fin()
		}
		return nil, nil, false, err
	}

	res.AuthClientInfo.New = false
	return &res, &toSend, len(toSend.Messages) == maxMessagesPerContact, nil
}

// getClientInfo loads basic information about a client. Returns nil if the client does
// not exist in the datastore.
func (c commsContext) getClientInfo(ctx context.Context, id common.ClientID) (*comms.ClientInfo, error) {
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

func (c commsContext) HandleMessagesFromClient(ctx context.Context, info *comms.ConnectionInfo, wcd *fspb.WrappedContactData) error {
	sigs, err := signatures.ValidateWrappedContactData(info.Client.ID, wcd)
	if err != nil {
		return err
	}

	accept, validationInfo := c.s.authorizer.Allow4(
		info.Addr,
		authorizer.ContactInfo{
			ID:           info.Client.ID,
			ContactSize:  len(wcd.ContactData),
			ClientLabels: wcd.ClientLabels,
		},
		info.AuthClientInfo,
		sigs)
	if !accept {
		return comms.ErrNotAuthorized
	}

	var cd fspb.ContactData
	if err = proto.Unmarshal(wcd.ContactData, &cd); err != nil {
		return fmt.Errorf("unable to parse contact_data: %v", err)
	}
	if len(cd.Messages) > maxMessagesPerContact {
		return fmt.Errorf("contact_data contains %d messages, only %d allowed", len(cd.Messages), maxMessagesPerContact)
	}

	info.AddMessageTokens(cd.AllowedMessages)

	err = c.handleMessagesFromClient(ctx, &info.Client, info.ContactID, &cd, validationInfo)
	if err != nil {
		return err
	}
	return nil
}

func (c commsContext) GetMessagesForClient(ctx context.Context, info *comms.ConnectionInfo) (*fspb.ContactData, bool, error) {
	toSend := fspb.ContactData{
		SequencingNonce: info.NonceSent,
	}
	var err error
	toSend.Messages, err = c.findMessagesForClient(ctx, info)
	if err != nil || len(toSend.Messages) == 0 {
		return nil, false, err
	}
	return &toSend, len(toSend.Messages) == maxMessagesPerContact, nil
}

// addClient adds a new client to the system.
func (c commsContext) addClient(ctx context.Context, id common.ClientID, key crypto.PublicKey) error {
	k, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		return err
	}
	return c.s.dataStore.AddClient(ctx, id, &db.ClientData{Key: k})
}

func isSpecialMessageToAck(m *fspb.Message) bool {
	return m.MessageType == "Die" && m.Destination != nil && m.Destination.ServiceName == "system"
}

// FindMessagesForClient finds unprocessed messages for a given client and
// reserves them for processing in the database. It limits the returned messages
// to those for which token, and decrements token counters as necessary.
func (c commsContext) findMessagesForClient(ctx context.Context, info *comms.ConnectionInfo) ([]*fspb.Message, error) {
	if info.Client.Blacklisted {
		log.Warningf("Contact from blacklisted id [%v], creating RekeyRequest.", info.Client.ID)
		m, err := c.MakeBlacklistMessage(ctx, &info.Client, info.ContactID)
		return []*fspb.Message{m}, err
	}
	tokens := info.MessageTokens()
	msgs, err := c.s.dataStore.ClientMessagesForProcessing(ctx, info.Client.ID, 100, tokens)
	if err != nil {
		if len(msgs) == 0 {
			return nil, err
		}
		log.Warning("Got %v messages along with error, continuing: %v", len(msgs), err)
	}
	log.V(1).Infof("FindMessagesForClient(%v): found %d messages for tokens: %v", info.Client.ID, len(msgs), tokens)
	if tokens != nil {
		cnts := make(map[string]uint64)
		for _, m := range msgs {
			if !isSpecialMessageToAck(m) {
				cnts[m.Destination.ServiceName]++
			}
		}
		info.SubtractMessageTokens(cnts)
		if log.V(2) {
			log.Infof("FindMessagesForClient(%v): updated tokens to: %v", info.Client.ID, info.MessageTokens())
		}
	}

	// If the client recently contacted us, the broadcast situation is unlikely to
	// have changed, so we skip checking for broadcasts. To keep this from delaying
	// broadcast distribution, the broadcast manager clears the client cache when it
	// finds more broadcasts.
	if !info.Client.Cached {
		bms, err := c.s.broadcastManager.MakeBroadcastMessagesForClient(ctx, info.Client.ID, info.Client.Labels)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, bms...)
	}

	if len(msgs) == 0 {
		return msgs, nil
	}

	mids := make([]common.MessageID, 0, len(msgs))
	var specialMidsToAck []common.MessageID
	for _, m := range msgs {
		id, err := common.BytesToMessageID(m.MessageId)
		if err != nil {
			return nil, err
		}
		mids = append(mids, id)
		if isSpecialMessageToAck(m) {
			specialMidsToAck = append(specialMidsToAck, id)
		}
	}
	if err := c.s.dataStore.LinkMessagesToContact(ctx, info.ContactID, mids); err != nil {
		return nil, err
	}
	for _, id := range specialMidsToAck {
		err := c.s.dataStore.SetMessageResult(ctx, info.Client.ID, id, &fspb.MessageResult{ProcessedTime: db.NowProto()})
		if err != nil {
			return nil, err
		}
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
