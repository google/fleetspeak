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
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/spanner"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"

	"google.golang.org/api/iterator"

	log "github.com/golang/glog"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
)

const (
	bytesToMIB = 1.0 / float64(1<<20)
)

type errClientNotFound struct {
	id common.ClientID
}

func (e errClientNotFound) Error() string {
	return fmt.Sprintf("client [%v] not found", e.id)
}

func bytesToUint64(b []byte) (uint64, error) {
	if len(b) != 8 {
		return 0, fmt.Errorf("error converting to uint64, expected 8 bytes, got %d", len(b))
	}
	return binary.LittleEndian.Uint64(b), nil
}

func uint64ToBytes(i uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, i)
	return b
}

// StreamClientIds implements db.Store.
func (d *Datastore) StreamClientIds(ctx context.Context, includeBlacklisted bool, lastContactAfter *time.Time, callback func(common.ClientID) error) error {
	query := "SELECT t.ClientID FROM Clients AS t"
	var params map[string]interface{}
	switch {
	case !includeBlacklisted && lastContactAfter == nil:
		query += " WHERE NOT t.Blacklisted"
	case !includeBlacklisted && lastContactAfter != nil:
		query += " WHERE NOT t.Blacklisted AND t.LastContactTime > @time"
		params = map[string]interface{}{
			"time": lastContactAfter,
		}
	case includeBlacklisted && lastContactAfter != nil:
		query += " WHERE t.LastContactTime > @time"
		params = map[string]interface{}{
			"time": lastContactAfter,
		}
	}
	stmt := spanner.Statement{
		SQL:    query,
		Params: params,
	}
	txn := d.dbClient.Single()
	defer txn.Close()
	iter := txn.Query(ctx, stmt)
	defer iter.Stop()
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			return nil
		}
		if err != nil {
			return err
		}
		var id []byte
		err = row.ColumnByName("ClientID", &id)
		if err != nil {
			return err
		}

		cid, err := common.BytesToClientID(id)
		if err != nil {
			return err
		}

		err = callback(cid)
		if err != nil {
			return err
		}
	}
}

// ListClients implements db.Store.
func (d *Datastore) ListClients(ctx context.Context, ids []common.ClientID) ([]*spb.Client, error) {
	var res []*spb.Client
	labels := make(map[string][]*fspb.Label)
	clientKeySet := spanner.KeySets()
	labelKeySet := spanner.KeySets()
	if len(ids) == 0 {
		clientKeySet = spanner.AllKeys()
		labelKeySet = spanner.AllKeys()
	} else {
		for _, id := range ids {
			clientKeySet = spanner.KeySets(spanner.KeySets(spanner.Key{id.Bytes()}, clientKeySet))
			labelKeySet = spanner.KeySets(spanner.KeySets(spanner.Key{id.Bytes()}.AsPrefix(), labelKeySet))
		}
	}
	txn := d.dbClient.ReadOnlyTransaction()
	defer txn.Close()
	iter := txn.Read(ctx, d.clientLabels, labelKeySet, []string{"ClientID", "Label"})
	defer iter.Stop()
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var id []byte
		label := &fspb.Label{}
		if err := row.Columns(&id, label); err != nil {
			return nil, err
		}
		labels[string(id)] = append(labels[string(id)], label)
	}
	iter = txn.Read(ctx, d.clients, clientKeySet,
		[]string{"ClientID", "LastContactTime", "LastContactAddress", "LastContactStreamingTo", "LastClock", "Blacklisted"})
	defer iter.Stop()
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		var id []byte
		var t time.Time
		var addr, streaming spanner.NullString
		var lc *tspb.Timestamp
		lcn := &spanner.NullProtoMessage{}
		lcn.ProtoMessageVal = &tspb.Timestamp{}

		var bl spanner.NullBool
		if err := row.Columns(&id, &t, &addr, &streaming, &lcn, &bl); err != nil {
			return nil, err
		}
		var ts *tspb.Timestamp
		if !t.IsZero() {
			p := tspb.New(t)
			err := p.CheckValid()
			if err != nil {
				return nil, fmt.Errorf("unable to parse LastContactTime for %x: %v", id, err)
			}
			ts = p
		}
		if !addr.Valid {
			addr.StringVal = ""
		}
		if !streaming.Valid {
			streaming.StringVal = ""
		}
		if lcn.Valid {
			lc = lcn.ProtoMessageVal.(*tspb.Timestamp)
		}
		res = append(res, &spb.Client{
			ClientId:               id,
			LastContactTime:        ts,
			LastContactAddress:     addr.StringVal,
			LastContactStreamingTo: streaming.StringVal,
			Labels:                 labels[string(id)],
			LastClock:              lc,
			Blacklisted:            bl.Valid && bl.Bool,
		})
	}
	return res, nil
}

// GetClientData implements db.Store.
func (d *Datastore) GetClientData(ctx context.Context, id common.ClientID) (*db.ClientData, error) {
	var cd *db.ClientData

	txn := d.dbClient.ReadOnlyTransaction()
	defer txn.Close()

	row, err := txn.ReadRow(ctx, d.clients, spanner.Key{id.Bytes()}, []string{"ClientKey", "Blacklisted"})
	if err == nil {
		var bl spanner.NullBool
		da := db.ClientData{}
		err = row.Columns(&da.Key, &bl)
		if err == nil {
			da.Blacklisted = bl.Valid && bl.Bool
			krp := spanner.Key{id.Bytes()}.AsPrefix()
			iter := txn.Read(ctx, d.clientLabels, krp, []string{"Label"})
			defer iter.Stop()
			for {
				row, err := iter.Next()
				if err == iterator.Done {
					break
				}
				if err != nil {
					return &da, err
				}
				var label fspb.Label
				if err := row.Columns(&label); err != nil {
					return &da, err
				}
				da.Labels = append(da.Labels, &label)
			}
			cd = &da
		}
	}
	return cd, err
}

// AddClient implements db.Store.
func (d *Datastore) AddClient(ctx context.Context, id common.ClientID, data *db.ClientData) error {
	_, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		return d.tryAddClient(txn, id, data)
	})
	return err
}

func (d *Datastore) tryAddClient(txn *spanner.ReadWriteTransaction, id common.ClientID, data *db.ClientData) error {
	bid := id.Bytes()
	clientCols := []string{"ClientID", "Blacklisted", "ClientKey", "LastContactTime"}
	labelCols := []string{"ClientID", "Label"}
	ms := []*spanner.Mutation{spanner.InsertOrUpdate(d.clients, clientCols, []interface{}{bid, data.Blacklisted, data.Key, spanner.CommitTimestamp})}
	for _, label := range data.Labels {
		ms = append(ms, spanner.InsertOrUpdate(d.clientLabels, labelCols, []interface{}{bid, label}))
	}
	txn.BufferWrite(ms)
	return nil
}

// AddClientLabel implements db.Store.
func (d *Datastore) AddClientLabel(ctx context.Context, id common.ClientID, label *fspb.Label) error {
	_, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		return d.tryAddClientLabel(txn, id, label)
	})
	return err
}

func (d *Datastore) tryAddClientLabel(tr *spanner.ReadWriteTransaction, id common.ClientID, label *fspb.Label) error {
	ms := []*spanner.Mutation{spanner.InsertOrUpdate(d.clientLabels, []string{"ClientID", "Label"}, []interface{}{id.Bytes(), label})}
	tr.BufferWrite(ms)
	return nil
}

// RemoveClientLabel implements db.Store.
func (d *Datastore) RemoveClientLabel(ctx context.Context, id common.ClientID, label *fspb.Label) error {
	_, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		return d.tryRemoveClientLabel(txn, id, label)
	})
	return err
}

func (d *Datastore) tryRemoveClientLabel(tr *spanner.ReadWriteTransaction, id common.ClientID, label *fspb.Label) error {
	ms := []*spanner.Mutation{spanner.Delete(d.clientLabels, spanner.Key{id.Bytes(), label.ServiceName, label.Label})}
	tr.BufferWrite(ms)
	return nil
}

// BlacklistClient implements db.Store.
func (d *Datastore) BlacklistClient(ctx context.Context, id common.ClientID) error {
	_, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		return d.tryBlacklistClient(txn, id)
	})
	return err
}

func (d *Datastore) tryBlacklistClient(txn *spanner.ReadWriteTransaction, id common.ClientID) error {
	ms := []*spanner.Mutation{spanner.Update(d.clients, []string{"ClientID", "Blacklisted"}, []interface{}{id.Bytes(), true})}
	txn.BufferWrite(ms)
	return nil
}

// RecordClientContact implements db.Store.
func (d *Datastore) RecordClientContact(ctx context.Context, data db.ContactData) (db.ContactID, error) {
	ts, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		return d.tryRecordClientContact(txn, data)
	})
	if err != nil {
		return "", err
	}
	return db.ContactID(data.ClientID.String() + ":" + strconv.FormatInt(ts.UnixMicro(), 16)), nil
}

func (d *Datastore) tryRecordClientContact(txn *spanner.ReadWriteTransaction, data db.ContactData) error {
	bid := data.ClientID.Bytes()
	var streaming spanner.NullString
	if data.StreamingTo != "" {
		streaming.StringVal = data.StreamingTo
		streaming.Valid = true
	}
	clientCols := []string{"ClientID", "LastContactTime", "LastContactAddress", "LastContactStreamingTo", "LastClock"}
	contactCols := []string{"ClientID", "Time", "SentNonce", "ReceivedNonce", "Address"}
	ms := []*spanner.Mutation{spanner.Update(d.clients, clientCols, []interface{}{bid, spanner.CommitTimestamp, data.Addr, streaming, data.ClientClock})}
	ms = append(ms, spanner.InsertOrUpdate(d.clientContacts, contactCols, []interface{}{bid, spanner.CommitTimestamp, uint64ToBytes(data.NonceSent), uint64ToBytes(data.NonceReceived), data.Addr}))
	txn.BufferWrite(ms)
	return nil
}

// StreamClientContacts implements db.Store.
func (d *Datastore) StreamClientContacts(ctx context.Context, id common.ClientID, callback func(*spb.ClientContact) error) error {
	iter := d.dbClient.Single().Read(ctx, d.clientContacts, spanner.Key{id.Bytes()}.AsPrefix(),
		[]string{"Time", "SentNonce", "ReceivedNonce", "Address"})
	defer iter.Stop()
	for {
		row, err := iter.Next()
		if err == iterator.Done {
			return nil
		}
		if err != nil {
			return err
		}
		var ts time.Time
		var sn, rn []byte
		var ad spanner.NullString
		if err := row.Columns(&ts, &sn, &rn, &ad); err != nil {
			return err
		}
		sentNonce, err := bytesToUint64(sn)
		if err != nil {
			return err
		}
		receivedNonce, err := bytesToUint64(rn)
		if err != nil {
			return err
		}
		if !ad.Valid {
			ad.String()
		}
		tp := tspb.New(ts)
		err = tp.CheckValid()
		if err != nil {
			return fmt.Errorf("can't make timestamp proto out of read timestamp [%v]: %v", ts, err)
		}
		err = callback(&spb.ClientContact{
			SentNonce:       sentNonce,
			ReceivedNonce:   receivedNonce,
			ObservedAddress: ad.StringVal,
			Timestamp:       tp,
		})
		if err != nil {
			return fmt.Errorf("callback failed: %v", err)
		}
	}
}

// ListClientContacts implements db.Store.
func (d *Datastore) ListClientContacts(ctx context.Context, id common.ClientID) ([]*spb.ClientContact, error) {
	var res []*spb.ClientContact
	callback := func(c *spb.ClientContact) error {
		res = append(res, c)
		return nil
	}
	err := d.StreamClientContacts(ctx, id, callback)
	return res, err
}

func splitContact(contact db.ContactID) (common.ClientID, int64, error) {
	s := strings.Split(string(contact), ":")
	if len(s) != 2 {
		return common.ClientID{}, 0, fmt.Errorf("unknown ContactID format: %v", string(contact))
	}
	cid, err := common.StringToClientID(s[0])
	if err != nil {
		return common.ClientID{}, 0, err
	}
	ts, err := strconv.ParseInt(string(s[1]), 16, 64)
	if err != nil {
		return common.ClientID{}, 0, err
	}
	return cid, ts, nil
}

// LinkMessagesToContact implements db.Store.
func (d *Datastore) LinkMessagesToContact(ctx context.Context, contact db.ContactID, ids []common.MessageID) error {
	if len(ids) == 0 {
		return nil
	}
	cid, ts, err := splitContact(contact)
	if err != nil {
		return err
	}
	_, err = d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		return d.tryLinkMessagesToContact(txn, cid, ts, ids)
	})
	return err
}

func (d *Datastore) tryLinkMessagesToContact(tr *spanner.ReadWriteTransaction, cid common.ClientID, ts int64, ids []common.MessageID) error {
	bcid := cid.Bytes()
	sts := time.UnixMicro(ts)
	var ms []*spanner.Mutation
	contactMsgsCols := []string{"ClientID", "Time", "MessageID"}
	for _, id := range ids {
		ms = append(ms, spanner.InsertOrUpdate(d.clientContactMessages, contactMsgsCols, []interface{}{bcid, sts, id.Bytes()}))
	}
	tr.BufferWrite(ms)
	return nil
}

// RecordResourceUsageData implements db.Store.
func (d *Datastore) RecordResourceUsageData(ctx context.Context, id common.ClientID, rud *mpb.ResourceUsageData) error {
	_, err := d.dbClient.ReadWriteTransaction(ctx, func(ctx context.Context, txn *spanner.ReadWriteTransaction) error {
		return d.tryRecordResourceUsageData(txn, id, rud)
	})
	return err
}

func (d *Datastore) tryRecordResourceUsageData(tr *spanner.ReadWriteTransaction, id common.ClientID, rud *mpb.ResourceUsageData) error {
	record := spb.ClientResourceUsageRecord{
		Scope:                 rud.Scope,
		Pid:                   rud.Pid,
		ProcessStartTime:      rud.ProcessStartTime,
		ClientTimestamp:       rud.DataTimestamp,
		ProcessTerminated:     rud.ProcessTerminated,
		MeanUserCpuRate:       float32(rud.ResourceUsage.MeanUserCpuRate),
		MaxUserCpuRate:        float32(rud.ResourceUsage.MaxUserCpuRate),
		MeanSystemCpuRate:     float32(rud.ResourceUsage.MeanSystemCpuRate),
		MaxSystemCpuRate:      float32(rud.ResourceUsage.MaxSystemCpuRate),
		MeanResidentMemoryMib: int32(rud.ResourceUsage.MeanResidentMemory * bytesToMIB),
		MaxResidentMemoryMib:  int32(float64(rud.ResourceUsage.MaxResidentMemory) * bytesToMIB),
		MeanNumFds:            int32(rud.ResourceUsage.GetMeanNumFds()),
		MaxNumFds:             int32(rud.ResourceUsage.GetMaxNumFds()),
	}
	resourceUsageCols := []string{"ClientID", "ServerTimestamp", "Record"}
	ms := []*spanner.Mutation{spanner.InsertOrUpdate(d.clientResourceUsageRecords, resourceUsageCols, []interface{}{id.Bytes(), spanner.CommitTimestamp, &record})}
	tr.BufferWrite(ms)
	return nil
}

// FetchResourceUsageRecords implements db.Store.
func (d *Datastore) FetchResourceUsageRecords(ctx context.Context, id common.ClientID, startTimestamp, endTimestamp *tspb.Timestamp) ([]*spb.ClientResourceUsageRecord, error) {
	if err := startTimestamp.CheckValid(); err != nil {
		return nil, fmt.Errorf("startTimestamp %v is not valid: %v", startTimestamp, err)
	}
	if err := endTimestamp.CheckValid(); err != nil {
		return nil, fmt.Errorf("endTimestamp %v is not valid: %v", endTimestamp, err)
	}
	if !endTimestamp.AsTime().After(startTimestamp.AsTime()) {
		return nil, fmt.Errorf("time range is invalid: endTimestamp is before or equal startTimestamp")
	}
	stmt := spanner.Statement{
		SQL: "SELECT " +
			"  t.ServerTimestamp, t.Record " +
			"FROM " +
			"  ClientResourceUsageRecords AS t " +
			"WHERE " +
			"  t.ClientID = @cID AND " +
			"  t.ServerTimestamp >= @start AND " +
			"  t.ServerTimestamp < @end",
		Params: map[string]interface{}{
			"cID":   id.Bytes(),
			"start": startTimestamp.AsTime(),
			"end":   endTimestamp.AsTime(),
		},
	}
	iter := d.dbClient.Single().Query(ctx, stmt)
	defer iter.Stop()
	var records []*spb.ClientResourceUsageRecord
	for {
		var serverTimestamp time.Time
		var record *spb.ClientResourceUsageRecord
		row, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		if err := row.Columns(&serverTimestamp, &record); err != nil {
			return nil, err
		}
		tsProto := tspb.New(serverTimestamp)
		err = tsProto.CheckValid()
		if err != nil {
			log.ErrorContextf(ctx, "Encountered invalid commit timestamp in Spanner for client-id %s: %v", id.String(), err)
		} else {
			record.ServerTimestamp = tsProto
		}
		records = append(records, record)
	}
	return records, nil
}