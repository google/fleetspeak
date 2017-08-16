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

// Package socketservice implements a service which runs and communicates with a separate independent
// process over a local unix socket (or similar).
package socketservice

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"log"
	"context"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/channel"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/socketservice/checks"

	sspb "github.com/google/fleetspeak/fleetspeak/src/client/socketservice/proto/fleetspeak_socketservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// Factory is a service.Factory returning a Service configured according to the
// provided configuration proto.
func Factory(conf *fspb.ClientServiceConfig) (service.Service, error) {
	ssConf := &sspb.Config{}
	if err := ptypes.UnmarshalAny(conf.Config, ssConf); err != nil {
		return nil, fmt.Errorf(
			"can't unmarshal the given ClientServiceConfig.config (%q): %v",
			conf.Config, err)
	}
	if ssConf.ApiProxyPath == "" {
		return nil, errors.New("api_proxy_path required")
	}

	return &Service{
		cfg:     ssConf,
		stop:    make(chan struct{}),
		msgs:    make(chan *fspb.Message),
		newChan: func() {},
	}, nil
}

// Service imlements service.Service which communicates with an agent process
// over a unix domain socket.
type Service struct {
	cfg *sspb.Config
	sc  service.Context
	l   *net.UnixListener

	stop chan struct{}      // closed to indicate that Stop() has been called
	msgs chan *fspb.Message // passes messages to the monitorChannel goroutine

	routines sync.WaitGroup // all active goroutines

	newChan func() // called when a new channel is created, meant for unit tests
}

// Start implements service.Service. Once it returns the service is listening
// for connections at the configured address.
func (s *Service) Start(sc service.Context) error {
	s.sc = sc

	// Ensure that the parent directory exists and that the socket itself does not.
	p := s.cfg.ApiProxyPath
	// We create the listener in this temporary location, chmod it, and then move
	// it. This prevents the client library from observing the listener before its
	// permissions are set.
	tmpP := p + ".tmp"
	parent := filepath.Dir(p)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return fmt.Errorf("os.MkdirAll failed: %v", err)
	}

	for _, f := range []string{p, tmpP} {
		if err := os.Remove(f); err != nil {
			if !os.IsNotExist(err) {
				return fmt.Errorf("os.Remove(%q): %v", f, err)
			}
		}
	}

	l, err := net.ListenUnix("unix", &net.UnixAddr{tmpP, "unix"})
	if err != nil {
		return fmt.Errorf("failed to create a Unix domain listener: %v", err)
	}

	if err := os.Chmod(tmpP, 0600); err != nil {
		return fmt.Errorf("failed to chmod a Unix domain listener: %v", err)
	}

	if err := os.Rename(tmpP, p); err != nil {
		return fmt.Errorf("failed to rename a Unix domain listener: %v", err)
	}

	if err := checks.CheckUnixSocket(p); err != nil {
		return fmt.Errorf("CheckUnixSocket(...): %v", err)
	}

	s.l = l
	s.routines.Add(1)
	go s.channelManagerLoop()

	return nil
}

// Stop implements service.Service.
func (s *Service) Stop() error {
	close(s.stop)
	s.routines.Wait()
	return nil
}

// ProcessMessage implements service.Service.
func (s *Service) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	select {
	case s.msgs <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type chanInfo struct {
	*channel.Channel
	ra    *channel.RelentlessAcknowledger
	uconn *net.UnixConn
	done  chan struct{}
}

func (s *Service) monitorChannel(ch *chanInfo) {
	defer func() {
		ch.uconn.Close()
		close(ch.done)
		s.routines.Done()
	}()
	for {
		select {
		case err := <-ch.Err:
			log.Printf("Channel closing with error: %v", err)
			return
		case m, ok := <-ch.ra.In:
			if !ok {
				return
			}
			if err := s.sc.Send(context.Background(), m); err != nil {
				log.Printf("Error sending message: %v", err)
			}
		}
	}
}

// feedChannel feeds ch.Out using messages read from s.msgs. If a message is provided, it
// is sent before reading from s.msgs.
//
// Returns true if shut down is occurring (if s.msgs has been closed), otherwise false.
//
// Returns a message if it has not been sent, but ch closed or died before
// accepting it.
func (s *Service) feedChannel(ch *chanInfo, msg *fspb.Message) (bool, *fspb.Message) {
	defer func() {
		ch.ra.Stop()
		close(ch.Out)
		// We are shutting down, and ch.ra is no longer draining ch.In, but the
		// Channel.readLoop goroutine might be locked trying to send. So we empty
		// it, and trust the other end to resend over the next channel.
		for range ch.In {
		}
	}()

	for {
		if msg == nil {
			select {
			case <-ch.done:
				return false, nil
			case <-s.stop:
				return true, nil
			case m, ok := <-s.msgs:
				if !ok {
					return true, nil
				}
				msg = m
			}
		}

		select {
		case <-ch.done:
			return false, msg
		case <-s.stop:
			return true, nil
		case ch.Out <- msg:
			msg = nil
		}
	}
}

func (s *Service) accept() *chanInfo {
	for {
		con, err := s.l.AcceptUnix()
		if err == nil {
			r := &chanInfo{
				Channel: channel.New(con, con),
				uconn:   con,
				done:    make(chan struct{}),
			}

			r.ra = channel.NewRelentlessAcknowledger(r.Channel, 100)
			return r
		}
		log.Printf("AcceptUnix returned error: %v", err)

		select {
		case <-s.stop:
			return nil
		case <-time.After(time.Second):
		}
	}
}

func (s *Service) channelManagerLoop() {
	defer s.routines.Done()
	defer s.l.Close()
	var msg *fspb.Message
	for {
		info := s.accept()
		if info == nil {
			return
		}
		s.newChan()
		s.routines.Add(1)
		go s.monitorChannel(info)

		var fin bool
		fin, msg = s.feedChannel(info, msg)
		if fin {
			return
		}
	}
}
