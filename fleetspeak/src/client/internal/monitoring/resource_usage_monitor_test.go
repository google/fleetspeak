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
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/client/clitesting"

	tspb "github.com/golang/protobuf/ptypes/timestamp"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
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

func TestResourceUsageMonitor(t *testing.T) {
	start := time.Now()
	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message),
	}
	pid := 1234
	fakeStart := int64(1234567890)
	samplePeriod := 100 * time.Millisecond
	sampleSize := 2
	doneChan := make(chan struct{})
	errChan := make(chan error)

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

	rum, err := New(&sc, ResourceUsageMonitorParams{
		Scope:            "test-scope",
		Pid:              pid,
		ProcessStartTime: time.Unix(fakeStart, 0),
		MaxSamplePeriod:  samplePeriod,
		SampleSize:       sampleSize,
		Done:             doneChan,
		Err:              errChan,
		ruf:              &ruf,
	})
	if err != nil {
		t.Fatal(err)
	}
	go rum.Run()

	timeout := 500 * time.Millisecond
	timeoutWhen := time.After(timeout)

	for protosReceived := 0; protosReceived < 2; protosReceived++ {
		select {
		case m := <-sc.OutChan:
			var got mpb.ResourceUsageData
			if err := ptypes.UnmarshalAny(m.Data, &got); err != nil {
				t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
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
					"Resource-usage proto %d received is not what we expect; got:\n%v\nwant:\n%v", protosReceived, got, want)
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
		case err := <-errChan:
			t.Error(err)
		case <-timeoutWhen:
			t.Fatalf("Received %d resource-usage protos after %v. Wanted 2.", protosReceived, timeout)
		}
	}
}
