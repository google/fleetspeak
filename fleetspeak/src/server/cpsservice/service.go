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

// Package cpsservice defines a service.Service which publishes all received messages to
// a Google Cloud Pub/Sub topic.
package cpsservice

import (
	"context"

	"google.golang.org/protobuf/proto"

	"cloud.google.com/go/pubsub"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"

	cpspb "github.com/google/fleetspeak/fleetspeak/src/server/cpsservice/proto/fleetspeak_cpsservice"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// CPSService is a service.Service which publishes all received
// messages to a Cloud Pub/Sub topic.
type CPSService struct {
	topic *pubsub.Topic
}

// Start is a noop for CPSService, but part of the Service interface.
func (s *CPSService) Start(sctx service.Context) error {
	return nil
}

// Stop will wait for all in-flight messages to be published and
// will clean up worker goroutines before returning.
func (s *CPSService) Stop() error {
	s.topic.Stop()
	return nil
}

// ProcessMessage will publish the message to the service CPS topic
// and wait for/return the publish operation result.
func (s *CPSService) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	data, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	res := s.topic.Publish(ctx, &pubsub.Message{Data: data})
	_, err = res.Get(ctx)
	return err
}

// Factory creates and returns a CPSService object, as defined by
// the configuration in cfg.
func Factory(cfg *spb.ServiceConfig) (service.Service, error) {
	conf := cpspb.Config{}
	if err := cfg.GetConfig().UnmarshalTo(&conf); err != nil {
		return nil, err
	}

	client, err := pubsub.NewClient(context.Background(), conf.GetProject())
	if err != nil {
		return nil, err
	}

	s := CPSService{topic: client.Topic(conf.GetTopic())}
	return &s, nil
}
