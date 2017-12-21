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
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/client/clitesting"

	sspb "github.com/google/fleetspeak/fleetspeak/src/client/stdinservice/proto/fleetspeak_stdinservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func TestStdinServiceWithEcho(t *testing.T) {
	ssc := &sspb.Config{
		Cmd: "python",
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
		Args: []string{"-c", `print "foo bar"`},
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
	wantStdoutWin := []byte("foo bar\r\n")
	if !bytes.Equal(om.Stdout, wantStdout) &&
		!bytes.Equal(om.Stdout, wantStdoutWin) {
		t.Fatalf("unexpected output; got %q, want %q", om.Stdout, wantStdout)
	}
}

func TestStdinServiceWithCat(t *testing.T) {
	ssc := &sspb.Config{
		Cmd: "python",
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
		Args: []string{"-c", `
try:
  while True:
    print raw_input()
except EOFError:
  pass
		`},
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

	wantStdout := []byte("foo bar\n")
	wantStdoutWin := []byte("foo bar\r\n")
	if !bytes.Equal(om.Stdout, wantStdout) &&
		!bytes.Equal(om.Stdout, wantStdoutWin) {
		t.Fatalf("unexpected output; got %q, want %q", om.Stdout, wantStdout)
	}
}

func TestStdinServiceReportsResourceUsage(t *testing.T) {
	ssc := &sspb.Config{
		Cmd: "python",
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
		// Generate some system (os.listdir) and user (everything else) execution time...
		Args: []string{"-c", `
import os
import time

t0 = time.time()
while time.time() - t0 < 1.:
  os.listdir(".")
		`},
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
		t.Fatal(".ProcessMessage (/bin/bash ...) expected to produce message, but none found")
	}

	om := &sspb.OutputMessage{}
	if err := ptypes.UnmarshalAny(output.Data, om); err != nil {
		t.Fatal(err)
	}

	if om.ResourceUsage == nil {
		t.Fatalf("unexpected output; StdinServiceOutputMessage.resource_usage not set: %q", om)
	}

	if om.ResourceUsage.MeanUserCpuRate <= 0 {
		t.Fatalf("unexpected output; StdinServiceOutputMessage.resource_usage.mean_user_cpu_rate not set: %q", om)
	}

	if om.ResourceUsage.MeanSystemCpuRate <= 0 {
		t.Fatalf("unexpected output; StdinServiceOutputMessage.resource_usage.mean_system_cpu_rate not set: %q", om)
	}

	if om.Timestamp.Seconds <= 0 {
		t.Fatalf("unexpected output; StdinServiceOutputMessage.timestamp.seconds not set: %q", om)
	}

	if om.ResourceUsage.MeanResidentMemory <= 0 {
		t.Fatalf("unexpected output; StdinServiceOutputMessage.resource_usage.mean_resident_memory not set: %q", om)
	}
}

func TestStdinServiceCancellation(t *testing.T) {
	ssc := &sspb.Config{
		Cmd: "python",
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
		Args: []string{"-c", fmt.Sprintf(`
import time

time.sleep(%f)
		`, clitesting.MockCommTimeout.Seconds())},
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
