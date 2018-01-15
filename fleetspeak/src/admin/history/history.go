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

// Package history analyzes client contact history to compute statistics and
// find anomalies.
package history

import (
	"fmt"
	"net"
	"sort"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"

	tpb "github.com/golang/protobuf/ptypes/timestamp"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// Summary describes the result of analyzing a sequence of contacts made by
// a single client id.
//
// The Splits, SplitPoints and Skips fields work together to recognize when a machine
// is restored from backup or cloned:
//
// In normal operation they will all be 0.
//
// When a machine is restored from a backup, restarted from a fixed VM image or
// otherwise caused to use old FS state, we will count 1 Split and 1 Skip for
// every restore. We also count 1 SplitPoint for every image that we restore
// from.
//
// NOTE: All SplitPoints occurring before the time range of contacts we are
// given are merged together. This this allows us to more accurately count past
// Splits, but means we might under count SplitPoints.
//
// When a machine is cloned n ways, Splits, SplitPoints and Skips will be
// counted as we would for n restores. However, we'll also see ~n Skips per poll
// interval (default poll interval is 5 min). Therefore Skips > Splits is
// evidence that a machine has been cloned.
type Summary struct {
	Start, End  time.Time // First and last contact analyzed.
	Count       int       // Number of contacts analyzed.
	IPCount     int       // Number of distinct IPs observed.
	Splits      int       // Number of excess references to nonces.
	SplitPoints int       // Number of distinct nonces with more than 1 reference.
	Skips       int       // Number of points which reference a nonce other than the immediately previous contact.
}

type contactSlice []*spb.ClientContact

func (s contactSlice) Len() int      { return len(s) }
func (s contactSlice) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s contactSlice) Less(i, j int) bool {
	return tsLess(s[i].Timestamp, s[j].Timestamp)
}

func tsLess(i, j *tpb.Timestamp) bool {
	return i.Seconds < j.Seconds || (i.Seconds == j.Seconds && i.Nanos < j.Nanos)
}

// Summarize computes a Summary for list of contacts.
func Summarize(cs []*spb.ClientContact) (*Summary, error) {
	if err := validate(cs); err != nil {
		return nil, err
	}
	if len(cs) == 0 {
		return &Summary{}, nil
	}

	sort.Sort(contactSlice(cs))

	nm := nonceMap(cs)
	if err := validateTime(nm); err != nil {
		return nil, err
	}

	splits, splitPoints := countSplits(nm)
	skips := countSkips(cs)

	start, _ := ptypes.Timestamp(cs[0].Timestamp)
	end, _ := ptypes.Timestamp(cs[len(cs)-1].Timestamp)

	return &Summary{
		Start:       start,
		End:         end,
		Count:       len(cs),
		IPCount:     countIPs(cs),
		Splits:      splits,
		SplitPoints: splitPoints,
		Skips:       skips,
	}, nil
}

func validate(contacts []*spb.ClientContact) error {
	times := make(map[int64]bool)
	nonces := make(map[uint64]bool)
	for _, c := range contacts {
		// The datastore assigns and is responsible for preventing duplicate contact
		// timestamps. If we see a duplicate, it is a data error that might confuse
		// other analysis.
		t, err := ptypes.Timestamp(c.Timestamp)
		if err != nil {
			return fmt.Errorf("bad timestamp proto [%v]: %v", c.Timestamp, err)
		}
		ts := t.UnixNano()
		if times[ts] {
			return fmt.Errorf("duplicate timestamp: %v", t)
		}
		times[ts] = true

		// The sent nonce is a 64 bit number that should have been chosen by the
		// server for each contact using a strong RNG. If we see a duplicate it is a
		// data error that might confuse other analysis.
		if nonces[c.SentNonce] {
			return fmt.Errorf("duplicate sent nonce: %d", c.SentNonce)
		}
		nonces[c.SentNonce] = true
	}
	return nil
}

func validateTime(nm map[uint64]*spb.ClientContact) error {

	for _, i := range nm {
		if i.ReceivedNonce == 0 {
			continue
		}
		if p := nm[i.ReceivedNonce]; p != nil {
			if !tsLess(p.Timestamp, i.Timestamp) {
				// The nonce received from a client cannot reference a nonce produced by
				// the server in the future. If this seems to have happened, it is a
				// data error that could confuse other analysis.
				return fmt.Errorf("nonce at [%v] references future time [%v]", i.Timestamp, p.Timestamp)
			}
		}
	}
	return nil
}

func countIPs(contacts []*spb.ClientContact) int {
	m := make(map[string]bool)
	for _, c := range contacts {
		h, _, err := net.SplitHostPort(c.ObservedAddress)
		if err != nil {
			log.Warningf("Unable to parse ObservedAddress [%s], ignoring: %v", c.ObservedAddress, err)
			continue
		}
		m[h] = true
	}
	return len(m)
}

func nonceMap(cs []*spb.ClientContact) map[uint64]*spb.ClientContact {
	n := make(map[uint64]*spb.ClientContact)
	for _, c := range cs {
		n[c.SentNonce] = c
	}
	return n
}

func countSplits(nm map[uint64]*spb.ClientContact) (splits, splitPoints int) {
	// A count of how many contacts target each nonce.
	ts := make(map[uint64]int)
	for _, c := range nm {
		// Anything not in nm must be too old - coalesce with the 0 target.
		if c.ReceivedNonce == 0 || nm[c.ReceivedNonce] == nil {
			ts[0]++
		} else {
			ts[c.ReceivedNonce]++
		}
	}
	for _, t := range ts {
		if t > 1 {
			splitPoints++
			splits += t - 1
		}
	}
	return splits, splitPoints
}

func countSkips(cs []*spb.ClientContact) int {
	var s int
	for i := 1; i < len(cs); i++ {
		if cs[i].ReceivedNonce != cs[i-1].SentNonce {
			s++
		}
	}
	return s
}
