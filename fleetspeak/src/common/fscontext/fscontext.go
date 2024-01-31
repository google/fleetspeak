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
	"fmt"
	"sync"
)

// ErrStopRequested is the cancelation cause to be used when callers
// intentionally cancel a context from the outside.
var ErrStopRequested = fmt.Errorf("%w: stop requested", context.Canceled)

// WithDoneChan returns a new context and cancel function.
//
// The context is automatically canceled with the given cause as soon as done is
// closed.
//
// This is not meant as a long-term solution, but as a migration path for code
// which has a "done" channel but which needs to provide a context.
//
// Callers must always call cancel after the context is done.
//
// Example usage:
//
//	 errDoneClosed := fmt.Errorf("my magic done channel was closed: %w", fscontext.ErrStopRequested)
//		ctx, cancel := fscontext.WithDoneChan(ctx, errDoneClosed, xyz.done)
//		defer cancel(nil)
func WithDoneChan(ctx context.Context, cause error, done <-chan struct{}) (newCtx context.Context, cancel context.CancelCauseFunc) {
	myCtx, myCancel := context.WithCancelCause(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			// no need to cancel - it is already canceled
		case <-done:
			myCancel(cause)
		}
	}()
	return myCtx, func(cause error) {
		wg.Wait()
		myCancel(cause)
	}
}
