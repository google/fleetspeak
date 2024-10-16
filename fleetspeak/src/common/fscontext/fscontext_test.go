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

package fscontext_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common/fscontext"
)

var (
	errMagic = errors.New("magic")
	errDone  = errors.New("done")
)

var shortDuration = 100 * time.Millisecond

func TestWithDoneChan_NotCanceled(t *testing.T) {
	// Given
	doneCh := make(chan struct{})
	defer close(doneCh)

	ctx, cancel := fscontext.WithDoneChan(context.TODO(), errDone, doneCh)
	defer cancel(nil)

	// When nothing happens...

	// Then
	select {
	case <-ctx.Done():
		t.Errorf("Expected ctx not canceled, got: %v", context.Cause(ctx))
	case <-time.After(shortDuration):
		t.Log("not canceled after", shortDuration, "- looks good")
	}
}

func TestWithDoneChan_CanceledThroughChannel(t *testing.T) {
	// Given
	doneCh := make(chan struct{})

	ctx, cancel := fscontext.WithDoneChan(context.TODO(), errDone, doneCh)
	defer cancel(nil)

	// When
	close(doneCh)

	// Then
	select {
	case <-time.After(shortDuration):
		t.Errorf("timeout waiting for context cancelation")
	case <-ctx.Done():
		if err := ctx.Err(); err != context.Canceled {
			t.Errorf("done channel closed: ctx.Err() = %v, want canceled", err)
		}
		if !errors.Is(context.Cause(ctx), errDone) {
			t.Errorf("done channel closed: context.Cause(ctx) = %v, want errors.Is(…, %v)", context.Cause(ctx), errDone)
		}
	}
}

func TestWithDoneChan_CanceledThroughOuterContext(t *testing.T) {
	// Given
	doneCh := make(chan struct{})
	defer close(doneCh)

	ctx, cancel1 := context.WithCancelCause(context.TODO())
	defer cancel1(nil)
	ctx, cancel2 := fscontext.WithDoneChan(ctx, errDone, doneCh)
	defer cancel2(nil)

	// When
	cancel1(errMagic)

	// Then
	select {
	case <-time.After(shortDuration):
		t.Errorf("timeout waiting for context cancelation")
	case <-ctx.Done():
		if err := ctx.Err(); err != context.Canceled {
			t.Errorf("done channel closed: ctx.Err() = %v, want canceled", err)
		}
		if !errors.Is(context.Cause(ctx), errMagic) {
			t.Errorf("done channel closed: context.Cause(ctx) = %v, want errors.Is(…, %v)", context.Cause(ctx), errMagic)
		}
	}
}

func TestWithDoneChan_CanceledThroughOwnContext(t *testing.T) {
	// Given
	doneCh := make(chan struct{})
	defer close(doneCh)

	ctx, cancel := fscontext.WithDoneChan(context.TODO(), errDone, doneCh)
	defer cancel(nil)

	// When
	cancel(errMagic)

	// Then
	select {
	case <-time.After(shortDuration):
		t.Errorf("timeout waiting for context cancelation")
	case <-ctx.Done():
		if err := ctx.Err(); err != context.Canceled {
			t.Errorf("done channel closed: ctx.Err() = %v, want canceled", err)
		}
		if !errors.Is(context.Cause(ctx), errMagic) {
			t.Errorf("done channel closed: context.Cause(ctx) = %v, want errors.Is(…, %v)", context.Cause(ctx), errMagic)
		}
	}
}

func TestAfterDelayFunc_DelayReached(t *testing.T) {
	// Given
	const delay = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	flag := atomic.Bool{}
	setFlag := func() {
		flag.Store(true)
	}

	stop := fscontext.AfterDelayFunc(ctx, delay, setFlag)
	defer stop()

	// When
	cancel()
	time.Sleep(2 * delay)

	// Then
	if !flag.Load() {
		t.Errorf("flag not set after %v", delay)
	}
	if stop() {
		t.Errorf("stop() returned true, but flag was set")
	}
}

func TestAfterDelayFunc_StopBeforeCancel(t *testing.T) {
	// Given
	const delay = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	flag := atomic.Bool{}
	setFlag := func() {
		flag.Store(true)
	}

	stop := fscontext.AfterDelayFunc(ctx, delay, setFlag)
	defer stop()

	// When
	stopped := stop()
	cancel()
	time.Sleep(2 * delay)

	// Then
	if !stopped {
		t.Errorf("stop() returned false, but context was not canceled")
	}
	if flag.Load() {
		t.Errorf("flag was set, but stop() was called before")
	}
	if stop() {
		t.Errorf("stop() returned true, but stop() was called before")
	}
}

func TestAfterDelayFunc_StopAfterCancel(t *testing.T) {
	// Given
	const delay = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	flag := atomic.Bool{}
	setFlag := func() {
		flag.Store(true)
	}

	stop := fscontext.AfterDelayFunc(ctx, delay, setFlag)
	defer stop()

	// When
	cancel()
	stopped := stop()
	time.Sleep(2 * delay)

	// Then
	if !stopped {
		t.Errorf("stop() returned false, but delay was not reached")
	}
	if flag.Load() {
		t.Errorf("flag was set, but stop() was called before")
	}
	if stop() {
		t.Errorf("stop() returned true, but stop() was called before")
	}
}

func TestAfterDelayFunc_StopAfterDelay(t *testing.T) {
	// Given
	const delay = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	flag := atomic.Bool{}
	setFlag := func() {
		flag.Store(true)
	}

	stop := fscontext.AfterDelayFunc(ctx, delay, setFlag)
	defer stop()

	// When
	cancel()
	time.Sleep(2 * delay)
	stopped := stop()

	// Then
	if stopped {
		t.Errorf("stop() returned true, but delay was reached")
	}
	if !flag.Load() {
		t.Errorf("flag was not set, but delay was reached")
	}
	if stop() {
		t.Errorf("stop() returned true, but stop() was called before")
	}
}
