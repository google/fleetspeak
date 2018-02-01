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

// Package service defines the interface that fleetspeak expects from its
// service implementations.
//
// An installation can provide the set of ServiceFactories which it requires,
// and then the factories will be used, according to the server configuration,
// to create the server services.
package service

import (
	"context"
	"net"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// A Service is a Fleetspeak module configured to send and processes messages on a
// Fleetspeak server.
type Service interface {
	// Start tells the service to begin operations. Once started, the service may make
	// calls into ServiceContext until Wait is called.
	Start(sctx Context) error

	// ProcessMessage requests that the service process a message. If it returns a
	// net.Error which reports as a timeout or temporary error, the message will
	// be retried according the ServerRetryPolicy. Otherwise the error will be
	// assumed to be permanent.
	ProcessMessage(context.Context, *fspb.Message) error

	// Stop initiates and waits for an orderly shut down. It will only be
	// called when there are no more active calls to ProcessMessage and
	// should not return until all calls to ServiceContext are completed.
	Stop() error
}

// Context allows a Fleetspeak Service to communicate back to the Fleetspeak system.
type Context interface {
	// Set sends a message to a client machine or other server component. It can be called
	// anytime after Service.Start begins and before Service.Stop returns.
	Send(context.Context, *fspb.Message) error

	// GetClientData retrieves basic information about a client.
	GetClientData(context.Context, common.ClientID) (*db.ClientData, error)
}

// A Factory is a function which creates a server service for the provided
// configuration.
type Factory func(*spb.ServiceConfig) (Service, error)

// NOOPFactory is a services.Factory that creates a service which drops all messages.
func NOOPFactory(conf *spb.ServiceConfig) (Service, error) {
	return noopService{}, nil
}

type noopService struct{}

func (s noopService) Start(sctx Context) error                                  { return nil }
func (s noopService) ProcessMessage(ctx context.Context, m *fspb.Message) error { return nil }
func (s noopService) Stop() error                                               { return nil }

// TemporaryError wraps an error and indicates that ProcessMessage should be
// retried some time in the future.
type TemporaryError struct {
	E error
}

// Error implements error by trivial delegation to E.
func (e TemporaryError) Error() string {
	return e.E.Error()
}

// Temporary implements net.Error.
func (e TemporaryError) Temporary() bool {
	return true
}

// Timeout implements net.Error.
func (e TemporaryError) Timeout() bool {
	return true
}

// IsTemporary determines if an error should be retried by the fleetspeak
// system.
func IsTemporary(err error) bool {
	e, ok := err.(net.Error)
	return ok && (e.Temporary() || e.Timeout())
}
