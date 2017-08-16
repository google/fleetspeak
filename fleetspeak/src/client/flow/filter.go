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

// Package flow contains structures and utility method relating to client-server
// flow control configuration.
package flow

import "sync/atomic"

const (
	lowFiltered int32 = 1 << iota
	medFiltered
	highFiltered
)

// Filter is a thread safe data structure which remembers 3 booleans, used to
// indicate which of three priority levels should be filtered. The underlying
// type should be seen as an implementation detail.
type Filter int32

// NewFilter returns a Filter which is not set to filter anything.
func NewFilter() *Filter {
	return new(Filter)
}

// Set sets the filter bits according to the provided booleans.
func (f *Filter) Set(low, medium, high bool) {
	var i int32
	if low {
		i |= lowFiltered
	}
	if medium {
		i |= medFiltered
	}
	if high {
		i |= highFiltered
	}
	atomic.StoreInt32((*int32)(f), i)
}

// Get returns the current state of the filter.
func (f *Filter) Get() (low, medium, high bool) {
	i := atomic.LoadInt32((*int32)(f))
	return (i&lowFiltered != 0), (i&medFiltered != 0), (i&highFiltered != 0)
}
