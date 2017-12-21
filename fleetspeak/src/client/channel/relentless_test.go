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
	"bytes"
	"io"
	"sort"
	"testing"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func TestRelentlessLoop(t *testing.T) {
	var mainEnd *Channel

	r := make(chan struct{}, 1)

	loopEnd := NewRelentlessChannel(func() (*Channel, func()) {
		log.Info("building channel")
		toLoopReader, toLoopWriter := io.Pipe()
		fromLoopReader, fromLoopWriter := io.Pipe()

		mainEnd = New(fromLoopReader, toLoopWriter)

		r <- struct{}{}

		return New(toLoopReader, fromLoopWriter), func() {
			log.Info("shutting down channel")
			toLoopReader.Close()
			toLoopWriter.Close()
			fromLoopReader.Close()
			fromLoopWriter.Close()
		}
	})

	// closed to shutdown loopback
	done := make(chan struct{})

	// channel of messages that the loopback has received an ack for.
	acked := make(chan *fspb.Message, 20)

	go func() {
		defer func() {
			log.Info("shutting down loopback")
			close(loopEnd.Out)
		}()
		for {
			select {
			case m, ok := <-loopEnd.In:
				if !ok {
					t.Error("loopEnd.In was closed.")
					return
				}
				loopEnd.Out <- service.AckMessage{M: m, Ack: func() { acked <- m }}
			case <-done:
				return
			}
		}
	}()

	// Wait for the first Builder call.
	<-r

	ra := NewRelentlessAcknowledger(mainEnd, 20)

	msgs := []*fspb.Message{
		{SourceMessageId: []byte("A"), Source: &fspb.Address{ServiceName: "TestService"}},
		{SourceMessageId: []byte("B"), Source: &fspb.Address{ServiceName: "TestService"}},
		{SourceMessageId: []byte("C"), Source: &fspb.Address{ServiceName: "TestService"}},
	}

	for _, m := range msgs {
		mainEnd.Out <- m
	}

	// should not be acknowledged yet:
	select {
	case m := <-acked:
		t.Errorf("Received unexpected ack: %v", m)
	default:
	}

	// read them and ack them - read should be in order
	for _, m := range msgs {
		am, ok := <-ra.In
		if !ok {
			t.Fatal("ra.In unexpectedly closed")
		}
		if !proto.Equal(m, am.M) {
			t.Errorf("Unexpected message read. got [%v], want [%v}", am.M, m)
		}
		am.Ack()
	}

	for i, m := range msgs {
		got := <-acked
		if !proto.Equal(m, got) {
			t.Errorf("Unexpected ack in position %d, got [%v], want [%v]", i, got, m)
		}
	}

	// Now we test the retry capability. Send 3 new messages.
	msgs = []*fspb.Message{
		{SourceMessageId: []byte("D"), Source: &fspb.Address{ServiceName: "TestService"}},
		{SourceMessageId: []byte("E"), Source: &fspb.Address{ServiceName: "TestService"}},
		{SourceMessageId: []byte("F"), Source: &fspb.Address{ServiceName: "TestService"}},
	}

	for _, m := range msgs {
		mainEnd.Out <- m
	}

	// read them but do not ack them - read should be in order
	for _, m := range msgs {
		am, ok := <-ra.In
		if !ok {
			t.Fatal("out unexpectedly closed")
		}
		if !proto.Equal(m, am.M) {
			t.Errorf("Unexpected message read. got [%v], want [%v}", am.M, m)
		}
	}

	// close mainEnd
	ra.Stop()
	close(mainEnd.Out)

	// wait for it to be rebuilt
	<-r

	// start a new Acknowledger
	ra = NewRelentlessAcknowledger(mainEnd, 20)

	// Should be able to read and ack the messages. Retry order may be different,
	// so we collect and sort.
	var got []*fspb.Message
	for range msgs {
		am, ok := <-ra.In
		if !ok {
			t.Fatal("ra.In unexpectedly closed")
		}
		am.Ack()
		got = append(got, am.M)
	}

	sort.Slice(got, func(i, j int) bool {
		return bytes.Compare(got[i].SourceMessageId, got[j].SourceMessageId) == -1
	})

	for i, m := range msgs {
		if !proto.Equal(m, got[i]) {
			t.Errorf("Unexpected message read. got [%v], want [%v}", got[i], m)
		}
	}

	ra.Stop()
	close(mainEnd.Out)
	close(done)
}
