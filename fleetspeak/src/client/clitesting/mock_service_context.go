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

// Package clitesting contains utilities useful for testing clients and client
// components.
package clitesting

import (
	"fmt"
	"time"

	"context"

	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

const (
	// MockCommTimeout is how long we wait for the test to accept messages
	// sent through MockServiceContext.
	MockCommTimeout = 5 * time.Second
)

// MockServiceContext is a mock service.Context which allows a test to run a service
// without a full Fleetspeak client.
type MockServiceContext struct {
	service.Context

	OutChan chan *fspb.Message
}

// Send implements service.Context by copying the message to sc.OutChan.
func (sc *MockServiceContext) Send(ctx context.Context, m service.AckMessage) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(MockCommTimeout):
		return fmt.Errorf("Timed out while waiting for the test to pick up output: %q", m)
	case sc.OutChan <- m.M:
		if m.Ack != nil {
			m.Ack()
		}
		return nil
	}
}
