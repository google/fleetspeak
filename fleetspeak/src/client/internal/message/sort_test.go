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
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/flow"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func TestSortLoop(t *testing.T) {
	in := make(chan comms.MessageInfo)
	out := make(chan comms.MessageInfo)
	go SortLoop(in, out, flow.NewFilter())
	defer close(in)

	for _, tc := range []struct {
		name string
		in   []*fspb.Message
		want []*fspb.Message
	}{
		{
			name: "LOW order doesn't change",
			in: []*fspb.Message{
				{MessageId: []byte{0, 0, 0}, Priority: fspb.Message_LOW},
				{MessageId: []byte{0, 0, 1}, Priority: fspb.Message_LOW},
				{MessageId: []byte{0, 0, 2}, Priority: fspb.Message_LOW},
			},
			want: []*fspb.Message{
				{MessageId: []byte{0, 0, 0}, Priority: fspb.Message_LOW},
				{MessageId: []byte{0, 0, 1}, Priority: fspb.Message_LOW},
				{MessageId: []byte{0, 0, 2}, Priority: fspb.Message_LOW},
			},
		},
		{
			name: "MEDIUM order doesn't change",
			in: []*fspb.Message{
				{MessageId: []byte{0, 0, 0}, Priority: fspb.Message_MEDIUM},
				{MessageId: []byte{0, 0, 1}, Priority: fspb.Message_MEDIUM},
				{MessageId: []byte{0, 0, 2}, Priority: fspb.Message_MEDIUM},
			},
			want: []*fspb.Message{
				{MessageId: []byte{0, 0, 0}, Priority: fspb.Message_MEDIUM},
				{MessageId: []byte{0, 0, 1}, Priority: fspb.Message_MEDIUM},
				{MessageId: []byte{0, 0, 2}, Priority: fspb.Message_MEDIUM},
			},
		},
		{
			name: "HIGH order doesn't change",
			in: []*fspb.Message{
				{MessageId: []byte{0, 0, 0}, Priority: fspb.Message_HIGH},
				{MessageId: []byte{0, 0, 1}, Priority: fspb.Message_HIGH},
				{MessageId: []byte{0, 0, 2}, Priority: fspb.Message_HIGH},
			},
			want: []*fspb.Message{
				{MessageId: []byte{0, 0, 0}, Priority: fspb.Message_HIGH},
				{MessageId: []byte{0, 0, 1}, Priority: fspb.Message_HIGH},
				{MessageId: []byte{0, 0, 2}, Priority: fspb.Message_HIGH},
			},
		},
		{
			name: "Sorts",
			in: []*fspb.Message{
				{MessageId: []byte{0, 1, 0}, Priority: fspb.Message_LOW},
				{MessageId: []byte{0, 2, 0}, Priority: fspb.Message_MEDIUM},
				{MessageId: []byte{0, 3, 0}, Priority: fspb.Message_HIGH},
				{MessageId: []byte{0, 4, 0}, Priority: fspb.Message_HIGH},
				{MessageId: []byte{0, 5, 0}, Priority: fspb.Message_MEDIUM},
				{MessageId: []byte{0, 6, 0}, Priority: fspb.Message_LOW},
			},
			want: []*fspb.Message{
				{MessageId: []byte{0, 3, 0}, Priority: fspb.Message_HIGH},
				{MessageId: []byte{0, 4, 0}, Priority: fspb.Message_HIGH},
				{MessageId: []byte{0, 2, 0}, Priority: fspb.Message_MEDIUM},
				{MessageId: []byte{0, 5, 0}, Priority: fspb.Message_MEDIUM},
				{MessageId: []byte{0, 1, 0}, Priority: fspb.Message_LOW},
				{MessageId: []byte{0, 6, 0}, Priority: fspb.Message_LOW},
			},
		},
	} {
		for _, m := range tc.in {
			in <- comms.MessageInfo{M: m}
		}
		for _, m := range tc.want {
			got := <-out
			if !proto.Equal(got.M, m) {
				t.Errorf("%s: Unexpected message from output, got %v want %v.", tc.name, got.M, m)
			}
		}
	}
}

func TestFilter(t *testing.T) {
	in := make(chan comms.MessageInfo)
	out := make(chan comms.MessageInfo)
	f := flow.NewFilter()
	go SortLoop(in, out, f)
	defer close(in)

	for _, tc := range []struct {
		name    string
		l, m, h bool
		in      []*fspb.Message
		want    []*fspb.Message
	}{
		{
			name: "Filter all 1",
			l:    true,
			m:    true,
			h:    true,
			in: []*fspb.Message{
				{MessageId: []byte{0, 0, 0}, Priority: fspb.Message_LOW},
				{MessageId: []byte{0, 0, 1}, Priority: fspb.Message_MEDIUM},
				{MessageId: []byte{0, 0, 2}, Priority: fspb.Message_HIGH},
			},
			want: []*fspb.Message{},
		},
		{
			name: "Filter none 1",
			in:   []*fspb.Message{},
			want: []*fspb.Message{
				{MessageId: []byte{0, 0, 2}, Priority: fspb.Message_HIGH},
				{MessageId: []byte{0, 0, 1}, Priority: fspb.Message_MEDIUM},
				{MessageId: []byte{0, 0, 0}, Priority: fspb.Message_LOW},
			},
		},
		{
			name: "Filter all 2",
			l:    true,
			m:    true,
			h:    true,
			in: []*fspb.Message{
				{MessageId: []byte{0, 0, 0}, Priority: fspb.Message_LOW},
				{MessageId: []byte{0, 0, 1}, Priority: fspb.Message_MEDIUM},
				{MessageId: []byte{0, 0, 2}, Priority: fspb.Message_HIGH},
			},
			want: []*fspb.Message{},
		},
		{
			name: "Filter low",
			l:    true,
			in:   []*fspb.Message{},
			want: []*fspb.Message{
				{MessageId: []byte{0, 0, 2}, Priority: fspb.Message_HIGH},
				{MessageId: []byte{0, 0, 1}, Priority: fspb.Message_MEDIUM},
			},
		},
		{
			name: "Filter none 2",
			in:   []*fspb.Message{},
			want: []*fspb.Message{
				{MessageId: []byte{0, 0, 0}, Priority: fspb.Message_LOW},
			},
		},
	} {
		f.Set(tc.l, tc.m, tc.h)
		for _, m := range tc.in {
			in <- comms.MessageInfo{M: m}
		}
		for _, m := range tc.want {
			got := <-out
			if !proto.Equal(got.M, m) {
				t.Errorf("%s: Unexpected message from output, got %v want %v.", tc.name, got.M, m)
			}
		}
		select {
		case got := <-out:
			t.Errorf("%s: Expected no more results, got %v", tc.name, got.M)
		case <-time.After(time.Second):
		}
	}
}

func TestBlockUntilDrained(t *testing.T) {
	in := make(chan comms.MessageInfo)
	out := make(chan comms.MessageInfo)

	m := &fspb.Message{
		MessageId: []byte{0},
		Priority:  fspb.Message_HIGH,
	}

	done := make(chan struct{})
	go func() {
		SortLoop(in, out, flow.NewFilter())
		done <- struct{}{}
	}()

	in <- comms.MessageInfo{M: m}
	close(in)

	select {
	case <-done:
		t.Fatalf("SortLoop returned before all messages were drained.")
	case <-time.After(200 * time.Millisecond):
	}

	got := <-out
	if !proto.Equal(got.M, m) {
		t.Errorf("Unexpected message from output, got %v want %v.", got.M, m)
	}

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("SortLoop did not return after all messages were drained.")
	}
}
