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

package message

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

type statsCollector struct {
	RetryLoopStatsCollector
	retries, pending, pendingSize atomic.Int64
}

func (sc *statsCollector) BeforeMessageRetry(msg *fspb.Message) {
	sc.retries.Add(1)
}

func (sc *statsCollector) MessagePending(msg *fspb.Message, size int) {
	sc.pending.Add(1)
	sc.pendingSize.Add(int64(size))
}

func (sc *statsCollector) MessageAcknowledged(msg *fspb.Message, size int) {
	sc.pending.Add(-1)
	sc.pendingSize.Add(-int64(size))
}

func makeMessages(count, size int) []service.AckMessage {
	var ret []service.AckMessage
	for i := 0; i < count; i++ {
		payload := make([]byte, size)
		rand.Read(payload)
		ret = append(ret, service.AckMessage{
			M: &fspb.Message{
				MessageId: []byte{0, 0, 0, byte(i >> 8), byte(i | 0xFF)},
				Source: &fspb.Address{
					ServiceName: "TestService",
					ClientId:    []byte{0, 0, 1},
				},
				Destination: &fspb.Address{
					ServiceName: "TestService",
				},
				MessageType: "TestMessageType",
				Data:        &anypb.Any{Value: payload},
			}})
	}
	return ret
}

func TestRetryLoopNormal(t *testing.T) {
	sc := &statsCollector{}
	in := make(chan service.AckMessage)
	out := make(chan comms.MessageInfo, 100)
	go RetryLoop(in, out, sc, 20*1024*1024, 100)
	defer close(in)

	// Normal flow.
	msgs := makeMessages(10, 5)
	for _, m := range msgs {
		in <- m
	}

	for _, m := range msgs {
		got := <-out
		if !proto.Equal(m.M, got.M) {
			t.Errorf("Unexpected read from output channel. Got %v, want %v.", got.M, m)
		}
		got.Ack()
	}
	select {
	case mi := <-out:
		t.Errorf("Expected empty output channel, but read: %v", mi.M)
	default:
	}

	retries := sc.retries.Load()
	if retries != 0 {
		t.Errorf("Unexpected number of retries reported, got: %d, want: 0", retries)
	}
}

func TestRetryLoopNACK(t *testing.T) {
	sc := &statsCollector{}
	in := make(chan service.AckMessage)
	out := make(chan comms.MessageInfo, 100)
	go RetryLoop(in, out, sc, 20*1024*1024, 100)
	defer close(in)

	// Nack flow.
	msgs := makeMessages(10, 5)

	for _, m := range msgs {
		in <- m
	}
	for _, m := range msgs {
		got := <-out
		if !proto.Equal(m.M, got.M) {
			t.Errorf("Unexpected read from output channel. Got %v, want %v.", got.M, m)
		}
		got.Nack()
	}
	for _, m := range msgs {
		got := <-out
		if !proto.Equal(m.M, got.M) {
			t.Errorf("Unexpected read from output channel. Got %v, want %v.", got.M, m)
		}
		got.Ack()
	}
	select {
	case mi := <-out:
		t.Errorf("Expected empty output channel, but read: %v", mi.M)
	default:
	}

	retries := sc.retries.Load()
	if retries != 10 {
		t.Errorf("Unexpected number of retries reported, got: %d, want: 10", retries)
	}
}

func TestRetryLoopSizing(t *testing.T) {
	sc := &statsCollector{}
	in := make(chan service.AckMessage)
	out := make(chan comms.MessageInfo, 100)
	go RetryLoop(in, out, sc, 20*1024*1024, 100)
	defer close(in)

	// Two test cases in which we try to overfill the buffer.
	for _, tc := range []struct {
		name                   string
		count, size, shouldFit int
	}{
		{"Small Messages", 300, 5, 100},
		{"Large Messages", 30, 1024 * 1024, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// shouldFit should fit
			msgs := makeMessages(tc.count, tc.size)
			for i := 0; i < tc.shouldFit; i++ {
				in <- msgs[i]
			}

			// Another message should not fit. Wait just a bit to make sure that it
			// really won't fit.
			select {
			case in <- service.AckMessage{M: &fspb.Message{MessageId: []byte("asdf")}}:
				t.Error("Was able to overstuff in.")
			case <-time.After(100 * time.Millisecond):
			}

			var w sync.WaitGroup
			w.Add(1)
			// stuff the rest in as they fit:
			go func() {
				for i := tc.shouldFit; i < len(msgs); i++ {
					in <- msgs[i]
				}
				w.Done()
			}()

			// Reading them all should be fine, so long as we ack them.
			for _, m := range msgs {
				got := <-out
				if !proto.Equal(m.M, got.M) {
					t.Errorf("Unexpected read from output channel. Got %v, want %v.", got.M, m)
				}
				got.Ack()
			}
			w.Wait()
			select {
			case mi := <-out:
				t.Errorf("Expected empty output channel, but read: %v", mi.M)
			default:
			}

			retries := sc.retries.Load()
			if retries != 0 {
				t.Errorf("Unexpected number of retries reported, got: %d, want: 0", retries)
			}
		})
	}
}

func TestRetryLoopReportsPendingMessages(t *testing.T) {
	sc := &statsCollector{}
	in := make(chan service.AckMessage)
	out := make(chan comms.MessageInfo, 100)
	go RetryLoop(in, out, sc, 20*1024*1024, 100)
	defer close(in)

	msgs := makeMessages(10, 5)
	var totalByteSize int64
	for _, m := range msgs {
		totalByteSize += int64(proto.Size(m.M))
		in <- m
	}

	// Give RetryLoop goroutine a short while to take in msgs
	time.Sleep(100 * time.Millisecond)
	pending := sc.pending.Load()
	if pending != 10 {
		t.Errorf("Unexpected number of pending messages, got: %d, want: 10", pending)
	}
	pendingSize := sc.pendingSize.Load()
	if pendingSize != totalByteSize {
		t.Errorf("Unexpected size of pending messages, got: %d, want: %d", pendingSize, totalByteSize)
	}

	for range msgs {
		got := <-out
		got.Ack()
	}

	// Give RetryLoop goroutine a short while to process acks
	time.Sleep(100 * time.Millisecond)
	pending = sc.pending.Load()
	if pending != 0 {
		t.Errorf("Unexpected number of pending messages, got: %d, want: 0", pending)
	}
	pendingSize = sc.pendingSize.Load()
	if pendingSize != 0 {
		t.Errorf("Unexpected size of pending messages, got: %d, want: 0", pendingSize)
	}
}
