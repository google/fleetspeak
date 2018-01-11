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

package sertesting

import (
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/server/internal/cache"
)

// SetClientCacheMaxAge adjusts the maximum age that cached client data is
// considered valid for. This can be used to make tests run faster.
func SetClientCacheMaxAge(d time.Duration) func() {
	o := cache.MaxAge
	cache.MaxAge = d
	return func() {
		cache.MaxAge = o
	}
}
