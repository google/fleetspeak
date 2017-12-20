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
	"math/rand"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// Builder must return a new Channel connected to the target process,
// along with a cancel function that should shut down the Channel and release
// any associated resources.
//
// May return (nil, nil) if the system is shutting down and the
// RelentlessChannel using the builder should stop. Otherwise, should only
// return once it has a Channel.
type Builder func() (c *Channel, cancel func())

// A RelentlessChannel is like a Channel, but relentless. Essentially it wraps a
// Channel, which it recreates on error. Furthermore, it maintains a collection
// of messages which have not been acknowledged, and resends them after channel
// recreation. It also provides a mechanism for messages sent through it to be
// acknowledged by the other side of the channel.
type RelentlessChannel struct {
	In  <-chan *fspb.Message      // Messages received from the other process.
	Out chan<- service.AckMessage // Messages to send to the other process. Close to shutdown.

	i chan<- *fspb.Message      // other end of In
	o <-chan service.AckMessage // other end of Out

	ch  *Channel // current Channel
	fin func()   // current cleanup function for ch

	pending map[string]service.AckMessage
	builder Builder
}

// NewRelentlessChannel returns a RelentlessChannel which wraps Builder, and
// uses it to create channels.
func NewRelentlessChannel(b Builder) *RelentlessChannel {
	i := make(chan *fspb.Message, 5)
	o := make(chan service.AckMessage)

	ret := RelentlessChannel{
		In:  i,
		Out: o,

		i: i,
		o: o,

		pending: make(map[string]service.AckMessage),
		builder: b,
	}
	go ret.processingLoop()
	return &ret
}

func (c *RelentlessChannel) processingLoop() {
	defer close(c.i)
	defer c.cleanupChan()

NewChan:
	for {
		c.cleanupChan()
		c.ch, c.fin = c.builder()
		if c.ch == nil {
			return
		}

		// We now have a new channel, start by re-sending everything we still have
		// pending.
		for _, m := range c.pending {
			if c.sendOne(m.M) {
				continue NewChan
			}
		}

		// Now get a message and pass it along.
		for {
			m, newChan, shutdown := c.receiveOne()
			if shutdown {
				return
			}
			if newChan {
				continue NewChan
			}
			if len(m.M.SourceMessageId) == 0 {
				m.M.SourceMessageId = make([]byte, 16)
				rand.Read(m.M.SourceMessageId)
			}
			c.pending[string(m.M.SourceMessageId)] = m
			if c.sendOne(m.M) {
				continue NewChan
			}
		}
	}
}

func (c *RelentlessChannel) cleanupChan() {
	if c.fin != nil {
		close(c.ch.Out)
		c.fin()
		c.fin = nil
		c.ch = nil
	}
}

func (c *RelentlessChannel) receiveOne() (am service.AckMessage, newChan, shutdown bool) {
	for {
		select {
		case e := <-c.ch.Err:
			log.Errorf("Channel failed with error: %v", e)
			newChan = true
			return
		case m2, ok := <-c.ch.In:
			if !ok {
				newChan = true
				return
			}
			if !c.processAck(m2) {
				c.i <- m2
			}
		case m, ok := <-c.o:
			if !ok {
				shutdown = true
				return
			}
			am = m
			return
		}
	}
}

func (c *RelentlessChannel) sendOne(m *fspb.Message) (newChan bool) {
	for {
		select {
		case e := <-c.ch.Err:
			log.Errorf("Channel failed with error: %v", e)
			return true
		case m2, ok := <-c.ch.In:
			if !ok {
				return true
			}
			if !c.processAck(m2) {
				c.i <- m2
			}
		case c.ch.Out <- m:
			return false
		}
	}
}

func (c *RelentlessChannel) processAck(m *fspb.Message) bool {
	if m.MessageType != "LocalAck" || m.Source.ServiceName != "client" {
		return false
	}
	var d fspb.MessageAckData
	if err := ptypes.UnmarshalAny(m.Data, &d); err != nil {
		log.Errorf("Error parsing m.Data: %v", err)
		return true
	}
	for _, id := range d.MessageIds {
		s := string(id)
		m, ok := c.pending[s]
		if !ok {
			// This should be uncommon, but could happen if we restart while the FS
			// server has acknowledgments for our predecessor.
			log.Warningf("Received unexpected id: %x", id)
			continue
		}
		if m.Ack != nil {
			m.Ack()
		}
		delete(c.pending, s)
	}
	return true
}

// RelentlessAcknowledger partially wraps a Channel. It assumes that the other end
// of the Channel is attached to a RelentlessChannel and implements the
// acknowledgement protocol which RelentlessChannel expects.
//
// Once a Channel is so wrapped, the caller should read from
// RelentlessAcknowledger.In instead of Channel.In. The resulting AckMessages should
// be acknowledged in order to inform the attached RelentlessChannel that the
// message was successfully handled.
type RelentlessAcknowledger struct {
	In <-chan service.AckMessage // Wraps Channel.In.

	c     *Channel
	acks  chan []byte // Communicates that a messages has been ack'd back to the struct's main loop.
	toAck [][]byte    // An accumulation of acks to send through c.

	stop chan struct{}
	done sync.WaitGroup
}

// NewRelentlessAcknowledger creates a RelentlessAcknowledger wrapping c, buffered to
// smoothly handle 'size' simultaneously unacknowledged messages.
func NewRelentlessAcknowledger(c *Channel, size int) *RelentlessAcknowledger {
	in := make(chan service.AckMessage)
	r := &RelentlessAcknowledger{
		In:   in,
		c:    c,
		acks: make(chan []byte, size),
		stop: make(chan struct{}),
	}
	r.done.Add(1)
	go r.acknowledgeLoop(in)

	return r
}

// flush clears a.toAck by sending an acknowledgement message through the
// Channel.
func (a *RelentlessAcknowledger) flush() {
	if len(a.toAck) > 0 {
		var d fspb.MessageAckData
		d.MessageIds = a.toAck
		data, err := ptypes.MarshalAny(&d)
		if err != nil {
			// Should never happen.
			log.Fatalf("Unable to marshal MessageAckData: %v", err)
		}

		m := &fspb.Message{
			Source:      &fspb.Address{ServiceName: "client"},
			MessageType: "LocalAck",
			Data:        data,
		}
		select {
		case a.c.Out <- m:
		case <-a.stop:
			// shutting down before we were able to flush
			return
		}
		a.toAck = nil
	}
}

// acknowledgeLoop is an event loop. It passes messages from a.c.In to a.In, and
// sends acknowledgements when necessary. Runs until a.Stop() is called or
// a.c.In is closed.
func (a *RelentlessAcknowledger) acknowledgeLoop(out chan<- service.AckMessage) {
	defer a.done.Done()
	defer close(out)
	defer a.flush()

	// Ticker used to flush() regularly.
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()

	for {
		select {
		case m, ok := <-a.c.In:
			// We read in a message from the Channel. Pass it along, attaching an Ack
			// which will write the message id to a.acks.
			if !ok {
				return
			}
			ac := service.AckMessage{
				M: m,
				Ack: func() {
					a.acks <- m.SourceMessageId
				},
			}
			select {
			case out <- ac:
			case <-a.stop:
				return
			}
		case id := <-a.acks:
			// A previously created messages has been acked. We'll tell the other end
			// the next time we flush().
			a.toAck = append(a.toAck, id)
		case <-t.C:
			a.flush()
		case <-a.stop:
			return
		}
	}
}

// Stop stops the RelentlessAcknowledger and closes a.In.
func (a *RelentlessAcknowledger) Stop() {
	close(a.stop)
	a.done.Wait()
}
