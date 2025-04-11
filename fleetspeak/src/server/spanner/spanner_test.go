// Copyright 2025 Google Inc.
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

package spanner

import (
	"context"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/spanner"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/dbtesting"
)

type spannerTestEnv struct {
	projectID          string
	instance           string
	database           string
	pubsubTopic        string
	pubsubSubscription string
}

func (e *spannerTestEnv) Create() error {
	return nil
}

func (e *spannerTestEnv) Clean() (db.Store, error) {
	ctx, fin := context.WithTimeout(context.Background(), 30*time.Second)
	defer fin()
	log.Info("Resetting PubSub Topic & Subscription")
	client, err := pubsub.NewClient(ctx, e.projectID)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	sub := client.Subscription(e.pubsubSubscription)
	if sub != nil {
		if err = sub.Delete(ctx); err != nil {
			return nil, err
		}
	}
	top := client.Topic(e.pubsubTopic)
	if top != nil {
		if err = top.Delete(ctx); err != nil {
			return nil, err
		}
	}
	top, err = client.CreateTopic(ctx, e.pubsubTopic)
	if err != nil {
		return nil, err
	}
	_, err = client.CreateSubscription(ctx, e.pubsubSubscription, pubsub.SubscriptionConfig{
		Topic: top,
	})
	if err != nil {
		return nil, err
	}
	log.Infof("Using database: %v", e.database)
	s, err := MakeDatastore(e.projectID, e.instance, e.database, e.pubsubTopic, e.pubsubSubscription)
	if err != nil {
		return nil, err
	}
	m := []*spanner.Mutation{
		spanner.Delete(s.clients, spanner.AllKeys()),
		spanner.Delete(s.messages, spanner.AllKeys()),
		spanner.Delete(s.broadcasts, spanner.AllKeys()),
		spanner.Delete(s.broadcastSent, spanner.AllKeys()),
		spanner.Delete(s.files, spanner.AllKeys()),
	}
	_, err = s.dbClient.Apply(ctx, m)
	return s, err
}

func (e *spannerTestEnv) Destroy() error {
	return nil
}

func newSpannerTestEnv(projectID, instance, database, pubsubTopic, pubsubSubscription string) *spannerTestEnv {
	return &spannerTestEnv{
		projectID:          projectID,
		instance:           instance,
		database:           database,
		pubsubTopic:        pubsubTopic,
		pubsubSubscription: pubsubSubscription,
	}
}

func TestSpannerStore(t *testing.T) {
	database := os.Getenv("SPANNER_DATABASE")
	instance := os.Getenv("SPANNER_INSTANCE")
	projectID := os.Getenv("SPANNER_PROJECT")
	pubsubTopic := os.Getenv("SPANNER_TOPIC")
	pubsubSubscription := os.Getenv("SPANNER_SUBSCRIPTION")

	if database == "" {
		t.Skip("SPANNER_DATABASE not set")
	}
	if instance == "" {
		t.Skip("SPANNER_INSTANCE not set")
	}
	if projectID == "" {
		t.Skip("SPANNER_PROJECT not set")
	}
	if pubsubTopic == "" {
		t.Skip("SPANNER_TOPIC not set")
	}
	if pubsubSubscription == "" {
		t.Skip("SPANNER_SUBSCRIPTION not set")
	}
	ctx, fin := context.WithTimeout(context.Background(), 30*time.Second)
	defer fin()
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		t.Skip("Failed to get PubSub client, is PubSub available?")
	}
	top, err := client.CreateTopic(ctx, pubsubTopic)
	if err != nil {
		t.Skip("Failed to create PubSub Topic")
	}
	_, err = client.CreateSubscription(ctx, pubsubSubscription, pubsub.SubscriptionConfig{
		Topic: top,
	})
	if err != nil {
		t.Skip("Failed to create PubSub Subscription")
	}
	defer client.Close()
	dbtesting.DataStoreTestSuite(t, newSpannerTestEnv(
		projectID, instance, database, pubsubTopic, pubsubSubscription))
}
