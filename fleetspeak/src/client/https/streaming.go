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

package https

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/common"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

const magic = uint32(0xf1ee1001)

type StreamingCommunicator struct {
	ctx         context.Context
	cctx        comms.Context
	conf        *clpb.CommunicatorConfig
	hc          *http.Client
	id          common.ClientID
	hosts       []string
	hostLock    sync.RWMutex // Protects hosts
	working     sync.WaitGroup
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error) // If set, will be used to initiate network connections to the server.

	// called to shutdown the communicator
	fin context.CancelFunc
}

func (c *StreamingCommunicator) Setup(cl comms.Context) error {
	c.cctx = cl
	c.ctx, c.fin = context.WithCancel(context.Background())
	c.conf = c.cctx.CommunicatorConfig()
	if c.conf == nil {
		return errors.New("no communicator_config in client configuration")
	}
	if c.conf.MinFailureDelaySeconds == 0 {
		c.conf.MinFailureDelaySeconds = 60 * 5
	}
	if c.conf.FailureSuicideTimeSeconds == 0 {
		c.conf.FailureSuicideTimeSeconds = 60 * 60 * 24 * 7
	}
	return c.configure()
}

func (c *StreamingCommunicator) Start() error {
	c.working.Add(1)
	go c.connectLoop()
	return nil
}

func (c *StreamingCommunicator) Stop() {
	c.fin()
	c.working.Wait()
}

func (c *StreamingCommunicator) GetFileIfModified(ctx context.Context, service, name string, modSince time.Time) (io.ReadCloser, time.Time, error) {
	c.hostLock.RLock()
	hosts := append([]string(nil), c.hosts...)
	c.hostLock.RUnlock()
	return getFileIfModified(ctx, hosts, c.hc, service, name, modSince)
}

func (c *StreamingCommunicator) configure() error {
	id, tr, err := makeTransport(c.cctx, c.DialContext)
	if err != nil {
		return err
	}
	c.id = id

	si, err := c.cctx.ServerInfo()
	if err != nil {
		return fmt.Errorf("unable to configure communicator, could not get server information: %v", err)
	}
	c.hosts = append([]string(nil), si.Servers...)
	if len(c.hosts) == 0 {
		return errors.New("no server_addresses in client configuration")
	}
	c.hc = &http.Client{
		Transport: tr,
		Timeout:   10 * time.Minute,
	}
	return nil
}

func (c *StreamingCommunicator) connectLoop() {
	defer c.working.Done()

	lastContact := time.Now()
	for {
		if c.id != c.cctx.CurrentID() {
			c.configure()
		}
		var con *connection
		var err error
		for i, h := range c.hosts {
			conCTX, fin := context.WithTimeout(c.ctx, 60*time.Second)
			// The server limits us to a 10m connection, and we prefer that
			// the server close the connection so it can minimize the chance of
			// a message getting lost while being sent to us.
			con, err = c.connect(conCTX, h, 12*time.Minute)
			fin()
			if err != nil {
				if c.ctx.Err() != nil {
					return
				}
				con = nil
				log.Warningf("Connection to %v failed with error: %v", h, err)
				continue
			}
			if con != nil {
				if i != 0 {
					c.hostLock.Lock()
					c.hosts[0], c.hosts[i] = c.hosts[i], c.hosts[0]
					c.hostLock.Unlock()
				}
				break
			}
		}
		if con == nil {
			log.V(1).Info("Connection failed, sleeping before retry.")
			// Failure!
			if time.Since(lastContact) > time.Duration(c.conf.FailureSuicideTimeSeconds)*time.Second {
				log.Fatalf("Too Lonely! Failed to contact server in %v.", time.Since(lastContact))
			}
			t := time.NewTimer(jitter(c.conf.MinFailureDelaySeconds))
			select {
			case <-t.C:
			case <-c.ctx.Done():
				t.Stop()
				return
			}
			continue
		}
		log.V(2).Infof("--%p: started", con)
		con.working.Wait()
		lastContact = time.Now()
		for _, l := range con.pending {
			for _, m := range l {
				m.Nack()
			}
		}
	}
}

func readContact(body *bufio.Reader) (*fspb.ContactData, error) {
	log.V(2).Info("->Reading contact size.")
	size, err := binary.ReadUvarint(body)
	if err != nil {
		return nil, err
	}
	log.V(2).Infof("->Reading body of size %d.", size)
	buf := make([]byte, size)
	_, err = io.ReadFull(body, buf)
	if err != nil {
		return nil, err
	}
	var cd fspb.ContactData
	if err := proto.Unmarshal(buf, &cd); err != nil {
		return nil, err
	}
	return &cd, nil
}

// connect performs an initial exchange and returns an active streaming
// connection to the given host. ctx only regulates this initial connection.
func (c *StreamingCommunicator) connect(ctx context.Context, host string, maxLife time.Duration) (*connection, error) {
	ret := connection{
		cctx:        c.cctx,
		pending:     make(map[int][]comms.MessageInfo),
		serverDone:  make(chan struct{}),
		writingDone: make(chan struct{}),
	}
	ret.ctx, ret.stop = context.WithTimeout(c.ctx, maxLife)

	// Spend up to 1 second getting some initial messages - we need to send
	// an initial WrappedContactData for the initial exchange whether or not
	// we have any messages.
	gctx, fin := context.WithTimeout(ctx, time.Second)
	toSend := ret.groupMessages(gctx)
	fin()

	defer func() {
		// If the messages are successfully sent, we'll clear toSend. If
		// we error out between now and then, Nack them.
		for _, m := range toSend {
			m.Nack()
		}
	}()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	msgs := make([]*fspb.Message, 0, len(toSend))
	for _, m := range toSend {
		msgs = append(msgs, m.M)
	}
	wcd, err := c.cctx.MakeContactData(msgs)
	if err != nil {
		return nil, err
	}
	buf := proto.NewBuffer(make([]byte, 0, 1024))
	if err := buf.EncodeMessage(wcd); err != nil {
		return nil, err
	}

	br, bw := io.Pipe()

	urn := url.URL{Scheme: "https", Host: host, Path: "/streaming-message"}
	req, err := http.NewRequest("POST", urn.String(), br)
	if err != nil {
		return nil, err
	}
	req.ContentLength = -1
	req.Close = true
	req.Header.Set("Expect", "100-continue")
	req = req.WithContext(ret.ctx)

	// If ctx terminates during the initial Do, we want ret.ctx to end, but
	// if we succeed we want ret.ctx to continue independently of ctx.
	ok := make(chan struct{})
	canceled := make(chan bool)
	go func() {
		select {
		case <-ok:
			canceled <- false
		case <-ctx.Done():
			ret.stop()
			canceled <- true
		}
	}()
	// We also need to feed the intital data into the pipe while Do
	// executes.
	go func() {
		binary.Write(bw, binary.LittleEndian, magic)
		bw.Write(buf.Bytes())
	}()
	resp, err := c.hc.Do(req)
	close(ok)
	if <-canceled {
		return nil, ctx.Err()
	}

	if err != nil {
		log.V(1).Infof("Streaming connection attempt failed: %v", err)
		ret.stop()
		return nil, err
	}
	go func() {
		<-ret.ctx.Done()
		err := resp.Body.Close()
		log.V(2).Infof("--%p: Context finished, body closed with result: %v", &ret, err)
	}()

	fail := func(err error) (*connection, error) {
		log.V(1).Infof("Streaming connection failed: %v", err)
		ret.stop()
		resp.Body.Close()
		return nil, err
	}

	if resp.StatusCode != 200 {
		return fail(fmt.Errorf("POST to %v failed with status: %v", host, resp.StatusCode))
	}
	body := bufio.NewReader(resp.Body)
	cd, err := readContact(body)
	if err != nil {
		return fail(err)
	}
	if err := c.cctx.ProcessContactData(ctx, cd, false); err != nil {
		return fail(err)
	}

	for _, m := range toSend {
		m.Ack()
	}
	toSend = nil

	ret.working.Add(2)
	go ret.readLoop(body, resp.Body)
	go ret.writeLoop(bw)

	log.V(2).Infof("--Streaming connection with %s started.", host)
	return &ret, nil
}

// connection stores the information related to a particular streaming
// connection to the server.
type connection struct {
	ctx  context.Context
	cctx comms.Context

	// pending maps the index of every not-yet-acknowledged already-sent
	// data record to the list of messages that was included in it.
	pending     map[int][]comms.MessageInfo
	pendingLock sync.Mutex

	serverDone  chan struct{} // done message received from server
	writingDone chan struct{} // writeloop finished

	// Used to wait until everything is done.
	working sync.WaitGroup
	// Closure which can be called to terminate the connection.
	stop func()
}

// groupMessages gets a group of messages to send. Note that we are committed to calling
// either Ack or Nack on every message that it returns.
func (c *connection) groupMessages(ctx context.Context) []comms.MessageInfo {
	b := c.cctx.Outbox()

	var r []comms.MessageInfo
	select {
	case <-c.serverDone:
		return nil
	case <-ctx.Done():
		return nil
	case m := <-b:
		r = append(r, m)
	}
	size := 2 + proto.Size(r[0].M)

	ctx, fin := context.WithTimeout(ctx, time.Second)
	defer fin()
	for {
		// Since we are streaming, we don't wait synchronously for a
		// response, so trigger at a smaller byte threshold to perhaps
		// increase pipelining.
		if size >= sendBytesThreshold/2 || len(r) >= sendCountThreshold {
			break
		}
		select {
		case <-ctx.Done():
			return r
		case m := <-b:
			r = append(r, m)
			size += proto.Size(m.M)
		}
	}
	return r
}

func (c *connection) writeLoop(bw *io.PipeWriter) {
	steppedShutdown := false
	defer func() {
		log.V(2).Infof("<-%p: writeLoop stopping, steppedShutdown: %t", c, steppedShutdown)
		c.stop()
		bw.Close()
		if !steppedShutdown {
			close(c.writingDone)
		}
		c.working.Done()
		log.V(2).Infof("<-%p: writeLoop stopped", c)
	}()

	buf := proto.NewBuffer(make([]byte, 0, 1024))
	cnt := 1
	for {
		// Immediatly add to c.pending, so that somebody will Ack/Nack
		// them.
		msgs := c.groupMessages(c.ctx)
		if msgs == nil {
			if c.ctx.Err() == nil {
				steppedShutdown = true
				close(c.writingDone)
				<-c.ctx.Done()
			}
			return
		}
		c.pendingLock.Lock()
		c.pending[cnt] = msgs
		c.pendingLock.Unlock()
		cnt++

		if c.ctx.Err() != nil {
			return
		}
		fsmsgs := make([]*fspb.Message, 0, len(msgs))
		for _, m := range msgs {
			fsmsgs = append(fsmsgs, m.M)
		}
		wcd, err := c.cctx.MakeContactData(fsmsgs)
		if err != nil {
			log.Errorf("Error creating streaming contact data: %v", err)
			return
		}
		if err := buf.EncodeMessage(wcd); err != nil {
			log.Errorf("Error encoding streaming contact data: %v", err)
			return
		}
		log.V(2).Infof("<-Starting write of %d bytes", len(buf.Bytes()))
		s, err := bw.Write(buf.Bytes())
		if err != nil {
			if c.ctx.Err() == nil {
				log.Errorf("Error writing streaming contact data: %v", err)
			}
			return
		}
		log.V(2).Infof("<-Wrote streaming ContactData of %d messages, and %d bytes", len(fsmsgs), s)
		buf.Reset()
	}
}

func (c *connection) readLoop(body *bufio.Reader, closer io.Closer) {
	defer func() {
		log.V(2).Infof("->%p: readLoop stopping", c)
		c.stop()
		closer.Close()
		c.working.Done()
		log.V(2).Infof("->%p: readLoop stopped", c)
	}()

	cnt := 1
	writingDone := false
	for {
		cd, err := readContact(body)
		if err != nil {
			if c.ctx.Err() == nil && err != io.EOF {
				log.Errorf("Error reading streaming ContactData: %v", err)
			}
			return
		}
		if log.V(2) {
			log.Infof("->Read streaming ContactData [AckIdx: %d, MessageCount: %d, DoneSending: %t]", cd.AckIndex, len(cd.Messages), cd.DoneSending)
		}
		if cd.AckIndex == 0 && len(cd.Messages) == 0 && !cd.DoneSending {
			log.Warningf("Read empty streaming ContactData.")
		}
		if cd.DoneSending {
			close(c.serverDone)
			select {
			case <-c.ctx.Done():
				return
			case <-c.writingDone:
				writingDone = true
			}
		}
		if cd.AckIndex != 0 {
			c.pendingLock.Lock()
			toAck := c.pending[cnt]
			delete(c.pending, cnt)
			l := len(c.pending)
			c.pendingLock.Unlock()
			cnt++
			for _, m := range toAck {
				m.Ack()
			}
			if writingDone && l == 0 {
				return
			}
			log.V(2).Infof("->Acked %d messages.", len(toAck))
		}
		if len(cd.Messages) != 0 {
			if err := c.cctx.ProcessContactData(c.ctx, cd, true); err != nil {
				log.Errorf("Failed to process received ContactData: %v", err)
			}
		} else {
			log.V(2).Infof("->Processed %d messages.", len(cd.Messages))
		}
	}
}
