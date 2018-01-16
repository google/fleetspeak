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

package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"testing"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/common"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

type fakeServiceContext struct {
	service.Context
	revokedCerts fspb.RevokedCertificateList
	out          chan fspb.Message
}

func (sc fakeServiceContext) Send(ctx context.Context, m service.AckMessage) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case sc.out <- *m.M:
		return nil
	}
}

func (sc fakeServiceContext) GetFileIfModified(_ context.Context, name string, _ time.Time) (data io.ReadCloser, mod time.Time, err error) {
	if name != "RevokedCertificates" {
		return nil, time.Time{}, fmt.Errorf("file [%v] not found", name)
	}
	d, err := proto.Marshal(&sc.revokedCerts)
	if err != nil {
		log.Fatalf("Failed to marshal revokedCerts: %v", err)
	}
	return ioutil.NopCloser(bytes.NewReader(d)), time.Now(), nil
}

type testEnv struct {
	c   Client
	ctx fakeServiceContext
	s   systemService
}

func (t *testEnv) Close() {
	t.s.Stop()
}

func newTestEnv() (*testEnv, error) {
	env := testEnv{}
	env.c = Client{
		acks:      make(chan common.MessageID, 1),
		errs:      make(chan *fspb.MessageErrorData, 1),
		pid:       os.Getpid(),
		startTime: time.Unix(1234567890, 0),
	}
	env.s = systemService{
		client: &env.c,
	}
	out := make(chan fspb.Message)
	env.ctx = fakeServiceContext{out: out}
	err := env.s.Start(&env.ctx)
	return &env, err
}

func TestAckMsg(t *testing.T) {
	env, err := newTestEnv()
	if err != nil {
		t.Fatalf("Failed to create testEnv: %v", err)
	}
	defer env.Close()
	id, err := common.BytesToMessageID([]byte("00000000000000000000000000000001"))
	if err != nil {
		t.Fatalf("Unable to create MessageID: %v", err)
	}
	env.c.acks <- id
	res := <-env.ctx.out
	wantMessage := fspb.Message{
		Destination: &fspb.Address{
			ServiceName: "system",
		},
		MessageType: "MessageAck",
		Priority:    fspb.Message_HIGH,
		Background:  true,
	}
	wantMessage.Data, err = ptypes.MarshalAny(&fspb.MessageAckData{MessageIds: [][]byte{id.Bytes()}})
	if err != nil {
		t.Errorf("Unable to marshal MessageAckData: %v", err)
	}
	if !proto.Equal(&res, &wantMessage) {
		t.Errorf("got %+v for ack result, want %+v", res, wantMessage)
	}
}

func TestErrorMsg(t *testing.T) {
	env, err := newTestEnv()
	if err != nil {
		t.Fatalf("Failed to create testEnv: %v", err)
	}
	defer env.Close()
	ed := fspb.MessageErrorData{
		MessageId: []byte("00000000000000000000000000000001"),
		Error:     "a terrible test error",
	}
	env.c.errs <- &ed
	res := <-env.ctx.out
	wantMessage := fspb.Message{
		Destination: &fspb.Address{
			ServiceName: "system",
		},
		MessageType: "MessageError",
		Priority:    fspb.Message_HIGH,
		Background:  true,
	}
	wantMessage.Data, err = ptypes.MarshalAny(&ed)
	if err != nil {
		t.Errorf("Unable to marshal MessageErrData: %v", err)
	}
	if !proto.Equal(&res, &wantMessage) {
		t.Errorf("got %+v for err result, want %+v", res, wantMessage)
	}
}

func TestStatsMsg(t *testing.T) {
	prevPeriod := StatsSamplePeriod
	prevSize := StatsSampleSize
	StatsSamplePeriod = 10 * time.Millisecond
	StatsSampleSize = 5
	defer func() {
		StatsSamplePeriod = prevPeriod
		StatsSampleSize = prevSize
	}()

	env, err := newTestEnv()
	if err != nil {
		t.Fatalf("Failed to create testEnv: %v", err)
	}
	defer env.Close()
	statsTimeout := 200 * time.Millisecond
	ticker := time.NewTicker(statsTimeout)
	defer ticker.Stop()

	for {
		select {
		case res := <-env.ctx.out:
			if res.MessageType != "ResourceUsage" {
				t.Errorf("Received message of unexpected type '%s'", res.MessageType)
				continue
			}
			var rud mpb.ResourceUsageData
			if err := ptypes.UnmarshalAny(res.Data, &rud); err != nil {
				t.Fatalf("Failed to unmarshal resource-usage data: %v", err)
			}
			if rud.Scope != "system" {
				t.Errorf("Expected scope 'system', got %s instead.", rud.Scope)
			}
			expectedStartTime := int64(1234567890)
			if rud.ProcessStartTime.Seconds != expectedStartTime {
				t.Errorf("Expected process_start_time to be %d, but it was %d", expectedStartTime, rud.ProcessStartTime.Seconds)
			}
			if rud.ResourceUsage.MeanResidentMemory <= 0.0 {
				t.Error("mean_resident_memory is not set")
			}
			if rud.ResourceUsage.MaxResidentMemory <= 0 {
				t.Error("max_resident_memory is not set")
			}
			return
		case <-ticker.C:
			t.Errorf("No message received after %v", statsTimeout)
			return
		}
	}
}
