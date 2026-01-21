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
	"context"
	"errors"
	"fmt"
	"time"

	log "github.com/golang/glog"
	"github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/execution"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/common/fscontext"
	"golang.org/x/sync/errgroup"

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
	if err := conf.Config.UnmarshalTo(dsConf); err != nil {
		return nil, fmt.Errorf(
			"can't unmarshal the given ClientServiceConfig.config (%q): %v",
			conf.Config, err)
	}

	var timeout time.Duration
	if dsConf.InactivityTimeout != nil {
		if err := dsConf.InactivityTimeout.CheckValid(); err != nil {
			return nil, fmt.Errorf(
				"can't convert the given DaemonServiceConfig.inactivity_timeout (%q) to time.Duration: %v",
				dsConf.InactivityTimeout, err)
		}
		timeout = dsConf.InactivityTimeout.AsDuration()
		dsConf.LazyStart = true
	}

	ret := Service{
		name:              conf.Name,
		cfg:               dsConf,
		inactivityTimeout: timeout,

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

	stop func() error
	msgs chan *fspb.Message // passes messages to executionManager goroutine.
}

// Start starts the service, starting the subprocess unless LazyStart.
func (s *Service) Start(sc service.Context) error {
	if s.sc != nil {
		return errors.New("service already started")
	}
	s.sc = sc

	ctx, cancel := context.WithCancelCause(context.Background())
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return s.executionManagerLoop(ctx)
	})

	s.stop = func() error {
		cancel(fscontext.ErrStopRequested)
		return eg.Wait()
	}
	return nil
}

// Stop implements service.Service to stop the service. This kills the current subprocess.
// Returns the cause which caused the process to exit, or nil in case of an intentional shutdown.
func (s *Service) Stop() error {
	err := s.stop()
	if errors.Is(err, fscontext.ErrStopRequested) {
		return nil
	}
	return err
}

// newExec creates a new execution. It retries on failure, giving up only if ctx
// is canceled, in which case it returns the context's cancellation cause.
func (s *Service) newExec(ctx context.Context, lastStart time.Time) (*execution.Execution, error) {
	for {
		delta := time.Until(lastStart.Add(RespawnDelay))
		if delta > 0 {
			t := time.NewTimer(delta)
			select {
			case <-ctx.Done():
				t.Stop()
				return nil, context.Cause(ctx)
			case <-t.C:
			}
		}
		exec, err := execution.New(ctx, s.name, s.cfg, s.sc)
		if err != nil {
			lastStart = time.Now()
			log.Errorf("Execution of service [%s] failed, retrying: %v", s.name, err)
			continue
		}
		return exec, nil
	}
}

// monitorExecution monitors the process execution.  It returns nil when the
// context is canceled, or it returns an error when the process was inactive
// after inactivityTimeout.  Process activity is determined through
// e.LastActive().
func monitorExecution(ctx context.Context, e *execution.Execution, inactivityTimeout time.Duration) error {
	timeout := func() time.Duration {
		return time.Until(e.LastActive().Add(inactivityTimeout))
	}

	// Remark: A receive on a nil t.C channel blocks forever.
	var t = &time.Timer{}
	if inactivityTimeout > 0 {
		t = time.NewTimer(timeout())
		defer t.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if timeout() <= 0 {
				return fmt.Errorf("process inactive after %v", inactivityTimeout)
			}
			t.Reset(timeout())
		}
	}
}

// feedExecution feeds messages to the given execution, starting with msg if msg
// is non-nil, and then reading from s.msgs.  It exits when the context is
// canceled and returns the message for the next execution to process, along
// with the context's cancellation cause.
func (s *Service) feedExecution(ctx context.Context, msg *fspb.Message, e *execution.Execution) (*fspb.Message, error) {
	for {
		if msg == nil {
			select {
			case <-ctx.Done():
				return nil, context.Cause(ctx)
			case m, ok := <-s.msgs:
				if !ok {
					return nil, errors.New("feedExecution: input channel closed")
				}
				msg = m
			}
		}

		err := e.SendMsg(ctx, msg)
		if err != nil {
			return msg, err
		}
		msg = nil
	}
}

// executionManagerLoop supervises the process and reads the arriving Fleetspeak
// messages.  It returns on error, of if the outside context was canceled.
func (s *Service) executionManagerLoop(ctx context.Context) error {
	var lastStart time.Time
	var msg *fspb.Message
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		default:
		}

		if s.cfg.LazyStart {
			select {
			case <-ctx.Done():
				return context.Cause(ctx)
			case m, ok := <-s.msgs:
				if !ok {
					return nil // channel closed
				}
				msg = m
			}
		}

		// We use a separate errgroup for wait(2), because it is not a cancelable
		// operation.  At the same time, if wait returns, we should immediately
		// cancel all other goroutines as well.
		ctx, cancel := context.WithCancelCause(ctx)
		defer cancel(nil)
		waitEg, ctx := errgroup.WithContext(ctx)
		eg, ctx := errgroup.WithContext(ctx)

		ex, err := s.newExec(ctx, lastStart)
		if err != nil {
			return err
		}
		lastStart = time.Now()
		waitEg.Go(func() error {
			err := ex.Wait()
			if err != nil {
				cancel(fmt.Errorf("subprocess exited: %w", err))
			} else {
				cancel(fmt.Errorf("subprocess exited unexpectedly"))
			}
			return err
		})

		eg.Go(func() error {
			return monitorExecution(ctx, ex, s.inactivityTimeout)
		})

		eg.Go(func() error {
			fMsg, fErr := s.feedExecution(ctx, msg, ex)
			msg = fMsg // access synchronized through waitgroup
			return fErr
		})

		err = eg.Wait()
		ex.Shutdown()
		wErr := waitEg.Wait()
		if err == nil {
			err = wErr
		}

		s.sc.Stats().DaemonServiceSubprocessFinished(s.name, err)
		if err != nil && !errors.Is(err, fscontext.ErrStopRequested) {
			log.Warningf("Execution of %q ended spontaneously: %v", s.name, err)
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
