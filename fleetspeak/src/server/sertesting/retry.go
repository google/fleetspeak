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

package sertesting

import (
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/server/internal/ftime"
)

// SetClientRetryTime changes db.ClientRetryTime method used by datastores to
// decide when to retry requests to clients. It returns a closure which returns
// the system to the previous state.
//
// This function and the returned closure are not thread safe. In particular, they should
// be called at the start and end of tests when no Fleetspeak component is started.
func SetClientRetryTime(f func() time.Time) func() {
	op := ftime.ClientRetryTime
	ftime.ClientRetryTime = f
	return func() {
		ftime.ClientRetryTime = op
	}
}

// SetServerRetryTime changes the db.ServerRetryTime used by the Fleetspeak
// server to decide when to retry requests to clients. It returns a closure
// which returns the system to the previous state.
//
// This function and the returned closure are not thread safe. In particular, they should
// be called at the start and end of tests when no Fleetspeak component is started.
func SetServerRetryTime(f func(uint32) time.Time) func() {
	op := ftime.ServerRetryTime
	ftime.ServerRetryTime = f
	return func() {
		ftime.ServerRetryTime = op
	}
}
