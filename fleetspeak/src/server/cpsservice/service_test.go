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

package cpsservice

import (
	"context"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/google/fleetspeak/fleetspeak/src/common/anypbtest"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"
	"google.golang.org/protobuf/proto"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	cpspb "github.com/google/fleetspeak/fleetspeak/src/server/cpsservice/proto/fleetspeak_cpsservice"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

func requireEmulation(t *testing.T) {
	if os.Getenv("PUBSUB_EMULATOR_HOST") == "" {
		t.Skipf("Cloud Pub/Sub emulator not found")
	}
}

type testEnv struct {
	svc service.Service
	sub *pubsub.Subscription
}

func createTestEnv(t *testing.T) testEnv {
	t.Helper()
	requireEmulation(t)

	svc, err := Factory(&spb.ServiceConfig{
		Config: anypbtest.New(t, &cpspb.Config{
			Project: "test-project",
			Topic:   "test-topic",
		}),
	})
	if err != nil {
		t.Fatalf("Error creating service: %v", err)
	}
	t.Cleanup(func() { svc.Stop() })

	client, err := pubsub.NewClient(context.Background(), "test-project")
	if err != nil {
		t.Fatalf("Error creating test client: %v", err)
	}

	topic, err := client.CreateTopic(context.Background(), "test-topic")
	if err != nil {
		t.Fatalf("Error creating test topic: %v", err)
	}
	t.Cleanup(func() {
		topic.Stop()
		topic.Delete(context.Background())
	})

	sub, err := client.CreateSubscription(
		context.Background(),
		"test-sub",
		pubsub.SubscriptionConfig{Topic: topic},
	)
	if err != nil {
		t.Fatalf("Error creating test subscription: %v", err)
	}
	t.Cleanup(func() {
		sub.Delete(context.Background())
	})

	return testEnv{
		svc: svc,
		sub: sub,
	}
}

func TestFactory(t *testing.T) {
	requireEmulation(t)

	svc, err := Factory(&spb.ServiceConfig{
		Config: anypbtest.New(t, &cpspb.Config{
			Project: "test-project",
			Topic:   "test-topic",
		}),
	})
	if err != nil {
		t.Fatalf("Error creating service: %v", err)
	}
	t.Cleanup(func() { svc.Stop() })
}

func TestProcessMessage(t *testing.T) {
	env := createTestEnv(t)

	msg := &fspb.Message{
		MessageType: "test-message",
	}
	err := env.svc.ProcessMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}

	recvCtx, recvCancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
	defer recvCancel()

	msgOk := false
	env.sub.Receive(recvCtx, func(ctx context.Context, m *pubsub.Message) {
		m.Ack()
		fsMsg := &fspb.Message{}
		if err := proto.Unmarshal(m.Data, fsMsg); err != nil {
			t.Fatalf("Error unmarshaling message: %v", err)
		}
		if fsMsg.MessageType != "test-message" {
			t.Fatalf("Unexpected message type: %v", fsMsg.MessageType)
		}
		msgOk = true
		recvCancel()
	})

	if !msgOk {
		t.Fatalf("Message not processed")
	}
}
