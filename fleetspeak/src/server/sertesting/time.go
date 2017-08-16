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

// Package sertesting contains utilities useful for testing the fleetspeak server
// and server components.
package sertesting

import (
	"sync/atomic"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/server/internal/ftime"
)

// FakeTime represents a fake time which can control the time seen by the fleetspeak
// system during unit tests.
type FakeTime struct {
	t    int64
	orig func() time.Time
}

// Get returns the current fake time.
func (t *FakeTime) Get() time.Time {
	return time.Unix(atomic.LoadInt64(&t.t), 0).UTC()
}

// SetSeconds sets the current fake time to s seconds since epoch.
func (t *FakeTime) SetSeconds(s int64) {
	atomic.StoreInt64(&t.t, s)
}

// AddSeconds adds s seconds to the fake time.
func (t *FakeTime) AddSeconds(s int64) {
	atomic.AddInt64(&t.t, s)
}

// Revert returns the time seen by the fleetspeak system to what it was
// previously.
func (t *FakeTime) Revert() {
	ftime.Now = t.orig
}

// FakeNow changes the implementation of Now used by the fleetspeak system. It
// returns a FakeTime which controls the time that the system sees until Revert
// is called.
//
// Note that this function, and FakeTime.Revert are not thread safe. In
// particular it they should be called at the start and end of tests when no Fleetspeak
// component is started.
func FakeNow(start int64) *FakeTime {
	r := FakeTime{
		t:    start,
		orig: ftime.Now,
	}
	ftime.Now = r.Get
	return &r
}
