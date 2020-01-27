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
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/client/internal/monitoring"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

var (
	// StatsSamplePeriod is the frequency with which resource-usage data for the Fleetspeak
	// process will be fetched from the OS.
	StatsSamplePeriod = 30 * time.Second

	// StatsSampleSize is the number of resource-usage query results that get aggregated into
	// a single resource-usage report sent to Fleetspeak servers.
	StatsSampleSize = 20
)

const (
	// SuicideExitCode is used as a distinctive exit code to signify a client committing suicide.
	SuicideExitCode = 7
)

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

func (s *systemService) Start(sc service.Context) error {
	s.sc = sc
	s.done = make(chan struct{})
	rum, err := monitoring.New(s.sc, monitoring.ResourceUsageMonitorParams{
		Scope:            "system",
		Pid:              s.client.pid,
		ProcessStartTime: s.client.startTime,
		MaxSamplePeriod:  StatsSamplePeriod,
		SampleSize:       StatsSampleSize,
		Done:             s.done,
	})
	if err != nil {
		rum = nil
		log.Errorf("Failed to start resource-usage monitor: %v", err)
	}
	s.wait.Add(4)
	// TODO: call pollRevokedCerts on startup.
	go s.ackLoop()
	go s.errLoop()
	go s.cfgLoop()
	go func() {
		defer s.wait.Done()
		if rum != nil {
			rum.Run()
		}
	}()
	return nil
}

func (s *systemService) ProcessMessage(_ context.Context, m *fspb.Message) error {
	switch m.MessageType {
	case "RekeyRequest":
		if err := s.client.config.Rekey(); err != nil {
			// Very unlikely.
			return fmt.Errorf("unable to rekey client: %v", err)
		}
		s.client.config.SendConfigUpdate()
	case "Die":
		dr := &fspb.DieRequest{}
		if err := ptypes.UnmarshalAny(m.Data, dr); err != nil {
			return fmt.Errorf("can't unmarshal DieRequest: %v", err)
		}
		if dr.Force {
			log.Info("Committing forced suicide on request.")
			os.Exit(SuicideExitCode)
		} else {
			log.Info("Committing graceful suicide on request.")
			// Stop the service and exit in a goroutine. As the "system" service
			// is currently processing the "Die" message, trying to stop it would
			// deadlock. We have to let the ProcessMessage return in order for
			// s.client.Stop() to complete.
			go func() {
				s.client.Stop()
				os.Exit(SuicideExitCode)
			}()
		}

	case "RestartService":
		rs := &fspb.RestartServiceRequest{}
		if err := ptypes.UnmarshalAny(m.Data, rs); err != nil {
			return fmt.Errorf("can't unmarshal RestartServiceRequest: %v", err)
		}
		log.Infof("Restarting service %s", rs.Name)

		if err := s.client.sc.RestartService(rs.Name); err != nil {
			log.Errorf("Failed to restart service '%s': %v", rs.Name, err)
			return err
		}
		log.Infof("Restarted service '%s'", rs.Name)
	default:
		return fmt.Errorf("unable to process message of type: %v", m.MessageType)
	}

	return nil
}

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
			if err := s.sc.Send(ctx, service.AckMessage{
				M: &fspb.Message{
					Destination: &fspb.Address{ServiceName: "system"},
					MessageType: "MessageAck",
					Priority:    fspb.Message_HIGH,
					Data:        d,
					Background:  true,
				},
			}); err != nil {
				log.Errorf("error acknowledging message: %v", err)
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
			if err := s.sc.Send(ctx, service.AckMessage{
				M: &fspb.Message{
					Destination: &fspb.Address{ServiceName: "system"},
					MessageType: "MessageError",
					Priority:    fspb.Message_HIGH,
					Data:        d,
					Background:  true,
				},
			}); err != nil {
				log.Errorf("error reporting message error: %v", err)
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
			if err := s.sc.Send(ctx, service.AckMessage{
				M: &fspb.Message{
					Destination: &fspb.Address{ServiceName: "system"},
					MessageType: "ClientInfo",
					Priority:    fspb.Message_HIGH,
					Data:        d,
					Background:  true,
				},
			}); err != nil {
				log.Errorf("error reporting configuration change: %v", err)
			}
			c()
		}
	}
}

func (s *systemService) pollRevokedCerts() {
	ctx, c := context.WithTimeout(context.Background(), 30*time.Second)
	defer c()
	data, _, err := s.sc.GetFileIfModified(ctx, "RevokedCertificates", time.Time{})
	if err != nil {
		log.Errorf("Unable to get revoked certificate list: %v", err)
		return
	}
	defer data.Close()

	b, err := ioutil.ReadAll(data)
	if err != nil {
		log.Errorf("Unable to read revoked certificate list: %v", err)
		return
	}
	if len(b) == 0 {
		return
	}
	var l fspb.RevokedCertificateList
	if err := proto.Unmarshal(b, &l); err != nil {
		log.Errorf("Unable to parse revoked certificate list: %v", err)
		return
	}
	s.client.config.AddRevokedSerials(l.Serials)
}
