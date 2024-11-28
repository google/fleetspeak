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

// Package batchedservice defines the interface that Fleetspeak expects from its
// server-side service implementations that support batched message processing.
package batchedservice

import (
	"context"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// A BatchedService is similar to Service but processes multiple messages
// instead of one at the time. It also does not retry messages that failed to
// be processed.
type BatchedService interface {
	// ProcessMessages is invoked with a batch of messages from the endpoint to be
	// processed by the service.
	//
	// Unlike with the Service interface, messages that fail to be processed
	// are not retried and are simply discarded.
	ProcessMessages(context.Context, []*fspb.Message) error

	// Stop initiates and waits for an orderly shut down.
	Stop() error
}

// A Factory is a function which creates a server batched service for the provided
// configuration.
type Factory func(*spb.BatchedServiceConfig) (BatchedService, error)
