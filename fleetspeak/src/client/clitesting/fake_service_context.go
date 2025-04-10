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
	"context"
	"errors"
	"fmt"
	"io"
	"testing/fstest"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

const (
	// FakeCommTimeout is how long we wait for the test to accept messages sent
	// through FakeServiceContext.
	FakeCommTimeout = 5 * time.Second
)

// FakeServiceContext is a fake [service.Context] which allows a test to run a
// service without a full Fleetspeak client.
type FakeServiceContext struct {
	service.Context

	OutChan        chan *fspb.Message
	StatsCollector stats.Collector
	LocalInfo      *service.LocalInfo
	Files          fstest.MapFS
}

// Send implements service.Context by writing the message to sc.OutChan.
func (sc *FakeServiceContext) Send(ctx context.Context, m service.AckMessage) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(FakeCommTimeout):
		return fmt.Errorf("Timed out while waiting for the test to pick up output: %+v", m)
	case sc.OutChan <- m.M:
		if m.Ack != nil {
			m.Ack()
		}
		return nil
	}
}

// Stats implements service.Context by returning the assigned stats.Collector or
// a NoopCollector if none was set.
func (sc *FakeServiceContext) Stats() stats.Collector {
	if sc.StatsCollector == nil {
		return stats.NoopCollector{}
	}
	return sc.StatsCollector
}

// GetLocalInfo implements service.Context.
func (sc *FakeServiceContext) GetLocalInfo() *service.LocalInfo {
	return sc.LocalInfo
}

// GetFileIfModified implements service.Context by looking up file contents and
// modification times in the assigned maps.
func (sc *FakeServiceContext) GetFileIfModified(ctx context.Context, name string, modSince time.Time) (data io.ReadCloser, mod time.Time, err error) {
	if sc.Files == nil {
		return nil, time.Time{}, errors.New("no files available")
	}
	f, err := sc.Files.Open(name)
	if err != nil {
		return nil, time.Time{}, err
	}
	info, err := f.Stat()
	if err != nil {
		return nil, time.Time{}, err
	}
	if info.ModTime().After(modSince) {
		return f, info.ModTime(), nil
	}
	return nil, info.ModTime(), nil
}
