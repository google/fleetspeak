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

package services

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/cache"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	monitoringpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
	anypb "google.golang.org/protobuf/types/known/anypb"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
)

type countingResourceUsageStatsCollector struct {
	testStatsCollector
	resourceUsageCalls atomic.Int32
}

func (s *countingResourceUsageStatsCollector) ResourceUsageDataReceived(cd *db.ClientData, rud *monitoringpb.ResourceUsageData, v *fspb.ValidationInfo) {
	s.resourceUsageCalls.Add(1)
}

func TestProcessResourceUsage_MissingResourceUsage(t *testing.T) {
	ctx := t.Context()
	ds := newTestDatastore(t, "ResourceUsageMissing")
	statsCollector := &countingResourceUsageStatsCollector{}
	mgr := NewManager(ds, nil, statsCollector, cache.NewClients())
	defer mgr.Stop()

	cid, err := common.StringToClientID("0102030405060708")
	if err != nil {
		t.Fatalf("StringToClientID() failed: %v", err)
	}
	if err := ds.AddClient(ctx, cid, &db.ClientData{Key: []byte("test-key")}); err != nil {
		t.Fatalf("AddClient(%v) failed: %v", cid, err)
	}

	payload, err := anypb.New(&monitoringpb.ResourceUsageData{
		Scope:            "system",
		ProcessStartTime: &tspb.Timestamp{Seconds: 1700000000},
		DataTimestamp:    &tspb.Timestamp{Seconds: 1700000001},
	})
	if err != nil {
		t.Fatalf("anypb.New() failed: %v", err)
	}

	msg, mid := newTestMessage(t, "system", "ResourceUsage", "ru-missing-1")
	msg.Data = payload

	if err := mgr.services["system"].service.ProcessMessage(ctx, msg); err == nil {
		t.Fatal("systemService.ProcessMessage(missing resource_usage) = nil, want non-nil error")
	}
	if got := statsCollector.resourceUsageCalls.Load(); got != 0 {
		t.Errorf("statsCollector.resourceUsageCalls = %d, want 0", got)
	}

	if err := mgr.HandleNewMessages(ctx, []*fspb.Message{msg}, ""); err != nil {
		t.Fatalf("HandleNewMessages() failed: %v", err)
	}
	res, err := ds.GetMessageResult(ctx, mid)
	if err != nil {
		t.Fatalf("GetMessageResult(%v) failed: %v", mid, err)
	}
	if !res.GetFailed() {
		t.Fatalf("GetMessageResult(%v) = %v, want Failed = true", mid, res)
	}
	if strings.Contains(res.GetFailedReason(), "panic") {
		t.Errorf("GetMessageResult(%v).FailedReason = %q, want validation error without panic", mid, res.GetFailedReason())
	}
}

func TestProcessResourceUsage_ValidResourceUsage(t *testing.T) {
	ctx := t.Context()
	ds := newTestDatastore(t, "ResourceUsageValid")
	statsCollector := &countingResourceUsageStatsCollector{}
	mgr := NewManager(ds, nil, statsCollector, cache.NewClients())
	defer mgr.Stop()

	cid, err := common.StringToClientID("0102030405060708")
	if err != nil {
		t.Fatalf("StringToClientID() failed: %v", err)
	}
	if err := ds.AddClient(ctx, cid, &db.ClientData{Key: []byte("test-key")}); err != nil {
		t.Fatalf("AddClient(%v) failed: %v", cid, err)
	}

	before := tspb.New(db.Now().Add(-time.Minute))
	payload, err := anypb.New(&monitoringpb.ResourceUsageData{
		Scope:            "system",
		Pid:              42,
		ProcessStartTime: &tspb.Timestamp{Seconds: 1700000000},
		DataTimestamp:    &tspb.Timestamp{Seconds: 1700000001},
		ResourceUsage: &monitoringpb.AggregatedResourceUsage{
			MeanUserCpuRate:   12.5,
			MaxResidentMemory: 1024 * 1024,
		},
	})
	if err != nil {
		t.Fatalf("anypb.New() failed: %v", err)
	}

	msg, mid := newTestMessage(t, "system", "ResourceUsage", "ru-valid-1")
	msg.Data = payload

	if err := mgr.HandleNewMessages(ctx, []*fspb.Message{msg}, ""); err != nil {
		t.Fatalf("HandleNewMessages() failed: %v", err)
	}
	res, err := ds.GetMessageResult(ctx, mid)
	if err != nil {
		t.Fatalf("GetMessageResult(%v) failed: %v", mid, err)
	}
	if res == nil || res.GetFailed() {
		t.Fatalf("GetMessageResult(%v) = %v, want non-nil successful result", mid, res)
	}
	if got := statsCollector.resourceUsageCalls.Load(); got != 1 {
		t.Errorf("statsCollector.resourceUsageCalls = %d, want 1", got)
	}

	after := tspb.New(db.Now().Add(time.Minute))
	records, err := ds.FetchResourceUsageRecords(ctx, cid, before, after)
	if err != nil {
		t.Fatalf("FetchResourceUsageRecords(%v) failed: %v", cid, err)
	}
	if len(records) != 1 {
		t.Fatalf("len(FetchResourceUsageRecords(%v)) = %d, want 1", cid, len(records))
	}
}
