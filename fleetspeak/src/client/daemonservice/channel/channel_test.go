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

package channel

import (
	"io"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func TestWithLoopback(t *testing.T) {
	toLoopReader, toLoopWriter := io.Pipe()
	fromLoopReader, fromLoopWriter := io.Pipe()

	mainEnd := New(fromLoopReader, toLoopWriter)
	loopEnd := New(toLoopReader, fromLoopWriter)

	// build a simple loopback onto the loopEnd:
	go func() {
		for {
			select {
			case e := <-loopEnd.Err:
				t.Error(e)
				return
			case m, ok := <-loopEnd.In:
				if ok {
					m.MessageType = m.MessageType + "Reply"
					loopEnd.Out <- m
				} else {
					close(loopEnd.Out)
					return
				}
			}
		}
	}()

	for _, m := range []*fspb.Message{
		&fspb.Message{
			MessageId:   []byte("abc"),
			MessageType: "MessageOne",
		},
		&fspb.Message{
			MessageId:   []byte("abcabc"),
			MessageType: "MessageTwo",
		},
		&fspb.Message{
			MessageId:   []byte("abcabcabc"),
			MessageType: "MessageThree",
		},
	} {
		mainEnd.Out <- m
		got := <-mainEnd.In
		m.MessageType = m.MessageType + "Reply"
		if !proto.Equal(got, m) {
			t.Errorf("Unexpected message at mainEnd, got [%v], want [%v]", m, got)
		}
	}

	close(mainEnd.Out)
	m, ok := <-mainEnd.In
	if ok {
		t.Errorf("Expected mainEnd.In to close, got: %v", m)
	}

	// There should be no errors present anywhere:
	select {
	case err := <-mainEnd.Err:
		t.Errorf("Unexpected error at mainEnd: %v", err)
	case err := <-loopEnd.Err:
		t.Errorf("Unexpected error at loopEnd: %v", err)
	default:
	}
}

func TestDeadEnd(t *testing.T) {
	// Test with a dead (non-responsive) other end.
	var omag, omess time.Duration
	MagicTimeout, omag = time.Second, MagicTimeout
	MessageTimeout, omess = time.Second, MessageTimeout
	defer func() {
		MagicTimeout = omag
		MessageTimeout = omess
	}()

	toLoopReader, toLoopWriter := io.Pipe()
	fromLoopReader, fromLoopWriter := io.Pipe()

	mainEnd := New(fromLoopReader, toLoopWriter)
	defer toLoopReader.Close()
	defer fromLoopWriter.Close()

	// The channel should not accept a single message for a dead end.
	select {
	case mainEnd.Out <- &fspb.Message{MessageId: []byte{0, 0, 1}}:
		t.Errorf("Channel accepted message for dead end.")
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case m := <-mainEnd.In:
		t.Errorf("Unexpected message from dead end: %v", m)
	case err := <-mainEnd.Err:
		if strings.Contains(err.Error(), "error writing magic number") {
			err = <-mainEnd.Err
		}
		if !strings.Contains(err.Error(), "timed out") {
			t.Errorf("Expected \"timed out\" error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("Expected error after 1 second, received nothing.")
	}
}

func TestBlackhole(t *testing.T) {
	// test with a bad (produces garbage) other end
	var omag, omess time.Duration
	MagicTimeout, omag = time.Second, MagicTimeout
	MessageTimeout, omess = time.Second, MessageTimeout
	defer func() {
		MagicTimeout = omag
		MessageTimeout = omess
	}()

	toLoopReader, toLoopWriter := io.Pipe()
	fromLoopReader, fromLoopWriter := io.Pipe()

	mainEnd := New(fromLoopReader, toLoopWriter)
	go func() {
		for {
			buf := make([]byte, 1024)
			if _, err := toLoopReader.Read(buf); err != nil {
				return
			}
		}
	}()
	defer toLoopReader.Close()
	defer fromLoopWriter.Close()

	go func() {
		for {
			b := make([]byte, 64)
			_, err := rand.Read(b)
			if err != nil {
				t.Errorf("Unable to Read random bytes: %v", err)
				return
			}
			_, err = fromLoopWriter.Write(b)
			if err != nil {
				return
			}
		}
	}()

	// The channel should not accept a single message for a blackhole.
	select {
	case mainEnd.Out <- &fspb.Message{MessageId: []byte{0, 0, 1}}:
		t.Errorf("Channel accepted message for blackhole.")
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case m, ok := <-mainEnd.In:
		// There should never be a message output, but occasionally
		// mainEnd.In closes before the error arrives. This is fine, but
		// we still expect an error.
		if ok {
			t.Errorf("Unexpected message from dead end: %v, %v", m, ok)
		} else {
			select {
			case err := <-mainEnd.Err:
				if !strings.Contains(err.Error(), "unexpected magic number") {
					t.Errorf("Expected \"unexpected magic number\" error, got: %v", err)
				}
			case <-time.After(time.Second):
				t.Errorf("Expected error, received nothing.")
			}
		}
	case err := <-mainEnd.Err:
		if !strings.Contains(err.Error(), "unexpected magic number") {
			t.Errorf("Expected \"unexpected magic number\" error, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Errorf("Expected error, received nothing.")
	}
}
