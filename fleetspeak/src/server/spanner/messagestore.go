// Copyright 2025 Google Inc.
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

package spanner

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/spanner"

	"google.golang.org/api/iterator"

	log "github.com/golang/glog"
	tspb "google.golang.org/protobuf/types/known/timestamppb"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

// dbMessage matches the schema of the messages table, optionally joined to the
// pending_messages table.
type dbMessage struct {
	messageID              []byte
	sourceClientID         []byte
	sourceServiceName      string
	sourceMessageID        []byte
	destinationClientID    []byte
	destinationServiceName string
	messageType            string
	creationTimeSeconds    int64
	creationTimeNanos      int32
	processedTimeSeconds   sql.NullInt64
	processedTimeNanos     sql.NullInt64
	validationInfo         []byte
	failed                 sql.NullBool
	failedReason           sql.NullString
	retryCount             int64
	dataTypeURL            sql.NullString
	dataValue              []byte
	annotations            []byte
}

type pendingMessage struct {
	ClientID               []byte
	MessageID              []byte
	RetryCount             int64
	ScheduledTime          time.Time
	DestinationServiceName spanner.NullString
}

type message struct {
	MessageID             []byte
	Source                *fspb.Address
	SourceMessageID       []byte
	Destination           *fspb.Address
	MessageType           string
	CreationTime          time.Time
	EncryptedData         *anypb.Any
	Result                *fspb.MessageResult
	ValidationInformation *fspb.ValidationInfo
	Annotations           *fspb.Annotations
}

func protoMessage(ms *message) *fspb.Message {
	ct := tspb.New(ms.CreationTime)
	err := ct.CheckValid()
	if err != nil {
		// Creation time should be set by fs server on ingestion. Hard to imagine
		// how this could happen.
		log.Fatalf("Error converting Creation time to time.Time(): %v", err)
	}
	// Prefer EncryptedData when present.
	var data *anypb.Any

	if ms.EncryptedData != nil {
		if len(ms.EncryptedData.Value) > 0 {
			data = &anypb.Any{
				TypeUrl: ms.EncryptedData.TypeUrl,
				Value:   ms.EncryptedData.Value,
			}
		}
	}

	return &fspb.Message{
		MessageId:       ms.MessageID,
		Source:          ms.Source,
		SourceMessageId: ms.SourceMessageID,
		Destination:     ms.Destination,
		MessageType:     ms.MessageType,
		CreationTime:    ct,
		Data:            data,
		ValidationInfo:  ms.ValidationInformation,
		Result:          ms.Result,
		Annotations:     ms.Annotations,
	}
}

// SetMessageResult implements db.MessageStore.
func (d *Datastore) SetMessageResult(ctx context.Context, dest common.ClientID, id common.MessageID, res *fspb.MessageResult) error {
	_, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		return d.trySetMessageResult(txn, dest, id, res)
	})
	return err
}

func (d *Datastore) trySetMessageResult(txn *spanner.ReadWriteTransaction, cid common.ClientID, mid common.MessageID, res *fspb.MessageResult) error {
	bmid := mid.Bytes()
	msgCols := []string{"MessageID", "EncryptedData", "Result"}
	ms := []*spanner.Mutation{spanner.Update(d.messages, msgCols, []interface{}{bmid, nil, res})}
	if !cid.IsNil() {
		bcid := cid.Bytes()
		ms = append(ms, spanner.Delete(d.clientPendingMessages, spanner.Key{bcid, bmid}))
	}
	txn.BufferWrite(ms)
	return nil
}

// GetPendingMessageCount implements db.MessageStore.
func (d *Datastore) GetPendingMessageCount(ctx context.Context, ids []common.ClientID) (uint64, error) {
	var result int64
	clientIds := make([][]byte, 0, len(ids))
	for _, id := range ids {
		clientIds = append(clientIds, id.Bytes())
	}
	stmt := spanner.Statement{
		SQL: "SELECT " +
			"  COUNT(*) Cnt " +
			"FROM " +
			"  ClientPendingMessages cpm " +
			"WHERE " +
			"  cpm.ClientId IN UNNEST(@idsBytes) ",
		Params: map[string]interface{}{
			"idsBytes": clientIds,
		},
	}
	txn := d.dbClient.Single()
	defer txn.Close()
	iter := txn.Query(ctx, stmt)
	defer iter.Stop()
	row, err := iter.Next()
	if err == nil {
		err = row.ColumnByName("Cnt", &result)
		if err == nil {
			return uint64(result), nil
		}
	}
	return 0, err
}

// GetPendingMessages implements db.MessageStore.
func (d *Datastore) GetPendingMessages(ctx context.Context, ids []common.ClientID, offset uint64, count uint64, wantData bool) ([]*fspb.Message, error) {
	clientIds := make([][]byte, 0, len(ids))
	for _, id := range ids {
		clientIds = append(clientIds, id.Bytes())
	}
	sql := "SELECT " +
		"  cpm.MessageId " +
		"FROM " +
		"  ClientPendingMessages cpm " +
		"WHERE " +
		"  cpm.ClientId IN UNNEST(@idsBytes) " +
		"ORDER BY " +
		"  cpm.MessageId "
	params := map[string]interface{}{
		"idsBytes": clientIds,
	}
	if offset == 0 {
		if count != 0 {
			params = map[string]interface{}{
				"idsBytes": clientIds,
				"limit":    int64(count),
			}
			sql += "LIMIT @limit "
		}
	} else {
		if count != 0 {
			params = map[string]interface{}{
				"idsBytes": clientIds,
				"limit":    int64(count),
				"offset":   int64(offset),
			}
			sql += "LIMIT @limit " +
				"OFFSET @offset "
		} else {
			return nil, fmt.Errorf("if offset is provided, a count must be provided as well")
		}
	}
	stmt := spanner.Statement{
		SQL:    sql,
		Params: params,
	}
	var ks = spanner.KeySets()
	txn := d.dbClient.Single()
	defer txn.Close()
	iter := txn.Query(ctx, stmt)
	defer iter.Stop()
	for {
		var messageId []byte
		row, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := row.ColumnByName("MessageId", &messageId); err != nil {
			return nil, err
		}
		ks = spanner.KeySets(spanner.KeySets(spanner.Key{messageId}, ks))
	}
	return d.tryGetMessages(ctx, ks, wantData)
}

func (d *Datastore) getPendingMessages(ctx context.Context, txn *spanner.ReadWriteTransaction, cids []common.ClientID) (map[common.MessageID]common.ClientID, error) {
	var keySet = spanner.KeySets()
	for _, cid := range cids {
		keySet = spanner.KeySets(spanner.KeySets(spanner.Key{cid.Bytes()}.AsPrefix(), keySet))
	}
	found := make(map[common.MessageID]common.ClientID)
	iter := txn.Read(ctx, d.clientPendingMessages, keySet, []string{"ClientID", "MessageID"})
	defer iter.Stop()
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var clientId []byte
		var messageId []byte
		if err := row.Columns(&clientId, &messageId); err != nil {
			return nil, err
		}
		cid, err := common.BytesToClientID(clientId)
		if err != nil {
			return nil, err
		}
		if err != nil {
			return nil, err
		}
		mid, err := common.BytesToMessageID(messageId)
		if err != nil {
			return nil, err
		}
		found[mid] = cid
	}
	return found, nil
}

// DeletePendingMessages implements db.MessageStore.
func (d *Datastore) DeletePendingMessages(ctx context.Context, cids []common.ClientID) error {
	_, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		toDel, _ := d.getPendingMessages(ctx, txn, cids)
		res := &fspb.MessageResult{
			ProcessedTime: db.NowProto(),
			Failed:        true,
			FailedReason:  "Removed by admin action.",
		}
		var ms []*spanner.Mutation
		msgCols := []string{"MessageID", "EncryptedData", "Result"}
		for mid, cid := range toDel {
			bcid := cid.Bytes()
			bmid := mid.Bytes()
			ms = append(ms, spanner.Update(d.messages, msgCols, []interface{}{bmid, nil, res}))
			ms = append(ms, spanner.Delete(d.clientPendingMessages, spanner.Key{bcid, bmid}))
		}
		err := txn.BufferWrite(ms)
		return err
	})
	return err
}

// StoreMessages implements db.MessageStore.
func (d *Datastore) StoreMessages(ctx context.Context, msgs []*fspb.Message, contact db.ContactID) error {
	blindStore := true
	for _, m := range msgs {
		// If it doesn't have a result, or it has a failed result, or is
		// for a client -> we cannot do a blind write and need to do the
		// long path.
		if m.Result == nil || m.Result.Failed || len(m.Destination.ClientId) > 0 {
			blindStore = false
			break
		}
	}
	if blindStore {
		return d.blindStoreProcessedMessages(ctx, msgs)
	}

	_, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		return d.tryStoreMessages(ctx, txn, msgs, contact)
	})
	return err
}

func (d *Datastore) blindStoreProcessedMessages(ctx context.Context, msgs []*fspb.Message) error {
	var ms []*spanner.Mutation
	for _, m := range msgs {
		err := m.CreationTime.CheckValid()
		if err != nil {
			return fmt.Errorf("unable to convert creation time to time.Time: %v", err)
		}
		ct := m.CreationTime.AsTime()
		cols := []string{"MessageID", "Source", "SourceMessageID", "Destination", "MessageType",
			"CreationTime", "ValidationInformation", "Result", "EncryptedData",
			"Annotations"}
		vals := []interface{}{m.MessageId, m.Source, m.SourceMessageId, m.Destination, m.MessageType,
			ct, m.ValidationInfo, m.Result, nil, m.Annotations}
		ms = append(ms, spanner.Replace(d.messages, cols, vals))
	}
	_, lastErr := d.dbClient.Apply(ctx, ms)
	return lastErr
}

// messageInfo is an excerpt of message containing only what StoreMessages needs
// to know in order to decide what do.
type messageInfo struct {
	MessageID   []byte
	Destination *fspb.Address
	Result      *fspb.MessageResult
}

func (d *Datastore) tryStoreMessages(ctx context.Context, txn *spanner.ReadWriteTransaction, msgs []*fspb.Message, contact db.ContactID) error {
	// First do a parallel read for existing messages.
	ids := spanner.KeySets()
	for _, m := range msgs {
		// If it is already processed, we don't want to save m.Data.
		if m.Result != nil {
			m.Data = nil
			if len(m.Destination.ClientId) == 0 {
				// We can do a blind write, so don't bother trying to read its info.
				continue
			}
		}
		ids = spanner.KeySets(spanner.KeySets(spanner.Key{m.MessageId}, ids))
	}
	found := make(map[common.MessageID]*messageInfo)
	iter := txn.Read(ctx, d.messages, ids, []string{"MessageID", "Destination", "Result"})
	defer iter.Stop()
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		var info messageInfo
		var messageId []byte
		var destination *fspb.Address
		result := &spanner.NullProtoMessage{}
		result.ProtoMessageVal = &fspb.MessageResult{}

		if err := row.Columns(&messageId, &destination, &result); err != nil {
			return err
		}
		id, err := common.BytesToMessageID(messageId)
		if err != nil {
			return err
		}
		info.MessageID = messageId
		info.Destination = destination
		if result.Valid {
			info.Result = result.ProtoMessageVal.(*fspb.MessageResult)
		}
		found[id] = &info
	}

	var toLink []common.MessageID
	// Now try to save/alter each message according to it status.
	for _, m := range msgs {
		id, err := common.BytesToMessageID(m.MessageId)
		if err != nil {
			return err
		}
		info, ok := found[id]
		switch {
		case m.Result != nil && !m.Result.Failed && len(m.Destination.ClientId) == 0:
			// tryStoreMessage will do a blind write in this case
			fallthrough
		case !ok:
			// Common case. Message not yet present. Store as normal.
			if err := d.tryStoreMessage(ctx, txn, m, false); err != nil {
				return err
			}
			toLink = append(toLink, id)
		case info.Result != nil && !info.Result.Failed:
			// Message previously successfully processed, ignore this reprocessing.
			continue
		case m.Result != nil && (info.Result == nil || !m.Result.Failed):
			// Since the last case didn't match, the message wasn't previously
			// successfully processed. The cases the transitions:
			//
			// no result -> any result
			// failed result -> successful result
			cid, err := common.BytesToClientID(info.Destination.ClientId)
			if err != nil {
				return err
			}
			mid, err := common.BytesToMessageID(info.MessageID)
			if err != nil {
				return err
			}
			if err := d.trySetMessageResult(txn, cid, mid, m.Result); err != nil {
				return err
			}
		default:
			// The message is already present, but unprocessed/failed, and this
			// processing didn't succeed or is ongoing. Nothing to do.
			continue
		}
	}
	if contact == "" {
		return nil
	}

	cid, ts, err := splitContact(contact)
	if err != nil {
		return err
	}

	return d.tryLinkMessagesToContact(txn, cid, ts, toLink)
}

func (d *Datastore) tryStoreMessage(ctx context.Context, txn *spanner.ReadWriteTransaction, m *fspb.Message, isBroadcast bool) error {
	sendServerMsgEvent := false
	err := m.CreationTime.CheckValid()
	if err != nil {
		return fmt.Errorf("unable to convert creation time to time.Time: %v", err)
	}
	var encryptedData *anypb.Any
	if m.Data != nil {
		encryptedData = &anypb.Any{
			TypeUrl: m.Data.TypeUrl,
			Value:   m.Data.Value,
		}
	}
	ct := m.CreationTime.AsTime()
	var ms []*spanner.Mutation
	msgCols := []string{"MessageID", "Source", "SourceMessageID", "Destination", "MessageType", "CreationTime",
		"ValidationInformation", "Result", "EncryptedData", "Annotations"}
	values := []interface{}{m.MessageId, m.Source, m.SourceMessageId, m.Destination, m.MessageType, ct,
		m.ValidationInfo, m.Result, encryptedData, m.Annotations}
	if m.Result != nil && !m.Result.Failed && len(m.Destination.ClientId) == 0 {
		// Replace has the side effect of deleting anything in
		// ServerMessageNotifications for this message.
		ms = append(ms, spanner.Replace(d.messages, msgCols, values))
	} else {
		ms = append(ms, spanner.InsertOrUpdate(d.messages, msgCols, values))
	}
	if m.Result == nil {
		if len(m.Destination.ClientId) == 0 {
			sendServerMsgEvent = true
		} else {
			var retryTime time.Time
			if isBroadcast {
				retryTime = db.ClientRetryTime()
			} else {
				retryTime = db.Now()
			}
			pendingMsgCols := []string{"ClientID", "MessageID", "RetryCount", "ScheduledTime", "CreationTime", "DestinationServiceName"}
			ms = append(ms, spanner.InsertOrUpdate(d.clientPendingMessages, pendingMsgCols,
				[]interface{}{m.Destination.ClientId, m.MessageId, int64(0), retryTime, ct, m.Destination.ServiceName}))
		}
	}
	err = txn.BufferWrite(ms)
	if err != nil {
		return err
	}
	if sendServerMsgEvent {
		result := d.pubsubTopic.Publish(ctx, &pubsub.Message{
			Data: m.MessageId,
		})
		if log.V(1) {
			log.InfoContextf(ctx, "ServerMessagesForProcessing(%v), result: %v", m.MessageId, result)
		}
	}
	return nil
}

// GetMessages implements db.Store.
func (d *Datastore) GetMessages(ctx context.Context, ids []common.MessageID, wantData bool) ([]*fspb.Message, error) {
	var msgKeySet = spanner.KeySets()
	for _, id := range ids {
		msgKeySet = spanner.KeySets(
			spanner.KeySets(spanner.Key{id.Bytes()}), msgKeySet)
	}
	return d.tryGetMessages(ctx, msgKeySet, wantData)
}

func (d *Datastore) tryGetMessages(ctx context.Context, msgKeySet spanner.KeySet, wantData bool) ([]*fspb.Message, error) {
	var ret []*fspb.Message
	var cols = []string{
		"MessageID",
		"Source",
		"SourceMessageID",
		"Destination",
		"MessageType",
		"CreationTime",
		"Result",
		"ValidationInformation",
		"Annotations"}
	if wantData {
		// Everything in msg, including Data
		cols = append(cols, "EncryptedData")
	}
	txn := d.dbClient.Single()
	defer txn.Close()
	iter := txn.Read(ctx, d.messages, msgKeySet, cols)
	defer iter.Stop()
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return ret, err
		}
		m := message{}
		var messageId []byte
		var source *fspb.Address
		var sourceMessageId []byte
		var destination *fspb.Address
		var messageType string
		var creationTime time.Time
		result := &spanner.NullProtoMessage{}
		result.ProtoMessageVal = &fspb.MessageResult{}
		validationInfo := &spanner.NullProtoMessage{}
		validationInfo.ProtoMessageVal = &fspb.ValidationInfo{}
		annotations := &spanner.NullProtoMessage{}
		annotations.ProtoMessageVal = &fspb.Annotations{}
		if wantData {
			encryptedData := &spanner.NullProtoMessage{}
			encryptedData.ProtoMessageVal = &anypb.Any{}
			if err := row.Columns(&messageId, &source, &sourceMessageId, &destination, &messageType, &creationTime, &result, &validationInfo, &annotations, &encryptedData); err != nil {
				return ret, err
			}
			if encryptedData.Valid {
				m.EncryptedData = encryptedData.ProtoMessageVal.(*anypb.Any)
			}
		} else {
			if err := row.Columns(&messageId, &source, &sourceMessageId, &destination, &messageType, &creationTime, &result, &validationInfo, &annotations); err != nil {
				return ret, err
			}
		}
		m.MessageID = messageId
		m.Source = source
		m.SourceMessageID = sourceMessageId
		m.Destination = destination
		m.MessageType = messageType
		m.CreationTime = creationTime
		if result.Valid {
			m.Result = result.ProtoMessageVal.(*fspb.MessageResult)
		}
		if validationInfo.Valid {
			m.ValidationInformation = validationInfo.ProtoMessageVal.(*fspb.ValidationInfo)
		}
		if annotations.Valid {
			m.Annotations = annotations.ProtoMessageVal.(*fspb.Annotations)
		}
		log.V(2).InfoContextf(ctx, "====== messagestore: Message - MessageID: %v", m.MessageID)
		ret = append(ret, protoMessage(&m))
	}
	return ret, nil
}

// GetMessageResult implements db.Store.
func (d *Datastore) GetMessageResult(ctx context.Context, id common.MessageID) (*fspb.MessageResult, error) {
	ret := &spanner.NullProtoMessage{}
	ret.ProtoMessageVal = &fspb.MessageResult{}
	txn := d.dbClient.Single()
	defer txn.Close()
	row, err := txn.ReadRow(ctx, d.messages, spanner.Key{id.Bytes()}, []string{"Result"})
	if err == nil {
		err = row.Column(0, &ret)
		if err != nil {
			return nil, err
		} else if ret.Valid {
			return ret.ProtoMessageVal.(*fspb.MessageResult), nil
		} else {
			return nil, nil
		}
	} else {
		return nil, err
	}
}

// ClientMessagesForProcessing implements db.MessageStore.
func (d *Datastore) ClientMessagesForProcessing(ctx context.Context, clientID common.ClientID, lim uint64, serviceLimits map[string]uint64) ([]*fspb.Message, error) {
	var pm pendingMessage
	var pendKeySet = spanner.KeySets()
	now := db.Now()
	var count uint64
	var readCount map[string]uint64
	if serviceLimits != nil {
		readCount = make(map[string]uint64)
	}
	txn := d.dbClient.Single()
	defer txn.Close()
	iter := txn.Read(ctx, d.clientPendingMessages,
		spanner.Key{clientID.Bytes()}.AsPrefix(),
		[]string{"ClientID", "MessageID", "RetryCount",
			"ScheduledTime", "DestinationServiceName"})
	defer iter.Stop()
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		if count >= lim {
			break
		}
		if err := row.ToStruct(&pm); err != nil {
			return nil, err
		}
		if serviceLimits != nil {
			if !pm.DestinationServiceName.Valid {
				log.WarningContextf(ctx, "DestinationServiceName not set for message %x, ignoring.", pm.MessageID)
				continue
			}
			n := pm.DestinationServiceName.StringVal
			val, ok := serviceLimits[n]
			if !ok || readCount[n] >= val {
				continue
			}
		}
		if pm.ScheduledTime.After(now) {
			continue
		}
		pendKeySet = spanner.KeySets(
			spanner.KeySets(spanner.Key{clientID.Bytes(), pm.MessageID}), pendKeySet)
		count++
		if serviceLimits != nil {
			readCount[pm.DestinationServiceName.StringVal]++
		}
	}

	if log.V(1) {
		if serviceLimits == nil {
			log.InfoContextf(ctx, "ClientMessagesForProcessing(%v): selected %d messages with limit %d.", clientID, lim, count)
		} else {
			log.InfoContextf(ctx, "ClientMessagesForProcessing(%v): selected %d (%v) messages with limit %d (%v).", clientID, count, readCount, lim, serviceLimits)
		}
	}

	msgs, mus, err := d.tryClientMessagesForProcessing(ctx, clientID, now, &pendKeySet)
	if err == nil {
		_, err = d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
			e := txn.BufferWrite(mus)
			return e
		})
	}

	log.V(2).InfoContextf(ctx, "ClientMessagesForProcessing(%v): returning %d messages.", clientID, len(msgs))
	return msgs, err
}

func (d *Datastore) tryClientMessagesForProcessing(ctx context.Context, id common.ClientID, now time.Time, pendKeySet *spanner.KeySet) ([]*fspb.Message, []*spanner.Mutation, error) {
	var pm pendingMessage
	var msgKeySet = spanner.KeySets()
	var count int
	var mus []*spanner.Mutation

	txn := d.dbClient.Single()
	defer txn.Close()
	iter := txn.Read(ctx, d.clientPendingMessages, *pendKeySet,
		[]string{"ClientID", "MessageID", "RetryCount",
			"ScheduledTime", "DestinationServiceName"})
	defer iter.Stop()
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, mus, err
		}
		if err := row.ToStruct(&pm); err != nil {
			return nil, mus, err
		}

		log.V(2).InfoContextf(ctx, "====== messagestore: PendingMessage - ClientID: %v, MessageID: %v", pm.ClientID, pm.MessageID)
		if pm.ScheduledTime.After(now) {
			log.WarningContextf(ctx, "Message %x for client %v changed during ClientMessageForProcessing - multiple connections?", pm.MessageID, id)
			return nil, mus, nil
		}
		pm.RetryCount = pm.RetryCount + 1
		pm.ScheduledTime = db.ClientRetryTime()

		mu, err := spanner.UpdateStruct(d.clientPendingMessages, pm)
		if err != nil {
			return nil, mus, err
		}
		mus = append(mus, mu)
		msgKeySet = spanner.KeySets(
			spanner.KeySets(spanner.Key{pm.MessageID}), msgKeySet)
		count++
	}

	rl := make([]*fspb.Message, 0, count)

	txn2 := d.dbClient.Single()
	defer txn2.Close()
	iter = txn2.Read(ctx, d.messages, msgKeySet,
		[]string{"MessageID", "Source", "SourceMessageID", "Destination", "MessageType", "CreationTime",
			"EncryptedData", "Result", "ValidationInformation", "Annotations"})
	defer iter.Stop()
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, mus, err
		}
		var messageId []byte
		var source *fspb.Address
		var sourceMessageId []byte
		var destination *fspb.Address
		var messageType string
		var creationTime time.Time
		encryptedData := &spanner.NullProtoMessage{}
		encryptedData.ProtoMessageVal = &anypb.Any{}
		result := &spanner.NullProtoMessage{}
		result.ProtoMessageVal = &fspb.MessageResult{}
		validationInfo := &spanner.NullProtoMessage{}
		validationInfo.ProtoMessageVal = &fspb.ValidationInfo{}
		annotations := &spanner.NullProtoMessage{}
		annotations.ProtoMessageVal = &fspb.Annotations{}
		if err := row.Columns(&messageId, &source, &sourceMessageId, &destination, &messageType, &creationTime, &encryptedData, &result, &validationInfo, &annotations); err != nil {
			return nil, mus, err
		}
		m := message{}
		m.MessageID = messageId
		m.Source = source
		m.SourceMessageID = sourceMessageId
		m.Destination = destination
		m.MessageType = messageType
		m.CreationTime = creationTime
		if encryptedData.Valid {
			m.EncryptedData = encryptedData.ProtoMessageVal.(*anypb.Any)
		}
		if result.Valid {
			m.Result = result.ProtoMessageVal.(*fspb.MessageResult)
		}
		if validationInfo.Valid {
			m.ValidationInformation = validationInfo.ProtoMessageVal.(*fspb.ValidationInfo)
		}
		if annotations.Valid {
			m.Annotations = annotations.ProtoMessageVal.(*fspb.Annotations)
		}

		log.V(2).InfoContextf(ctx, "====== messagestore: Message - MessageID: %v", m.MessageID)
		rl = append(rl, protoMessage(&m))
	}

	return rl, mus, nil
}

// RegisterMessageProcessor implements db.Store.
func (d *Datastore) RegisterMessageProcessor(mp db.MessageProcessor) {
	ctx := context.Background()
	go func() {
		err := d.pubsubSub.Receive(ctx, func(_ context.Context, msg *pubsub.Message) {
			var msgKeySet = spanner.KeySets()
			msgKeySet = spanner.KeySets(
				spanner.KeySets(spanner.Key{msg.Data}), msgKeySet)
			msgs, err := d.tryGetMessages(ctx, msgKeySet, true)
			if err == nil {
				msg.Ack()
				mp.ProcessMessages(msgs)
			} else {
				msg.Nack()
			}
		})
		if err != nil && err != context.Canceled {
			log.Errorf("Failed to receive server message for processing: %v", err)
		}
	}()
}

func (d *Datastore) StopMessageProcessor() {
	log.Error("+++ messagestore: StopMessageProcessor() called")
}