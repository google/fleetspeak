// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package fscontext contains helpers for migrating Fleetspeak to context.Context.
//
// Fleetspeak should better use context.Context for cancelation than chan struct{}s.
package fscontext

import (
	"context"
	"sync"
)

// FromDoneChanTODO returns a context and cancel function.
//
// The context is automatically canceled as soon as done is closed.
//
// This is not meant as a long-term solution, but as a migration path for code
// which has a "done" channel but which needs to provide a context.  Hence the
// name.
//
// Example usage:
//
//	ctx, cancel := fscontext.FromDoneChanTODO(foo.done)
//	defer cancel()
func FromDoneChanTODO(done <-chan struct{}) (ctx context.Context, cancel func()) {
	ctx, myCancel := context.WithCancel(context.TODO())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-done
		myCancel()
	}()
	return ctx, func() {
		wg.Wait()
		myCancel()
	}
}
