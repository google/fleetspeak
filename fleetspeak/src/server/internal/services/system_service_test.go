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

type fakeSystemServiceContext struct {
	service.Context
	getClientDataErr error
}

func (f *fakeSystemServiceContext) GetClientData(_ context.Context, _ common.ClientID) (*db.ClientData, error) {
	if f.getClientDataErr != nil {
		return nil, f.getClientDataErr
	}
	return &db.ClientData{}, nil
}

type fakeResourceUsageStore struct {
	db.Store
	notFoundErr error
	recordErr   error
}

func (f *fakeResourceUsageStore) IsNotFound(err error) bool {
	return f.notFoundErr != nil && errors.Is(err, f.notFoundErr)
}

func (f *fakeResourceUsageStore) RecordResourceUsageData(_ context.Context, _ common.ClientID, _ *monitoringpb.ResourceUsageData) error {
	return f.recordErr
}

func TestProcessResourceUsageErrorClassification(t *testing.T) {
	errNotFound := errors.New("not found")
	errTransient := errors.New("spanner unavailable")
	cid, err := common.StringToClientID("0102030405060708")
	if err != nil {
		t.Fatalf("StringToClientID() failed: %v", err)
	}

	validRU := &monitoringpb.AggregatedResourceUsage{MeanUserCpuRate: 1.0}
	badTS := &tspb.Timestamp{Nanos: -1}

	testCases := []struct {
		name             string
		rud              *monitoringpb.ResourceUsageData
		getClientDataErr error
		recordErr        error
		wantErr          bool
		wantTemporary    bool
	}{
		{
			name: "unset timestamps succeed when datastore allows them",
			rud: &monitoringpb.ResourceUsageData{
				Scope:         "system",
				ResourceUsage: validRU,
			},
			wantErr: false,
		},
		{
			name: "invalid process_start_time fails permanently",
			rud: &monitoringpb.ResourceUsageData{
				Scope:            "system",
				ProcessStartTime: badTS,
				ResourceUsage:    validRU,
			},
			wantErr:       true,
			wantTemporary: false,
		},
		{
			name: "invalid data_timestamp fails permanently",
			rud: &monitoringpb.ResourceUsageData{
				Scope:         "system",
				DataTimestamp: badTS,
				ResourceUsage: validRU,
			},
			wantErr:       true,
			wantTemporary: false,
		},
		{
			name:             "GetClientData not found fails permanently",
			rud:              &monitoringpb.ResourceUsageData{Scope: "system", ResourceUsage: validRU},
			getClientDataErr: errNotFound,
			wantErr:          true,
			wantTemporary:    false,
		},
		{
			name:             "GetClientData transient error returns TemporaryError",
			rud:              &monitoringpb.ResourceUsageData{Scope: "system", ResourceUsage: validRU},
			getClientDataErr: errTransient,
			wantErr:          true,
			wantTemporary:    true,
		},
		{
			name:          "RecordResourceUsageData not found fails permanently",
			rud:           &monitoringpb.ResourceUsageData{Scope: "system", ResourceUsage: validRU},
			recordErr:     errNotFound,
			wantErr:       true,
			wantTemporary: false,
		},
		{
			name:          "RecordResourceUsageData transient error returns TemporaryError",
			rud:           &monitoringpb.ResourceUsageData{Scope: "system", ResourceUsage: validRU},
			recordErr:     errTransient,
			wantErr:       true,
			wantTemporary: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := anypb.New(tc.rud)
			if err != nil {
				t.Fatalf("anypb.New() failed: %v", err)
			}
			s := &systemService{
				sctx:      &fakeSystemServiceContext{getClientDataErr: tc.getClientDataErr},
				stats:     &countingResourceUsageStatsCollector{},
				datastore: &fakeResourceUsageStore{notFoundErr: errNotFound, recordErr: tc.recordErr},
			}
			err = s.processResourceUsage(t.Context(), cid, payload, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("processResourceUsage() err = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantErr && service.IsTemporary(err) != tc.wantTemporary {
				t.Errorf("service.IsTemporary(%v) = %v, want %v", err, service.IsTemporary(err), tc.wantTemporary)
			}
		})
	}
}

func TestProcessKillNotificationErrorClassification(t *testing.T) {
	errNotFound := errors.New("not found")
	errTransient := errors.New("spanner unavailable")
	cid, err := common.StringToClientID("0102030405060708")
	if err != nil {
		t.Fatalf("StringToClientID() failed: %v", err)
	}
	payload, err := anypb.New(&monitoringpb.KillNotification{Service: "test"})
	if err != nil {
		t.Fatalf("anypb.New() failed: %v", err)
	}

	testCases := []struct {
		name             string
		getClientDataErr error
		wantTemporary    bool
	}{
		{
			name:             "GetClientData not found fails permanently",
			getClientDataErr: errNotFound,
			wantTemporary:    false,
		},
		{
			name:             "GetClientData transient error returns TemporaryError",
			getClientDataErr: errTransient,
			wantTemporary:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := &systemService{
				sctx:      &fakeSystemServiceContext{getClientDataErr: tc.getClientDataErr},
				stats:     &countingResourceUsageStatsCollector{},
				datastore: &fakeResourceUsageStore{notFoundErr: errNotFound},
			}
			err := s.processKillNotification(t.Context(), cid, payload)
			if err == nil {
				t.Fatal("processKillNotification() = nil, want error")
			}
			if service.IsTemporary(err) != tc.wantTemporary {
				t.Errorf("service.IsTemporary(%v) = %v, want %v", err, service.IsTemporary(err), tc.wantTemporary)
			}
		})
	}
}
