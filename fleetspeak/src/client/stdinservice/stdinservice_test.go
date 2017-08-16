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

package stdinservice

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"context"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/client/clitesting"

	durpb "github.com/golang/protobuf/ptypes/duration"
	sspb "github.com/google/fleetspeak/fleetspeak/src/client/stdinservice/proto/fleetspeak_stdinservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func TestStdinServiceWithEcho(t *testing.T) {
	ssc := &sspb.Config{
		Cmd: "/bin/echo",
	}
	sscAny, err := ptypes.MarshalAny(ssc)
	if err != nil {
		t.Fatal(err)
	}

	s, err := Factory(&fspb.ClientServiceConfig{
		Name:   "EchoService",
		Config: sscAny,
	})
	if err != nil {
		t.Fatal(err)
	}

	outChan := make(chan *fspb.Message, 1)
	err = s.Start(&clitesting.MockServiceContext{
		OutChan: outChan,
	})
	if err != nil {
		t.Fatal(err)
	}

	m := &sspb.InputMessage{
		Args: []string{"foo", "bar"},
	}
	mAny, err := ptypes.MarshalAny(m)
	if err != nil {
		t.Fatal(err)
	}

	err = s.ProcessMessage(context.Background(),
		&fspb.Message{
			MessageType: "StdinServiceInputMessage",
			Data:        mAny,
		})
	if err != nil {
		t.Fatal(err)
	}

	var output *fspb.Message
	select {
	case output = <-outChan:
	default:
		t.Fatal(".ProcessMessage (/bin/echo foo bar) expected to produce message, but none found")
	}

	om := &sspb.OutputMessage{}
	if err := ptypes.UnmarshalAny(output.Data, om); err != nil {
		t.Fatal(err)
	}

	wantStdout := []byte("foo bar\n")
	if !bytes.Equal(om.Stdout, wantStdout) {
		t.Fatalf("unexpected output; got %q, want %q", om.Stdout, wantStdout)
	}
}

func TestStdinServiceWithCat(t *testing.T) {
	ssc := &sspb.Config{
		Cmd: "/bin/cat",
	}
	sscAny, err := ptypes.MarshalAny(ssc)
	if err != nil {
		t.Fatal(err)
	}

	s, err := Factory(&fspb.ClientServiceConfig{
		Name:   "CatService",
		Config: sscAny,
	})
	if err != nil {
		t.Fatal(err)
	}

	outChan := make(chan *fspb.Message, 1)
	err = s.Start(&clitesting.MockServiceContext{
		OutChan: outChan,
	})
	if err != nil {
		t.Fatal(err)
	}

	m := &sspb.InputMessage{
		Input: []byte("foo bar"),
	}
	mAny, err := ptypes.MarshalAny(m)
	if err != nil {
		t.Fatal(err)
	}

	err = s.ProcessMessage(context.Background(),
		&fspb.Message{
			MessageType: "StdinServiceInputMessage",
			Data:        mAny,
		})
	if err != nil {
		t.Fatal(err)
	}

	var output *fspb.Message
	select {
	case output = <-outChan:
	default:
		t.Fatal(".ProcessMessage (/bin/cat <<< 'foo bar') expected to produce message, but none found")
	}

	om := &sspb.OutputMessage{}
	if err := ptypes.UnmarshalAny(output.Data, om); err != nil {
		t.Fatal(err)
	}

	wantStdout := []byte("foo bar")
	if !bytes.Equal(om.Stdout, wantStdout) {
		t.Fatalf("unexpected output; got %q, want %q", om.Stdout, wantStdout)
	}
}

func TestStdinServiceReportsResourceUsage(t *testing.T) {
	ssc := &sspb.Config{
		Cmd: "/bin/bash",
	}
	sscAny, err := ptypes.MarshalAny(ssc)
	if err != nil {
		t.Fatal(err)
	}

	s, err := Factory(&fspb.ClientServiceConfig{
		Name:   "BashService",
		Config: sscAny,
	})
	if err != nil {
		t.Fatal(err)
	}

	outChan := make(chan *fspb.Message, 1)
	err = s.Start(&clitesting.MockServiceContext{
		OutChan: outChan,
	})
	if err != nil {
		t.Fatal(err)
	}

	m := &sspb.InputMessage{
		// Generate some userspace execution time... Compute Σ(i=1..100000)i .
		Args: []string{"-c", `
			/usr/bin/seq 100000 |
			/usr/bin/paste -s -d + |
			/usr/bin/bc
		`},
	}
	mAny, err := ptypes.MarshalAny(m)
	if err != nil {
		t.Fatal(err)
	}

	preStartT := time.Now()

	err = s.ProcessMessage(context.Background(),
		&fspb.Message{
			MessageType: "StdinServiceInputMessage",
			Data:        mAny,
		})
	if err != nil {
		t.Fatal(err)
	}

	var output *fspb.Message
	select {
	case output = <-outChan:
	default:
		t.Fatal(".ProcessMessage (/bin/bash ...) expected to produce message, but none found")
	}

	outputReceivedT := time.Now()

	om := &sspb.OutputMessage{}
	if err := ptypes.UnmarshalAny(output.Data, om); err != nil {
		t.Fatal(err)
	}

	wantStdout := []byte("5000050000\n")
	if !bytes.Equal(om.Stdout, wantStdout) {
		t.Fatalf("unexpected output; got %q, want %q", om.Stdout, wantStdout)
	}

	if om.ResourceUsage == nil {
		t.Fatalf("unexpected output; StdinServiceOutputMessage.resource_usage not set: %q", om)
	}

	protoDurationToTimeDuration := func(d *durpb.Duration) (ret time.Duration) {
		if d != nil {
			var err error
			ret, err = ptypes.Duration(d)
			if err != nil {
				t.Fatal(err)
			}
		}

		return
	}

	ut := protoDurationToTimeDuration(om.ResourceUsage.UserTime)
	if ut <= 0 {
		t.Fatalf("unexpected output; StdinServiceOutputMessage.resource_usage.user_time not set: %q", om)
	}

	// If we require positive SystemTime, the test becomes flaky, because the
	// system space execution time is so small it sometimes rounds down to zero.

	if om.ResourceUsage.Timestamp.Seconds <= 0 {
		t.Fatalf("unexpected output; StdinServiceOutputMessage.resource_usage.timestamp.seconds not set: %q", om)
	}

	deltaT := outputReceivedT.Sub(preStartT)

	statsTime := time.Unix(om.ResourceUsage.Timestamp.Seconds, int64(om.ResourceUsage.Timestamp.Nanos))
	execDuration := statsTime.Sub(preStartT)
	if execDuration >= deltaT {
		t.Fatalf("unexpected output; the reported execution wall time is longer than it took the test to run the process and collect its output (pre-execution-time: %v, post-output-collection-time: %v, delta: %v): %q", preStartT, outputReceivedT, deltaT, om)
	}

	if om.ResourceUsage.ResidentMemory <= 0 {
		t.Fatalf("unexpected output; StdinServiceOutputMessage.resource_usage.resident_memory not set: %q", om)
	}

	if om.ResourceUsage.VerboseStatus == "" {
		t.Fatalf("unexpected output; StdinServiceOutputMessage.resource_usage.verbose_status not set: %q", om)
	}
}

func TestStdinServiceCancellation(t *testing.T) {
	ssc := &sspb.Config{
		Cmd: "/bin/sleep",
	}
	sscAny, err := ptypes.MarshalAny(ssc)
	if err != nil {
		t.Fatal(err)
	}

	s, err := Factory(&fspb.ClientServiceConfig{
		Name:   "SleepService",
		Config: sscAny,
	})
	if err != nil {
		t.Fatal(err)
	}

	outChan := make(chan *fspb.Message, 1)
	err = s.Start(&clitesting.MockServiceContext{
		OutChan: outChan,
	})
	if err != nil {
		t.Fatal(err)
	}

	m := &sspb.InputMessage{
		Args: []string{fmt.Sprintf("%f", clitesting.MockCommTimeout.Seconds())},
	}
	mAny, err := ptypes.MarshalAny(m)
	if err != nil {
		t.Fatal(err)
	}

	ctx, c := context.WithCancel(context.Background())
	c()

	if err := s.ProcessMessage(ctx,
		&fspb.Message{
			MessageType: "StdinServiceInputMessage",
			Data:        mAny,
		}); err != nil && !strings.HasSuffix(err.Error(), "context canceled") {
		t.Fatal(err)
	} else if err == nil {
		t.Fatal(".ProcessMessage was expected to be cancelled, but returned with no error")
	}
}
