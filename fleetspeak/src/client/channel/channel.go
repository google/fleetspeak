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

// Package channel provides fleetspeak.Message passing over interprocess pipes.
package channel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	fcpb "github.com/google/fleetspeak/fleetspeak/src/client/channel/proto/fleetspeak_channel"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// The wire protocol is the following, repeated per message
// 1) 4 byte magic number, as a sanity and synchronization check
// 2) 4 byte size (max of 2MB)
// 3) size bytes of serialized fspb.Message
//
// Notes:
//
// - Steps 1) and 2) are in little endian byte order.
//
// - An orderly shutdown is to close the connection instead of performing step
//   2. In particular, this means that a valid sequence begins and ends with the
//   magic number.
//
// - Steps 1) and 3) are expected to happen without significant delay.
const (
	magic   = uint32(0xf1ee1001)
	maxSize = uint32(2 * 1024 * 1024) // 2MB
)

var (
	// MagicTimeout is how long we are willing to wait for a magic
	// number. Public to support testing. Should only be changed when no
	// channels are active.
	MagicTimeout = 5 * time.Minute

	// MessageTimeout is how long we are willing to wait for a message
	// body. Public to support testing. Should only be changed when no
	// channels are active.
	MessageTimeout = 5 * time.Minute

	byteOrder = binary.LittleEndian
)

// Channel handles the communication of fspb.Messages over interprocess pipes.
//
// NOTE: once any error occurs, the channel may be only partially functional. In
// that case, the channel should be shutdown and recreated.
//
// In particular, once an error is written to Err, the user of Channel is
// responsible for ensuring that any current operations against the provided
// io.Reader and io.Writer interfaces will unblock and terminate.
type Channel struct {
	In       <-chan *fspb.Message // Messages received from the other process. Will be closed when the underlying pipe is closed.
	pipeRead io.ReadCloser
	i        chan<- *fspb.Message // Other end of In.

	Out       chan<- *fspb.Message // Messages to send to the other process. Close to shutdown the Channel.
	pipeWrite io.WriteCloser
	o         <-chan *fspb.Message // Other end of Out.

	Err <-chan error // Any errors encountered.
	e   chan<- error // other end of Err

	magicRead chan bool // signals if the first magic number read succeeds or fails
	inProcess sync.WaitGroup

	startupDataOut *fcpb.StartupData        // Startup data to send to the other process.
	StartupDataIn  <-chan *fcpb.StartupData // Startup data sent by the other process.
	sdi            chan<- *fcpb.StartupData // Other end of StartupDataIn
}

// NewServiceChannel instantiates a Channel for a Fleetspeak-dependent service.
// pr and pw will be closed when the Channel is shutdown.
func NewServiceChannel(pr io.ReadCloser, pw io.WriteCloser, sdo *fcpb.StartupData) *Channel {
	return newChannel(pr, pw, sdo)
}

// NewSystemChannel creates a Channel for Fleetspeak.
// pr and pw will be closed when the Channel is shutdown.
func NewSystemChannel(pr io.ReadCloser, pw io.WriteCloser) *Channel {
	return newChannel(pr, pw, nil)
}

func newChannel(pr io.ReadCloser, pw io.WriteCloser, sdo *fcpb.StartupData) *Channel {
	// Leave these unbuffered to minimize data loss if the other end
	// freezes.
	i := make(chan *fspb.Message)
	o := make(chan *fspb.Message)

	// Buffer the error channel, so that our cleanup won't be delayed.
	e := make(chan error, 5)

	sdi := make(chan *fcpb.StartupData, 1)

	ret := &Channel{
		In:       i,
		pipeRead: pr,
		i:        i,

		Out:       o,
		pipeWrite: pw,
		o:         o,

		Err: e,
		e:   e,

		magicRead: make(chan bool, 1),

		startupDataOut: sdo,
		StartupDataIn:  sdi,
		sdi:            sdi,
	}

	ret.inProcess.Add(2)
	go ret.readLoop()
	go ret.writeLoop()

	return ret
}

func (c *Channel) read(n int, d time.Duration) ([]byte, error) {
	var b [8]byte
	var bs []byte
	if n > len(b) {
		bs = make([]byte, n)
	} else {
		bs = b[:n]
	}
	t := time.AfterFunc(d, func() {
		c.e <- fmt.Errorf("read of length %d timed out", n)
		// Aborting a os level read is tricky, and not well supported by
		// go. So we just send the error now and trust the user of
		// channel to kill the other end (or suicide) to get things
		// working again.
	})
	_, err := io.ReadFull(c.pipeRead, bs)
	if !t.Stop() {
		return nil, errors.New("timed out")
	}
	return bs, err
}

func (c *Channel) readLoop() {
	magicRead := false
	defer func() {
		close(c.i)
		c.pipeRead.Close()
		if !magicRead {
			c.magicRead <- false
		}
		c.inProcess.Done()
	}()
	// Read first message sent through the channel. If it contains startup data, write the
	// data to c.StartupDataIn
	initialMsg := c.readMsg(&magicRead)
	if initialMsg == nil {
		return // An error occurred while trying to read from the channel.
	}
	sdRead := false
	if initialMsg.Destination == nil && initialMsg.MessageType == "StartupData" {
		sd := &fcpb.StartupData{}
		if err := ptypes.UnmarshalAny(initialMsg.Data, sd); err != nil {
			log.Warningf("Failed to parse startup data from initial message: %v", err)
		} else {
			sdRead = true
			c.sdi <- sd
			close(c.sdi) // No more values to send through the channel.
		}
	}
	if !sdRead {
		// Handle the first message like any other message.
		c.i <- initialMsg
	}
	for {
		msg := c.readMsg(&magicRead)
		if msg == nil {
			return
		}
		c.i <- msg
	}
}

// readMsg blocks until a new message from the other side of the channel is read,
// or until an error occurs. Returns nil if the read failed.
func (c *Channel) readMsg(magicRead *bool) *fspb.Message {
	// The magic number should always come quickly.
	b, err := c.read(4, MagicTimeout)
	if err != nil {
		log.Errorf("error reading magic number: %v", err)
		c.e <- fmt.Errorf("error reading magic number: %v", err)
		return nil
	}
	m := byteOrder.Uint32(b)
	if m != magic {
		c.e <- fmt.Errorf("read unexpected magic number, want [%x], got [%x]", magic, m)
		return nil
	}
	if !*magicRead {
		c.magicRead <- true
		*magicRead = true
	}

	// No time limit - we wait until there is a message.
	var size uint32
	if err := binary.Read(c.pipeRead, byteOrder, &size); err != nil {
		// closed pipe while waiting for the next size is a normal shutdown.
		if !(err == io.ErrClosedPipe || err == io.EOF) {
			c.e <- fmt.Errorf("error reading size: %v", err)
		}
		return nil
	}
	if size > maxSize {
		c.e <- fmt.Errorf("read unexpected size, want less than [%x], got [%x]", maxSize, size)
		return nil
	}

	// The message should come soon after the size.
	b, err = c.read(int(size), MessageTimeout)
	if err != nil {
		c.e <- fmt.Errorf("error reading message: %v", err)
		return nil
	}

	msg := &fspb.Message{}
	if err := proto.Unmarshal(b, msg); err != nil {
		c.e <- fmt.Errorf("error parsing received message: %v", err)
		return nil
	}
	return msg
}

func (c *Channel) writeLoop() {
	defer func() {
		c.pipeWrite.Close()
		c.inProcess.Done()
	}()

	// Write the first magic number, even if we don't yet have a message to
	// send.
	if err := binary.Write(c.pipeWrite, byteOrder, magic); err != nil {
		c.e <- fmt.Errorf("error writing magic number: %v", err)
		return
	}
	// Send startup data.
	if c.startupDataOut != nil {
		sd, err := ptypes.MarshalAny(c.startupDataOut)
		if err != nil {
			c.e <- fmt.Errorf("unable to marshal StartupData: %v", err)
			return
		}
		// We do not set a destination for the initial message, since it is not meant for delivery
		// to any particular component (other than the channel implementation).
		initialMsg := &fspb.Message{
			MessageType: "StartupData",
			Data:        sd,
		}
		if !c.writeMsg(initialMsg) {
			return
		}
	}
	if !<-c.magicRead {
		return
	}
	for msg := range c.o {
		if !c.writeMsg(msg) {
			return
		}
	}
}

// writeMsg sends a message through the channel, followed by the magic number. Returns true if no
// serious error occurred (one that would cause us to tear down the channel).
func (c *Channel) writeMsg(msg *fspb.Message) bool {
	buf, err := proto.Marshal(msg)
	if err != nil {
		log.Errorf("Unable to marshal message to send: %v", err)
		return true
	}
	if len(buf) > int(maxSize) {
		log.Errorf("Overlarge message to send, want less than [%x] got [%x]", maxSize, len(buf))
		return true
	}
	if err := binary.Write(c.pipeWrite, byteOrder, int32(len(buf))); err != nil {
		c.e <- fmt.Errorf("error writing message length: %v", err)
		return false
	}
	if _, err := c.pipeWrite.Write(buf); err != nil {
		c.e <- fmt.Errorf("error writing message: %v", err)
		return false
	}
	if err := binary.Write(c.pipeWrite, byteOrder, magic); err != nil {
		c.e <- fmt.Errorf("error writing magic number: %v", err)
		return false
	}
	return true
}

// Wait waits for all underlying threads to shutdown.
func (c *Channel) Wait() {
	c.inProcess.Wait()
	close(c.e)
}
