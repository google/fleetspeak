// Copyright 2018 Google Inc.
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

package cache

import (
	"bytes"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/ftime"
)

func TestClients(t *testing.T) {
	oi := expireInterval
	expireInterval = time.Second
	defer func() {
		expireInterval = oi
	}()

	// We'd use sertesting.FakeNow, but that creates a dependency loop. So we
	// do it directly.
	otime := ftime.Now
	fakeTime := int64(20000)
	ftime.Now = func() time.Time {
		return time.Unix(atomic.LoadInt64(&fakeTime), 0).UTC()
	}
	defer func() {
		ftime.Now = otime
	}()

	c := NewClients()
	defer c.Stop()

	id1, _ := common.StringToClientID("0000000000000001")
	id2, _ := common.StringToClientID("0000000000000002")

	for _, id := range []common.ClientID{id1, id2} {
		got := c.Get(id)
		if got != nil {
			t.Errorf("Get(%v) = %v, expected nil", id, got)
		}
	}

	c.Update(id1, &db.ClientData{
		Key: []byte("key 1"),
	})

	atomic.StoreInt64(&fakeTime, 20044)

	got := c.Get(id1)
	if got == nil || !bytes.Equal(got.Key, []byte("key 1")) {
		t.Errorf("Get(%v) = %v, expected {Key: \"key 1\"}", id1, got)
	}
	got = c.Get(id2)
	if got != nil {
		t.Errorf("Get(%v) = %v, expected nil", id2, got)
	}

	// Set key 2, make sure that clearing it works.
	c.Update(id2, &db.ClientData{
		Key: []byte("key 2"),
	})
	s := c.Size()
	if s != 2 {
		t.Errorf("Expected cache size of 2, got Size: %d", s)
	}
	c.Update(id2, nil)
	got = c.Get(id2)
	if got != nil {
		t.Errorf("Get(%v) = %v, expected nil", id2, got)
	}
	// A second clear should not panic.
	c.Update(id2, nil)

	// Advance the clock enough to expire id1, wait for expire to run.
	atomic.StoreInt64(&fakeTime, 20046)
	time.Sleep(2 * time.Second)

	// Everything should be nil.
	for _, id := range []common.ClientID{id1, id2} {
		got := c.Get(id)
		if got != nil {
			t.Errorf("Get(%v) = %v, expected nil", id, got)
		}
	}

	// We shouldn't be using any extra ram.
	s = c.Size()
	if s != 0 {
		t.Errorf("Expected empty cache, got Size: %d", s)
	}
}
