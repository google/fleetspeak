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
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"context"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	tspb "github.com/golang/protobuf/ptypes/timestamp"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

type fakeResourceUsageFetcher struct {
	resourceUsageFetcherI
	ruList           []ResourceUsage // Fake resource-usage data to cycle through
	statusList       []string        // Fake debug status strings to cycle through
	ruCTR, statusCTR int
}

func (f *fakeResourceUsageFetcher) ResourceUsageForPID(pid int) (*ResourceUsage, error) {
	ret := f.ruList[f.ruCTR]
	f.ruCTR++
	f.ruCTR %= len(f.ruList)
	return &ret, nil
}

func (f *fakeResourceUsageFetcher) DebugStatusForPID(pid int) (string, error) {
	ret := f.statusList[f.statusCTR]
	f.statusCTR++
	f.statusCTR %= len(f.statusList)
	return ret, nil
}

func (f *fakeResourceUsageFetcher) setResourceUsageData(ruList []ResourceUsage) {
	f.ruList = ruList
}

func (f *fakeResourceUsageFetcher) setDebugStatusStrings(statusList []string) {
	f.statusList = statusList
}

type fakeServiceContext struct {
	service.Context
}

func (sc fakeServiceContext) Send(ctx context.Context, m service.AckMessage) error {
	// No-Op
	return nil
}

func (sc fakeServiceContext) GetFileIfModified(_ context.Context, name string, _ time.Time) (data io.ReadCloser, mod time.Time, err error) {
	// No-Op
	return nil, time.Time{}, nil
}

func TestNewResourceUsageMonitor(t *testing.T) {
	start := time.Now()
	sc := fakeServiceContext{}
	pid := 1234
	fakeStart := int64(1234567890)
	samplePeriod := 10 * time.Millisecond
	sampleSize := 2
	timeout := 100 * time.Millisecond
	doneChan := make(chan struct{})

	fakeRU0 := ResourceUsage{
		Timestamp:       start,
		UserCPUMillis:   13.0,
		SystemCPUMillis: 5.0,
		ResidentMemory:  10000,
	}
	fakeRU1 := ResourceUsage{
		Timestamp:       start.Add(10 * time.Millisecond),
		UserCPUMillis:   27.0,
		SystemCPUMillis: 9.0,
		ResidentMemory:  30000,
	}
	ruf := fakeResourceUsageFetcher{}
	ruf.setResourceUsageData([]ResourceUsage{fakeRU0, fakeRU1})
	ruf.setDebugStatusStrings([]string{"Fake Debug Status 0", "Fake Debug Status 1"})

	rum, err := newResourceUsageMonitor(
		sc, &ruf, "test-scope", pid, time.Unix(fakeStart, 0), samplePeriod, sampleSize, doneChan)
	if err != nil {
		t.Fatal(err)
	}
	timeoutTicker := time.NewTicker(timeout)
	defer timeoutTicker.Stop()

	protosReceived := 0
	for {
		select {
		case got := <-rum.RChan:
			if err != nil {
				t.Fatal(err)
			}
			want := mpb.ResourceUsageData{
				Scope:            "test-scope",
				Pid:              int64(pid),
				ProcessStartTime: &tspb.Timestamp{Seconds: fakeStart},
				ResourceUsage: &mpb.AggregatedResourceUsage{
					MeanUserCpuRate:    1400.0,
					MaxUserCpuRate:     1400.0,
					MeanSystemCpuRate:  400.0,
					MaxSystemCpuRate:   400.0,
					MeanResidentMemory: 20000.0,
					MaxResidentMemory:  30000,
				},
				DebugStatus:   fmt.Sprintf("Fake Debug Status %d", protosReceived),
				DataTimestamp: got.DataTimestamp,
			}
			if !proto.Equal(&got, &want) {
				t.Errorf(
					"Resource-usage proto %d received is not what we expect; got:\n%q\nwant:\n%q", protosReceived, got, want)
			}

			timestamp, err := ptypes.Timestamp(got.DataTimestamp)
			if err != nil {
				t.Fatal(err)
			}
			if timestamp.Before(start) {
				t.Errorf(
					"Timestamp for resource-usage proto %d [%v] is expected to be after %v",
					protosReceived, timestamp, start)
			}
			protosReceived++
			if protosReceived >= 2 {
				return
			}
		case err := <-rum.ErrChan:
			t.Error(err)
			return
		case <-timeoutTicker.C:
			t.Errorf("Received %d resource-usage protos after %v. Wanted 2.", protosReceived, timeout)
			return
		}
	}
}

func TestStatsReporterLoop(t *testing.T) {
	var wg sync.WaitGroup

	sc := fakeServiceContext{}
	start := time.Now()
	pid := 1234
	fakeStart := int64(1234567890)
	samplePeriod := 100 * time.Millisecond
	sampleSize := 2
	doneChan := make(chan struct{})

	fakeRU0 := ResourceUsage{
		Timestamp:       start,
		UserCPUMillis:   13.0,
		SystemCPUMillis: 5.0,
		ResidentMemory:  10000,
	}
	fakeRU1 := ResourceUsage{
		Timestamp:       start.Add(10 * time.Millisecond),
		UserCPUMillis:   27.0,
		SystemCPUMillis: 9.0,
		ResidentMemory:  30000,
	}

	ruf := fakeResourceUsageFetcher{}
	ruf.setResourceUsageData([]ResourceUsage{fakeRU0, fakeRU1})
	ruf.setDebugStatusStrings([]string{"Fake Debug Status 0", "Fake Debug Status 1"})

	rum, err := newResourceUsageMonitor(
		sc, &ruf, "test-scope", pid, time.Unix(fakeStart, 0), samplePeriod, sampleSize, doneChan)
	if err != nil {
		t.Fatal(err)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		rum.StatsReporterLoop()
	}()
	time.Sleep(250 * time.Millisecond)
	close(doneChan)
	wg.Wait()
	if !rum.StatsSent() {
		t.Error("stats were not sent")
	}
}
