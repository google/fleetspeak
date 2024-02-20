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

package daemonservice

import (
	"bytes"
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client/clitesting"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
	"github.com/google/fleetspeak/fleetspeak/src/common/anypbtest"

	dspb "github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/proto/fleetspeak_daemonservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
	anypb "google.golang.org/protobuf/types/known/anypb"
	durpb "google.golang.org/protobuf/types/known/durationpb"
)

type testStatsCollector struct {
	stats.NoopCollector
	subprocessesFinished atomic.Int64
}

// DaemonServiceSubprocessFinished implements stats.DaemonServiceCollector.
func (sc *testStatsCollector) DaemonServiceSubprocessFinished(service string, err error) {
	sc.subprocessesFinished.Add(1)
}

func startTestClient(t *testing.T, client []string, mode string, sc service.Context, dsc *dspb.Config) *Service {
	t.Helper()
	dsc = proto.Clone(dsc).(*dspb.Config)
	dsc.ResourceMonitoringSampleSize = 2
	dsc.ResourceMonitoringSamplePeriod = durpb.New(30 * time.Millisecond)
	dsc.Argv = append(dsc.Argv, client...)
	if mode != "" {
		dsc.Argv = append(dsc.Argv, "--mode="+mode)
	}

	if d := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); d != "" {
		dsc.Argv = append(dsc.Argv, "--log_dir="+d)
	}

	s, err := Factory(&fspb.ClientServiceConfig{
		Name:   "TestDaemonService",
		Config: anypbtest.New(t, dsc),
	})
	if err != nil {
		t.Fatalf("Factory(...): %v", err)
	}

	if err := s.Start(sc); err != nil {
		t.Fatalf("daemonservice.Service.Start(...): %v", err)
	}
	return s.(*Service)
}

func exerciseLoopback(t *testing.T, client []string) {
	t.Logf("Starting loopback exercise for client %v", client)
	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message),
	}
	s := startTestClient(t, client, "loopback", &sc, &dspb.Config{})
	defer func() {
		go func() {
			for range sc.OutChan {
			}
		}()
		if err := s.Stop(); err != nil {
			t.Errorf("Unable to stop service: %v", err)
		}
	}()

	msgs := []*fspb.Message{
		{
			MessageId:   []byte("\000\000\000"),
			MessageType: "RequestTypeA",
		},
		{
			MessageId:   []byte("\000\000\001"),
			MessageType: "RequestTypeB",
		},
	}
	go func() {
		ctx := context.Background()
		for _, m := range msgs {
			err := s.ProcessMessage(ctx, m)
			if err != nil {
				t.Errorf("Error processing message: %v", err)
			}
		}
	}()
	for _, m := range msgs {
		got := <-sc.OutChan
		want := proto.Clone(m).(*fspb.Message)
		want.MessageType = want.MessageType + "Response"
		if !proto.Equal(got, want) {
			t.Errorf("Unexpected message from loopback: got [%v], want [%v]", got, want)
		}
	}
}

func TestLoopback(t *testing.T) {
	exerciseLoopback(t, testClient(t))
}

func TestLoopbackPY(t *testing.T) {
	exerciseLoopback(t, testClientPY(t))
}

// Tests that Fleetspeak uses self-reported PIDs for monitoring resource-usage of
// daemon services.
func TestSelfReportedPIDs(t *testing.T) {
	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message),
	}
	s := startTestClient(t, testClientLauncherPY(t), "", &sc, &dspb.Config{})

	var ruMsgs []*mpb.ResourceUsageData
	var lastRUMsg *mpb.ResourceUsageData

	defer func() {
		if len(ruMsgs) == 0 {
			t.Fatal("No resource-usage reports received for testclient process.")
		}
		if lastRUMsg == nil {
			t.Fatal("No final resource-usage report received for testclient_launcher process.")
		}
		for _, ruMsg := range ruMsgs {
			if ruMsg.Pid != ruMsgs[0].Pid {
				t.Fatalf("PID for testclient process changed (respawn?). Got %d. Want %d.", ruMsg.Pid, ruMsgs[0].Pid)
			}
			if ruMsg.Version != "0.5" {
				t.Errorf("Version reported by testclient process was \"%s\", but expected \"0.5\"", ruMsg.Version)
			}
		}
		if lastRUMsg.Pid == ruMsgs[0].Pid {
			t.Fatalf("PID for testclient_launcher process is the same as that of the testclient process (%d).", lastRUMsg.Pid)
		}
	}()

	serviceStopped := false
	stopService := func() {
		if !serviceStopped {
			go func() {
				if err := s.Stop(); err != nil {
					t.Errorf("Unable to stop daemon service: %v", err)
				}
			}()
			serviceStopped = true
		}
	}

	// Collect all resource-usage reports until the daemon service terminates, or
	// until timeout.
	timeout := 10 * time.Second
	timeoutWhen := time.After(timeout)
	for {
		select {
		case <-timeoutWhen:
			t.Errorf("%v timeout exceeded.", timeout)
			stopService()
			return
		case m := <-sc.OutChan:
			if m.MessageType != "ResourceUsage" {
				continue
			}
			rud := &mpb.ResourceUsageData{}
			if err := m.Data.UnmarshalTo(rud); err != nil {
				t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
			}
			t.Logf("Received resource-usage message (PID %d).", rud.Pid)
			if rud.ProcessTerminated {
				lastRUMsg = rud
				return
			}
			ruMsgs = append(ruMsgs, rud)
			// Only one resource-usage report for the testclient process is needed.
			stopService()
		}
	}
}

// mustPatchGlobalRespawnDelay patches the package-level RespawnDelay variable.
//
// Tests which use this should not run in parallel, because the global state
// change could interfere with concurrent test executions.  We should try to
// pass the respawn delay through a regular API boundary instead.
func mustPatchGlobalRespawnDelay(t *testing.T, d time.Duration) {
	t.Helper()
	orig := RespawnDelay
	RespawnDelay = d // mutating a global variable
	t.Cleanup(func() {
		RespawnDelay = orig
	})
}

func TestRespawn(t *testing.T) {
	// May not run in parallel due to global variable mutation.
	mustPatchGlobalRespawnDelay(t, time.Second)

	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message, 100),
	}
	s := startTestClient(t, testClient(t), "die", &sc, &dspb.Config{})
	defer func() {
		if err := s.Stop(); err != nil {
			t.Errorf("Unable to stop service: %v", err)
		}
	}()

	start := time.Now()

	// Every execution should produce at least one resource usage.  With our
	// modified RespawnDelay, we should see a respawn approximately once per
	// second.
	//
	// test that it takes more than 2 seconds to see 3 different pids, but
	// less than 10 seconds.
	seen := make(map[int64]bool)
	late := time.After(10 * time.Second)

L:
	for len(seen) < 3 {
		select {
		case <-late:
			t.Errorf("Expected 3 pids in 10 seconds, only saw: %d", len(seen))
			break L
		case m := <-sc.OutChan:
			if m.MessageType != "ResourceUsage" {
				t.Errorf("Received unexpected message type: %s", m.MessageType)
				continue
			}
			rud := &mpb.ResourceUsageData{}
			if err := m.Data.UnmarshalTo(rud); err != nil {
				t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
			}
			seen[rud.Pid] = true
		}
	}
	delta := time.Since(start)
	if delta < 2*time.Second {
		t.Errorf("Expected to need at least 2 seconds to see 3 pids, but only needed %v", delta)
	}
}

func TestInactivityTimeout(t *testing.T) {
	// May not run in parallel due to global variable mutation.
	mustPatchGlobalRespawnDelay(t, time.Second)

	dsc := &dspb.Config{
		InactivityTimeout: durpb.New(time.Second),
	}
	dsc.Argv = append(dsc.Argv, testClient(t)...)
	dsc.Argv = append(dsc.Argv, "--mode=loopback")

	s, err := Factory(&fspb.ClientServiceConfig{
		Name:   "TestDaemonService",
		Config: anypbtest.New(t, dsc),
	})
	if err != nil {
		t.Fatalf("Factory(...): %v", err)
	}
	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message, 100),
	}
	if err := s.Start(&sc); err != nil {
		t.Fatalf("DaemonService.Start(...): %v", err)
	}

	ctx, stopInput := context.WithCancel(context.Background())
	var inputStopped sync.WaitGroup
	inputStopped.Add(1)
	go func() {
		defer inputStopped.Done()
		// Send messages through the loopback, waiting more than the
		// InactivityTimeout between them. In principle we should see a
		// new pid every round, but this isn't guaranteed as the kill
		// might take longer.
		//
		// So we keep trying until ctx is stopped.
		for {
			if err := s.ProcessMessage(ctx, &fspb.Message{
				MessageType: "DummyMessage",
			}); err != nil {
				if err != ctx.Err() {
					t.Errorf("Error processing message: %v", err)
				}
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(1500 * time.Millisecond):
			}
		}
	}()

	seen := make(map[int64]bool)
	late := time.After(10 * time.Second)
waitForThree:
	for len(seen) < 3 {
		select {
		case <-late:
			t.Errorf("Expected at least 3 pids in 10 seconds, only saw: %d", len(seen))
			break waitForThree
		case m := <-sc.OutChan:
			if m.MessageType == "DummyMessageResponse" {
				continue
			}
			if m.MessageType == "StdOutput" {
				continue
			}
			if m.MessageType != "ResourceUsage" {
				t.Errorf("Received unexpected message type: %s", m.MessageType)
				continue
			}
			rud := &mpb.ResourceUsageData{}
			if err := m.Data.UnmarshalTo(rud); err != nil {
				t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
			}
			seen[rud.Pid] = true
		}
	}

	stopInput()
	inputStopped.Wait()

	if err := s.Stop(); err != nil {
		t.Errorf("Unexpected error from DaemonService.Stop(): %v", err)
	}
}

func exerciseBacklog(t *testing.T, client []string) {
	t.Logf("Starting backlog exercise for client %v", client)
	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message),
	}
	s := startTestClient(t, client, "loopback", &sc, &dspb.Config{})
	defer func() {
		go func() {
			for range sc.OutChan {
			}
		}()
		if err := s.Stop(); err != nil {
			t.Errorf("Unable to stop service: %v", err)
		}
	}()

	// We send large messages through loopback, without draining our end.
	// Eventually, the backlog should cause us to block until ProcessMessage times
	// out.
	msgCnt := 0
	msg := &fspb.Message{
		MessageId:   []byte("\000\000\000"),
		MessageType: "RequestTypeA",
		Data:        &anypb.Any{Value: bytes.Repeat([]byte{42}, 16*1024)},
	}
	var err error
	for {
		timeout := 100 * time.Millisecond
		ctx, cancel := context.WithTimeout(context.Background(), timeout)

		start := time.Now()
		err = s.ProcessMessage(ctx, msg) // err in outer block, checked at loop end
		if err == nil {
			msgCnt++
		}
		cancel()
		rt := time.Since(start)
		// verify that ProcessMessage respects ctx.
		if rt > 2*timeout {
			t.Errorf("ProcessMessage with %v timeout took %v", timeout, rt)
			break
		}
		if err != nil {
			break
		}
	}
	if err != context.DeadlineExceeded {
		t.Errorf("Expected DeadlineExceeded error from last ProcessMessage, got: %v", err)
	}
	t.Logf("buffers filled after %d messages", msgCnt)
	for range msgCnt {
		<-sc.OutChan
	}
}

func TestBacklog(t *testing.T) {
	exerciseBacklog(t, testClient(t))
}

func TestBacklogPY(t *testing.T) {
	exerciseBacklog(t, testClientPY(t))
}

func nextResourceUsage(t *testing.T, ch <-chan *fspb.Message) (rud *mpb.ResourceUsageData) {
	t.Helper()
	var m *fspb.Message
	for m == nil || m.MessageType != "ResourceUsage" {
		m = <-ch
	}

	rud = &mpb.ResourceUsageData{}
	if err := m.Data.UnmarshalTo(rud); err != nil {
		t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
	}
	return rud
}

// Tests that Fleetspeak restarts daemonservices that exceed their memory limits.
func TestMemoryLimit(t *testing.T) {
	mustPatchGlobalRespawnDelay(t, time.Second)

	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message),
	}
	s := startTestClient(t, testClientPY(t), "memoryhog", &sc, &dspb.Config{
		MemoryLimit: 1024 * 1024 * 10, // 10MB
	})
	defer func() {
		if err := s.Stop(); err != nil {
			t.Errorf("Unable to stop service: %v", err)
		}
	}()

	rud := nextResourceUsage(t, sc.OutChan)
	oldPid, curPid := rud.Pid, rud.Pid
	// Wait for the process to be restarted.
	for oldPid == curPid {
		rud = nextResourceUsage(t, sc.OutChan)
		curPid = rud.Pid
	}
}

// Tests that Fleetspeak restarts daemonservices that don't heartbeat, if
// configured to use heartbeats.
func TestNoHeartbeats(t *testing.T) {
	mustPatchGlobalRespawnDelay(t, time.Second)
	stats := &testStatsCollector{}

	sc := clitesting.MockServiceContext{
		OutChan:        make(chan *fspb.Message),
		StatsCollector: stats,
	}
	s := startTestClient(t, testClientPY(t), "freezed", &sc, &dspb.Config{
		MonitorHeartbeats:                true,
		HeartbeatUnresponsiveGracePeriod: durpb.New(0),
		HeartbeatUnresponsiveKillPeriod:  durpb.New(100 * time.Millisecond),
	})
	defer func() {
		// Drain the channel
		go func() {
			for range sc.OutChan {
			}
		}()

		if err := s.Stop(); err != nil {
			t.Errorf("Unable to stop service: %v", err)
		}
	}()

	// Get messages until a KillNotification message appears.
	m := <-sc.OutChan
	for m.MessageType != "KillNotification" {
		m = <-sc.OutChan
	}

	kn0 := &mpb.KillNotification{}
	if err := m.Data.UnmarshalTo(kn0); err != nil {
		t.Fatalf("Unable to unmarshal KillNotification: %v", err)
	}

	kn1 := proto.Clone(kn0).(*mpb.KillNotification)
	// Wait for the process to be restarted.
	for kn0.Pid == kn1.Pid {
		// Get messages until a KillNotification message appears.
		m = <-sc.OutChan
		for m.MessageType != "KillNotification" {
			m = <-sc.OutChan
		}

		if err := m.Data.UnmarshalTo(kn1); err != nil {
			t.Fatalf("Unable to unmarshal KillNotification: %v", err)
		}
	}

	terminations := stats.subprocessesFinished.Load()
	if want := int64(1); terminations != want {
		t.Errorf("Got %d terminations, want %d", terminations, want)
	}
}

// Tests that Fleetspeak doesn't kill daemonservices that heartbeat, when
// configured to use heartbeats.
func TestHeartbeat(t *testing.T) {
	mustPatchGlobalRespawnDelay(t, 20*time.Millisecond)

	stats := &testStatsCollector{}

	sc := clitesting.MockServiceContext{
		OutChan:        make(chan *fspb.Message),
		StatsCollector: stats,
	}
	s := startTestClient(t, testClientPY(t), "100ms-heartbeat", &sc, &dspb.Config{
		MonitorHeartbeats:                true,
		HeartbeatUnresponsiveGracePeriod: durpb.New(0 * time.Second),
		HeartbeatUnresponsiveKillPeriod:  durpb.New(310 * time.Millisecond),
	})
	defer func() {
		// Drain the channel.
		go func() {
			for range sc.OutChan {
			}
		}()

		if err := s.Stop(); err != nil {
			t.Errorf("Unable to stop service: %v", err)
		}
	}()

	// Get messages until a ResourceUsage message appears.
	m := <-sc.OutChan
	for m.MessageType != "ResourceUsage" {
		m = <-sc.OutChan
	}

	rud0 := &mpb.ResourceUsageData{}
	rud1 := &mpb.ResourceUsageData{}
	if err := m.Data.UnmarshalTo(rud0); err != nil {
		t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
	}

	// Ensure the process is not restarted within the next time - the PID should not change.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	n := 0
waitLoop:
	for {
		select {
		case m = <-sc.OutChan:
			if m.MessageType != "ResourceUsage" {
				continue
			}

			n++

			if err := m.Data.UnmarshalTo(rud1); err != nil {
				t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
			}

			if rud0.Pid != rud1.Pid {
				t.Error("The service has been restarted.")
			}
		case <-ctx.Done():
			// The desired case - the service does not restart in the first 10
			// seconds, so we exit the test without error.
			break waitLoop
		}
	}

	if 0 < n {
		t.Logf("Observed %v heartbeats: OK", n)
	} else {
		t.Errorf("Expected more than one subsequent heartbeat, got %v", n)
	}

	terminations := stats.subprocessesFinished.Load()
	if want := int64(0); terminations != want {
		t.Errorf("Got %d terminations, want %d", terminations, want)
	}
}
