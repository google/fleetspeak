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
	"context"
	"io"
	"strconv"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/ids"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/ftime"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

type noopStatsCollector struct{}

func (s noopStatsCollector) MessageIngested(backlogged bool, m *fspb.Message) {
}

func (s noopStatsCollector) MessageSaved(service, messageType string, forClient bool, savedPayloadBytes int) {
}

func (s noopStatsCollector) MessageProcessed(start, end time.Time, service, messageType string) {
}

func (s noopStatsCollector) MessageErrored(start, end time.Time, service, messageType string, isTemp bool) {
}

func (s noopStatsCollector) MessageDropped(service, messageType string) {
}

func (s noopStatsCollector) ClientPoll(info stats.PollInfo) {
}

func (s noopStatsCollector) DatastoreOperation(start, end time.Time, operation string, result error) {
}

func (s noopStatsCollector) ResourceUsageDataReceived(cd *db.ClientData, rud mpb.ResourceUsageData, v *fspb.ValidationInfo) {
}

func (s noopStatsCollector) KillNotificationReceived(cd *db.ClientData, kn mpb.KillNotification) {
}

var (
	// Metric collectors for PrometheusStatsCollector struct
	messagesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "fleetspeak_messages_ingested_total",
		Help: "The total number of messages ingested by Fleetspeak server",
	},
		[]string{"backlogged", "source_service_name", "destination_service_name", "message_type"},
	)

	messagesSaved = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "fleetspeak_messages_saved_total",
		Help: "The total number of messages saved by Fleetspeak server",
	},
		[]string{"service", "message_type", "for_client"},
	)

	messagesSavedSize = promauto.NewCounter(prometheus.CounterOpts{
		Name: "fleetspeak_messages_saved_payload_size",
		Help: "The total payload size of messages saved by Fleetspeak server (in bytes)",
	})

	messagesProcessed = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_messages_processed",
		Help: "The number of messages processed by Fleetspeak server",
	},
		[]string{"message_type"},
	)

	messagesErrored = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_messages_errored",
		Help: "The number of message processings that returned an error",
	},
		[]string{"message_type", "is_temp"},
	)

	messagesDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "fleetspeak_server_messages_dropped_total",
		Help: "The total number of messages dropped by Fleetspeak server when too many messages for the sevices are being processed.",
	})

	clientPolls = promauto.NewCounter(prometheus.CounterOpts{
		Name: "fleetspeak_server_client_polls_total",
		Help: "The total number of times a client polls the Fleetspeak server.",
	})

	datastoreOperationsCompleted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "fleetspeak_server_datastore_operations_completed_total",
		Help: "The total number of datastore operations completed.",
	})

	resourcesUsageDataReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "fleetspeak_server_resource_usage_data_received_total",
		Help: "The total number of times a client-resource-usage proto is received.",
	})

	killNotificationsReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "fleetspeak_server_kill_notifications_received_total",
		Help: "The total number of times a kill notification is received from a client.",
	})
)

// A PrometheusStatsCollector is an implementation of a Collector interface.
// It exports stats to a Prometheus HTTP handler, which are exposed at :2112/metrics
// and are scrapable by Prometheus.
type PrometheusStatsCollector struct{}

func (s PrometheusStatsCollector) MessageIngested(backlogged bool, m *fspb.Message) {
	messagesIngested.WithLabelValues(strconv.FormatBool(backlogged), m.Source.ServiceName, m.Destination.ServiceName, m.MessageType).Inc()
}

func (s PrometheusStatsCollector) MessageSaved(service, messageType string, forClient bool, savedPayloadBytes int) {
	messagesSaved.WithLabelValues(service, messageType, strconv.FormatBool(forClient)).Inc()
	messagesSavedSize.Add(float64(savedPayloadBytes))
}

func (s PrometheusStatsCollector) MessageProcessed(start, end time.Time, service, messageType string) {
	messagesProcessed.WithLabelValues(messageType).Observe(end.Sub(start).Seconds())
}

func (s PrometheusStatsCollector) MessageErrored(start, end time.Time, service, messageType string, isTemp bool) {
	messagesProcessed.WithLabelValues(messageType, strconv.FormatBool(isTemp)).Observe(end.Sub(start).Seconds())
}

func (s PrometheusStatsCollector) MessageDropped(service, messageType string) {
	messagesDropped.Inc()
}

func (s PrometheusStatsCollector) ClientPoll(info stats.PollInfo) {
	clientPolls.Inc()
}

func (s PrometheusStatsCollector) DatastoreOperation(start, end time.Time, operation string, result error) {
	datastoreOperationsCompleted.Inc()
}

func (s PrometheusStatsCollector) ResourceUsageDataReceived(cd *db.ClientData, rud mpb.ResourceUsageData, v *fspb.ValidationInfo) {
	resourcesUsageDataReceived.Inc()
}

func (s PrometheusStatsCollector) KillNotificationReceived(cd *db.ClientData, kn mpb.KillNotification) {
	killNotificationsReceived.Inc()
}

// A MonitoredDatastore wraps a base Datastore and collects statistics about all
// datastore operations.
type MonitoredDatastore struct {
	D db.Store
	C stats.Collector
}

func (d MonitoredDatastore) ClientMessagesForProcessing(ctx context.Context, id common.ClientID, lim uint64, serviceLimits map[string]uint64) ([]*fspb.Message, error) {
	s := ftime.Now()
	res, err := d.D.ClientMessagesForProcessing(ctx, id, lim, serviceLimits)
	d.C.DatastoreOperation(s, ftime.Now(), "ClientMessagesForProcessing", err)
	return res, err
}

func (d MonitoredDatastore) StoreMessages(ctx context.Context, msgs []*fspb.Message, contact db.ContactID) error {
	s := ftime.Now()
	err := d.D.StoreMessages(ctx, msgs, contact)
	d.C.DatastoreOperation(s, ftime.Now(), "StoreMessages", err)
	return err
}

func (d MonitoredDatastore) DeletePendingMessages(ctx context.Context, ids []common.ClientID) error {
	s := ftime.Now()
	err := d.D.DeletePendingMessages(ctx, ids)
	d.C.DatastoreOperation(s, ftime.Now(), "DeletePendingMessages", err)
	return err
}

func (d MonitoredDatastore) GetMessages(ctx context.Context, ids []common.MessageID, wantData bool) ([]*fspb.Message, error) {
	s := ftime.Now()
	res, err := d.D.GetMessages(ctx, ids, wantData)
	d.C.DatastoreOperation(s, ftime.Now(), "GetMessages", err)
	return res, err
}

func (d MonitoredDatastore) SetMessageResult(ctx context.Context, dest common.ClientID, id common.MessageID, res *fspb.MessageResult) error {
	s := ftime.Now()
	err := d.D.SetMessageResult(ctx, dest, id, res)
	d.C.DatastoreOperation(s, ftime.Now(), "SetMessageResult", err)
	return err
}

func (d MonitoredDatastore) GetMessageResult(ctx context.Context, id common.MessageID) (*fspb.MessageResult, error) {
	s := ftime.Now()
	res, err := d.D.GetMessageResult(ctx, id)
	d.C.DatastoreOperation(s, ftime.Now(), "GetMessageResult", err)
	return res, err
}

func (d MonitoredDatastore) ListClients(ctx context.Context, ids []common.ClientID) ([]*spb.Client, error) {
	s := ftime.Now()
	res, err := d.D.ListClients(ctx, ids)
	d.C.DatastoreOperation(s, ftime.Now(), "ListClients", err)
	return res, err
}

func (d MonitoredDatastore) GetClientData(ctx context.Context, id common.ClientID) (*db.ClientData, error) {
	s := ftime.Now()
	res, err := d.D.GetClientData(ctx, id)
	e := err
	if e != nil && d.D.IsNotFound(e) {
		e = nil
	}
	d.C.DatastoreOperation(s, ftime.Now(), "GetClientData", e)
	return res, err
}

func (d MonitoredDatastore) AddClient(ctx context.Context, id common.ClientID, data *db.ClientData) error {
	s := ftime.Now()
	err := d.D.AddClient(ctx, id, data)
	d.C.DatastoreOperation(s, ftime.Now(), "AddClient", err)
	return err
}

func (d MonitoredDatastore) AddClientLabel(ctx context.Context, id common.ClientID, l *fspb.Label) error {
	s := ftime.Now()
	err := d.D.AddClientLabel(ctx, id, l)
	d.C.DatastoreOperation(s, ftime.Now(), "AddClientLabel", err)
	return err
}

func (d MonitoredDatastore) RemoveClientLabel(ctx context.Context, id common.ClientID, l *fspb.Label) error {
	s := ftime.Now()
	err := d.D.RemoveClientLabel(ctx, id, l)
	d.C.DatastoreOperation(s, ftime.Now(), "RemoveClientLabel", err)
	return err
}

func (d MonitoredDatastore) BlacklistClient(ctx context.Context, id common.ClientID) error {
	s := ftime.Now()
	err := d.D.BlacklistClient(ctx, id)
	d.C.DatastoreOperation(s, ftime.Now(), "BlacklistClient", err)
	return err
}

func (d MonitoredDatastore) RecordClientContact(ctx context.Context, data db.ContactData) (db.ContactID, error) {
	s := ftime.Now()
	res, err := d.D.RecordClientContact(ctx, data)
	d.C.DatastoreOperation(s, ftime.Now(), "RecordClientContact", err)
	return res, err
}

func (d MonitoredDatastore) ListClientContacts(ctx context.Context, id common.ClientID) ([]*spb.ClientContact, error) {
	s := ftime.Now()
	res, err := d.D.ListClientContacts(ctx, id)
	d.C.DatastoreOperation(s, ftime.Now(), "ListClientContacts", err)
	return res, err
}

func (d MonitoredDatastore) RecordResourceUsageData(ctx context.Context, id common.ClientID, rud mpb.ResourceUsageData) error {
	s := ftime.Now()
	err := d.D.RecordResourceUsageData(ctx, id, rud)
	d.C.DatastoreOperation(s, ftime.Now(), "RecordResourceUsageData", err)
	return err
}

func (d MonitoredDatastore) FetchResourceUsageRecords(ctx context.Context, id common.ClientID, limit int) ([]*spb.ClientResourceUsageRecord, error) {
	s := ftime.Now()
	res, err := d.D.FetchResourceUsageRecords(ctx, id, limit)
	d.C.DatastoreOperation(s, ftime.Now(), "FetchResourceUsageRecords", err)
	return res, err
}

func (d MonitoredDatastore) LinkMessagesToContact(ctx context.Context, contact db.ContactID, msgs []common.MessageID) error {
	s := ftime.Now()
	err := d.D.LinkMessagesToContact(ctx, contact, msgs)
	d.C.DatastoreOperation(s, ftime.Now(), "LinkMessagesToContact", err)
	return err
}

func (d MonitoredDatastore) CreateBroadcast(ctx context.Context, b *spb.Broadcast, limit uint64) error {
	s := ftime.Now()
	err := d.D.CreateBroadcast(ctx, b, limit)
	d.C.DatastoreOperation(s, ftime.Now(), "CreateBroadcast", err)
	return err
}

func (d MonitoredDatastore) SetBroadcastLimit(ctx context.Context, id ids.BroadcastID, limit uint64) error {
	s := ftime.Now()
	err := d.D.SetBroadcastLimit(ctx, id, limit)
	d.C.DatastoreOperation(s, ftime.Now(), "SetBroadcastLimit", err)
	return err
}

func (d MonitoredDatastore) SaveBroadcastMessage(ctx context.Context, msg *fspb.Message, bid ids.BroadcastID, cid common.ClientID, aid ids.AllocationID) error {
	s := ftime.Now()
	err := d.D.SaveBroadcastMessage(ctx, msg, bid, cid, aid)
	d.C.DatastoreOperation(s, ftime.Now(), "SaveBroadcastMessage", err)
	return err
}

func (d MonitoredDatastore) ListActiveBroadcasts(ctx context.Context) ([]*db.BroadcastInfo, error) {
	s := ftime.Now()
	res, err := d.D.ListActiveBroadcasts(ctx)
	d.C.DatastoreOperation(s, ftime.Now(), "ListActiveBroadcasts", err)
	return res, err
}

func (d MonitoredDatastore) ListSentBroadcasts(ctx context.Context, id common.ClientID) ([]ids.BroadcastID, error) {
	s := ftime.Now()
	res, err := d.D.ListSentBroadcasts(ctx, id)
	d.C.DatastoreOperation(s, ftime.Now(), "ListSentBroadcasts", err)
	return res, err
}

func (d MonitoredDatastore) CreateAllocation(ctx context.Context, id ids.BroadcastID, frac float32, expiry time.Time) (*db.AllocationInfo, error) {
	s := ftime.Now()
	res, err := d.D.CreateAllocation(ctx, id, frac, expiry)
	d.C.DatastoreOperation(s, ftime.Now(), "CreateAllocation", err)
	return res, err
}

func (d MonitoredDatastore) CleanupAllocation(ctx context.Context, bid ids.BroadcastID, aid ids.AllocationID) error {
	s := ftime.Now()
	err := d.D.CleanupAllocation(ctx, bid, aid)
	d.C.DatastoreOperation(s, ftime.Now(), "CleanupAllocation", err)
	return err
}

func (d MonitoredDatastore) RegisterMessageProcessor(mp db.MessageProcessor) {
	d.D.RegisterMessageProcessor(mp)
}

func (d MonitoredDatastore) StopMessageProcessor() {
	d.D.StopMessageProcessor()
}

func (d MonitoredDatastore) StoreFile(ctx context.Context, service, name string, data io.Reader) error {
	return d.D.StoreFile(ctx, service, name, data)
}

func (d MonitoredDatastore) StatFile(ctx context.Context, service, name string) (time.Time, error) {
	return d.D.StatFile(ctx, service, name)
}

func (d MonitoredDatastore) ReadFile(ctx context.Context, service, name string) (db.ReadSeekerCloser, time.Time, error) {
	return d.D.ReadFile(ctx, service, name)
}

func (d MonitoredDatastore) IsNotFound(err error) bool {
	return d.D.IsNotFound(err)
}

func (d MonitoredDatastore) Close() error {
	return d.D.Close()
}
