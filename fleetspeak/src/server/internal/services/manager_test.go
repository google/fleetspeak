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
	"path"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/cache"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"
	"github.com/google/fleetspeak/fleetspeak/src/server/sqlite"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

type testStatsCollector struct {
	panicOnIngest bool
}

func (s *testStatsCollector) MessageIngested(backlogged bool, m *fspb.Message, cd *db.ClientData) {
	if s.panicOnIngest {
		panic("stats collector ingest panic")
	}
}

func (s *testStatsCollector) MessageSent(m *fspb.Message) {}

func (s *testStatsCollector) MessageSaved(forClient bool, m *fspb.Message, cd *db.ClientData) {}

func (s *testStatsCollector) MessageProcessed(start, end time.Time, m *fspb.Message, isFirstTry bool, cd *db.ClientData) {
}

func (s *testStatsCollector) MessageErrored(start, end time.Time, isTemp bool, m *fspb.Message, isFirstTry bool, cd *db.ClientData) {
}

func (s *testStatsCollector) MessageDropped(m *fspb.Message, isFirstTry bool, cd *db.ClientData) {}

func (s *testStatsCollector) ClientPoll(info stats.PollInfo) {}

func (s *testStatsCollector) DatastoreOperation(start, end time.Time, operation string, result error) {
}

func (s *testStatsCollector) ResourceUsageDataReceived(cd *db.ClientData, rud *mpb.ResourceUsageData, v *fspb.ValidationInfo) {
}

func (s *testStatsCollector) KillNotificationReceived(cd *db.ClientData, kn *mpb.KillNotification) {}

type panickingService struct {
	panicOnType string
	processed   atomic.Int32
}

func (s *panickingService) Start(sctx service.Context) error { return nil }

func (s *panickingService) Stop() error { return nil }

func (s *panickingService) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	if m.MessageType == s.panicOnType {
		panic("boom in ProcessMessage")
	}
	s.processed.Add(1)
	return nil
}

type panickingBatchedService struct {
	panickingService
}

func (s *panickingBatchedService) ProcessMessageBatch(ctx context.Context, msgs []*fspb.Message) error {
	panic("boom in ProcessMessageBatch")
}

func newTestDatastore(t *testing.T, name string) *sqlite.Datastore {
	t.Helper()
	tmpDir, cleanup := comtesting.GetTempDir("services")
	t.Cleanup(cleanup)
	ds, err := sqlite.MakeDatastore(path.Join(tmpDir, name+".sqlite"))
	if err != nil {
		t.Fatalf("MakeDatastore(%q) failed: %v", name, err)
	}
	t.Cleanup(func() {
		ds.Close()
	})
	return ds
}

func newTestMessage(t *testing.T, svcName, msgType, nonce string) (*fspb.Message, common.MessageID) {
	t.Helper()
	cid, err := common.StringToClientID("0102030405060708")
	if err != nil {
		t.Fatalf("StringToClientID failed: %v", err)
	}
	src := &fspb.Address{
		ClientId:    cid.Bytes(),
		ServiceName: svcName,
	}
	rawID := []byte(nonce)
	mid := common.MakeMessageID(src, rawID)
	return &fspb.Message{
		MessageId:       mid.Bytes(),
		Source:          src,
		Destination:     &fspb.Address{ServiceName: svcName},
		SourceMessageId: rawID,
		MessageType:     msgType,
		CreationTime:    db.NowProto(),
	}, mid
}

func TestHandleNewMessages_ServicePanicRecovered(t *testing.T) {
	ctx := context.Background()
	ds := newTestDatastore(t, "HandleNewMessagesPanic")
	svc := &panickingService{panicOnType: "PanicMsg"}
	mgr := NewManager(ds, map[string]service.Factory{
		"TestFactory": func(*spb.ServiceConfig) (service.Service, error) { return svc, nil },
	}, &testStatsCollector{}, cache.NewClients())
	defer mgr.Stop()

	if err := mgr.Install(&spb.ServiceConfig{
		Name:           "TestService",
		Factory:        "TestFactory",
		MaxParallelism: 1,
	}); err != nil {
		t.Fatalf("mgr.Install failed: %v", err)
	}

	panicMsg, panicID := newTestMessage(t, "TestService", "PanicMsg", "msg-1")
	if err := mgr.HandleNewMessages(ctx, []*fspb.Message{panicMsg}, ""); err != nil {
		t.Fatalf("HandleNewMessages(panicMsg) unexpected error: %v", err)
	}

	res, err := ds.GetMessageResult(ctx, panicID)
	if err != nil {
		t.Fatalf("GetMessageResult(%v) failed: %v", panicID, err)
	}
	if res == nil || !res.Failed {
		t.Fatalf("GetMessageResult(%v) = %v, want Failed = true", panicID, res)
	}
	if !strings.Contains(res.FailedReason, "panic in ProcessMessage") {
		t.Errorf("MessageResult.FailedReason = %q, want substring %q", res.FailedReason, "panic in ProcessMessage")
	}

	// Verify parallelism counter was decremented (MaxParallelism is 1) so
	// subsequent messages are processed rather than dropped.
	okMsg, okID := newTestMessage(t, "TestService", "NormalMsg", "msg-2")
	if err := mgr.HandleNewMessages(ctx, []*fspb.Message{okMsg}, ""); err != nil {
		t.Fatalf("HandleNewMessages(okMsg) unexpected error: %v", err)
	}
	if got := svc.processed.Load(); got != 1 {
		t.Errorf("svc.processed = %d, want 1", got)
	}
	okRes, err := ds.GetMessageResult(ctx, okID)
	if err != nil {
		t.Fatalf("GetMessageResult(%v) failed: %v", okID, err)
	}
	if okRes == nil || okRes.Failed {
		t.Errorf("GetMessageResult(%v) = %v, want non-nil successful result", okID, okRes)
	}
}

func TestProcessMessages_ServicePanicRecovered(t *testing.T) {
	ctx := context.Background()
	ds := newTestDatastore(t, "ProcessMessagesPanic")
	svc := &panickingService{panicOnType: "PanicMsg"}
	mgr := NewManager(ds, map[string]service.Factory{
		"TestFactory": func(*spb.ServiceConfig) (service.Service, error) { return svc, nil },
	}, &testStatsCollector{}, cache.NewClients())
	defer mgr.Stop()

	if err := mgr.Install(&spb.ServiceConfig{
		Name:           "TestService",
		Factory:        "TestFactory",
		MaxParallelism: 1,
	}); err != nil {
		t.Fatalf("mgr.Install failed: %v", err)
	}

	panicMsg, panicID := newTestMessage(t, "TestService", "PanicMsg", "backlog-1")
	if err := ds.StoreMessages(ctx, []*fspb.Message{panicMsg}, ""); err != nil {
		t.Fatalf("StoreMessages failed: %v", err)
	}

	mgr.ProcessMessages([]*fspb.Message{panicMsg})

	res, err := ds.GetMessageResult(ctx, panicID)
	if err != nil {
		t.Fatalf("GetMessageResult(%v) failed: %v", panicID, err)
	}
	if res == nil || !res.Failed {
		t.Fatalf("GetMessageResult(%v) = %v, want Failed = true", panicID, res)
	}
}

func TestGoroutineOuterPanicRecovered(t *testing.T) {
	ctx := context.Background()
	ds := newTestDatastore(t, "OuterGoroutinePanic")
	svc := &panickingService{}
	mgr := NewManager(ds, map[string]service.Factory{
		"TestFactory": func(*spb.ServiceConfig) (service.Service, error) { return svc, nil },
	}, &testStatsCollector{panicOnIngest: true}, cache.NewClients())
	defer mgr.Stop()

	if err := mgr.Install(&spb.ServiceConfig{
		Name:    "TestService",
		Factory: "TestFactory",
	}); err != nil {
		t.Fatalf("mgr.Install failed: %v", err)
	}

	msg1, _ := newTestMessage(t, "TestService", "NormalMsg", "outer-1")
	if err := mgr.HandleNewMessages(ctx, []*fspb.Message{msg1}, ""); err != nil {
		t.Fatalf("HandleNewMessages unexpected error: %v", err)
	}

	msg2, _ := newTestMessage(t, "TestService", "NormalMsg", "outer-2")
	mgr.ProcessMessages([]*fspb.Message{msg2})
}

func TestProcessMessageBatch_PanicAndNonBatchedHandled(t *testing.T) {
	ctx := context.Background()
	ds := newTestDatastore(t, "ProcessMessageBatchPanic")
	mgr := NewManager(ds, map[string]service.Factory{
		"SingleFactory":  func(*spb.ServiceConfig) (service.Service, error) { return &panickingService{}, nil },
		"BatchedFactory": func(*spb.ServiceConfig) (service.Service, error) { return &panickingBatchedService{}, nil },
	}, &testStatsCollector{}, cache.NewClients())
	defer mgr.Stop()

	if err := mgr.Install(&spb.ServiceConfig{Name: "SingleSvc", Factory: "SingleFactory"}); err != nil {
		t.Fatalf("mgr.Install(SingleSvc) failed: %v", err)
	}
	if err := mgr.Install(&spb.ServiceConfig{Name: "BatchedSvc", Factory: "BatchedFactory"}); err != nil {
		t.Fatalf("mgr.Install(BatchedSvc) failed: %v", err)
	}

	msg, _ := newTestMessage(t, "SingleSvc", "NormalMsg", "batch-1")
	// Calling ProcessMessageBatch on a non-BatchedService must return cleanly without a nil-interface panic.
	mgr.ProcessMessageBatch(ctx, "SingleSvc", []*fspb.Message{msg})
	// Calling ProcessMessageBatch on a panicking BatchedService must recover without crashing.
	mgr.ProcessMessageBatch(ctx, "BatchedSvc", []*fspb.Message{msg})
}
