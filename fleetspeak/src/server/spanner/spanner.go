// Copyright 2024 Google Inc.
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

// Package spanner implements the fleetspeak datastore interface using a
// Spanner database.
package spanner

import (
	"context"
	"errors"
	
	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/spanner"
)

// Datastore wraps a mysql backed sql.DB and implements db.Store.
type Datastore struct {
	db                         string
	dbClient                   *spanner.Client
	clients                    string
	clientLabels               string
	clientContacts             string
	clientContactMessages      string
	clientResourceUsageRecords string
	messages                   string
	clientPendingMessages      string
	broadcasts                 string
	broadcastAllocations       string
	broadcastSent              string
	files                      string
	pubsubClient               *pubsub.Client
	pubsubSub                  *pubsub.Subscription
	pubsubTopic                *pubsub.Topic
}

// MakeDatastore creates any missing tables and returns a Datastore. The db
// parameter must be connected to a mysql database, e.g. using the mymysql
// driver.
func MakeDatastore(projectId, inst, db, pubsubTopic, pubsubSubscription string) (*Datastore, error) {
	ctx := context.Background()
	dbClient, err := initDB(ctx, projectId, inst, db)
	if err != nil {
		return nil, err
	}

	pubsubClient, err := pubsub.NewClient(ctx, projectId)

	if err != nil {
		return nil, err
	}

	sub := pubsubClient.Subscription(pubsubSubscription)
	topic := pubsubClient.Topic(pubsubTopic)

	return &Datastore{
		db:                         db,
		dbClient:                   dbClient,
		clients:                    "Clients",
		clientLabels:               "ClientLabels",
		clientContacts:             "ClientContacts",
		clientContactMessages:      "ClientContactMessages",
		clientResourceUsageRecords: "ClientResourceUsageRecords",
		messages:                   "Messages",
		clientPendingMessages:      "ClientPendingMessages",
		broadcasts:                 "Broadcasts",
		broadcastAllocations:       "BroadcastAllocations",
		broadcastSent:              "BroadcastSent",
		files:                      "Files",
		pubsubClient:               pubsubClient,
		pubsubSub:                  sub,
		pubsubTopic:                topic,
		}, err
}

// Close implements db.Store.
// Close closes the underlying database resources.
func (d *Datastore) Close() error {
	d.dbClient.Close()
	d.pubsubClient.Close()
	return nil
}

// IsNotFound implements db.Store.
func (d *Datastore) IsNotFound(err error) bool {
	switch err.(type) {
	case errClientNotFound:
		return true
	case *errClientNotFound:
		return true
	default:
		if errors.Is(err, spanner.ErrRowNotFound) {
			return true
		}
		return false
	}
}

func initDB(ctx context.Context, projectId, inst, db string) (*spanner.Client, error) {
	dbString := "projects/"+projectId+"/instances/"+inst+"/databases/"+db
	client, err := spanner.NewClient(ctx, dbString)
	if err != nil {
		return nil, err
	}
	return client, nil
}
