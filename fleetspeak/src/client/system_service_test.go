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
	"os"
	"sync"
	"testing"
	"time"

	log "github.com/golang/glog"
	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/common/anypbtest"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

type fakeServiceContext struct {
	service.Context
	revokedCerts *fspb.RevokedCertificateList
	out          chan *fspb.Message
}

func (sc fakeServiceContext) Send(ctx context.Context, m service.AckMessage) error {
	log.Errorf("XYZ fake service context: Send(m) of %+v", m.M)
	select {
	case <-ctx.Done():
		log.Errorf("XYZ fake service context: Send(m) context canceled: %v cause: %v", ctx.Err(), context.Cause(ctx))
		return ctx.Err()
	case sc.out <- m.M:
		log.Errorf("XYZ fake service context: Send(m) OK")
		return nil
	}
}

func (sc fakeServiceContext) GetFileIfModified(_ context.Context, name string, _ time.Time) (data io.ReadCloser, mod time.Time, err error) {
	if name != "RevokedCertificates" {
		return nil, time.Time{}, fmt.Errorf("file [%v] not found", name)
	}
	d, err := proto.Marshal(sc.revokedCerts)
	if err != nil {
		log.Fatalf("Failed to marshal revokedCerts: %v", err)
	}
	return io.NopCloser(bytes.NewReader(d)), time.Now(), nil
}

type testEnv struct {
	client Client
	ctx    fakeServiceContext
}

func setUpTestEnv(t *testing.T) *testEnv {
	t.Helper()

	out := make(chan *fspb.Message)
	env := testEnv{
		client: Client{
			acks:      make(chan common.MessageID, 1),
			errs:      make(chan *fspb.MessageErrorData, 1),
			pid:       os.Getpid(),
			startTime: time.Unix(1234567890, 0),
		},
		ctx: fakeServiceContext{
			out:          out,
			revokedCerts: &fspb.RevokedCertificateList{},
		},
	}

	srv := systemService{
		client: &env.client,
	}
	err := srv.Start(&env.ctx)
	if err != nil {
		t.Fatalf("Starting test environment: %v", err)
	}
	t.Cleanup(func() {
		// Empty the out channel in parallel,
		// so that it can be written to during shutdown.
		var wg sync.WaitGroup
		defer wg.Wait()
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Errorf("XYZ srv.Stop draining out chan")
			for m := range out {
				log.Errorf("XYZ Discarded message after stop: %+v", m)
			}
		}()

		err := srv.Stop()
		if err != nil {
			t.Fatalf("Stopping test environment: %v", err)
		}
		close(out)
	})
	return &env
}

func TestAckMsg(t *testing.T) {
	env := setUpTestEnv(t)
	id, err := common.BytesToMessageID([]byte("00000000000000000000000000000001"))
	if err != nil {
		t.Fatalf("Unable to create MessageID: %v", err)
	}
	env.client.acks <- id
	res := <-env.ctx.out
	wantMessage := fspb.Message{
		Destination: &fspb.Address{
			ServiceName: "system",
		},
		MessageType: "MessageAck",
		Priority:    fspb.Message_HIGH,
		Background:  true,
	}
	wantMessage.Data = anypbtest.New(t, &fspb.MessageAckData{MessageIds: [][]byte{id.Bytes()}})
	if !proto.Equal(res, &wantMessage) {
		t.Errorf("got %+v for ack result, want %+v", res.String(), wantMessage.String())
	}
}

func TestErrorMsg(t *testing.T) {
	env := setUpTestEnv(t)
	ed := &fspb.MessageErrorData{
		MessageId: []byte("00000000000000000000000000000001"),
		Error:     "a terrible test error",
	}
	env.client.errs <- ed
	res := <-env.ctx.out
	wantMessage := fspb.Message{
		Destination: &fspb.Address{
			ServiceName: "system",
		},
		MessageType: "MessageError",
		Priority:    fspb.Message_HIGH,
		Background:  true,
	}
	wantMessage.Data = anypbtest.New(t, ed)
	if !proto.Equal(res, &wantMessage) {
		t.Errorf("got %+v for err result, want %+v", res.String(), wantMessage.String())
	}
}

func TestStatsMsg(t *testing.T) {
	// Note: this changes global state which should rather be configurable from
	// the outside.
	prevPeriod := StatsSamplePeriod
	prevSize := StatsSampleSize
	StatsSamplePeriod = 10 * time.Millisecond
	StatsSampleSize = 5
	defer func() {
		StatsSamplePeriod = prevPeriod
		StatsSampleSize = prevSize
	}()

	env := setUpTestEnv(t)
	statsTimeout := 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
	defer cancel()

	for {
		select {
		case res := <-env.ctx.out:
			log.Errorf("XYZ test res := <-env.ctx.out with res.MessageType=%q", res.MessageType)
			if res.MessageType != "ResourceUsage" {
				t.Errorf("Received message of unexpected type '%s'", res.MessageType)
				continue
			}
			rud := &mpb.ResourceUsageData{}
			if err := res.Data.UnmarshalTo(rud); err != nil {
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
		case <-ctx.Done():
			t.Errorf("No message received after %v", statsTimeout)
			return
		}
	}
}
