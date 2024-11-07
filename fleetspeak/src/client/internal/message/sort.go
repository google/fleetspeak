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
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/flow"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// SortLoop connects in and out in a sorted manner. That is, it acts essentially
// as a buffered channel between in and out which sorts any messages within it.
// The caller is responsible for implementing any needed size limit.
// SortLoop returns when in is closed, and the buffered messages have been
// drained through out, to make sure no messages are lost.
func SortLoop(in <-chan comms.MessageInfo, out chan<- comms.MessageInfo, f *flow.Filter) {
	// Keep a slice of messages for each priority level. These are used as fifo
	// queues, appending to the end and retrieving from head.
	var low, medium, high []comms.MessageInfo

	// Block until all messages have been drained.
	defer func() {
		for _, mi := range high {
			out <- mi
		}
		for _, mi := range medium {
			out <- mi
		}
		for _, mi := range low {
			out <- mi
		}
	}()

	// Append a message to the correct list.
	appendMI := func(mi comms.MessageInfo) {
		switch mi.M.Priority {
		case fspb.Message_LOW:
			low = append(low, mi)
		case fspb.Message_MEDIUM:
			medium = append(medium, mi)
		case fspb.Message_HIGH:
			high = append(high, mi)
		default:
			medium = append(medium, mi)
		}
	}

	// Used to poll f occasionally, in case it changes while we are waiting for
	// something.
	filterPoll := time.NewTicker(time.Second)
	defer filterPoll.Stop()

	for {
		// Try to send from the highest priority non-empty queue. Simultaneously try
		// to receive. (First true case matches, of course.)
		//
		// Don't send from queues which are filtered.
		lf, mf, hf := f.Get()
		switch {
		case len(high) != 0 && !hf:
			select {
			case mi, ok := <-in:
				if !ok {
					return
				}
				appendMI(mi)
			case out <- high[0]:
				high = high[1:]
			case <-filterPoll.C:
			}
		case len(medium) != 0 && !mf:
			select {
			case mi, ok := <-in:
				if !ok {
					return
				}
				appendMI(mi)
			case out <- medium[0]:
				medium = medium[1:]
			case <-filterPoll.C:
			}
		case len(low) != 0 && !lf:
			select {
			case mi, ok := <-in:
				if !ok {
					return
				}
				appendMI(mi)
			case out <- low[0]:
				low = low[1:]
			case <-filterPoll.C:
			}
		default:
			// Nothing to send, we can only receive.
			select {
			case mi, ok := <-in:
				if !ok {
					return
				}
				appendMI(mi)
			case <-filterPoll.C:
			}
		}
	}
}
