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
	"io"
	"time"

	"context"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/ids"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/ftime"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

type noopStatsCollector struct{}

func (s noopStatsCollector) MessageIngested(service, messageType string, backlogged bool, payloadBytes int) {
}

func (s noopStatsCollector) MessageSaved(service, messageType string, forClient bool, savedPayloadBytes int) {
}

func (s noopStatsCollector) MessageProcessed(start, end time.Time, service, messageType string) {
}

func (s noopStatsCollector) MessageErrored(start, end time.Time, service, messageType string, isTemp bool) {
}

func (s noopStatsCollector) MessageDropped(service, messageType string) {
}

func (s noopStatsCollector) ClientPoll(start, end time.Time, httpStatus int) {
}

func (s noopStatsCollector) DatastoreOperation(start, end time.Time, operation string, result error) {
}

// A monitoredDatastore wraps a base Datastore and collects statistics about all
// datastore operations.
type monitoredDatastore struct {
	d db.Store
	c stats.Collector
}

func (d monitoredDatastore) ClientMessagesForProcessing(ctx context.Context, id common.ClientID, lim int) ([]*fspb.Message, error) {
	s := ftime.Now()
	res, err := d.d.ClientMessagesForProcessing(ctx, id, lim)
	d.c.DatastoreOperation(s, ftime.Now(), "ClientMessagesForProcessing", err)
	return res, err
}

func (d monitoredDatastore) StoreMessages(ctx context.Context, msgs []*fspb.Message, contact db.ContactID) error {
	s := ftime.Now()
	err := d.d.StoreMessages(ctx, msgs, contact)
	d.c.DatastoreOperation(s, ftime.Now(), "StoreMessages", err)
	return err
}

func (d monitoredDatastore) GetMessages(ctx context.Context, ids []common.MessageID, wantData bool) ([]*fspb.Message, error) {
	s := ftime.Now()
	res, err := d.d.GetMessages(ctx, ids, wantData)
	d.c.DatastoreOperation(s, ftime.Now(), "GetMessages", err)
	return res, err
}

func (d monitoredDatastore) GetMessageResult(ctx context.Context, id common.MessageID) (*fspb.MessageResult, error) {
	s := ftime.Now()
	res, err := d.d.GetMessageResult(ctx, id)
	d.c.DatastoreOperation(s, ftime.Now(), "GetMessageStatus", err)
	return res, err
}

func (d monitoredDatastore) ListClients(ctx context.Context) ([]*spb.Client, error) {
	s := ftime.Now()
	res, err := d.d.ListClients(ctx)
	d.c.DatastoreOperation(s, ftime.Now(), "ListClients", err)
	return res, err
}

func (d monitoredDatastore) GetClientData(ctx context.Context, id common.ClientID) (*db.ClientData, error) {
	s := ftime.Now()
	res, err := d.d.GetClientData(ctx, id)
	d.c.DatastoreOperation(s, ftime.Now(), "GetClientData", err)
	return res, err
}

func (d monitoredDatastore) AddClient(ctx context.Context, id common.ClientID, data *db.ClientData) error {
	s := ftime.Now()
	err := d.d.AddClient(ctx, id, data)
	d.c.DatastoreOperation(s, ftime.Now(), "AddClient", err)
	return err
}

func (d monitoredDatastore) AddClientLabel(ctx context.Context, id common.ClientID, l *fspb.Label) error {
	s := ftime.Now()
	err := d.d.AddClientLabel(ctx, id, l)
	d.c.DatastoreOperation(s, ftime.Now(), "AddClientLabel", err)
	return err
}

func (d monitoredDatastore) RemoveClientLabel(ctx context.Context, id common.ClientID, l *fspb.Label) error {
	s := ftime.Now()
	err := d.d.RemoveClientLabel(ctx, id, l)
	d.c.DatastoreOperation(s, ftime.Now(), "RemoveClientLabel", err)
	return err
}

func (d monitoredDatastore) RecordClientContact(ctx context.Context, id common.ClientID, nonceSent, nonceReceived uint64, addr string) (db.ContactID, error) {
	s := ftime.Now()
	res, err := d.d.RecordClientContact(ctx, id, nonceSent, nonceReceived, addr)
	d.c.DatastoreOperation(s, ftime.Now(), "RecordClientContact", err)
	return res, err
}

func (d monitoredDatastore) LinkMessagesToContact(ctx context.Context, contact db.ContactID, msgs []common.MessageID) error {
	s := ftime.Now()
	err := d.d.LinkMessagesToContact(ctx, contact, msgs)
	d.c.DatastoreOperation(s, ftime.Now(), "LinkMessagesToContact", err)
	return err
}

func (d monitoredDatastore) CreateBroadcast(ctx context.Context, b *spb.Broadcast, limit uint64) error {
	s := ftime.Now()
	err := d.d.CreateBroadcast(ctx, b, limit)
	d.c.DatastoreOperation(s, ftime.Now(), "CreateBroadcast", err)
	return err
}

func (d monitoredDatastore) SetBroadcastLimit(ctx context.Context, id ids.BroadcastID, limit uint64) error {
	s := ftime.Now()
	err := d.d.SetBroadcastLimit(ctx, id, limit)
	d.c.DatastoreOperation(s, ftime.Now(), "SetBroadcastLimit", err)
	return err
}

func (d monitoredDatastore) SaveBroadcastMessage(ctx context.Context, msg *fspb.Message, bid ids.BroadcastID, cid common.ClientID, aid ids.AllocationID) error {
	s := ftime.Now()
	err := d.d.SaveBroadcastMessage(ctx, msg, bid, cid, aid)
	d.c.DatastoreOperation(s, ftime.Now(), "SaveBroadcastMessage", err)
	return err
}

func (d monitoredDatastore) ListActiveBroadcasts(ctx context.Context) ([]*db.BroadcastInfo, error) {
	s := ftime.Now()
	res, err := d.d.ListActiveBroadcasts(ctx)
	d.c.DatastoreOperation(s, ftime.Now(), "ListActiveBroadcasts", err)
	return res, err
}

func (d monitoredDatastore) ListSentBroadcasts(ctx context.Context, id common.ClientID) ([]ids.BroadcastID, error) {
	s := ftime.Now()
	res, err := d.d.ListSentBroadcasts(ctx, id)
	d.c.DatastoreOperation(s, ftime.Now(), "ListSentBroadcasts", err)
	return res, err
}

func (d monitoredDatastore) CreateAllocation(ctx context.Context, id ids.BroadcastID, frac float32, expiry time.Time) (*db.AllocationInfo, error) {
	s := ftime.Now()
	res, err := d.d.CreateAllocation(ctx, id, frac, expiry)
	d.c.DatastoreOperation(s, ftime.Now(), "CreateAllocation", err)
	return res, err
}

func (d monitoredDatastore) CleanupAllocation(ctx context.Context, bid ids.BroadcastID, aid ids.AllocationID) error {
	s := ftime.Now()
	err := d.d.CleanupAllocation(ctx, bid, aid)
	d.c.DatastoreOperation(s, ftime.Now(), "CleanupAllocation", err)
	return err
}

func (d monitoredDatastore) RegisterMessageProcessor(mp db.MessageProcessor) {
	d.d.RegisterMessageProcessor(mp)
}

func (d monitoredDatastore) StopMessageProcessor() {
	d.d.StopMessageProcessor()
}

func (d monitoredDatastore) StoreFile(ctx context.Context, service, name string, data io.Reader) error {
	return d.d.StoreFile(ctx, service, name, data)
}

func (d monitoredDatastore) StatFile(ctx context.Context, service, name string) (time.Time, error) {
	return d.d.StatFile(ctx, service, name)
}

func (d monitoredDatastore) ReadFile(ctx context.Context, service, name string) (db.ReadSeekerCloser, time.Time, error) {
	return d.d.ReadFile(ctx, service, name)
}

func (d monitoredDatastore) IsNotFound(err error) bool {
	return d.d.IsNotFound(err)
}

func (d monitoredDatastore) Close() error {
	return d.d.Close()
}
