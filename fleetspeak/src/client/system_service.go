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
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	"log"
	"context"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/channel"
	"github.com/google/fleetspeak/fleetspeak/src/client/monitoring"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

// StatsPeriod is the interval with which resource-usage stats for the
// Fleetspeak service are sent to the server. Configurable for testing.
var StatsPeriod = 10 * time.Minute

// systemService implements Service. It handles messages for the built in
// 'system' service. It is installed directly by client.New and is given direct
// access to the resulting Client object.
type systemService struct {
	client        *Client
	done          chan struct{}
	sc            service.Context
	configChanges <-chan *fspb.ClientInfoData
	wait          sync.WaitGroup
}

// Start implements Service.
func (s *systemService) Start(sc service.Context) error {
	s.sc = sc
	s.done = make(chan struct{})
	s.wait.Add(4)
	// TODO: call pollRevokedCerts on startup.
	go s.ackLoop()
	go s.errLoop()
	go s.cfgLoop()
	go s.statsLoop()
	return nil
}

// ProcessMessage implements Service.
func (s *systemService) ProcessMessage(_ context.Context, m *fspb.Message) error {
	// TODO: Add support to handle incoming messages that, e.g.,
	// configure a new service on the client or request a rekey.
	return fmt.Errorf("unable to process message of type: %v", m.MessageType)
}

// Stop implements Service.
func (s *systemService) Stop() error {
	close(s.done)
	s.wait.Wait()
	return nil
}

func (s *systemService) ackLoop() {
	defer s.wait.Done()
	for {
		select {
		case <-s.done:
			return
		case mid := <-s.client.acks:
			a := fspb.MessageAckData{MessageIds: [][]byte{mid.Bytes()}}
			t := time.NewTimer(time.Second)
		groupLoop:
			for {
				select {
				case <-s.done:
					t.Stop()
					return
				case mid = <-s.client.acks:
					a.MessageIds = append(a.MessageIds, mid.Bytes())
				case <-t.C:
					break groupLoop
				}
			}
			d, err := ptypes.MarshalAny(&a)
			if err != nil {
				log.Fatalf("Unable to marshal MessageAckData: %v", err)
			}
			ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
			if err := s.sc.Send(ctx, channel.AckMessage{
				M: &fspb.Message{
					Destination: &fspb.Address{ServiceName: "system"},
					MessageType: "MessageAck",
					Priority:    fspb.Message_HIGH,
					Data:        d,
				},
			}); err != nil {
				log.Printf("error acknowledging message: %v", err)
			}
			c()
		}
	}
}

func (s *systemService) errLoop() {
	defer s.wait.Done()
	for {
		select {
		case <-s.done:
			return
		case e := <-s.client.errs:
			d, err := ptypes.MarshalAny(e)
			if err != nil {
				log.Fatalf("unable to marshal MessageErrData: %v", err)
			}
			ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
			if err := s.sc.Send(ctx, channel.AckMessage{
				M: &fspb.Message{
					Destination: &fspb.Address{ServiceName: "system"},
					MessageType: "MessageError",
					Priority:    fspb.Message_HIGH,
					Data:        d,
				},
			}); err != nil {
				log.Printf("error reporting message error: %v", err)
			}
			c()
		}
	}
}

func (s *systemService) cfgLoop() {
	defer s.wait.Done()
	certTicker := time.NewTicker(time.Hour)
	defer certTicker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-certTicker.C:
			s.pollRevokedCerts()
		case chg := <-s.configChanges:
			d, err := ptypes.MarshalAny(chg)
			if err != nil {
				log.Fatalf("unable to marshal ClientInfoData: %v", err)
			}
			ctx, c := context.WithTimeout(context.Background(), 5*time.Minute)
			if err := s.sc.Send(ctx, channel.AckMessage{
				M: &fspb.Message{
					Destination: &fspb.Address{ServiceName: "system"},
					MessageType: "ClientInfo",
					Priority:    fspb.Message_HIGH,
					Data:        d,
				},
			}); err != nil {
				log.Printf("error reporting configuration change: %v", err)
			}
			c()
		}
	}
}

func (s *systemService) statsLoop() {
	defer s.wait.Done()
	statsTicker := time.NewTicker(StatsPeriod)
	defer statsTicker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-statsTicker.C:
			s.reportResourceUsage()
		}
	}
}

func (s *systemService) reportResourceUsage() {
	ru, err := monitoring.ResourceUsageForPID(s.client.pid)
	if err != nil {
		log.Printf(
			"Error getting resource usage for Fleetspeak process[%d]: %v", s.client.pid, err)
		return
	}
	startTime, err := ptypes.TimestampProto(s.client.startTime)
	if err != nil {
		// Well, this really should never happen, since the field is set to the return
		// value of time.Now()
		log.Printf("Client start time timestamp failed validation check: %v", s.client.startTime)
	}
	rud := mpb.ResourceUsageData{
		Scope:            "system",
		Pid:              int64(s.client.pid),
		ProcessStartTime: startTime,
		ResourceUsage:    ru,
	}
	d, err := ptypes.MarshalAny(&rud)
	if err != nil {
		log.Printf("Unable to marshal ResourceUsageData: %v", err)
		return
	}
	ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
	defer c()
	if err := s.sc.Send(ctx, channel.AckMessage{
		M: &fspb.Message{
			Destination: &fspb.Address{ServiceName: "system"},
			MessageType: "ResourceUsage",
			Data:        d,
		},
	}); err != nil {
		log.Printf("Error sending resource usage data: %v", err)
	}
}

func (s *systemService) pollRevokedCerts() {
	ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
	defer c()
	data, _, err := s.sc.GetFileIfModified(ctx, "RevokedCertificates", time.Time{})
	if err != nil {
		log.Printf("Unable to get revoked certificate list: %v", err)
		return
	}
	defer data.Close()

	b, err := ioutil.ReadAll(data)
	if err != nil {
		log.Printf("Unable to read revoked certificate list: %v", err)
		return
	}
	if len(b) == 0 {
		return
	}
	var l fspb.RevokedCertificateList
	if err := proto.Unmarshal(b, &l); err != nil {
		log.Printf("Unable to parse revoked certificate list: %v", err)
		return
	}
	s.client.config.AddRevokedSerials(l.Serials)
}
