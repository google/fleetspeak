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

// Package daemonservice implements a service which runs and communicates with a
// separate daemon subprocess.
package daemonservice

import (
	"fmt"
	"sync"
	"time"

	"log"
	"context"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/execution"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	dspb "github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/proto/fleetspeak_daemonservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// RespawnDelay is the minimum time between process starts. It exists to limit
// the speed of crash loops, it is variable to support testing.
var RespawnDelay = time.Minute

// TODO migrate log.* calls to report errors to the server, once we
// have a mechanism to do so.

// Factory is a service.Factory returning a Service configured according to the
// provided configuration proto.
func Factory(conf *fspb.ClientServiceConfig) (service.Service, error) {
	dsConf := &dspb.Config{}
	if err := ptypes.UnmarshalAny(conf.Config, dsConf); err != nil {
		return nil, fmt.Errorf(
			"can't unmarshal the given ClientServiceConfig.config (%q): %v",
			conf.Config, err)
	}

	var timeout time.Duration
	if dsConf.InactivityTimeout != nil {
		var err error
		timeout, err = ptypes.Duration(dsConf.InactivityTimeout)
		if err != nil {
			return nil, fmt.Errorf(
				"can't convert the given DaemonServiceConfig.inactivity_timeout (%q) to time.Duration: %v",
				dsConf.InactivityTimeout, err)
		}
		dsConf.LazyStart = true
	}

	ret := Service{
		name:              conf.Name,
		cfg:               dsConf,
		inactivityTimeout: timeout,

		stop: make(chan struct{}),
		msgs: make(chan *fspb.Message),
	}
	return &ret, nil
}

// Service implements service.Service, delegating message processing to a subprocess.
type Service struct {
	name              string
	cfg               *dspb.Config
	sc                service.Context
	inactivityTimeout time.Duration

	stop chan struct{}      // closed to indicate that Stop() has been called
	msgs chan *fspb.Message // passes messages to executionManager goroutine.

	routines sync.WaitGroup // currently active goroutines and executions.
}

// Start starts the service, starting the subprocess unless LazyStart.
func (s *Service) Start(sc service.Context) error {
	s.sc = sc

	s.routines.Add(1)
	go s.executionManagerLoop()

	return nil
}

// Stop implements service.Service to stop the service. This kills the current subprocess.
func (s *Service) Stop() error {
	close(s.stop)
	s.routines.Wait()
	return nil
}

// newExec creates a new execution. It retries on failure, giving up only
// if stop is closed, in which case it returns nil.
func (s *Service) newExec(lastStart time.Time) *execution.Execution {
	for {
		delta := time.Until(lastStart.Add(RespawnDelay))
		if delta > 0 {
			t := time.NewTimer(delta)
			select {
			case <-s.stop:
				t.Stop()
				return nil
			case <-t.C:
			}
		}
		exec, err := execution.New(s.name, s.cfg, s.sc)
		if err == nil {
			return exec
		}
		lastStart = time.Now()
		log.Printf("Execution of service [%s] failed, retrying: %v", s.name, err)
	}
}

func (s *Service) monitorExecution(e *execution.Execution) {
	defer s.routines.Done()
	defer e.Wait()

	var t <-chan time.Time
	for {
		if s.inactivityTimeout > 0 {
			t = time.After(time.Until(e.LastActive().Add(s.inactivityTimeout)))
		}
		select {
		case <-e.Done:
			log.Printf("Execution of [%s] ended spontaneously.", s.name)
			return
		case <-s.stop:
			return
		case <-t:
			if time.Now().After(e.LastActive().Add(s.inactivityTimeout)) {
				e.Shutdown()
				return
			}
		}
	}
}

// feedExecution feeds messages to the given execution, starting with msg if msg
// is non-nil, and then reading from s.msgs. It returns whether the service is
// shutting down, and, if it is not shutting down, may return a message for the
// next execution to process.
func (s *Service) feedExecution(msg *fspb.Message, e *execution.Execution) (bool, *fspb.Message) {
	defer func() { go e.Shutdown() }()
	defer close(e.Out)
	for {
		if msg == nil {
			select {
			case <-e.Done:
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
		case <-e.Done:
			return false, msg
		case <-s.stop:
			return true, nil
		case e.Out <- msg:
			msg = nil
		}
	}
}

func (s *Service) executionManagerLoop() {
	defer s.routines.Done()
	var lastStart time.Time
	for {
		var msg *fspb.Message
		if s.cfg.LazyStart {
			select {
			case <-s.stop:
				return
			case m, ok := <-s.msgs:
				if !ok {
					return
				}
				msg = m
			}
		}

		ex := s.newExec(lastStart)
		lastStart = time.Now()
		if ex == nil {
			return
		}

		s.routines.Add(1)
		go s.monitorExecution(ex)

		var stopping bool
		stopping, msg = s.feedExecution(msg, ex)
		if stopping {
			return
		}
	}
}

// ProcessMessage implements service.Service by passing m to the dependent subprocess.
func (s *Service) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	select {
	case s.msgs <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
