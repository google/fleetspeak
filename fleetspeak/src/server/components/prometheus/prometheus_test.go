// Copyright 2026 Google Inc.
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
	"testing"

	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/prometheus/client_golang/prometheus"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	monitoringpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

func hasCounterScope(t *testing.T, metricName, scope string) bool {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("DefaultGatherer.Gather() failed: %v", err)
	}
	for _, mf := range families {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "scope" && lp.GetValue() == scope {
					return m.GetCounter().GetValue() > 0
				}
			}
		}
	}
	return false
}

func TestResourceUsageDataReceived_MissingResourceUsage(t *testing.T) {
	sc := StatsCollector{}
	cd := &db.ClientData{
		Labels: []*fspb.Label{{ServiceName: "client", Label: "linux"}},
	}

	sc.ResourceUsageDataReceived(cd, nil, nil)
	sc.ResourceUsageDataReceived(cd, &monitoringpb.ResourceUsageData{
		Scope:   "test-missing-usage",
		Version: "1.0",
	}, nil)

	if hasCounterScope(t, "fleetspeak_server_resource_usage_data_received_total", "test-missing-usage") {
		t.Error("ResourceUsageDataReceived(nil ResourceUsage) recorded counter sample for scope \"test-missing-usage\", want none")
	}

	sc.ResourceUsageDataReceived(cd, &monitoringpb.ResourceUsageData{
		Scope:   "test-valid-usage",
		Version: "1.0",
		ResourceUsage: &monitoringpb.AggregatedResourceUsage{
			MeanUserCpuRate: 10.0,
		},
	}, nil)

	if !hasCounterScope(t, "fleetspeak_server_resource_usage_data_received_total", "test-valid-usage") {
		t.Error("ResourceUsageDataReceived(valid ResourceUsage) recorded no counter sample for scope \"test-valid-usage\", want > 0")
	}
}
