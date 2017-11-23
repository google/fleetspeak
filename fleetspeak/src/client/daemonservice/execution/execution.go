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

// Package execution provides an abstraction for a single execution of a command
// with the context of daemonservice.
package execution

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"flag"
	"log"
	"context"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/fleetspeak/fleetspeak/src/client/channel"
	"github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/command"
	"github.com/google/fleetspeak/fleetspeak/src/client/internal/monitoring"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	dspb "github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/proto/fleetspeak_daemonservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

// We flush the output when either of these thresholds are met.
const (
	outFlushSize = 1 << 20 // 1MB
	outFlushTime = 5 * time.Second
)

var (
	// MaxStatsSamplePeriod is the period with which resource-usage data for the
	// Fleetspeak process will be fetched from the OS (but the first few samples
	// are collected more often, see resource_usage_monitor.go).
	MaxStatsSamplePeriod = 30 * time.Second

	// StatsSampleSize is the number of resource-usage query results that get aggregated into
	// a single resource-usage report sent to Fleetspeak servers.
	StatsSampleSize = 20

	// ErrShuttingDown is returned once an execution has started shutting down and
	// is no longer accepting messages.
	ErrShuttingDown = errors.New("shutting down")

	stdForward = flag.Bool("std_forward", false,
		"If set attaches the dependent service to the client's stdin, stdout, stderr. Meant for testing individual daemonservice integrations.")
)

// An Execution represents a specific execution of a daemonservice.
type Execution struct {
	daemonServiceName string

	Done chan struct{}        // Closed when this execution is dead or dying - essentially when Shutdown has been called.
	Out  chan<- *fspb.Message // Messages to send to the process go here. User should close when finished.

	sc        service.Context
	channel   *channel.Channel
	cmd       *command.Command
	StartTime time.Time

	outData *dspb.StdOutputData // The next output data to send, once full.
	lastOut time.Time           // Time of most recent output of outData.
	outLock sync.Mutex          // Protects outData, lastOut

	shutdown   sync.Once
	lastActive int64 // Time of the last message input or output, since epoch (UTC).

	dead       chan struct{} // closed when the underlying process has died.
	waitResult error         // result of Wait call - should only be read after dead is closed

	inProcess sync.WaitGroup // count of active goroutines
}

// New creates and starts an execution of the command described in cfg. Messages
// received from the resulting process are passed to sc, as are StdOutput and
// ResourceUsage messages.
func New(daemonServiceName string, cfg *dspb.Config, sc service.Context) (*Execution, error) {
	ret := Execution{
		daemonServiceName: daemonServiceName,

		Done: make(chan struct{}),

		sc:        sc,
		cmd:       &command.Command{Cmd: *exec.Command(cfg.Argv[0], cfg.Argv[1:]...)},
		StartTime: time.Now(),

		outData: &dspb.StdOutputData{},
		lastOut: time.Now(),

		dead: make(chan struct{}),
	}

	var err error
	ret.channel, err = ret.cmd.SetupCommsChannel()
	if err != nil {
		return nil, fmt.Errorf("failed to setup a comms channel: %v", err)
	}
	ret.Out = ret.channel.Out

	if *stdForward {
		log.Printf("std_forward is set, connecting std... to %s", cfg.Argv[0])
		ret.cmd.Stdin = os.Stdin
		ret.cmd.Stdout = os.Stdout
		ret.cmd.Stderr = os.Stderr
	} else {
		ret.cmd.Stdout = stdoutWriter{&ret}
		ret.cmd.Stderr = stderrWriter{&ret}
	}

	if err := ret.cmd.Start(); err != nil {
		close(ret.Done)
		return nil, err
	}

	ruf := monitoring.ResourceUsageFetcher{}
	initialRU, err := ruf.ResourceUsageForPID(ret.cmd.Process.Pid)
	if err != nil {
		log.Printf("Failed to get resource usage for daemon process: %v", err)
	}

	rum, err := monitoring.NewResourceUsageMonitor(
		ret.sc, ret.daemonServiceName, ret.cmd.Process.Pid, ret.StartTime, MaxStatsSamplePeriod, StatsSampleSize, ret.Done)
	if err != nil {
		rum = nil
		log.Printf("Failed to start resource-usage monitor: %v", err)
	}

	ret.inProcess.Add(4)
	go ret.flushLoop()
	go ret.inLoop()
	go func() {
		defer ret.inProcess.Done()
		if rum != nil {
			rum.StatsReporterLoop()
		}
	}()
	go func() {
		defer func() {
			ret.Shutdown()
			ret.inProcess.Done()
		}()
		ret.waitResult = ret.cmd.Wait()
		close(ret.dead)
		if ret.waitResult != nil {
			log.Printf("subprocess ended with error: %v", ret.waitResult)
		}
		if rum != nil && rum.StatsSent() || initialRU == nil {
			return
		}
		// Send resource-usage data to the server if the stats loop didn't get a chance to
		// do that e.g. for short-running processes.
		finalRU := ruf.ResourceUsageFromFinishedCmd(&ret.cmd.Cmd)
		aggRU, err := monitoring.AggregateResourceUsageForFinishedCmd(initialRU, finalRU)
		if err != nil {
			log.Printf("Aggregation of resource-usage data failed: %v", err)
			return
		}
		ctx, c := context.WithTimeout(context.Background(), time.Second)
		ret.sendStats(ctx, aggRU)
		c()
	}()

	return &ret, nil
}

// Wait waits for all aspects of this execution to finish. This should happen
// soon after shutdown is called.
//
// Note that while it is a bug for this to take more than some seconds, the
// method isn't needed in normal operation - it exists primarily for tests to
// ensure that resources are not leaked.
func (e *Execution) Wait() {
	<-e.Done
	e.channel.Wait()
	e.inProcess.Wait()
}

// LastActive returns the last time that a message was sent or received, to the
// nearest second.
func (e *Execution) LastActive() time.Time {
	return time.Unix(atomic.LoadInt64(&e.lastActive), 0).UTC()
}

func (e *Execution) setLastActive(t time.Time) {
	atomic.StoreInt64(&e.lastActive, t.Unix())
}

func dataSize(o *dspb.StdOutputData) int {
	return len(o.Stdout) + len(o.Stderr)
}

// flushOut flushes e.outData. It assumes that e.outLock is already held.
func (e *Execution) flushOut() {
	// set lastOut before the blocking call to sc.Send, so the next flush
	// has an accurate sense of how stale the data might be.
	n := time.Now()
	e.lastOut = n
	if dataSize(e.outData) == 0 {
		return
	}
	e.setLastActive(n)

	e.outData.Pid = int64(e.cmd.Process.Pid)
	d, err := ptypes.MarshalAny(e.outData)
	if err != nil {
		log.Printf("unable to marshal StdOutputData: %v", err)
	} else {
		e.sc.Send(context.Background(), service.AckMessage{
			M: &fspb.Message{
				MessageType: "StdOutput",
				Data:        d,
			}})
	}
	e.outData = &dspb.StdOutputData{
		MessageIndex: e.outData.MessageIndex + 1,
	}
}

func (e *Execution) writeToOut(p []byte, isErr bool) {
	e.outLock.Lock()
	defer e.outLock.Unlock()

	for {
		currSize := dataSize(e.outData)
		// If it all fits, write it and return.
		if currSize+len(p) <= outFlushSize {
			if isErr {
				e.outData.Stderr = append(e.outData.Stderr, p...)
			} else {
				e.outData.Stdout = append(e.outData.Stdout, p...)
			}
			return
		}
		// Write what does fit, flush, continue.
		toWrite := outFlushSize - currSize
		if isErr {
			e.outData.Stderr = append(e.outData.Stderr, p[:toWrite]...)
		} else {
			e.outData.Stdout = append(e.outData.Stdout, p[:toWrite]...)
		}
		p = p[toWrite:]
		e.flushOut()
	}
}

type stdoutWriter struct {
	e *Execution
}

func (w stdoutWriter) Write(p []byte) (int, error) {
	w.e.writeToOut(p, false)
	return len(p), nil
}

type stderrWriter struct {
	e *Execution
}

func (w stderrWriter) Write(p []byte) (int, error) {
	w.e.writeToOut(p, true)
	return len(p), nil
}

func (e *Execution) flushLoop() {
	defer e.inProcess.Done()
	t := time.NewTicker(outFlushTime)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			e.outLock.Lock()
			if e.lastOut.Add(outFlushTime).Before(time.Now()) {
				e.flushOut()
			}
			e.outLock.Unlock()
		case <-e.dead:
			e.outLock.Lock()
			e.flushOut()
			e.outLock.Unlock()
			return
		}
	}
}

func (e *Execution) waitForDeath(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-e.dead:
		return true
	case <-t.C:
		return false
	}
}

// Shutdown shuts down this execution.
func (e *Execution) Shutdown() {

	e.shutdown.Do(func() {
		// First we attempt a gentle shutdown. Closing e.Done tells our
		// user not to give us any more data, in response they should
		// close e.Out a.k.a e.channel.Out. Closing e.channel.Out will
		// cause channel to close the pipe to the dependent process,
		// which then causes it to clean up nicely.
		close(e.Done)

		if e.waitForDeath(time.Second) {
			return
		}
		// This pattern is technically racy - the process could end and the process
		// id could be recycled since the end of waitForDeath and before we SoftKill
		// or Kill using the process id.
		//
		// A formally correct way to implement this is to spawn a wrapper process
		// which does not die in response to SIGTERM but forwards the signal to the
		// wrapped process - its child. This would ensure that the process is still
		// around all the way to the SIGKILL.
		if err := e.cmd.SoftKill(); err != nil {
			log.Printf("SoftKill [%d] returned error: %v", e.cmd.Process.Pid, err)
		}
		if e.waitForDeath(time.Second) {
			return
		}
		if err := e.cmd.Kill(); err != nil {
			log.Printf("Kill [%d] returned error: %v", e.cmd.Process.Pid, err)
		}
		if e.waitForDeath(time.Second) {
			return
		}
		// It is hard to imagine how we might end up here - maybe the process is
		// somehow stuck in a system call or there is some other OS level weirdness.
		// One possibility is that cmd is a zombie process now.
		log.Printf("Subprocess [%d] appears to have survived SIGKILL.", e.cmd.Process.Pid)
	})
}

// inLoop reads messages from the dependent process and passes them to
// fleetspeak.
func (e *Execution) inLoop() {
	defer func() {
		e.Shutdown()
		e.inProcess.Done()
	}()
	for {
		select {
		case m, ok := <-e.channel.In:
			if !ok {
				return
			}
			e.setLastActive(time.Now())
			if err := e.sc.Send(context.Background(), service.AckMessage{M: m}); err != nil {
				log.Printf("error sending message to server: %v", err)
			}
		case err := <-e.channel.Err:
			log.Printf("channel produced error: %v", err)
			return
		}
	}
}

// sendStats attempts to send an mpb.ResourceUsageData proto to the server.
func (e *Execution) sendStats(ctx context.Context, aggRU *mpb.AggregatedResourceUsage) {
	startTime, err := ptypes.TimestampProto(e.StartTime)
	if err != nil {
		// Well, this really should never happen, since the field is set to the return
		// value of time.Now()
		log.Printf("Client start time timestamp failed validation check: %v", e.StartTime)
		return
	}
	rud := mpb.ResourceUsageData{
		Scope:            e.daemonServiceName,
		Pid:              int64(e.cmd.Process.Pid),
		ProcessStartTime: startTime,
		DataTimestamp:    ptypes.TimestampNow(),
		ResourceUsage:    aggRU,
	}
	d, err := ptypes.MarshalAny(&rud)
	if err != nil {
		log.Printf("unable to marshal ResourceUsageData: %v", err)
	} else {
		e.sc.Send(ctx, service.AckMessage{
			M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: "system"},
				MessageType: "ResourceUsage",
				Data:        d,
				Priority:    fspb.Message_LOW,
			},
		})
	}
}
