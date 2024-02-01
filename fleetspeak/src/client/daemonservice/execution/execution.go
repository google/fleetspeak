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
	"google.golang.org/protobuf/proto"
	anypb "google.golang.org/protobuf/types/known/anypb"
	tspb "google.golang.org/protobuf/types/known/timestamppb"

	"github.com/google/fleetspeak/fleetspeak/src/client/channel"
	"github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/command"
	"github.com/google/fleetspeak/fleetspeak/src/client/internal/monitoring"
	"github.com/google/fleetspeak/fleetspeak/src/client/internal/process"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	fcpb "github.com/google/fleetspeak/fleetspeak/src/client/channel/proto/fleetspeak_channel"
	dspb "github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/proto/fleetspeak_daemonservice"
	"github.com/google/fleetspeak/fleetspeak/src/common/fscontext"
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
)

type atomicString struct {
	v atomic.Value
}

func (s *atomicString) Set(val string) {
	s.v.Store(val)
}

func (s *atomicString) Get() string {
	stored := s.v.Load()
	if stored == nil {
		return ""
	}
	return stored.(string)
}

type atomicBool struct {
	v atomic.Value
}

func (b *atomicBool) Set(val bool) {
	b.v.Store(val)
}

func (b *atomicBool) Get() bool {
	stored := b.v.Load()
	if stored == nil {
		return false
	}
	return stored.(bool)
}

type atomicTime struct {
	v atomic.Value
}

func (t *atomicTime) Set(val time.Time) {
	t.v.Store(val)
}

func (t *atomicTime) Get() time.Time {
	stored := t.v.Load()
	if stored == nil {
		return time.Unix(0, 0).UTC()
	}
	return stored.(time.Time)
}

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

	dead chan struct{} // closed when the underlying process has died.

	waitForGoroutines func()
	startupData       chan *fcpb.StartupData // Startup data sent by the daemon process.

	heartbeat               atomicTime    // Time when the last message was received from the daemon process.
	monitorHeartbeats       bool          // Whether to monitor the daemon process's heartbeats, killing it if it doesn't heartbeat often enough.
	initialHeartbeatTimeout time.Duration // How long to wait for the initial heartbeat.
	heartbeatTimeout        time.Duration // How long to wait for subsequent heartbeats.
	sending                 atomicBool    // Indicates whether a message-send operation is in progress.
	serviceVersion          atomicString  // Version reported by the daemon process.
}

var errProcessTerminated = errors.New("process terminated")

// New creates and starts an execution of the command described in cfg. Messages
// received from the resulting process are passed to sc, as are StdOutput and
// ResourceUsage messages.
func New(daemonServiceName string, cfg *dspb.Config, sc service.Context) (*Execution, error) {
	cfg = proto.Clone(cfg).(*dspb.Config)

	var wg sync.WaitGroup

	ret := Execution{
		daemonServiceName: daemonServiceName,
		memoryLimit:       cfg.MemoryLimit,
		sampleSize:        int(cfg.ResourceMonitoringSampleSize),
		samplePeriod:      cfg.ResourceMonitoringSamplePeriod.AsDuration(),

		Done: make(chan struct{}),

		sc:        sc,
		cmd:       &command.Command{Cmd: *exec.Command(cfg.Argv[0], cfg.Argv[1:]...)},
		StartTime: time.Now(),

		outData: &dspb.StdOutputData{},
		lastOut: time.Now(),

		dead:              make(chan struct{}),
		startupData:       make(chan *fcpb.StartupData, 1),
		waitForGoroutines: wg.Wait,

		monitorHeartbeats:       cfg.MonitorHeartbeats,
		initialHeartbeatTimeout: cfg.HeartbeatUnresponsiveGracePeriod.AsDuration(),
		heartbeatTimeout:        cfg.HeartbeatUnresponsiveKillPeriod.AsDuration(),
	}

	if ret.initialHeartbeatTimeout <= 0 {
		ret.initialHeartbeatTimeout = time.Duration(cfg.HeartbeatUnresponsiveGracePeriodSeconds) * time.Second
	}
	if ret.heartbeatTimeout <= 0 {
		ret.heartbeatTimeout = time.Duration(cfg.HeartbeatUnresponsiveKillPeriodSeconds) * time.Second
	}
	if ret.samplePeriod <= 0 {
		ret.samplePeriod = time.Duration(cfg.ResourceMonitoringSamplePeriodSeconds) * time.Second
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
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := fscontext.WithDoneChan(context.TODO(), ErrShuttingDown, ret.dead)
			defer cancel(nil)
			period := time.Second * time.Duration(cfg.StdParams.FlushTimeSeconds)
			ret.stdFlushRoutine(ctx, period)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		ret.inRoutine()
	}()
	if !cfg.DisableResourceMonitoring {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := fscontext.WithDoneChan(context.TODO(), errProcessTerminated, ret.dead)
			defer cancel(nil)

			ret.statsRoutine(ctx)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer ret.Shutdown()
		waitResult := ret.cmd.Wait()
		close(ret.dead)
		if waitResult != nil {
			log.Warningf("subprocess ended with error: %v", waitResult)
		}
		startTime := tspb.New(ret.StartTime)
		if err := startTime.CheckValid(); err != nil {
			log.Errorf("Failed to convert process start time: %v", err)
			return
		}
		if !cfg.DisableResourceMonitoring {
			rud := &mpb.ResourceUsageData{
				Scope:             ret.daemonServiceName,
				Pid:               int64(ret.cmd.Process.Pid),
				ProcessStartTime:  startTime,
				DataTimestamp:     tspb.Now(),
				ResourceUsage:     &mpb.AggregatedResourceUsage{},
				ProcessTerminated: true,
			}
			ctx := context.TODO()
			if err := monitoring.SendProtoToServer(ctx, rud, "ResourceUsage", ret.sc); err != nil {
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
	e.waitForGoroutines()
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
	d, err := anypb.New(e.outData)
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

// stdFlushRoutine calls e.flushOut() periodically with e.outLock held.
// When ctx is canceled, it does it one last time and returns.
func (e *Execution) stdFlushRoutine(ctx context.Context, period time.Duration) {
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			e.outLock.Lock()
			if e.lastOut.Add(period).Before(time.Now()) {
				e.flushOut()
			}
			e.outLock.Unlock()
		case <-ctx.Done():
			e.outLock.Lock()
			e.flushOut()
			e.outLock.Unlock()
			return
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return
	case <-t.C:
		return
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

		// Context bound to the process lifetime
		ctx, cancel := fscontext.WithDoneChan(context.TODO(), errProcessTerminated, e.dead)
		defer cancel(nil)

		sleepCtx(ctx, time.Second)
		if ctx.Err() != nil {
			return
		}

		// This pattern is technically racy - the process could end and the process
		// id could be recycled since the end of sleepCtx and before we SoftKill
		// or Kill using the process id.
		//
		// A formally correct way to implement this is to spawn a wrapper process
		// which does not die in response to SIGTERM but forwards the signal to the
		// wrapped process - its child. This would ensure that the process is still
		// around all the way to the SIGKILL.
		if err := e.cmd.SoftKill(); err != nil {
			log.Errorf("SoftKill [%d] returned error: %v", e.cmd.Process.Pid, err)
		}
		sleepCtx(ctx, time.Second)
		if ctx.Err() != nil {
			return
		}

		if err := e.cmd.Kill(); err != nil {
			log.Errorf("Kill [%d] returned error: %v", e.cmd.Process.Pid, err)
		}
		sleepCtx(ctx, time.Second)
		if ctx.Err() != nil {
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
	defer e.Shutdown()

	var startupDone bool

	for {
		m := e.readMsg()
		if m == nil {
			return
		}
		e.setLastActive(time.Now())
		e.heartbeat.Set(time.Now())

		if m.Destination != nil && m.Destination.ServiceName == "system" {
			switch m.MessageType {
			case "StartupData":
				if startupDone {
					log.Warning("Received spurious startup message, ignoring.")
					break
				}
				startupDone = true

				sd := &fcpb.StartupData{}
				if err := m.Data.UnmarshalTo(sd); err != nil {
					log.Warningf("Failed to parse startup data from initial message: %v", err)
				} else {
					if sd.Version != "" {
						e.serviceVersion.Set(sd.Version)
					}
					e.startupData <- sd
				}
				close(e.startupData) // No more values to send through the channel.
			case "Heartbeat":
				// Pass, handled above.
			default:
				log.Warningf("Unknown system message type: %s", m.MessageType)
			}
		} else {
			e.sending.Set(true)
			// sc.Send() buffers the message locally for sending to the Fleetspeak server. It will
			// block if the buffer is full.
			if err := e.sc.Send(context.Background(), service.AckMessage{M: m}); err != nil {
				log.Errorf("error sending message to server: %v", err)
			}
			e.sending.Set(false)
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

// waitForStartupData waits for startup data from e.startupData
// and returns early on context cancelation
func (e *Execution) waitForStartupData(ctx context.Context) (pid int, version string) {
	pid = e.cmd.Process.Pid

	// How long to wait for the daemon service to send startup data before
	// starting to monitor resource-usage.
	const startupDataTimeout = 10 * time.Second

	ctx, cancel := context.WithTimeout(ctx, startupDataTimeout)
	defer cancel()

	select {
	case sd, ok := <-e.startupData:
		if !ok {
			// Channel closed.
			log.Warningf("%s startup data not received", e.daemonServiceName)
			return 0, ""
		}
		if int(sd.Pid) != pid {
			log.Infof("%s's self-reported PID (%d) is different from that of the process launched by Fleetspeak (%d)", e.daemonServiceName, sd.Pid, pid)
			pid = int(sd.Pid)
		}
		version = sd.Version
		return pid, version
	case <-ctx.Done():
		log.Warningf("%s startup data not received due to %v (cause: %v)", e.daemonServiceName, ctx.Err(), context.Cause(ctx))
		return 0, ""
	}
}

// statsRoutine monitors the daemon process's resource usage, sending reports to the server
// at regular intervals.
func (e *Execution) statsRoutine(ctx context.Context) {
	pid, version := e.waitForStartupData(ctx)

	select {
	case <-ctx.Done():
		return
	default:
	}

	var wg sync.WaitGroup
	defer wg.Wait()
	if e.monitorHeartbeats {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.heartbeatMonitorRoutine(ctx, pid)
		}()
	}

	rum, err := monitoring.New(e.sc, monitoring.ResourceUsageMonitorParams{
		Scope:            e.daemonServiceName,
		Pid:              pid,
		MemoryLimit:      e.memoryLimit,
		ProcessStartTime: e.StartTime,
		Version:          version,
		MaxSamplePeriod:  e.samplePeriod,
		SampleSize:       e.sampleSize,
	})
	if err != nil {
		log.Errorf("Failed to create resource-usage monitor: %v", err)
		return
	}

	// This blocks until ctx is canceled.
	rum.Run(ctx)
}

// busySleep sleeps *roughly* for the given duration, not counting the time when
// the Fleetspeak process is suspended.  It returns early when ctx is canceled.
func busySleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()

	for d > 0 {
		select {
		case <-timer.C:
			step := min(d, time.Second)
			timer.Reset(step)
			d = d - step
		case <-ctx.Done():
			return
		}
	}
}

// heartbeatMonitorRoutine monitors the daemon process's heartbeats and kills
// unresponsive processes.
//
// It runs until ctx is canceled.
func (e *Execution) heartbeatMonitorRoutine(ctx context.Context, pid int) {
	// Give the child process some time to start up. During boot it sometimes
	// takes significantly more time than the unresponsive_kill_period to start
	// the child so we disable checking for heartbeats for a while.
	e.heartbeat.Set(time.Now())
	busySleep(ctx, e.initialHeartbeatTimeout)
	if ctx.Err() != nil {
		return
	}

	for {
		if e.sending.Get() { // Blocked trying to buffer a message for sending to the FS server.
			busySleep(ctx, e.heartbeatTimeout)
			if ctx.Err() != nil {
				return
			}
			continue
		}
		timeSinceLastHB := time.Since(e.heartbeat.Get())
		if timeSinceLastHB > e.heartbeatTimeout {
			// There is a very unlikely race condition if the machine gets suspended
			// for longer than unresponsive_kill_period seconds so we give the client
			// some time to catch up.
			busySleep(ctx, 2*time.Second)
			if ctx.Err() != nil {
				return
			}

			timeSinceLastHB = time.Since(e.heartbeat.Get())
			if timeSinceLastHB > e.heartbeatTimeout && !e.sending.Get() {
				// We have not received a heartbeat in a while, kill the child.
				log.Warningf("No heartbeat received from %s (pid %d), killing.", e.daemonServiceName, pid)

				// For consistency with MEMORY_EXCEEDED kills, send a notification before attempting to
				// kill the process.
				startTime := tspb.New(e.StartTime)
				if err := startTime.CheckValid(); err != nil {
					log.Errorf("Failed to convert process start time: %v", err)
					startTime = nil
				}
				kn := &mpb.KillNotification{
					Service:          e.daemonServiceName,
					Pid:              int64(pid),
					ProcessStartTime: startTime,
					KilledWhen:       tspb.Now(),
					Reason:           mpb.KillNotification_HEARTBEAT_FAILURE,
				}
				if version := e.serviceVersion.Get(); version != "" {
					kn.Version = version
				}
				if err := monitoring.SendProtoToServer(ctx, kn, "KillNotification", e.sc); err != nil {
					log.Errorf("Failed to send kill notification to server: %v", err)
				}

				if err := process.KillByPid(pid); err != nil {
					log.Errorf("Error while killing a process that doesn't heartbeat - %s (pid %d): %v", e.daemonServiceName, pid, err)
					continue // Keep retrying.
				}
				return
			}
		}
		// Sleep until when the next heartbeat is due.
		busySleep(ctx, e.heartbeatTimeout-timeSinceLastHB)
		if ctx.Err() != nil {
			return
		}
	}
}
