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
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/cache"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"

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

type fakeClientInfoStore struct {
	db.Store
	cd        *db.ClientData
	removeErr error
	addErr    error
}

func (f *fakeClientInfoStore) GetClientData(_ context.Context, _ common.ClientID) (*db.ClientData, error) {
	return &db.ClientData{
		Key:    f.cd.Key,
		Labels: append([]*fspb.Label(nil), f.cd.Labels...),
	}, nil
}

func (f *fakeClientInfoStore) RemoveClientLabel(_ context.Context, _ common.ClientID, _ *fspb.Label) error {
	return f.removeErr
}

func (f *fakeClientInfoStore) AddClientLabel(_ context.Context, _ common.ClientID, _ *fspb.Label) error {
	return f.addErr
}

func TestProcessClientInfo_EvictsCacheOnWriteError(t *testing.T) {
	cid, err := common.StringToClientID("0102030405060708")
	if err != nil {
		t.Fatalf("StringToClientID() failed: %v", err)
	}
	errWrite := errors.New("datastore write failed")

	testCases := []struct {
		name      string
		existing  []*fspb.Label
		incoming  []*fspb.Label
		removeErr error
		addErr    error
	}{
		{
			name:      "RemoveClientLabel failure evicts cached client",
			existing:  []*fspb.Label{{ServiceName: "client", Label: "old"}},
			incoming:  nil,
			removeErr: errWrite,
		},
		{
			name:     "AddClientLabel failure evicts cached client",
			existing: []*fspb.Label{{ServiceName: "client", Label: "keep"}},
			incoming: []*fspb.Label{
				{ServiceName: "client", Label: "keep"},
				{ServiceName: "client", Label: "new"},
			},
			addErr: errWrite,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cc := cache.NewClients()
			cd := &db.ClientData{Key: []byte("test-key"), Labels: tc.existing}
			cc.Update(cid, cd)
			if cc.Get(cid) == nil {
				t.Fatal("cc.Get(cid) = nil after seeding cache, want non-nil")
			}

			payload, err := anypb.New(&fspb.ClientInfoData{Labels: tc.incoming})
			if err != nil {
				t.Fatalf("anypb.New() failed: %v", err)
			}

			s := &systemService{
				datastore: &fakeClientInfoStore{
					cd:        cd,
					removeErr: tc.removeErr,
					addErr:    tc.addErr,
				},
				cc: cc,
			}
			err = s.processClientInfo(t.Context(), cid, payload)
			if err == nil {
				t.Fatal("processClientInfo() = nil, want error")
			}
			if !service.IsTemporary(err) {
				t.Errorf("service.IsTemporary(%v) = false, want true", err)
			}
			if got := cc.Get(cid); got != nil {
				t.Errorf("cc.Get(cid) after write error = %v, want nil (evicted)", got)
			}
		})
	}
}
