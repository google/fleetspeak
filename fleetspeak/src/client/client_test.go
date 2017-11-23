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
	"reflect"
	"strings"
	"testing"

	"context"

	"github.com/google/fleetspeak/fleetspeak/src/client/config"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/common"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// A fakeService implements Service and passes all received messages into a channel.
type fakeService struct {
	c  chan *fspb.Message
	sc service.Context
}

func (s *fakeService) Start(sc service.Context) error {
	s.sc = sc
	return nil
}

func (s *fakeService) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	s.c <- m
	return nil
}

func (s *fakeService) Stop() error {
	return nil
}

func TestMessageDelivery(t *testing.T) {
	msgs := make(chan *fspb.Message)
	fs := fakeService{c: msgs}

	fakeServiceFactory := func(*fspb.ClientServiceConfig) (service.Service, error) {
		return &fs, nil
	}

	cl, err := New(
		config.Configuration{
			Ephemeral:     true,
			FixedServices: []*fspb.ClientServiceConfig{{Name: "FakeService", Factory: "FakeService"}},
		},
		Components{
			ServiceFactories: map[string]service.Factory{
				"NOOP":        service.NOOPFactory,
				"FakeService": fakeServiceFactory,
			},
		})
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}

	mid, err := common.RandomMessageID()
	if err != nil {
		t.Fatalf("unable to create message id: %v", err)
	}

	if err := cl.ProcessMessage(context.Background(),
		service.AckMessage{
			M: &fspb.Message{
				MessageId: mid.Bytes(),
				Source:    &fspb.Address{ServiceName: "FakeService"},
				Destination: &fspb.Address{
					ClientId:    cl.config.ClientID().Bytes(),
					ServiceName: "FakeService"},
			}}); err != nil {
		t.Fatalf("unable to process message: %v", err)
	}

	m := <-msgs

	if !bytes.Equal(m.MessageId, mid.Bytes()) {
		t.Errorf("Expected message with id: %v, got: %v", mid, m)
	}

	// Check that the service.Context provided to fs has a working LocalInfo.
	// TODO: move this to a separate test.
	li := fs.sc.GetLocalInfo()
	if li.ClientID != cl.config.ClientID() {
		t.Errorf("Expected LocalInfo to have ClientID: %v, got %v", cl.config.ClientID(), li.ClientID)
	}
	if !reflect.DeepEqual(li.Services, []string{"FakeService"}) {
		t.Errorf("Expected LocalInfo to be %v, got %v", []string{"FakeService"}, li.Services)
	}

	cl.Stop()
}

func TestMessageValidation(t *testing.T) {
	msgs := make(chan *fspb.Message)
	fs := fakeService{c: msgs}

	fakeServiceFactory := func(*fspb.ClientServiceConfig) (service.Service, error) {
		return &fs, nil
	}

	cl, err := New(
		config.Configuration{
			Ephemeral:     true,
			FixedServices: []*fspb.ClientServiceConfig{{Name: "FakeService", Factory: "FakeService"}},
		},
		Components{
			ServiceFactories: map[string]service.Factory{
				"NOOP":        service.NOOPFactory,
				"FakeService": fakeServiceFactory,
			},
		})
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}

	for _, tc := range []struct {
		m    fspb.Message
		want string
	}{
		{m: fspb.Message{},
			want: "destination must have ServiceName",
		},
		{m: fspb.Message{Destination: &fspb.Address{ServiceName: ""}},
			want: "destination must have ServiceName",
		},
		{m: fspb.Message{Destination: &fspb.Address{ServiceName: "FakeService", ClientId: []byte("abcdef")}},
			want: "cannot send directly to client",
		},
	} {
		err := cl.ProcessMessage(context.Background(), service.AckMessage{M: &tc.m})
		if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
			t.Errorf("ProcessMessage(%v) should give error starting with [%v] but got [%v]", tc.m, tc.want, err)
		}
	}

	cl.Stop()
}
