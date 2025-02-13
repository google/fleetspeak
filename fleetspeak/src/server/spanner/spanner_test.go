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

package spanner

import (
	"context"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/spanner"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/dbtesting"
)

type spannerTestEnv struct {
	projectId string
	instance string
	database string
	pubsubTopic string
	pubsubSubscription string
}

func (e *spannerTestEnv) Create() error {
	return nil
}

func (e *spannerTestEnv) Clean() (db.Store, error) {
	ctx, fin := context.WithTimeout(context.Background(), 30*time.Second)
	defer fin()
	log.Infof("Using database: %v", )
	s, err := MakeDatastore(e.projectId, e.instance, e.database, e.pubsubTopic, e.pubsubSubscription)
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

func newSpannerTestEnv(projectId, instance, database, pubsubTopic, pubsubSubscription string) *spannerTestEnv {
	return &spannerTestEnv{
		projectId:   projectId,
		instance:   instance,
		database:   database,
		pubsubTopic: pubsubTopic,
		pubsubSubscription: pubsubSubscription,
	}
}

func TestSpannerStore(t *testing.T) {
	var database = os.Getenv("SPANNER_DATABASE")
	var instance = os.Getenv("SPANNER_INSTANCE")
	var projectId = os.Getenv("SPANNER_PROJECT")
	var pubsubTopic = os.Getenv("SPANNER_TOPIC")
	var pubsubSubscription = os.Getenv("SPANNER_SUBSCRIPTION")

	if database == "" {
		t.Skip("SPANNER_DATABASE not set")
	}
	if instance == "" {
		t.Skip("SPANNER_INSTANCE not set")
	}
	if projectId == "" {
		t.Skip("SPANNER_PROJECT not set")
	}
	if pubsubTopic == "" {
		t.Skip("SPANNER_TOPIC not set")
	}
	if pubsubSubscription == "" {
		t.Skip("SPANNER_SUBSCRIPTION not set")
	}
	dbtesting.DataStoreTestSuite(t, newSpannerTestEnv(
		projectId, instance, database, pubsubTopic, pubsubSubscription))
}
