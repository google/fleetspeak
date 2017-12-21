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

// Package service defines the interface that fleetspeak client side services must implement,
// along with some related types.
package service

import (
	"context"
	"io"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// AckMessage is a message that can be acknowledged. If no acknowledgement is
// required, Ack may be nil.
type AckMessage struct {
	M   *fspb.Message
	Ack func()
}

// A Service represents a configured client component which can accept and send
// messages via Fleetspeak.
type Service interface {
	// Start performs any necessary setup for the Service.
	//
	// If it returns nil Fleetspeak will assume that the service is ready to
	// process messages. Otherwise Fleetspeak will consider the service
	// unavailable.
	Start(Context) error

	// ProcessMessage processes one message.
	//
	// If it returns nil, Fleetspeak will acknowledge the message and it
	// will be marked as successfully processed. If it returns an error, the
	// message will be marked as failed.
	//
	// Note however that in either case, the delivery of the acknowledgement
	// is not guaranteed and there is a chance that the message will be sent
	// again.
	ProcessMessage(context.Context, *fspb.Message) error

	// Stop shuts down the service in an orderly manner.
	Stop() error
}

// LocalInfo stores summary information about the local client.
type LocalInfo struct {
	ClientID common.ClientID // The ClientID of the local client.
	Labels   []*fspb.Label   // The client labels (ServiceName="client") set on the local client.
	Services []string        // The Fleetspeak services currently configured on the local client.
}

// A Context encapsulates the functionality that the Fleetspeak client
// installation provides to a configured service.
type Context interface {
	// Send provides a message to the Fleetspeak system for delivery to the server or to other
	// Fleetspeak services running on the local client.
	Send(context.Context, AckMessage) error

	// GetLocalInfo returns information about the local client.
	GetLocalInfo() *LocalInfo

	// GetFileIfModified attempts to retrieve a file from the server, if it been modified since
	// modSince. Return values follow the semantics of Communicator.GetFileIfModified.
	GetFileIfModified(ctx context.Context, name string, modSince time.Time) (data io.ReadCloser, mod time.Time, err error)
}

// A Factory creates a Service according to a provided configuration.
type Factory func(conf *fspb.ClientServiceConfig) (Service, error)

// NOOPFactory is a Factory which produces a service that does nothing. It
// accepts any message without processing it, and produces no messages for the
// server.
func NOOPFactory(conf *fspb.ClientServiceConfig) (Service, error) {
	return noopService{}, nil
}

type noopService struct{}

func (s noopService) Start(Context) error {
	return nil
}

func (s noopService) ProcessMessage(context.Context, *fspb.Message) error {
	return nil
}

func (s noopService) Stop() error {
	return nil
}
