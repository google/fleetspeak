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
	"sync"
	"time"

	"log"
	"context"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/fleetspeak/fleetspeak/src/client/channel"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"

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

// Service implements service.Service which communicates with an agent process
// over a Unix domain socket (or a Windows named pipe).
type Service struct {
	cfg *sspb.Config
	sc  service.Context
	l   net.Listener

	stop chan struct{}      // closed to indicate that Stop() has been called
	msgs chan *fspb.Message // passes messages to the monitorChannel goroutine

	routines sync.WaitGroup // all active goroutines

	newChan func() // called when a new channel is created, meant for unit tests
}

// Start implements service.Service. Once it returns the service is listening
// for connections at the configured address.
func (s *Service) Start(sc service.Context) error {
	s.sc = sc

	l, err := listen(s.cfg.ApiProxyPath)
	if err != nil {
		return err
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
	ra   *channel.RelentlessAcknowledger
	conn net.Conn
	done chan struct{}
}

func (s *Service) monitorChannel(ch *chanInfo) {
	defer func() {
		ch.conn.Close()
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
		con, err := s.l.Accept()
		if err == nil {
			r := &chanInfo{
				Channel: channel.New(con, con),
				conn:    con,
				done:    make(chan struct{}),
			}

			r.ra = channel.NewRelentlessAcknowledger(r.Channel, 100)
			return r
		}
		log.Printf("Accept returned error: %v", err)

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
