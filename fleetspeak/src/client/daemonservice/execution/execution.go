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
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/client/channel"
	"github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/command"
	"github.com/google/fleetspeak/fleetspeak/src/client/internal/monitoring"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	fcpb "github.com/google/fleetspeak/fleetspeak/src/client/channel/proto/fleetspeak_channel"
	dspb "github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/proto/fleetspeak_daemonservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

// We flush the output when either of these thresholds are met.
const (
	maxFlushBytes           = 1 << 20 // 1MB
	defaultFlushTimeSeconds = int32(60)
)

var (
	// ErrShuttingDown is returned once an execution has started shutting down and
	// is no longer accepting messages.
	ErrShuttingDown = errors.New("shutting down")

	stdForward = flag.Bool("std_forward", false,
		"If set attaches the dependent service to the client's stdin, stdout, stderr. Meant for testing individual daemonservice integrations.")

	// How long to wait for the daemon service to send startup data before starting to
	// monitor resource-usage.
	startupDataTimeout = 10 * time.Second
)

// An Execution represents a specific execution of a daemonservice.
type Execution struct {
	daemonServiceName string
	memoryLimit       int64
	samplePeriod      time.Duration
	sampleSize        int

	Done chan struct{}        // Closed when this execution is dead or dying - essentially when Shutdown has been called.
	Out  chan<- *fspb.Message // Messages to send to the process go here. User should close when finished.

	sc        service.Context
	channel   *channel.Channel
	cmd       *command.Command
	StartTime time.Time

	outData        *dspb.StdOutputData // The next output data to send, once full.
	lastOut        time.Time           // Time of most recent output of outData.
	outLock        sync.Mutex          // Protects outData, lastOut
	outFlushBytes  int                 // How many bytes trigger an output. Constant.
	outServiceName string              // The service to send StdOutput messages to. Constant.

	shutdown   sync.Once
	lastActive int64 // Time of the last message input or output in seconds since epoch (UTC), atomic access only.

	dead       chan struct{} // closed when the underlying process has died.
	waitResult error         // result of Wait call - should only be read after dead is closed

	inProcess   sync.WaitGroup         // count of active goroutines
	startupData chan *fcpb.StartupData // Startup data sent by the daemon process.

	heartbeat                        int64         // Time of the last input message in seconds since epoch (UTC), atomic access only.
	monitorHeartbeats                bool          // Whether to monitor the daemon process's hearbeats and kill unresponsive processes.
	heartbeatUnresponsiveGracePeriod time.Duration // How long to wait for initial heartbeat.
	heartbeatUnresponsiveKillPeriod  time.Duration // How long to wait for subsequent heartbeats.
	sending                          int32         // Non-zero when we are sending a messaging into FS. atomic access only.
}

// New creates and starts an execution of the command described in cfg. Messages
// received from the resulting process are passed to sc, as are StdOutput and
// ResourceUsage messages.
func New(daemonServiceName string, cfg *dspb.Config, sc service.Context) (*Execution, error) {
	cfg = proto.Clone(cfg).(*dspb.Config)

	ret := Execution{
		daemonServiceName: daemonServiceName,
		memoryLimit:       cfg.MemoryLimit,
		sampleSize:        int(cfg.ResourceMonitoringSampleSize),
		samplePeriod:      time.Duration(cfg.ResourceMonitoringSamplePeriodSeconds) * time.Second,

		Done: make(chan struct{}),

		sc:        sc,
		cmd:       &command.Command{Cmd: *exec.Command(cfg.Argv[0], cfg.Argv[1:]...)},
		StartTime: time.Now(),

		outData: &dspb.StdOutputData{},
		lastOut: time.Now(),

		dead:        make(chan struct{}),
		startupData: make(chan *fcpb.StartupData, 1),

		monitorHeartbeats:                cfg.MonitorHeartbeats,
		heartbeatUnresponsiveGracePeriod: time.Duration(cfg.HeartbeatUnresponsiveGracePeriodSeconds) * time.Second,
		heartbeatUnresponsiveKillPeriod:  time.Duration(cfg.HeartbeatUnresponsiveKillPeriodSeconds) * time.Second,
	}

	var err error
	ret.channel, err = ret.cmd.SetupCommsChannel()
	if err != nil {
		return nil, fmt.Errorf("failed to setup a comms channel: %v", err)
	}
	ret.Out = ret.channel.Out

	if cfg.StdParams != nil && cfg.StdParams.ServiceName == "" {
		log.Errorf("std_params is set, but service_name is empty. Ignoring std_params: %v", cfg.StdParams)
		cfg.StdParams = nil
	}

	if *stdForward {
		log.Warningf("std_forward is set, connecting std... to %s", cfg.Argv[0])
		ret.cmd.Stdin = os.Stdin
		ret.cmd.Stdout = os.Stdout
		ret.cmd.Stderr = os.Stderr
	} else if cfg.StdParams != nil {
		if cfg.StdParams.FlushBytes <= 0 || cfg.StdParams.FlushBytes > maxFlushBytes {
			cfg.StdParams.FlushBytes = maxFlushBytes
		}
		if cfg.StdParams.FlushTimeSeconds <= 0 {
			cfg.StdParams.FlushTimeSeconds = defaultFlushTimeSeconds
		}
		ret.outServiceName = cfg.StdParams.ServiceName
		ret.outFlushBytes = int(cfg.StdParams.FlushBytes)
		ret.cmd.Stdout = stdoutWriter{&ret}
		ret.cmd.Stderr = stderrWriter{&ret}
	} else {
		ret.cmd.Stdout = nil
		ret.cmd.Stderr = nil
	}

	if err := ret.cmd.Start(); err != nil {
		close(ret.Done)
		return nil, err
	}

	if cfg.StdParams != nil {
		ret.inProcess.Add(1)
		go ret.stdFlushRoutine(time.Second * time.Duration(cfg.StdParams.FlushTimeSeconds))
	}
	ret.inProcess.Add(2)
	go ret.inRoutine()
	if !cfg.DisableResourceMonitoring {
		ret.inProcess.Add(1)
		go ret.statsRoutine()
	}
	go func() {
		defer func() {
			ret.Shutdown()
			ret.inProcess.Done()
		}()
		ret.waitResult = ret.cmd.Wait()
		close(ret.dead)
		if ret.waitResult != nil {
			log.Warningf("subprocess ended with error: %v", ret.waitResult)
		}
		startTime, err := ptypes.TimestampProto(ret.StartTime)
		if err != nil {
			log.Errorf("Failed to convert process start time: %v", err)
			return
		}
		if !cfg.DisableResourceMonitoring {
			rud := &mpb.ResourceUsageData{
				Scope:             ret.daemonServiceName,
				Pid:               int64(ret.cmd.Process.Pid),
				ProcessStartTime:  startTime,
				DataTimestamp:     ptypes.TimestampNow(),
				ResourceUsage:     &mpb.AggregatedResourceUsage{},
				ProcessTerminated: true,
			}
			if err := monitoring.SendResourceUsage(rud, ret.sc); err != nil {
				log.Errorf("Failed to send final resource-usage proto: %v", err)
			}
		}
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

// getHeartbeat returns the last time that a message was received, to the nearest
// second.
func (e *Execution) getHeartbeat() time.Time {
	return time.Unix(atomic.LoadInt64(&e.heartbeat), 0).UTC()
}

func (e *Execution) recordHeartbeat() {
	atomic.StoreInt64(&e.heartbeat, time.Now().Unix())
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
		log.Errorf("unable to marshal StdOutputData: %v", err)
	} else {
		e.sc.Send(context.Background(), service.AckMessage{
			M: &fspb.Message{
				Destination: &fspb.Address{ServiceName: e.outServiceName},
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
		if currSize+len(p) <= e.outFlushBytes {
			if isErr {
				e.outData.Stderr = append(e.outData.Stderr, p...)
			} else {
				e.outData.Stdout = append(e.outData.Stdout, p...)
			}
			return
		}
		// Write what does fit, flush, continue.
		toWrite := e.outFlushBytes - currSize
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

func (e *Execution) stdFlushRoutine(flushTime time.Duration) {
	defer e.inProcess.Done()
	t := time.NewTicker(flushTime)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			e.outLock.Lock()
			if e.lastOut.Add(flushTime).Before(time.Now()) {
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
			log.Errorf("SoftKill [%d] returned error: %v", e.cmd.Process.Pid, err)
		}
		if e.waitForDeath(time.Second) {
			return
		}
		if err := e.cmd.Kill(); err != nil {
			log.Errorf("Kill [%d] returned error: %v", e.cmd.Process.Pid, err)
		}
		if e.waitForDeath(time.Second) {
			return
		}
		// It is hard to imagine how we might end up here - maybe the process is
		// somehow stuck in a system call or there is some other OS level weirdness.
		// One possibility is that cmd is a zombie process now.
		log.Errorf("Subprocess [%d] appears to have survived SIGKILL.", e.cmd.Process.Pid)
	})
}

// inRoutine reads messages from the dependent process and passes them to
// fleetspeak.
func (e *Execution) inRoutine() {
	defer func() {
		e.Shutdown()
		e.inProcess.Done()
	}()

	var startupDone bool

	for {
		m := e.readMsg()
		if m == nil {
			return
		}
		e.setLastActive(time.Now())
		e.recordHeartbeat()

		if m.Destination != nil && m.Destination.ServiceName == "system" {
			switch m.MessageType {
			case "StartupData":
				if startupDone {
					log.Warning("Received spurious startup message, ignoring.")
					break
				}
				startupDone = true

				sd := &fcpb.StartupData{}
				if err := ptypes.UnmarshalAny(m.Data, sd); err != nil {
					log.Warningf("Failed to parse startup data from initial message: %v", err)
				} else {
					e.startupData <- sd
				}
				close(e.startupData) // No more values to send through the channel.
			case "Heartbeat":
				// Pass, handled above.
			default:
				log.Warningf("Unknown system message type: %s", m.MessageType)
			}
		} else {
			atomic.StoreInt32(&e.sending, 1)
			if err := e.sc.Send(context.Background(), service.AckMessage{M: m}); err != nil {
				log.Errorf("error sending message to server: %v", err)
			}
			atomic.StoreInt32(&e.sending, 0)
		}

	}
}

// readMsg blocks until a message is available from the channel.
func (e *Execution) readMsg() *fspb.Message {
	select {
	case m, ok := <-e.channel.In:
		if !ok {
			return nil
		}
		return m
	case err := <-e.channel.Err:
		log.Errorf("channel produced error: %v", err)
		return nil
	}
}

// statsRoutine monitors the daemon process's resource usage, sending reports to the server
// at regular intervals.
func (e *Execution) statsRoutine() {
	defer e.inProcess.Done()
	pid := e.cmd.Process.Pid
	var version string
	select {
	case sd, ok := <-e.startupData:
		if ok {
			if int(sd.Pid) != pid {
				log.Infof("%s's self-reported PID (%d) is different from that of the process launched by Fleetspeak (%d)", e.daemonServiceName, sd.Pid, pid)
				pid = int(sd.Pid)
			}
			version = sd.Version
		} else {
			log.Warningf("%s startup data not received", e.daemonServiceName)
		}
	case <-time.After(startupDataTimeout):
		log.Warningf("%s startup data not received after %v", e.daemonServiceName, startupDataTimeout)
	case <-e.Done:
		return
	}

	if e.monitorHeartbeats {
		e.inProcess.Add(1)
		go e.heartbeatMonitorRoutine(pid)
	}

	rum, err := monitoring.New(e.sc, monitoring.ResourceUsageMonitorParams{
		Scope:            e.daemonServiceName,
		Pid:              pid,
		MemoryLimit:      e.memoryLimit,
		ProcessStartTime: e.StartTime,
		Version:          version,
		MaxSamplePeriod:  e.samplePeriod,
		SampleSize:       e.sampleSize,
		Done:             e.Done,
	})
	if err != nil {
		log.Errorf("Failed to create resource-usage monitor: %v", err)
		return
	}

	// This blocks until the daemon process terminates.
	rum.Run()
}

// heartbeatMonitorRoutine monitors the daemon process's hearbeats and kills
// unresponsive processes.
func (e *Execution) heartbeatMonitorRoutine(pid int) {
	defer e.inProcess.Done()

	// Give the child process some time to start up. During boot it sometimes
	// takes significantly more time than the unresponsive_kill_period to start
	// the child so we disable checking for heartbeats for a while.
	e.recordHeartbeat()
	sleepTime := e.heartbeatUnresponsiveGracePeriod

	for {
		select {
		case <-time.After(sleepTime):
		case <-e.dead:
			return
		}

		now := time.Now()
		var heartbeat time.Time
		if atomic.LoadInt32(&e.sending) != 0 {
			// We are blocked waiting for Send to complete, e.g. because there is no
			// network connection.  Treat this as if we got a heartbeat 'now'.
			heartbeat = now
		} else {
			heartbeat = e.getHeartbeat()
		}
		sleepTime = e.heartbeatUnresponsiveKillPeriod - now.Sub(heartbeat)
		if now.Sub(heartbeat) > e.heartbeatUnresponsiveKillPeriod {
			// There is a very unlikely race condition if the machine gets suspended
			// for longer than unresponsive_kill_period seconds so we give the client
			// some time to catch up.
			select {
			case <-time.After(2 * time.Second):
			case <-e.dead:
				return
			}

			heartbeat = e.getHeartbeat()
			if now.Sub(heartbeat) > e.heartbeatUnresponsiveKillPeriod && atomic.LoadInt32(&e.sending) == 0 {
				// We have not received a heartbeat in a while, kill the child.
				log.Warningf("No heartbeat received from %s (pid %d), killing.", e.daemonServiceName, pid)
				p := os.Process{Pid: pid}
				if err := p.Kill(); err != nil {
					log.Errorf("Error while killing a process that doesn't heartbeat - %s (pid %d): %v", e.daemonServiceName, pid, err)
				}
				return
			}
		}
	}
}
