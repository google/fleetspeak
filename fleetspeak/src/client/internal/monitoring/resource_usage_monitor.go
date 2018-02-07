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

package monitoring

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	tspb "github.com/golang/protobuf/ptypes/timestamp"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

const (
	epsilon float64 = 1e-4
)

// AggregateResourceUsage is a helper function for aggregating resource-usage data across multiple
// resource-usage queries. It should be called once, in sequence, for each ResourceUsage result.
//
// 'numRUCalls' is the number of resource-usage samples aggregated into one AggregatedResourceUsage
// proto; it is used to compute mean metrics.
// 'aggRU' is only updated if no error is encountered.
//
// We don't get memory usage data from finished commands. The commandFinished
// bool argument makes this function skip memory usage aggregation.
func AggregateResourceUsage(prevRU *ResourceUsage, currRU *ResourceUsage, numRUCalls int, aggRU *mpb.AggregatedResourceUsage, commandFinished bool) error {
	if numRUCalls < 2 {
		return errors.New("number of resource-usage calls should be at least 2 (for rate computation)")
	}
	if aggRU == nil {
		return errors.New("aggregated resource-usage proto should not be nil")
	}

	if prevRU == nil {
		if !proto.Equal(aggRU, &mpb.AggregatedResourceUsage{}) {
			return fmt.Errorf(
				"previous resource-usage is nil, but aggregated proto already has fields set: %v", aggRU)
		}
		aggRU.MeanResidentMemory = float64(currRU.ResidentMemory) / float64(numRUCalls)
		aggRU.MaxResidentMemory = currRU.ResidentMemory
		return nil
	}

	if !currRU.Timestamp.After(prevRU.Timestamp) {
		return fmt.Errorf(
			"timestamp for current resource-usage[%v] should be > that of previous resource-usage[%v]",
			currRU.Timestamp, prevRU.Timestamp)
	}

	if err := aggregateTimeResourceUsage(prevRU, currRU, numRUCalls, aggRU); err != nil {
		return err
	}

	if commandFinished {
		return nil
	}

	return aggregateMemoryResourceUsage(currRU, numRUCalls, aggRU)
}

func aggregateTimeResourceUsage(prevRU *ResourceUsage, currRU *ResourceUsage, numRUCalls int, aggRU *mpb.AggregatedResourceUsage) error {
	if currRU.UserCPUMillis+epsilon < prevRU.UserCPUMillis {
		return fmt.Errorf(
			"cumulative user-mode CPU-usage is not expected to decrease: [%v -> %v]",
			prevRU.UserCPUMillis, currRU.UserCPUMillis)
	}

	if currRU.SystemCPUMillis+epsilon < prevRU.SystemCPUMillis {
		return fmt.Errorf(
			"cumulative system-mode CPU-usage is not expected to decrease: [%v -> %v]",
			prevRU.SystemCPUMillis, currRU.SystemCPUMillis)
	}

	elapsedSecs := currRU.Timestamp.Sub(prevRU.Timestamp).Seconds()
	userCPURate := (currRU.UserCPUMillis - prevRU.UserCPUMillis) / elapsedSecs
	systemCPURate := (currRU.SystemCPUMillis - prevRU.SystemCPUMillis) / elapsedSecs

	// Note that since rates are computed between two consecutive data-points, their
	// average uses a sample size of n - 1, where n is the number of resource-usage queries.
	aggRU.MeanUserCpuRate += userCPURate / float64(numRUCalls-1)
	aggRU.MaxUserCpuRate = math.Max(userCPURate, aggRU.MaxUserCpuRate)
	aggRU.MeanSystemCpuRate += systemCPURate / float64(numRUCalls-1)
	aggRU.MaxSystemCpuRate = math.Max(systemCPURate, aggRU.MaxSystemCpuRate)
	return nil
}

func aggregateMemoryResourceUsage(currRU *ResourceUsage, numRUCalls int, aggRU *mpb.AggregatedResourceUsage) error {
	// Note that since rates are computed between two consecutive data-points, their
	// average uses a sample size of n - 1, where n is the number of resource-usage queries.
	aggRU.MeanResidentMemory += float64(currRU.ResidentMemory) / float64(numRUCalls)
	if currRU.ResidentMemory > aggRU.MaxResidentMemory {
		aggRU.MaxResidentMemory = currRU.ResidentMemory
	}
	return nil
}

// AggregateResourceUsageForFinishedCmd computes resource-usage for a finished process, given
// resource-usage before and after the process ran.
func AggregateResourceUsageForFinishedCmd(initialRU, finalRU *ResourceUsage) (*mpb.AggregatedResourceUsage, error) {
	aggRU := mpb.AggregatedResourceUsage{}
	err := AggregateResourceUsage(nil, initialRU, 2, &aggRU, true)
	if err != nil {
		return nil, err
	}
	err = AggregateResourceUsage(initialRU, finalRU, 2, &aggRU, true)
	if err != nil {
		return nil, err
	}

	// If this field is untouched, we have not aggregated memory resource usage
	// for this process yet. We fill it in with what we have.
	// TODO
	if aggRU.MaxResidentMemory == 0 {
		aggRU.MeanResidentMemory = float64(initialRU.ResidentMemory)
		aggRU.MaxResidentMemory = initialRU.ResidentMemory
	}

	return &aggRU, nil
}

// Interface for ResourceUsageFetcher, to facilitate stubbing out of the real fetcher in tests.
type resourceUsageFetcherI interface {
	ResourceUsageForPID(pid int) (*ResourceUsage, error)
	DebugStatusForPID(pid int) (string, error)
}

// ResourceUsageMonitor computes resource-usage metrics for a process and delivers them periodically
// via a channel.
type ResourceUsageMonitor struct {
	sc service.Context

	scope             string
	pid               int
	version           string
	processStartTime  *tspb.Timestamp
	maxSamplePeriod   time.Duration
	initialSampleSize int
	sampleSize        int

	ruf      resourceUsageFetcherI
	errChan  chan<- error
	doneChan <-chan struct{}
}

type ResourceUsageMonitorParams struct {
	// What we are monitoring. Typicaly a service name, or 'system' for the
	// Fleetspeak client itself.
	Scope string

	// The version string of the service that we are monitoring, if known.
	Version string

	// The process id that we are monitoring.
	Pid int

	// The time that the processes was started (if known).
	ProcessStartTime time.Time

	// The longest time to wait between samples.
	MaxSamplePeriod time.Duration

	// The number of resource-usage query results that get aggregated into
	// a single resource-usage report sent to Fleetspeak servers.
	SampleSize int

	// The resource monitor will shut down when this channel is closed.
	Done <-chan struct{}

	// If set, the resource monitor will report errors on this channel. If unset,
	// errors will be logged.
	Err chan<- error

	// If set, stubs out the actual resource fetching. Meant for use only in unit tests.
	ruf resourceUsageFetcherI
}

func New(sc service.Context, params ResourceUsageMonitorParams) (*ResourceUsageMonitor, error) {
	var startTimeProto *tspb.Timestamp
	var err error

	if !params.ProcessStartTime.IsZero() {
		startTimeProto, err = ptypes.TimestampProto(params.ProcessStartTime)
		if err != nil {
			return nil, fmt.Errorf("process start time is invalid: %v", err)
		}
	}

	if params.SampleSize < 2 {
		return nil, fmt.Errorf("sample size %d invalid - must be at least 2 (for rate computation)", params.SampleSize)
	}

	maxSamplePeriodSecs := int(params.MaxSamplePeriod / time.Second)
	var backoffSize int
	if maxSamplePeriodSecs == 0 {
		backoffSize = 0
	} else {
		backoffSize = int(math.Log2(float64(maxSamplePeriodSecs)))
	}
	// First sample is bigger because of the backoff.
	initialSampleSize := params.SampleSize + backoffSize

	if params.ruf == nil {
		params.ruf = ResourceUsageFetcher{}
	}

	m := ResourceUsageMonitor{
		sc: sc,

		scope:             params.Scope,
		pid:               params.Pid,
		version:           params.Version,
		processStartTime:  startTimeProto,
		maxSamplePeriod:   params.MaxSamplePeriod,
		initialSampleSize: initialSampleSize,
		sampleSize:        params.SampleSize,

		ruf:      params.ruf,
		doneChan: params.Done,
		errChan:  params.Err,
	}

	return &m, nil
}

// Run is the business method of the resource-usage monitor. It blocks until doneChan is closed.
func (m *ResourceUsageMonitor) Run() {
	min := func(a, b time.Duration) time.Duration {
		if b < a {
			return b
		}
		return a
	}

	// 1s, 2s, 4s, 8s, 16s, ..., m.maxSamplePeriod, m.maxSamplePeriod, m.maxSamplePeriod, ...
	backoffPeriod := min(time.Second, m.maxSamplePeriod)
	a := time.After(backoffPeriod)
	initialSample := true

	var prevRU *ResourceUsage
	aggRU := mpb.AggregatedResourceUsage{}
	numSamplesCollected := 0

	resetSamples := func() {
		prevRU = nil
		aggRU = mpb.AggregatedResourceUsage{}
		numSamplesCollected = 0
		initialSample = false
	}

	for {
		select {
		case <-m.doneChan:
			return
		case <-a:
			backoffPeriod = min(backoffPeriod*2, m.maxSamplePeriod)
			a = time.After(backoffPeriod)

			currRU, err := m.ruf.ResourceUsageForPID(m.pid)
			if err != nil {
				m.errorf("failed to get resource usage for process[%d]: %v", m.pid, err)
				resetSamples()
				continue
			}

			var ss int
			if initialSample {
				ss = m.initialSampleSize
			} else {
				ss = m.sampleSize
			}

			err = AggregateResourceUsage(prevRU, currRU, ss, &aggRU, false)
			if err != nil {
				m.errorf("aggregation error: %v", err)
				resetSamples()
				continue
			}

			prevRU = currRU
			numSamplesCollected++

			if numSamplesCollected == ss {
				debugStatus, err := m.ruf.DebugStatusForPID(m.pid)
				if err != nil {
					m.errorf("failed to get debug status for process[%d]: %v", m.pid, err)
				}
				rud := &mpb.ResourceUsageData{
					Scope:            m.scope,
					Pid:              int64(m.pid),
					ProcessStartTime: m.processStartTime,
					Version:          m.version,
					DataTimestamp:    ptypes.TimestampNow(),
					ResourceUsage:    &aggRU,
					DebugStatus:      debugStatus,
				}
				if err := SendResourceUsage(rud, m.sc); err != nil {
					m.errorf("failed to send resource-usage data to the server: %v", err)
					continue
				}
				resetSamples()
			}
		}
	}
}

func (m *ResourceUsageMonitor) errorf(format string, a ...interface{}) {
	err := fmt.Errorf(format, a...)
	if m.errChan == nil {
		log.Errorf("Resource-usage monitor encountered an error: %v", err)
	} else {
		m.errChan <- err
	}
}

// SendResourceUsage packages up resource-usage data and sends it to the server.
func SendResourceUsage(rud *mpb.ResourceUsageData, sc service.Context) error {
	d, err := ptypes.MarshalAny(rud)
	if err != nil {
		return err
	}
	ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
	defer c()
	return sc.Send(ctx, service.AckMessage{
		M: &fspb.Message{
			Destination: &fspb.Address{ServiceName: "system"},
			MessageType: "ResourceUsage",
			Data:        d,
			Priority:    fspb.Message_LOW,
			Background:  true,
		},
	})
}
