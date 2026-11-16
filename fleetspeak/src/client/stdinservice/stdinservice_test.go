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
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/client/clitesting"
	"github.com/google/fleetspeak/fleetspeak/src/common/anypbtest"

	sspb "github.com/google/fleetspeak/fleetspeak/src/client/stdinservice/proto/fleetspeak_stdinservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

const helperArg = "--stdinservice-test-helper"

func init() {
	if len(os.Args) < 3 || os.Args[1] != helperArg {
		return
	}
	mode, rest := os.Args[2], os.Args[3:]
	switch mode {
	case "echo":
		fmt.Fprint(os.Stdout, strings.Join(rest, " "))
	case "cat":
		if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
			fmt.Fprintf(os.Stderr, "io.Copy: %v\n", err)
			os.Exit(1)
		}
	case "busy":
		end := time.Now().Add(time.Second)
		for time.Now().Before(end) {
			_, _ = os.ReadDir(".")
		}
	case "sleep":
		if len(rest) < 1 {
			fmt.Fprintln(os.Stderr, "sleep mode requires a duration argument")
			os.Exit(2)
		}
		d, err := time.ParseDuration(rest[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "time.ParseDuration: %v\n", err)
			os.Exit(1)
		}
		time.Sleep(d)
	case "log":
		fmt.Fprint(os.Stdout, "stdout_")
		fmt.Fprint(os.Stderr, "stderr_")
		fmt.Fprint(os.Stdout, "stdout_")
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode: %q\n", mode)
		os.Exit(2)
	}
	os.Exit(0)
}

func TestStdinServiceWithEcho(t *testing.T) {
	s, err := Factory(&fspb.ClientServiceConfig{
		Name: "EchoService",
		Config: anypbtest.New(t, &sspb.Config{
			Cmd:  os.Args[0],
			Args: []string{helperArg, "echo"},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	outChan := make(chan *fspb.Message, 1)
	err = s.Start(&clitesting.FakeServiceContext{
		OutChan: outChan,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = s.ProcessMessage(t.Context(),
		&fspb.Message{
			MessageType: "StdinServiceInputMessage",
			Data: anypbtest.New(t, &sspb.InputMessage{
				Args: []string{"a", "b"},
			}),
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
	if err := output.Data.UnmarshalTo(om); err != nil {
		t.Fatal(err)
	}

	wantStdout := []byte("a b")
	if !bytes.Equal(om.Stdout, wantStdout) {
		t.Fatalf("unexpected output; got %q, want %q", om.Stdout, wantStdout)
	}
}

func TestStdinServiceWithCat(t *testing.T) {
	s, err := Factory(&fspb.ClientServiceConfig{
		Name: "CatService",
		Config: anypbtest.New(t, &sspb.Config{
			Cmd: os.Args[0],
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	outChan := make(chan *fspb.Message, 1)
	err = s.Start(&clitesting.FakeServiceContext{
		OutChan: outChan,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = s.ProcessMessage(t.Context(),
		&fspb.Message{
			MessageType: "StdinServiceInputMessage",
			Data: anypbtest.New(t, &sspb.InputMessage{
				Args:  []string{helperArg, "cat"},
				Input: []byte("foo bar"),
			}),
		})
	if err != nil {
		t.Fatalf("s.ProcessMessage(...) = %q, want success", err)
	}

	var output *fspb.Message
	select {
	case output = <-outChan:
	default:
		t.Fatal(".ProcessMessage (/bin/cat <<< 'foo bar') expected to produce message, but none found")
	}

	om := &sspb.OutputMessage{}
	if err := output.Data.UnmarshalTo(om); err != nil {
		t.Fatal(err)
	}

	wantStdout := []byte("foo bar")
	if !bytes.Equal(om.Stdout, wantStdout) {
		t.Fatalf("unexpected output; got %q, want %q", om.Stdout, wantStdout)
	}
}

func TestStdinServiceReportsResourceUsage(t *testing.T) {
	s, err := Factory(&fspb.ClientServiceConfig{
		Name: "BashService",
		Config: anypbtest.New(t, &sspb.Config{
			Cmd: os.Args[0],
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	outChan := make(chan *fspb.Message, 1)
	err = s.Start(&clitesting.FakeServiceContext{
		OutChan: outChan,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = s.ProcessMessage(t.Context(),
		&fspb.Message{
			MessageType: "StdinServiceInputMessage",
			Data: anypbtest.New(t, &sspb.InputMessage{
				// Generate some system (os.ReadDir) and user execution time.
				Args: []string{helperArg, "busy"},
			}),
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
	if err := output.Data.UnmarshalTo(om); err != nil {
		t.Fatal(err)
	}

	// We don't test for ResourceUsage.MeanResidentMemory because memory is currently not being
	// queried after the process has terminated. It's only queried right after launching the command
	// in which case it can be recorded as "0" which would be indistinguishable from it not being set
	// at all, resulting in a flaky test case. The fact that the other resource usage metrics have
	// been set here is good enough for now.

	if om.Timestamp.Seconds <= 0 {
		t.Fatalf("unexpected output; StdinServiceOutputMessage.timestamp.seconds not set: %q", om)
	}
}

func TestStdinServiceCancellation(t *testing.T) {
	s, err := Factory(&fspb.ClientServiceConfig{
		Name: "SleepService",
		Config: anypbtest.New(t, &sspb.Config{
			Cmd: os.Args[0],
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	outChan := make(chan *fspb.Message, 1)
	err = s.Start(&clitesting.FakeServiceContext{
		OutChan: outChan,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, c := context.WithCancel(t.Context())
	c()

	if err := s.ProcessMessage(ctx,
		&fspb.Message{
			MessageType: "StdinServiceInputMessage",
			Data: anypbtest.New(t, &sspb.InputMessage{
				Args: []string{helperArg, "sleep", clitesting.FakeCommTimeout.String()},
			}),
		}); err != nil && !strings.HasSuffix(err.Error(), "context canceled") {
		t.Fatal(err)
	} else if err == nil {
		t.Fatal(".ProcessMessage was expected to be cancelled, but returned with no error")
	}
}

func TestStdinServiceLogLevel(t *testing.T) {
	oldLogOutputFunc := logOutputFunc
	t.Cleanup(func() { logOutputFunc = oldLogOutputFunc })

	tests := []struct {
		name    string
		level   sspb.LogLevel
		wantLog string
	}{
		{"None", sspb.LogLevel_LOG_LEVEL_NONE, ""},
		{"Stderr", sspb.LogLevel_LOG_LEVEL_STDERR, "stderr_"},
		{"All", sspb.LogLevel_LOG_LEVEL_ALL, "stderr_stdout_stdout_"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotLog string
			logOutputFunc = func(_ string, _ string, buf *bytes.Buffer) {
				gotLog += buf.String()
			}

			s, err := Factory(&fspb.ClientServiceConfig{
				Config: anypbtest.New(t, &sspb.Config{
					Cmd:      os.Args[0],
					Args:     []string{helperArg, "log"},
					LogLevel: tc.level,
				}),
			})
			if err != nil {
				t.Fatalf("Factory: %v", err)
			}

			outChan := make(chan *fspb.Message, 1)
			if err := s.Start(&clitesting.FakeServiceContext{
				OutChan: outChan,
			}); err != nil {
				t.Fatalf("Start: %v", err)
			}

			err = s.ProcessMessage(t.Context(),
				&fspb.Message{
					MessageType: "StdinServiceInputMessage",
					Data:        anypbtest.New(t, &sspb.InputMessage{}),
				})
			if err != nil {
				t.Fatalf("ProcessMessage: %v", err)
			}
			if gotLog != tc.wantLog {
				t.Errorf("unexpected log output: got %q, want %q", gotLog, tc.wantLog)
			}
		})
	}
}
