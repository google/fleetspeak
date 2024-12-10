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

	"github.com/google/fleetspeak/fleetspeak/src/client/clitesting"
	"github.com/google/fleetspeak/fleetspeak/src/common/anypbtest"

	sspb "github.com/google/fleetspeak/fleetspeak/src/client/stdinservice/proto/fleetspeak_stdinservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func mustProcessStdinService(t *testing.T, conf *sspb.Config, im *sspb.InputMessage) *sspb.OutputMessage {
	t.Helper()
	s, err := Factory(&fspb.ClientServiceConfig{
		Name:   "TestService",
		Config: anypbtest.New(t, conf),
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

	err = s.ProcessMessage(context.Background(),
		&fspb.Message{
			MessageType: "StdinServiceInputMessage",
			Data:        anypbtest.New(t, im),
		})
	if err != nil {
		t.Fatal(err)
	}

	var output *fspb.Message
	select {
	case output = <-outChan:
	default:
		t.Fatal("ProcessMessage() expected to produce message, but none found")
	}

	om := &sspb.OutputMessage{}
	if err := output.Data.UnmarshalTo(om); err != nil {
		t.Fatal(err)
	}
	return om
}

func TestStdinService_AcceptsArgs(t *testing.T) {
	conf := &sspb.Config{
		Cmd: "echo",
	}
	im := &sspb.InputMessage{
		Args: []string{"foo bar"},
	}

	om := mustProcessStdinService(t, conf, im)
	wantStdout := []byte("foo bar\n")
	if !bytes.Equal(om.Stdout, wantStdout) {
		t.Fatalf("unexpected output; got %q, want %q", om.Stdout, wantStdout)
	}
}

func TestStdinService_AcceptsStdin(t *testing.T) {
	conf := &sspb.Config{
		Cmd: "cat",
	}
	im := &sspb.InputMessage{
		Input: []byte("foo bar"),
	}

	om := mustProcessStdinService(t, conf, im)

	wantStdout := []byte("foo bar")
	if !bytes.Equal(om.Stdout, wantStdout) {
		t.Fatalf("unexpected output; got %q, want %q", om.Stdout, wantStdout)
	}
}

func TestStdinService_RejectsArgs(t *testing.T) {
	conf := &sspb.Config{
		Cmd:        "echo",
		RejectArgs: true,
	}
	im := &sspb.InputMessage{
		Args: []string{"don't print this"},
	}
	om := mustProcessStdinService(t, conf, im)

	wantStdout := []byte("\n")
	if !bytes.Equal(om.Stdout, wantStdout) {
		t.Fatalf("unexpected output; got %q, want %q", om.Stdout, wantStdout)
	}
}

func TestStdinService_RejectsStdin(t *testing.T) {
	conf := &sspb.Config{
		Cmd:         "cat",
		RejectStdin: true,
	}
	im := &sspb.InputMessage{
		Input: []byte("don't print this"),
	}
	om := mustProcessStdinService(t, conf, im)

	wantStdout := []byte("")
	if !bytes.Equal(om.Stdout, wantStdout) {
		t.Fatalf("unexpected output; got %q, want %q", om.Stdout, wantStdout)
	}
}

func TestStdinService_Cancelation(t *testing.T) {
	s, err := Factory(&fspb.ClientServiceConfig{
		Name: "SleepService",
		Config: anypbtest.New(t, &sspb.Config{
			Cmd: "python",
		}),
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

	ctx, c := context.WithCancel(context.Background())
	c()

	if err := s.ProcessMessage(ctx,
		&fspb.Message{
			MessageType: "StdinServiceInputMessage",
			Data: anypbtest.New(t, &sspb.InputMessage{
				Args: []string{"-c", fmt.Sprintf(`
import time

time.sleep(%f)
		`, clitesting.MockCommTimeout.Seconds())},
			}),
		}); err != nil && !strings.HasSuffix(err.Error(), "context canceled") {
		t.Fatal(err)
	} else if err == nil {
		t.Fatal(".ProcessMessage was expected to be cancelled, but returned with no error")
	}
}
