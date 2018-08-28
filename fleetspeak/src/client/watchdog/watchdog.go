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

// Package watchdog implements a simple watchdog timer which dumps a trace of
// all goroutines to a file, and then ends the program, if the timer reaches its
// limit.
package watchdog

import (
	"io/ioutil"
	"runtime/pprof"
	"time"

	log "github.com/golang/glog"
)

type Watchdog struct {
	dir      string
	prefix   string
	duration time.Duration
	reset    chan struct{}
	exit     bool
}

// MakeWatchdog creates and starts running a watchdog timer. If <duration>
// passes without a reset it writes stack traces to a temporary file determined
// by dir, prefix. Then it exits the program if exit is set.
func MakeWatchdog(dir, prefix string, duration time.Duration, exit bool) *Watchdog {
	r := &Watchdog{
		dir:      dir,
		prefix:   prefix,
		duration: duration,
		reset:    make(chan struct{}),
		exit:     exit,
	}
	go r.watch()
	return r
}

// Reset resets the watchdog's timer to 0.
func (w *Watchdog) Reset() {
	select {
	case w.reset <- struct{}{}:
	default:
	}
}

func (w *Watchdog) watch() {
	var t *time.Timer
	defer func() {
		if t != nil {
			t.Stop()
		}
	}()
	for {
		t = time.NewTimer(w.duration)
		select {
		case _, ok := <-w.reset:
			if !ok {
				return
			}
			t.Stop()
			t = nil
		case <-t.C:
			// We may have just woke up from sleep, wait another 5
			// seconds for a connection attempt.
			t = time.NewTimer(5 * time.Second)
			select {
			case _, ok := <-w.reset:
				if !ok {
					return
				}
				t.Stop()
				t = nil
			case <-t.C:
				log.Errorf("Watchdog expired, attempting to write goroutine traces.")
				f, err := ioutil.TempFile(w.dir, w.prefix)
				if err != nil {
					log.Errorf("Unable to create file for goroutine traces: %v", err)
				} else {
					if err := pprof.Lookup("goroutine").WriteTo(f, 2); err != nil {
						log.Errorf("Unable to write goroutine traces to [%s]: %v", f.Name(), err)
					}
					if err := f.Close(); err != nil {
						log.Errorf("Unable to close file [%s]: %v", f.Name(), err)
					}
					log.Infof("Wrote goroutine traces to %s", f.Name())
				}
				if w.exit {
					log.Exitf("Watchdog expired.")
					return
				}
			}
		}
	}
}

// Stop stops the watchdog timer, so that it will no longer trigger.
func (w *Watchdog) Stop() {
	close(w.reset)
}
