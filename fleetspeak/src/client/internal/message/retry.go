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

// Package message implements utility structures and methods used by the FS
// client to manage messages.
package message

import (
	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
)

type sizedMessage struct {
	size int
	m    service.AckMessage
}

// RetryLoop is a loop which reads from in and writes to out.
//
// Messages are considered pending until MessageInfo.Ack is called and output
// again if MessageInfo.Nack is called. The loop will pause reading from input
// when at least maxSize bytes of messages, or maxCount messages are pending.
//
// To shutdown gracefully, close out and Ack any pending messages.
func RetryLoop(in <-chan service.AckMessage, out chan<- comms.MessageInfo, stats stats.RetryLoopCollector, maxSize, maxCount int) {
	// Used to send acks/nacks back to this loop. Buffered to prevent ack,nack
	// callbacks from ever blocking.
	acks := make(chan sizedMessage, maxCount)
	nacks := make(chan sizedMessage, maxCount)

	makeInfo := func(sm sizedMessage) comms.MessageInfo {
		return comms.MessageInfo{
			M: sm.m.M,
			Ack: func() {
				if sm.m.Ack != nil {
					sm.m.Ack()
				}
				stats.MessageAcknowledged(sm.m.M, sm.size)
				acks <- sm
			},
			Nack: func() { nacks <- sm },
		}
	}

	var size, count int
	var optIn <-chan service.AckMessage
	for {
		if size >= maxSize || count >= maxCount {
			optIn = nil
		} else {
			optIn = in
		}

		select {
		case sm := <-acks:
			size -= sm.size
			count--
		case sm := <-nacks:
			stats.BeforeMessageRetry(sm.m.M)
			out <- makeInfo(sm)
		case m, ok := <-optIn:
			if !ok {
				return
			}
			sm := sizedMessage{proto.Size(m.M), m}
			size += sm.size
			count++
			stats.MessagePending(m.M, sm.size)
			out <- makeInfo(sm)
		}
	}
}
