// Copyright 2020 Google Inc.
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

package prometheus

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

var (
	// Metric collectors for PrometheusStatsCollector struct
	messagesIngested = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "fleetspeak_messages_ingested_total",
		Help: "The total number of messages ingested by Fleetspeak server",
	},
		[]string{"backlogged", "source_service", "destination_service", "message_type"},
	)

	messagesIngestedSize = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "fleetspeak_messages_ingested_payload_bytes_size",
		Help: "The total payload size of messages ingested by Fleetspeak server (in bytes)",
	},
		[]string{"backlogged", "source_service", "destination_service", "message_type"},
	)

	messagesSaved = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "fleetspeak_messages_saved_total",
		Help: "The total number of messages saved by Fleetspeak server",
	},
		[]string{"service", "message_type", "for_client"},
	)

	messagesSavedSize = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "fleetspeak_messages_saved_payload_bytes_size",
		Help: "The total payload size of messages saved by Fleetspeak server (in bytes)",
	},
		[]string{"service", "message_type", "for_client"},
	)

	messagesProcessed = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_messages_processed_latency",
		Help: "The latency distribution of messages processed by Fleetspeak server",
	},
		[]string{"message_type", "service"},
	)

	messagesErrored = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_messages_errored_latency",
		Help: "The latency distribution of message processings that returned an error",
	},
		[]string{"message_type", "is_temp"},
	)

	messagesDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "fleetspeak_server_messages_dropped_total",
		Help: "The total number of messages dropped by Fleetspeak server when too many messages for the sevices are being processed.",
	},
		[]string{"service", "message_type"},
	)

	clientPolls = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "fleetspeak_server_client_polls_total",
		Help: "The total number of times a client polls the Fleetspeak server.",
	},
		[]string{"http_status_code", "poll_type", "cache_hit"},
	)

	clientPollsOpTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_client_polls_operation_time_latency",
		Help: "The latency distribution of times a client polls the Fleetspeak server (based on when the operation started and ended).",
	},
		[]string{"http_status_code", "poll_type", "cache_hit"},
	)

	clientPollsReadTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_client_polls_read_time_latency",
		Help: "The latency distribution of times a client polls the Fleetspeak server (based on the time spent reading messages).",
	},
		[]string{"http_status_code", "poll_type", "cache_hit"},
	)

	clientPollsWriteTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_client_polls_write_time_latency",
		Help: "The latency distribution of times a client polls the Fleetspeak server (based on the time spent writing messages).",
	},
		[]string{"http_status_code", "poll_type", "cache_hit"},
	)

	clientPollsReadMegabytes = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_client_polls_read_megabytes_size_distribution",
		Help: "The size distribution of times a client polls the Fleetspeak server (based on Megabytes read).",
	},
		[]string{"http_status_code", "poll_type", "cache_hit"},
	)

	clientPollsWriteMegabytes = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_client_polls_write_megabytes_size_distribution",
		Help: "The size distribution of times a client polls the Fleetspeak server (based on Megabytes written).",
	},
		[]string{"http_status_code", "poll_type", "cache_hit"},
	)

	datastoreOperationsCompleted = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_datastore_operations_completed_latency",
		Help: "The latency distribution of datastore operations completed.",
	},
		[]string{"operation", "errored"},
	)

	resourcesUsageDataReceivedCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "fleetspeak_server_resource_usage_data_received_total",
		Help: "The total number of times a client-resource-usage proto is received.",
	},
		[]string{"client_data_labels", "blacklisted", "scope", "version"},
	)

	resourcesUsageDataReceivedByMeanUserCPURate = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_resource_usage_data_received_mean_user_cpu_rate_distribution",
		Help: "The distribution of times a client-resource-usage proto is received (based on mean user CPU rate).",
	},
		[]string{"client_data_labels", "blacklisted", "scope", "version"},
	)

	resourcesUsageDataReceivedByMaxUserCPURate = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_resource_usage_data_received_max_user_cpu_rate_distribution",
		Help: "The distribution of times a client-resource-usage proto is received (based on max user CPU rate).",
	},
		[]string{"client_data_labels", "blacklisted", "scope", "version"},
	)

	resourcesUsageDataReceivedByMeanSystemCPURate = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_resource_usage_data_received_mean_system_cpu_rate_distribution",
		Help: "The distribution of times a client-resource-usage proto is received (based on mean system CPU rate).",
	},
		[]string{"client_data_labels", "blacklisted", "scope", "version"},
	)

	resourcesUsageDataReceivedByMaxSystemCPURate = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_resource_usage_data_received_max_system_cpu_rate",
		Help: "The total number of times a client-resource-usage proto is received (based on max system CPU rate).",
	},
		[]string{"client_data_labels", "blacklisted", "scope", "version"},
	)

	resourcesUsageDataReceivedByMeanResidentMemory = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_resource_usage_data_received_mean_resident_memory_bytes_distribution",
		Help: "The distribution of times a client-resource-usage proto is received (based on mean resident memory).",
	},
		[]string{"client_data_labels", "blacklisted", "scope", "version"},
	)

	resourcesUsageDataReceivedByMaxResidentMemory = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "fleetspeak_server_resource_usage_data_received_max_resident_memory_bytes_distribution",
		Help: "The distribution of times a client-resource-usage proto is received (based on max resident memory).",
	},
		[]string{"client_data_labels", "blacklisted", "scope", "version"},
	)

	killNotificationsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "fleetspeak_server_kill_notifications_received_total",
		Help: "The total number of times a kill notification is received from a client.",
	},
		[]string{"client_data_labels", "blacklisted", "service", "reason"},
	)
)

// A PrometheusStatsCollector is an implementation of a Collector interface.
// It exports stats to a Prometheus HTTP handler, which are exposed at :<configured_port>/metrics
// and are scrapable by Prometheus (The port is configured in the server components config file).
type StatsCollector struct{}

func (s StatsCollector) MessageIngested(backlogged bool, m *fspb.Message) {
	messagesIngested.WithLabelValues(strconv.FormatBool(backlogged), m.Source.ServiceName, m.Destination.ServiceName, m.MessageType).Inc()
	payloadBytes := calculatePayloadBytes(m)
	messagesIngestedSize.WithLabelValues(strconv.FormatBool(backlogged), m.Source.ServiceName, m.Destination.ServiceName, m.MessageType).Add(float64(payloadBytes))
}

func calculatePayloadBytes(m *fspb.Message) int {
	payloadBytes := 0
	if m.Data != nil {
		payloadBytes = len(m.Data.TypeUrl) + len(m.Data.Value)
	}
	return payloadBytes
}

func (s StatsCollector) MessageSaved(forClient bool, m *fspb.Message) {
	messagesSaved.WithLabelValues(m.Destination.ServiceName, m.MessageType, strconv.FormatBool(forClient)).Inc()
	savedPayloadBytes := calculatePayloadBytes(m)
	messagesSavedSize.WithLabelValues(m.Destination.ServiceName, m.MessageType, strconv.FormatBool(forClient)).Add(float64(savedPayloadBytes))
}

func (s StatsCollector) MessageProcessed(start, end time.Time, service string, m *fspb.Message) {
	messagesProcessed.WithLabelValues(m.MessageType, service).Observe(end.Sub(start).Seconds())
}

func (s StatsCollector) MessageErrored(start, end time.Time, service string, isTemp bool, m *fspb.Message) {
	messagesErrored.WithLabelValues(m.MessageType, strconv.FormatBool(isTemp)).Observe(end.Sub(start).Seconds())
}

func (s StatsCollector) MessageDropped(service, messageType string) {
	messagesDropped.WithLabelValues(service, messageType).Inc()
}

func (s StatsCollector) ClientPoll(info stats.PollInfo) {
	httpStatusCode := strconv.Itoa(info.Status)
	pollType := info.Type.String()
	cacheHit := strconv.FormatBool(info.CacheHit)

	// CounterVec
	clientPolls.WithLabelValues(httpStatusCode, pollType, cacheHit).Inc()

	// HistogramVecs
	clientPollsOpTime.WithLabelValues(httpStatusCode, pollType, cacheHit).Observe(info.End.Sub(info.Start).Seconds())
	clientPollsReadTime.WithLabelValues(httpStatusCode, pollType, cacheHit).Observe(info.ReadTime.Seconds())
	clientPollsWriteTime.WithLabelValues(httpStatusCode, pollType, cacheHit).Observe(info.WriteTime.Seconds())
	clientPollsReadMegabytes.WithLabelValues(httpStatusCode, pollType, cacheHit).Observe(convertBytesToMegabytes(info.ReadBytes))
	clientPollsWriteMegabytes.WithLabelValues(httpStatusCode, pollType, cacheHit).Observe(convertBytesToMegabytes(info.WriteBytes))
}

func convertBytesToMegabytes(bytes int) float64 {
	return float64(bytes) / 1000000.0
}

func (s StatsCollector) DatastoreOperation(start, end time.Time, operation string, result error) {
	datastoreOperationsCompleted.WithLabelValues(operation, strconv.FormatBool(result != nil)).Observe(end.Sub(start).Seconds())
}

func getClientDataLabelsConcatenated(cd *db.ClientData) string {
	var clientDataLabels []string
	for _, labelStruct := range cd.Labels {
		clientDataLabels = append(clientDataLabels, labelStruct.GetLabel())
	}
	sort.Strings(clientDataLabels)
	return strings.Join(clientDataLabels[:], ",")
}

func (s StatsCollector) ResourceUsageDataReceived(cd *db.ClientData, rud mpb.ResourceUsageData, v *fspb.ValidationInfo) {
	clientDataLabels := getClientDataLabelsConcatenated(cd)
	blacklisted := strconv.FormatBool(cd.Blacklisted)
	scope := rud.Scope
	version := rud.Version

	// CounterVec
	resourcesUsageDataReceivedCount.WithLabelValues(clientDataLabels, strconv.FormatBool(cd.Blacklisted), scope, rud.Version).Inc()

	// HistorgramVecs
	resourcesUsageDataReceivedByMeanUserCPURate.WithLabelValues(clientDataLabels, blacklisted, scope, version).Observe(rud.ResourceUsage.GetMeanUserCpuRate())
	resourcesUsageDataReceivedByMaxUserCPURate.WithLabelValues(clientDataLabels, blacklisted, scope, version).Observe(rud.ResourceUsage.GetMaxUserCpuRate())
	resourcesUsageDataReceivedByMeanSystemCPURate.WithLabelValues(clientDataLabels, blacklisted, scope, version).Observe(rud.ResourceUsage.GetMeanSystemCpuRate())
	resourcesUsageDataReceivedByMaxSystemCPURate.WithLabelValues(clientDataLabels, blacklisted, scope, version).Observe(rud.ResourceUsage.GetMaxSystemCpuRate())
	resourcesUsageDataReceivedByMeanResidentMemory.WithLabelValues(clientDataLabels, blacklisted, scope, version).Observe(rud.ResourceUsage.GetMeanResidentMemory())
	resourcesUsageDataReceivedByMaxResidentMemory.WithLabelValues(clientDataLabels, blacklisted, scope, version).Observe(float64(rud.ResourceUsage.GetMaxResidentMemory()))
}

func (s StatsCollector) KillNotificationReceived(cd *db.ClientData, kn mpb.KillNotification) {
	clientDataLabels := getClientDataLabelsConcatenated(cd)
	killNotificationsReceived.WithLabelValues(clientDataLabels, strconv.FormatBool(cd.Blacklisted), kn.Service, kn.Reason.String()).Inc()
}
