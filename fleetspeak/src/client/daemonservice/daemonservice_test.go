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
	"context"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/fleetspeak/fleetspeak/src/client/clitesting"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	anypb "github.com/golang/protobuf/ptypes/any"
	durpb "github.com/golang/protobuf/ptypes/duration"
	dspb "github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/proto/fleetspeak_daemonservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

func testClient() []string {
	if runtime.GOOS == "windows" {
		return []string{`testclient\testclient.exe`}
	}

	return []string{"testclient/testclient"}
}

func testClientPY() []string {
	if runtime.GOOS == "windows" {
		return []string{"python", `testclient\testclient.py`}
	}

	return []string{"testclient/testclient.py"}
}

func testClientLauncherPY() []string {
	if runtime.GOOS == "windows" {
		return []string{"python", `testclient\testclient_launcher.py`}
	}

	return []string{"testclient/testclient_launcher.py"}
}

func startTestClient(t *testing.T, client []string, mode string, sc service.Context, dsc dspb.Config) *Service {
	dsc.ResourceMonitoringSampleSize = 2
	dsc.ResourceMonitoringSamplePeriodSeconds = 1
	dsc.Argv = append(dsc.Argv, client...)
	dsc.Argv = append(dsc.Argv, "--mode="+mode)

	if d := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); d != "" {
		dsc.Argv = append(dsc.Argv, "--log_dir="+d)
	}

	dscAny, err := ptypes.MarshalAny(&dsc)
	if err != nil {
		t.Fatalf("ptypes.MarshalAny(*daemonservice.Config): %v", err)
	}
	s, err := Factory(&fspb.ClientServiceConfig{
		Name:   "TestDaemonService",
		Config: dscAny,
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
	s := startTestClient(t, client, "loopback", &sc, dspb.Config{})
	defer func() {
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
	for _, client := range [][]string{testClient(), testClientPY()} {
		exerciseLoopback(t, client)
	}
}

// Tests that Fleetspeak uses self-reported PIDs for monitoring resource-usage of
// daemon services.
func TestSelfReportedPIDs(t *testing.T) {
	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message),
	}
	s := startTestClient(t, testClientLauncherPY(), "loopback", &sc, dspb.Config{})

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
			if err := ptypes.UnmarshalAny(m.Data, rud); err != nil {
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

func TestRespawn(t *testing.T) {
	var ord time.Duration
	RespawnDelay, ord = time.Second, RespawnDelay
	defer func() {
		RespawnDelay = ord
	}()

	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message, 100),
	}
	s := startTestClient(t, testClient(), "die", &sc, dspb.Config{})
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
			var rud mpb.ResourceUsageData
			if err := ptypes.UnmarshalAny(m.Data, &rud); err != nil {
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
	var ord time.Duration
	RespawnDelay, ord = time.Second, RespawnDelay
	defer func() {
		RespawnDelay = ord
	}()

	dsc := dspb.Config{
		InactivityTimeout: &durpb.Duration{Seconds: 1},
	}
	dsc.Argv = append(dsc.Argv, testClient()...)
	dsc.Argv = append(dsc.Argv, "--mode=loopback")

	dscAny, err := ptypes.MarshalAny(&dsc)
	if err != nil {
		t.Fatalf("ptypes.MarshalAny(*DaemonServiceConfig): %v", err)
	}
	s, err := Factory(&fspb.ClientServiceConfig{
		Name:   "TestDaemonService",
		Config: dscAny,
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
	for len(seen) < 3 {
		select {
		case <-late:
			t.Errorf("Expected at least 3 pids in 10 seconds, only saw: %d", len(seen))
			break
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
			var rud mpb.ResourceUsageData
			if err := ptypes.UnmarshalAny(m.Data, &rud); err != nil {
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
	s := startTestClient(t, client, "loopback", &sc, dspb.Config{})
	defer func() {
		if err := s.Stop(); err != nil {
			t.Errorf("Unable to stop service: %v", err)
		}
	}()

	// We send 16kb messages through loopback, without draining our end.
	// Eventually, the backlog should cause us to block until ProcessMessage times
	// out.
	msgCnt := 0
	msg := &fspb.Message{
		MessageId:   []byte("\000\000\000"),
		MessageType: "RequestTypeA",
		Data:        &anypb.Any{Value: []byte(strings.Repeat("0123456789abcdef", 1024))},
	}
	var err error
	for {
		ctx, c := context.WithTimeout(context.Background(), time.Second)

		start := time.Now()
		err = s.ProcessMessage(ctx, msg) // err in outer block, checked at loop end
		if err == nil {
			msgCnt++
		}
		c()
		rt := time.Since(start)
		// verify that ProcessMessage respects ctx.
		if rt > 2*time.Second {
			t.Errorf("ProcessMessage with 1 second timeout took %v", rt)
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
	for i := 0; i < msgCnt; i++ {
		<-sc.OutChan
	}
}

func TestBacklog(t *testing.T) {
	for _, client := range [][]string{testClient(), testClientPY()} {
		exerciseBacklog(t, client)
	}
}

// Tests that Fleetspeak kills daemonservices that exceed their memory limits.
func TestMemoryLimit(t *testing.T) {
	var ord time.Duration
	RespawnDelay, ord = time.Second, RespawnDelay
	defer func() {
		RespawnDelay = ord
	}()

	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message),
	}
	s := startTestClient(t, testClientPY(), "memoryhog", &sc, dspb.Config{
		MemoryLimit: 1024*1024*10, // 10MB
	})
	defer func() {
		if err := s.Stop(); err != nil {
			t.Errorf("Unable to stop service: %v", err)
		}
	}()

	// Get messages until a ResourceUsage message appears.
	m := <-sc.OutChan
	for m.MessageType != "ResourceUsage" {
		m = <-sc.OutChan
	}

	var rud0, rud1 mpb.ResourceUsageData
	if err := ptypes.UnmarshalAny(m.Data, &rud0); err != nil {
		t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
	}

	rud1 = rud0
	// Wait for the process to be restarted.
	for rud0.Pid == rud1.Pid {
		// Get messages until a ResourceUsage message appears.
		m = <-sc.OutChan
		for m.MessageType != "ResourceUsage" {
			m = <-sc.OutChan
		}

		if err := ptypes.UnmarshalAny(m.Data, &rud1); err != nil {
			t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
		}
	}
}

// Tests that Fleetspeak restarts daemonservices that don't heartbeat, if
// configured to use heartbeats.
func TestNoHeartbeats(t *testing.T) {
	var ord time.Duration
	RespawnDelay, ord = time.Second, RespawnDelay
	defer func() {
		RespawnDelay = ord
	}()

	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message),
	}
	s := startTestClient(t, testClientPY(), "freezed", &sc, dspb.Config{
		MonitorHeartbeats: true,
		HeartbeatUnresponsiveGracePeriodSeconds: 0,
		HeartbeatUnresponsiveKillPeriodSeconds: 1,
	})
	defer func() {
		// Drain the channel
		go func() {
			for _, ok := <-sc.OutChan; ok; _, ok = <-sc.OutChan {}
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

	var rud0, rud1 mpb.ResourceUsageData
	if err := ptypes.UnmarshalAny(m.Data, &rud0); err != nil {
		t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
	}

	rud1 = rud0
	// Wait for the process to be restarted.
	for rud0.Pid == rud1.Pid {
		// Get messages until a ResourceUsage message appears.
		m = <-sc.OutChan
		for m.MessageType != "ResourceUsage" {
			m = <-sc.OutChan
		}

		if err := ptypes.UnmarshalAny(m.Data, &rud1); err != nil {
			t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
		}
	}
}

// Tests that Fleetspeak doesn't kill daemonservices that heartbeat, when
// configured to use heartbeats.
func TestHeartbeat(t *testing.T) {
	var ord time.Duration
	RespawnDelay, ord = time.Second, RespawnDelay
	defer func() {
		RespawnDelay = ord
	}()

	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message),
	}
	s := startTestClient(t, testClientPY(), "heartbeat", &sc, dspb.Config{
		MonitorHeartbeats: true,
		HeartbeatUnresponsiveGracePeriodSeconds: 0,
		HeartbeatUnresponsiveKillPeriodSeconds: 1,
	})
	defer func() {
		// Drain the channel.
		go func() {
			for _, ok := <-sc.OutChan; ok; _, ok = <-sc.OutChan {}
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

	var rud0, rud1 mpb.ResourceUsageData
	if err := ptypes.UnmarshalAny(m.Data, &rud0); err != nil {
		t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
	}

	// Ensure the process is not restarted in 2 seconds - the PID should not change.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m = <-sc.OutChan
		if m.MessageType != "ResourceUsage" {
			continue
		}

		if err := ptypes.UnmarshalAny(m.Data, &rud1); err != nil {
			t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
		}

		if rud0.Pid != rud1.Pid {
			t.Error("The service has been restarted.")
		}
	}
}
