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
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"log"
	"context"
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

	return aggregateMemoryResourceUsage(prevRU, currRU, numRUCalls, aggRU)
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

func aggregateMemoryResourceUsage(prevRU *ResourceUsage, currRU *ResourceUsage, numRUCalls int, aggRU *mpb.AggregatedResourceUsage) error {
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
	sc      service.Context
	RChan   <-chan mpb.ResourceUsageData
	ErrChan <-chan error

	scope             string
	pid               int
	processStartTime  *tspb.Timestamp
	maxSamplePeriod   time.Duration
	initialSampleSize int
	sampleSize        int

	ruf      resourceUsageFetcherI
	rChan    chan<- mpb.ResourceUsageData
	errChan  chan<- error
	doneChan chan struct{}

	statsSent atomic.Value // whether the StatsReportLoop has sent at least one resource-usage report
}

// NewResourceUsageMonitor creates a new ResourceUsageMonitor and starts it in a separate goroutine.
func NewResourceUsageMonitor(sc service.Context, scope string, pid int, processStartTime time.Time, maxSamplePeriod time.Duration, sampleSize int, doneChan chan struct{}) (*ResourceUsageMonitor, error) {
	return newResourceUsageMonitor(sc, ResourceUsageFetcher{}, scope, pid, processStartTime, maxSamplePeriod, sampleSize, doneChan)
}

func newResourceUsageMonitor(sc service.Context, ruf resourceUsageFetcherI, scope string, pid int, processStartTime time.Time, maxSamplePeriod time.Duration, sampleSize int, doneChan chan struct{}) (*ResourceUsageMonitor, error) {
	startTimeProto, err := ptypes.TimestampProto(processStartTime)
	if err != nil {
		return nil, fmt.Errorf("process start time is invalid: %v", err)
	}

	if sampleSize < 2 {
		return nil, errors.New("sample size must be at least 2 (for rate computation)")
	}

	maxSamplePeriodSecs := int(maxSamplePeriod / time.Second)
	var backoffSize int
	if maxSamplePeriodSecs == 0 {
		backoffSize = 0
	} else {
		backoffSize = int(math.Log2(float64(maxSamplePeriodSecs)))
	}
	// First sample is bigger because of the backoff.
	initialSampleSize := sampleSize + backoffSize

	rChan := make(chan mpb.ResourceUsageData)
	errChan := make(chan error)
	m := ResourceUsageMonitor{
		sc:      sc,
		RChan:   rChan,
		ErrChan: errChan,

		scope:             scope,
		pid:               pid,
		processStartTime:  startTimeProto,
		maxSamplePeriod:   maxSamplePeriod,
		initialSampleSize: initialSampleSize,
		sampleSize:        sampleSize,

		ruf:      ruf,
		rChan:    rChan,
		errChan:  errChan,
		doneChan: doneChan,
	}

	go m.run()

	return &m, nil
}

// StatsSent returns whether or not the StatsReportLoop has sent at least one resource-usage report.
func (m *ResourceUsageMonitor) StatsSent() bool {
	return m.statsSent.Load() != nil
}

// StatsReporterLoop gathers stats from the dependent process and sends them to
// fleetspeek approximately once every (maxSamplePeriod * sampleSize).
func (m *ResourceUsageMonitor) StatsReporterLoop() {
	ruReported := false
	for {
		select {
		case <-m.doneChan:
			return
		case rud := <-m.RChan:
			d, err := ptypes.MarshalAny(&rud)
			if err != nil {
				log.Printf("Unable to marshal ResourceUsageData: %v", err)
				continue
			}
			ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
			m.sc.Send(ctx, service.AckMessage{
				M: &fspb.Message{
					Destination: &fspb.Address{ServiceName: "system"},
					MessageType: "ResourceUsage",
					Data:        d,
				},
			})
			if !ruReported {
				ruReported = true
				m.statsSent.Store(true)
			}
			c()
		case err := <-m.ErrChan:
			log.Printf("Resource-usage monitor encountered an error: %v", err)
			continue
		}
	}
}

func (m *ResourceUsageMonitor) run() {
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
				m.errChan <- fmt.Errorf("failed to get resource usage for process[%d]: %v", m.pid, err)
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
				m.errChan <- fmt.Errorf("aggregation error: %v", err)
				resetSamples()
				continue
			}

			prevRU = currRU
			numSamplesCollected++

			if numSamplesCollected == ss {
				m.send(aggRU)
				resetSamples()
			}
		}
	}
}

func (m *ResourceUsageMonitor) send(aggRU mpb.AggregatedResourceUsage) {
	debugStatus, err := m.ruf.DebugStatusForPID(m.pid)
	if err != nil {
		m.errChan <- fmt.Errorf("failed to get debug status for process[%d]: %v", m.pid, err)
	}
	m.rChan <- mpb.ResourceUsageData{
		Scope:            m.scope,
		Pid:              int64(m.pid),
		ProcessStartTime: m.processStartTime,
		DataTimestamp:    ptypes.TimestampNow(),
		ResourceUsage:    &aggRU,
		DebugStatus:      debugStatus,
	}
}
