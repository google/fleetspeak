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

package execution

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/client/channel"
	"github.com/google/fleetspeak/fleetspeak/src/client/clitesting"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"google.golang.org/protobuf/proto"

	dspb "github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/proto/fleetspeak_daemonservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

func patchDuration(t *testing.T, d *time.Duration, value time.Duration) {
	t.Helper()
	prev := *d
	*d = value
	t.Cleanup(func() { *d = prev })
}

func TestFailures(t *testing.T) {
	patchDuration(t, &channel.MagicTimeout, 50*time.Millisecond)
	patchDuration(t, &channel.MessageTimeout, 50*time.Millisecond)
	patchDuration(t, &startupDataTimeout, time.Second)

	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message, 5),
	}
	if _, err := os.Stat(testClient(t)); err != nil {
		t.Fatalf("can't stat testclient binary [%v]: %v", testClient(t), err)
	}

	// These misbehaviors should fail after MagicTimeout, or sooner.
	for _, mode := range []string{"freeze", "freezeHard", "garbage", "die"} {
		t.Run(mode, func(t *testing.T) {
			dsc := &dspb.Config{
				Argv: []string{testClient(t), "--mode=" + mode},
			}
			if d := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); d != "" {
				dsc.Argv = append(dsc.Argv, "--log_dir="+d)
			}

			ex, err := New(context.Background(), "TestService", dsc, &sc)
			if err != nil {
				t.Fatalf("execution.New returned error: %v", err)
			}
			defer ex.Shutdown()

			waitRes := make(chan error)
			go func() {
				waitRes <- ex.Wait()
			}()
			select {
			case err := <-waitRes:
				if err == nil {
					t.Errorf("expected error from process, got nil")
				}
			case <-time.After(10 * time.Second):
				t.Errorf("process should have been canceled, but kept running")
			}
		})
	}
}

func TestLoopback(t *testing.T) {
	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message),
	}
	dsc := &dspb.Config{
		Argv: []string{testClient(t), "--mode=loopback"},
	}
	if d := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); d != "" {
		dsc.Argv = append(dsc.Argv, "--log_dir="+d)
	}
	ex := mustNew(t, "TestService", dsc, &sc)
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
		for _, m := range msgs {
			err := ex.SendMsg(context.Background(), m)
			if err != nil {
				t.Errorf("ex.SendMsg: %v", err)
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

func TestStd(t *testing.T) {
	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message, 20),
	}
	dsc := &dspb.Config{
		Argv: []string{testClient(t), "--mode=stdSpam"},
		StdParams: &dspb.Config_StdParams{
			ServiceName:      "TestService",
			FlushTimeSeconds: 5,
		},
	}
	if d := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); d != "" {
		dsc.Argv = append(dsc.Argv, "--log_dir="+d)
	}
	ex := mustNew(t, "TestService", dsc, &sc)
	_ = ex // only indirectly used through input and output

	wantIn := []byte(strings.Repeat("The quick brown fox jumped over the lazy dogs.\n", 128*1024))
	wantErr := []byte(strings.Repeat("The brown quick fox jumped over some lazy dogs.\n", 128*1024))

	var bufIn bytes.Buffer
	bufIn.Grow(len(wantIn))
	var bufErr bytes.Buffer
	bufErr.Grow(len(wantErr))

	start := time.Now()
	for bufIn.Len() < len(wantIn) || bufErr.Len() < len(wantErr) {
		m := <-sc.OutChan
		if m.MessageType == "ResourceUsage" {
			continue
		}
		if m.MessageType != "StdOutput" {
			t.Errorf("Received unexpected message type: %s", m.MessageType)
			continue
		}
		od := &dspb.StdOutputData{}
		if err := m.Data.UnmarshalTo(od); err != nil {
			t.Fatalf("Unable to unmarshal StdOutputData: %v", err)
		}
		bufIn.Write(od.Stdout)
		bufErr.Write(od.Stderr)
	}

	// Flushes should happen when the buffer is full. The last flush might take up
	// to 5 seconds occur, the rest should occur quickly.
	runtime := time.Since(start)
	deadline := 10 * time.Second
	if runtime > deadline {
		t.Errorf("took %v to receive std data, should be less than %v", runtime, deadline)
	}

	if !bytes.Equal(bufIn.Bytes(), wantIn) {
		t.Error("received stdout does not match expected")
	}
	if !bytes.Equal(bufErr.Bytes(), wantErr) {
		t.Error("received stderr does not match expected")
	}
}

func TestStats(t *testing.T) {
	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message, 2000),
	}
	dsc := &dspb.Config{
		Argv:                                  []string{testClient(t), "--mode=loopback"},
		ResourceMonitoringSampleSize:          2,
		ResourceMonitoringSamplePeriodSeconds: 1,
	}
	if d := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); d != "" {
		dsc.Argv = append(dsc.Argv, "--log_dir="+d)
	}
	ex := mustNew(t, "TestService", dsc, &sc)

	// Run TestService for 4.2 seconds. We expect a resource-usage
	// report to be sent every 2000 milliseconds (samplePeriod * sampleSize).
	done := make(chan struct{})
	go func() {
		for i := 0; i < 42; i++ {
			time.Sleep(100 * time.Millisecond)
			m := &fspb.Message{
				MessageId:   []byte{byte(i)},
				MessageType: "DummyMessage",
			}
			err := ex.SendMsg(context.TODO(), m)
			if err != nil {
				t.Errorf("ex.SendMsg: %v", err)
			}
		}
		close(done)
	}()

	ruCnt := 0
	finalRUReceived := false

	for !finalRUReceived {
		select {
		case <-done:
			return
		case m := <-sc.OutChan:
			if m.MessageType == "DummyMessageResponse" {
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
			ruCnt++
			if rud.ProcessTerminated {
				finalRUReceived = true
				continue
			}
			ru := rud.ResourceUsage
			if ru == nil {
				t.Error("ResourceUsageData should have non-nil ResourceUsage")
				continue
			}
			if ru.MeanResidentMemory <= 0.0 {
				t.Errorf("ResourceUsage.MeanResidentMemory should be >0, got: %.2f", ru.MeanResidentMemory)
				continue
			}
		}
	}

	if !finalRUReceived {
		t.Error("Last resource-usage report from finished process was not received.")
	}

	// We expect floor(4200 / 2000) regular resource-usage reports, plus the last one sent after
	// the process terminates.
	if ruCnt != 3 {
		t.Errorf("Unexpected number of resource-usage reports received. Got %d. Want 3.", ruCnt)
	}
}

func mustNew(t *testing.T, name string, cfg *dspb.Config, sc service.Context) *Execution {
	t.Helper()
	ex, err := New(context.Background(), name, cfg, sc)
	if err != nil {
		t.Fatalf("execution.New returned error: %v", err)
	}
	t.Cleanup(func() {
		ex.Shutdown()
		err := ex.Wait()
		if err != nil {
			t.Errorf("unexpected execution error: %v", err)
		}
	})
	return ex
}
