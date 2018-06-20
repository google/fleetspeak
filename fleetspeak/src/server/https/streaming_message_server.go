// Copyright 2018 Google Inc.
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
	"crypto"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

const magic = uint32(0xf1ee1001)

type fullResponseWriter interface {
	http.ResponseWriter
	http.CloseNotifier
	http.Flusher
}

func readUint32(body *bufio.Reader) (uint32, error) {
	b := make([]byte, 4)
	if _, err := io.ReadAtLeast(body, b, 4); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func writeUint32(res fullResponseWriter, i uint32) error {
	return binary.Write(res, binary.LittleEndian, i)
}

// messageServer wraps a Communicator in order to handle clients polls.
type streamingMessageServer struct {
	*Communicator
}

func (s streamingMessageServer) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	earlyError := func(msg string, status int) {
		log.ErrorDepth(1, fmt.Sprintf("%s: %s", http.StatusText(status), msg))
		s.fs.StatsCollector().ClientPoll(stats.PollInfo{
			CTX:    req.Context(),
			Start:  db.Now(),
			End:    db.Now(),
			Status: status,
			Type:   stats.StreamStart,
		})
	}

	if !s.startProcessing() {
		earlyError("server not ready", http.StatusInternalServerError)
		return
	}
	defer s.stopProcessing()

	fullRes, ok := res.(fullResponseWriter)
	if !ok {
		earlyError("/streaming-message requested, but not supported. ResponseWriter is not a fullResponseWriter", http.StatusNotFound)
		return
	}

	if req.Method != http.MethodPost {
		earlyError(fmt.Sprintf("%v not supported", req.Method), http.StatusBadRequest)
		return
	}

	if req.TLS == nil {
		earlyError("TLS information not found", http.StatusBadRequest)
		return
	}

	if len(req.TLS.PeerCertificates) != 1 {
		earlyError(fmt.Sprintf("expected 1 client cert, received %v", len(req.TLS.PeerCertificates)), http.StatusBadRequest)
		return
	}

	cert := req.TLS.PeerCertificates[0]
	if cert.PublicKey == nil {
		earlyError("public key not present in client cert", http.StatusBadRequest)
		return
	}

	body := bufio.NewReader(req.Body)

	// Set a 10 min overall maximum lifespan of the connection.
	ctx, fin := context.WithTimeout(req.Context(), s.p.StreamingLifespan+time.Duration(float32(s.p.StreamingJitter)*rand.Float32()))
	defer fin()

	// Also create a way to terminate early in case of error.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	info, moreMsgs, err := s.initialPoll(ctx, addrFromString(req.RemoteAddr), cert.PublicKey, fullRes, body)
	if err != nil || info == nil {
		return
	}

	m := streamManager{
		ctx:  ctx,
		s:    s,
		info: info,
		res:  fullRes,
		body: body,

		localNotices: make(chan struct{}, 1),
		out:          make(chan *fspb.ContactData, 5),

		cancel: cancel,
	}
	defer func() {
		// Shutdown is a bit subtle.
		//
		// We get here iff m.ctx is canceled, timed out, etc.
		//
		// Once ctx is canceled, writeLoop will notice, close the outgoing
		// ResponseWriter, and begin blindly draining m.out.
		//
		// Closing ResponseWriter will cause any pending read to error out and
		// readLoop to return.
		//
		// Once the readLoop returns, we can safely close m.out and wait for
		// writeLoop to finish.
		info.Fin()
		m.reading.Wait()
		close(m.out)
		m.writing.Wait()
	}()

	m.reading.Add(2)
	go m.readLoop()
	go m.notifyLoop(s.p.StreamingCloseTime, moreMsgs)

	m.writing.Add(1)
	go m.writeLoop()

	select {
	case <-ctx.Done():
	case <-fullRes.CloseNotify():
	case <-s.stopping:
	}
	m.cancel()
}

func (s streamingMessageServer) initialPoll(ctx context.Context, addr net.Addr, key crypto.PublicKey, res fullResponseWriter, body *bufio.Reader) (*comms.ConnectionInfo, bool, error) {
	ctx, fin := context.WithTimeout(ctx, time.Minute)

	pi := stats.PollInfo{
		CTX:    ctx,
		Start:  db.Now(),
		Status: http.StatusTeapot, // Should never actually be returned
		Type:   stats.StreamStart,
	}
	defer func() {
		fin()
		if pi.Status == http.StatusTeapot {
			log.Errorf("Forgot to set status, PollInfo: %v", pi)
		}
		pi.End = db.Now()
		s.fs.StatsCollector().ClientPoll(pi)
	}()

	makeError := func(msg string, status int) error {
		log.ErrorDepth(1, fmt.Sprintf("%s: %s", http.StatusText(status), msg))
		pi.Status = status
		return errors.New(msg)
	}

	id, err := common.MakeClientID(key)
	if err != nil {
		return nil, false, makeError(fmt.Sprintf("unable to create client id from public key: %v", err), http.StatusBadRequest)
	}
	pi.ID = id

	m, err := readUint32(body)
	if err != nil {
		return nil, false, makeError(fmt.Sprintf("error reading magic number: %v", err), http.StatusBadRequest)
	}
	if m != magic {
		return nil, false, makeError(fmt.Sprintf("unknown magic number: got %x, expected %x", m, magic), http.StatusBadRequest)
	}

	st := time.Now()
	size, err := binary.ReadUvarint(body)
	if err != nil {
		return nil, false, makeError(fmt.Sprintf("error reading size: %v", err), http.StatusBadRequest)
	}
	if size > MaxContactSize {
		return nil, false, makeError(fmt.Sprintf("initial contact size too large: got %d, expected at most %d", size, MaxContactSize), http.StatusBadRequest)
	}

	buf := make([]byte, size)
	_, err = io.ReadFull(body, buf)
	if err != nil {
		return nil, false, makeError(fmt.Sprintf("error reading body for initial exchange: %v", err), http.StatusBadRequest)
	}
	pi.ReadTime = time.Since(st)
	pi.ReadBytes = int(size)

	var wcd fspb.WrappedContactData
	if err := proto.Unmarshal(buf, &wcd); err != nil {
		return nil, false, makeError(fmt.Sprintf("error parsing body: %v", err), http.StatusBadRequest)
	}

	info, toSend, more, err := s.fs.InitializeConnection(ctx, addr, key, &wcd, true)
	if err == comms.ErrNotAuthorized {
		return nil, false, makeError("not authorized", http.StatusServiceUnavailable)
	}
	if err != nil {
		return nil, false, makeError(fmt.Sprintf("error processing contact: %v", err), http.StatusInternalServerError)
	}
	pi.CacheHit = info.Client.Cached

	out := proto.NewBuffer(make([]byte, 0, 1024))
	// EncodeMessage prepends the size as a Varint.
	if err := out.EncodeMessage(toSend); err != nil {
		info.Fin()
		return nil, false, makeError(fmt.Sprintf("error preparing messages: %v", err), http.StatusInternalServerError)
	}
	st = time.Now()
	sz, err := res.Write(out.Bytes())
	if err != nil {
		info.Fin()
		return nil, false, makeError(fmt.Sprintf("error writing body: %v", err), http.StatusInternalServerError)
	}
	res.Flush()

	pi.WriteTime = time.Since(st)
	pi.End = time.Now()
	pi.WriteBytes = sz
	pi.Status = http.StatusOK
	return info, more, nil
}

type streamManager struct {
	ctx context.Context
	s   streamingMessageServer

	info *comms.ConnectionInfo
	res  fullResponseWriter
	body *bufio.Reader

	// Signals that a we have more tokens and might retry sending.
	localNotices chan struct{}

	// The read- and writeLoop will wait for these. Separate because readloop
	// needs to finish before writeLoop.
	reading sync.WaitGroup
	writing sync.WaitGroup

	out chan *fspb.ContactData

	cancel func() // Shuts down the stream when called.
}

func (m *streamManager) readLoop() {
	defer m.reading.Done()
	defer m.cancel()

	cnt := uint64(0)

	for {
		pi, err := m.readOne()
		if err != nil {
			// If the context has been canceled, it is probably a 'normal' termination
			// - disconnect, max connection durating, etc. But if it is still active,
			// we are going to tear down everything because of an unexpected read
			// error and should log/record why.
			if m.ctx.Err() == nil && pi != nil {
				m.s.fs.StatsCollector().ClientPoll(*pi)
				log.Errorf("Streaming Connection to %v terminated with error: %v", m.info.Client.ID, err)
			}
			return
		}
		m.s.fs.StatsCollector().ClientPoll(*pi)
		cnt++
		m.out <- &fspb.ContactData{AckIndex: cnt}
	}
}

func (m *streamManager) readOne() (*stats.PollInfo, error) {
	size, err := binary.ReadUvarint(m.body)
	if err != nil {
		return nil, err
	}
	if size > MaxContactSize {
		return nil, fmt.Errorf("streaming contact size too large: got %d, expected at most %d", size, MaxContactSize)
	}

	pi := stats.PollInfo{
		CTX:      m.ctx,
		ID:       m.info.Client.ID,
		Start:    db.Now(),
		Status:   http.StatusTeapot,
		CacheHit: true,
		Type:     stats.StreamFromClient,
	}
	defer func() {
		if pi.Status == http.StatusTeapot {
			log.Errorf("Forgot to set status.")
		}
		pi.End = db.Now()
	}()
	buf := make([]byte, size)
	if _, err := io.ReadFull(m.body, buf); err != nil {
		pi.Status = http.StatusBadRequest
		return &pi, fmt.Errorf("error reading streamed data: %v", err)
	}
	pi.ReadTime = time.Since(pi.Start)
	pi.ReadBytes = int(size)

	var wcd fspb.WrappedContactData
	if err = proto.Unmarshal(buf, &wcd); err != nil {
		pi.Status = http.StatusBadRequest
		return &pi, fmt.Errorf("error parsing streamed data: %v", err)
	}

	var blockedServices []string
	for k, v := range m.info.MessageTokens {
		if v == 0 {
			blockedServices = append(blockedServices, k)
		}
	}
	if err := m.s.fs.HandleMessagesFromClient(m.ctx, m.info, &wcd); err != nil {
		if err == comms.ErrNotAuthorized {
			pi.Status = http.StatusServiceUnavailable
		} else {
			pi.Status = http.StatusInternalServerError
			err = fmt.Errorf("error processing streamed messages: %v", err)
		}
		return &pi, err
	}
	for _, s := range blockedServices {
		if m.info.MessageTokens[s] > 0 {
			select {
			case m.localNotices <- struct{}{}:
			default:
			}
		}
	}
	pi.Status = http.StatusOK
	return &pi, nil
}

func (m *streamManager) notifyLoop(closeTime time.Duration, moreMsgs bool) {
	defer m.reading.Done()

	for {
		if !moreMsgs {
			select {
			case _, ok := <-m.info.Notices:
				if !ok {
					return
				}
			case <-m.localNotices:
			}
		}
		// Stop sending messages to the client 30 seconds before our hard timelimit.
		d, ok := m.ctx.Deadline()
		if ok && time.Now().After(d.Add(-closeTime)) {
			m.out <- &fspb.ContactData{DoneSending: true}
			return
		}
		if !moreMsgs {
			// Wait up to 1 second for extra notifications/messages, ignoring additional
			// notifications during this time.
			t := time.NewTimer(time.Second)
		L:
			for {
				select {
				case _, ok := <-m.info.Notices:
					if !ok {
						break L
					}
					continue L
				case <-t.C:
					break L
				}
			}
			t.Stop()
		}
		var cd *fspb.ContactData
		var err error
		cd, moreMsgs, err = m.s.fs.GetMessagesForClient(m.ctx, m.info)
		if err != nil {
			log.Errorf("Error getting messages for streaming client [%v]: %v", m.info.Client.ID, err)
		}
		if cd != nil {
			m.out <- cd
		}
	}
}

func (m *streamManager) writeLoop() {
	defer m.writing.Done()
	defer func() {
		for range m.out {
		}
	}()

	for {
		select {
		case cd, ok := <-m.out:
			if !ok {
				return
			}
			pi, err := m.writeOne(cd)
			if err != nil {
				if m.ctx.Err() != nil {
					log.Errorf("Error sending ContactData to client [%v]: %v", m.info.Client.ID, err)
					m.cancel()
					m.s.fs.StatsCollector().ClientPoll(pi)
				}
				// ctx was already canceled - more or less normal shutdown, so don't log
				// as a poll.
				return
			}
			m.s.fs.StatsCollector().ClientPoll(pi)
		case <-m.ctx.Done():
			return
		}
	}
}

func (m *streamManager) writeOne(cd *fspb.ContactData) (stats.PollInfo, error) {
	pi := stats.PollInfo{
		CTX:      m.ctx,
		ID:       m.info.Client.ID,
		Start:    db.Now(),
		Status:   http.StatusTeapot,
		CacheHit: true,
		Type:     stats.StreamToClient,
	}
	defer func() {
		if pi.Status == http.StatusTeapot {
			log.Errorf("Forgot to set status.")
		}
		pi.End = db.Now()
	}()

	buf := proto.NewBuffer(make([]byte, 0, 1024))
	if err := buf.EncodeMessage(cd); err != nil {
		return pi, err
	}

	sw := time.Now()
	s, err := m.res.Write(buf.Bytes())
	if err != nil {
		return pi, err
	}
	m.res.Flush()
	pi.WriteTime = time.Since(sw)
	pi.WriteBytes = s
	pi.Status = http.StatusOK

	return pi, nil
}
