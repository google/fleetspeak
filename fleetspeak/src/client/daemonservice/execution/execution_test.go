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
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/fleetspeak/fleetspeak/src/client/channel"
	"github.com/google/fleetspeak/fleetspeak/src/client/clitesting"

	dspb "github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/proto/fleetspeak_daemonservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

func testClient() string {
	if runtime.GOOS == "windows" {
		return `..\testclient\testclient.exe`
	}

	return "../testclient/testclient"
}

func TestFailures(t *testing.T) {
	prevMagicTimeout := channel.MagicTimeout
	prevMessageTimeout := channel.MessageTimeout
	channel.MagicTimeout = 5 * time.Second
	channel.MessageTimeout = 5 * time.Second
	defer func() {
		channel.MagicTimeout = prevMagicTimeout
		channel.MessageTimeout = prevMessageTimeout
	}()

	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message, 5),
	}
	if _, err := os.Stat(testClient()); err != nil {
		t.Fatalf("can't stat testclient binary [%v]: %v", testClient(), err)
	}

	// These misbehaviors should fail after MagicTimeout, or sooner.
	for _, mode := range []string{"freeze", "freezeHard", "garbage", "die"} {
		dsc := &dspb.Config{
			Argv: []string{testClient(), "--mode=" + mode},
		}
		if d := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); d != "" {
			dsc.Argv = append(dsc.Argv, "--log_dir="+d)
		}
		ex, err := New("TestService", dsc, &sc)
		if err != nil {
			t.Fatalf("execution.New returned error: %v", err)
		}
		log.Infof("started %s on %p", mode, ex)
		// This should close to indicate we gave up waiting and the execution is over.
		<-ex.Done
		close(ex.Out)
		ex.Wait()
	}
}

func TestLoopback(t *testing.T) {
	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message),
	}
	dsc := &dspb.Config{
		Argv: []string{testClient(), "--mode=loopback"},
	}
	if d := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); d != "" {
		dsc.Argv = append(dsc.Argv, "--log_dir="+d)
	}
	ex, err := New("TestService", dsc, &sc)
	if err != nil {
		t.Fatalf("execution.New returned error: %v", err)
	}
	defer func() {
		close(ex.Out)
		ex.Shutdown()
		ex.Wait()
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
		for _, m := range msgs {
			ex.Out <- m
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

func TestStd(t *testing.T) {
	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message, 20),
	}
	dsc := &dspb.Config{
		Argv: []string{testClient(), "--mode=stdSpam"},
		StdParams: &dspb.Config_StdParams{
			ServiceName:      "TestService",
			FlushTimeSeconds: 5,
		},
	}
	if d := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); d != "" {
		dsc.Argv = append(dsc.Argv, "--log_dir="+d)
	}
	ex, err := New("TestService", dsc, &sc)
	if err != nil {
		t.Fatalf("execution.New returned error: %v", err)
	}
	defer func() {
		close(ex.Out)
		ex.Shutdown()
		ex.Wait()
	}()

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
		var od dspb.StdOutputData
		if err := ptypes.UnmarshalAny(m.Data, &od); err != nil {
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
		Argv: []string{testClient(), "--mode=loopback"},
		ResourceMonitoringSampleSize:          2,
		ResourceMonitoringSamplePeriodSeconds: 1,
	}
	if d := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); d != "" {
		dsc.Argv = append(dsc.Argv, "--log_dir="+d)
	}
	ex, err := New("TestService", dsc, &sc)
	if err != nil {
		t.Fatalf("execution.New returned error: %v", err)
	}

	// Run TestService for 4.2 seconds. We expect a resource-usage
	// report to be sent every 2000 milliseconds (samplePeriod * sampleSize).
	done := make(chan struct{})
	go func() {
		for i := 0; i < 42; i++ {
			time.Sleep(100 * time.Millisecond)
			m := fspb.Message{
				MessageId:   []byte{byte(i)},
				MessageType: "DummyMessage",
			}
			ex.Out <- &m
		}
		close(ex.Out)
		ex.Shutdown()
		close(done)
	}()

	ruCnt := 0
	finalRUReceived := false

	for !finalRUReceived {
		select {
		case <-done:
			ex.Wait()
			return
		case m := <-sc.OutChan:
			if m.MessageType == "DummyMessageResponse" {
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
