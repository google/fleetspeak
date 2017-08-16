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

// Package broadcasts contains code for a Fleetspeak server to manage
// broadcasts. See in particular the Manager.
package broadcasts

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"log"
	"context"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/ids"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/ftime"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

const (
	allocFrac     = 0.2
	allocDuration = 10 * time.Minute
)

// A Manager keeps tracts of the active broadcasts in a system. It allows a
// fleetspeak system to deliver broadcasts to client without all of the
// fleetspeak servers trying to modify the same datastore records at the same
// time.
type Manager struct {
	bs           db.BroadcastStore
	infos        map[ids.BroadcastID]*bInfo
	l            sync.RWMutex // Protects the structure of i.
	done         chan bool    // Closes to indicate it is time to shut down.
	basePollWait time.Duration
}

// MakeManager creates a Manager, populates it with the
// current set of broadcasts, and begins updating the broadcasts in the
// background, the time between updates is always between pw and 2*pw.
func MakeManager(ctx context.Context, bs db.BroadcastStore, pw time.Duration) (*Manager, error) {
	r := &Manager{
		bs:           bs,
		infos:        make(map[ids.BroadcastID]*bInfo),
		done:         make(chan bool),
		basePollWait: pw,
	}
	if err := r.refreshInfo(ctx); err != nil {
		return nil, err
	}
	go r.refreshLoop()
	return r, nil
}

// bInfo containes what a broadcast manager needs to know about a broadcast.
type bInfo struct {
	bID      ids.BroadcastID
	b        *spb.Broadcast
	useCount sync.WaitGroup // How many goroutines are using this bInfo and associated allocation.

	// The remaining fields describe the allocation that we have for the broadcast. If limit is
	// set to BroadcastUnlimited, then we don't actualy have (or need) an allocation.
	aID    ids.AllocationID
	limit  uint64
	sent   uint64 // How many messages have we sent under the current allocation, only accessed through atomic.
	expiry time.Time
	lock   sync.Mutex // Used to sychronize updates to the allocation record in the database.
}

// limitedAtomicIncrement atomically adds one to *addr, unless the result would
// exceed limit. Return true if successful.
func limitedAtomicIncrement(addr *uint64, limit uint64) bool {
	for {
		c := atomic.LoadUint64(addr)
		if c >= limit {
			return false
		}
		if atomic.CompareAndSwapUint64(addr, c, c+1) {
			return true
		}
	}
}

// pollWait picks a time to wait before the next refresh.
func (m *Manager) pollWait() time.Duration {
	return m.basePollWait + time.Duration(float64(m.basePollWait)*rand.Float64())
}

// label shadows fspb.Label, but is safe to use as a map key
type label struct {
	serviceName string
	label       string
}

func labelFromProto(l *fspb.Label) label {
	return label{serviceName: l.ServiceName, label: l.Label}
}

// MakeBroadcastMessagesForClient finds broadcasts that the client is eligible
// to receive.
func (m *Manager) MakeBroadcastMessagesForClient(ctx context.Context, id common.ClientID, labels []*fspb.Label) ([]*fspb.Message, error) {
	labelSet := make(map[label]bool)
	for _, l := range labels {
		labelSet[labelFromProto(l)] = true
	}

	sent, err := m.bs.ListSentBroadcasts(ctx, id)
	if err != nil {
		return nil, err
	}
	sentSet := make(map[ids.BroadcastID]bool)
	for _, s := range sent {
		sentSet[s] = true
	}

	var is []*bInfo
	m.l.RLock()
Infos:
	for _, info := range m.infos {
		if sentSet[info.bID] {
			continue
		}
		if !info.expiry.IsZero() && info.expiry.Before(ftime.Now()) {
			continue
		}
		for _, kw := range info.b.RequiredLabels {
			if !labelSet[labelFromProto(kw)] {
				continue Infos
			}
		}
		if info.limit == db.BroadcastUnlimited {
			atomic.AddUint64(&info.sent, 1)
		} else {
			if !limitedAtomicIncrement(&info.sent, info.limit) {
				continue
			}
		}
		info.useCount.Add(1)
		is = append(is, info)
	}
	m.l.RUnlock()

	// NOTE: we must call useCount.Done() on everything in "is", or the
	// allocation update goroutine will get stuck. From this point on we log
	// errors but don't stop.
	msgs := make([]*fspb.Message, 0, len(is))
	for _, i := range is {
		mid, err := common.RandomMessageID()
		if err != nil {
			log.Printf("unable to create message id: %v", err)
			if i.limit != db.BroadcastUnlimited {
				// Incantation to decrement a uint64, recommend AddUint64 docs:
				atomic.AddUint64(&i.sent, ^uint64(0))
			}
			i.useCount.Done()
			continue
		}
		msg := &fspb.Message{
			MessageId: mid.Bytes(),
			Source:    i.b.Source,
			Destination: &fspb.Address{
				ClientId:    id.Bytes(),
				ServiceName: i.b.Source.ServiceName,
			},
			MessageType:  i.b.MessageType,
			Data:         i.b.Data,
			CreationTime: db.NowProto(),
		}
		i.lock.Lock()
		err = m.bs.SaveBroadcastMessage(ctx, msg, i.bID, id, i.aID)
		i.lock.Unlock()
		if err != nil {
			log.Printf("SaveBroadcastMessage of instance of broadcast %v failed. Not sending. [%v]", i.bID, err)
			if i.limit != db.BroadcastUnlimited {
				// Incantation to decrement a uint64, recommend by AddUint64 docs:
				atomic.AddUint64(&i.sent, ^uint64(0))
			}
			i.useCount.Done()
			continue
		}
		msgs = append(msgs, msg)
		i.useCount.Done()
	}
	return msgs, nil
}

func (m *Manager) refreshLoop() {
	ctx := context.Background()
	for {
		select {
		case _, ok := <-m.done:
			if !ok {
				return
			}
		case <-time.After(m.pollWait()):
		}

		if err := m.refreshInfo(ctx); err != nil {
			log.Printf("Error refreshing broadcast infos: %v", err)

		}
	}
}

// refreshInfo refreshes the bInfo map using the data from the database.
func (m *Manager) refreshInfo(ctx context.Context) error {
	// Find the allocations that we don't need (or want) to change.
	curr := m.findCurrentAllocs()

	// Find the active broadcasts.
	bs, err := m.bs.ListActiveBroadcasts(ctx)
	if err != nil {
		return fmt.Errorf("unable to list active broadcasts: %v", err)
	}

	// Create any new allocations that we'll need: everything in bs but not curr.
	newAllocs := make(map[ids.BroadcastID]*bInfo)
	activeIds := make(map[ids.BroadcastID]bool)
	for _, b := range bs {
		id, err := ids.BytesToBroadcastID(b.Broadcast.BroadcastId)
		if err != nil {
			log.Printf("Broadcast [%v] has bad id, skipping: %v", b.Broadcast, err)
			continue
		}
		activeIds[id] = true
		if !curr[id] {
			if b.Sent == b.Limit {
				continue
			}
			a, err := m.bs.CreateAllocation(ctx, id, allocFrac, ftime.Now().Add(allocDuration))
			if err != nil {
				log.Printf("Unable to create alloc for broadcast %v, skipping: %v", id, err)
				continue
			}
			if a != nil {
				newAllocs[id] = &bInfo{
					bID: id,
					b:   b.Broadcast,

					aID:    a.ID,
					limit:  a.Limit,
					sent:   0,
					expiry: a.Expiry,
				}
			}
		}
	}

	// Some things in curr might no longer be active, e.g. if the broadcast
	// was canceled. Remove them from curr so that updateAllocs knows to
	// clear them.
	for id := range curr {
		if !activeIds[id] {
			delete(curr, id)
		}
	}

	// Swap/insert the new allocations.
	c := m.updateAllocs(curr, newAllocs)

	var errMsgs []string
	// Cleanup the dead allocations. They've been removed from m.infos, so
	// once useCount reaches 0, no new actions with them will start.  We
	// cleanup everything we can, even if there are errors.
	for _, a := range c {
		a.useCount.Wait()
		if err := m.bs.CleanupAllocation(ctx, a.bID, a.aID); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("[%v,%v]:\"%v\"", a.bID, a.aID, err))
		}
	}

	if len(errMsgs) > 0 {
		return errors.New("errors cleaning up allocations - " + strings.Join(errMsgs, " "))
	}
	return nil
}

func (m *Manager) findCurrentAllocs() map[ids.BroadcastID]bool {
	r := make(map[ids.BroadcastID]bool)
	// We should be the only goroutine that modifies m.infos, so we don't need to
	// touch m.l.

	for _, info := range m.infos {
		// If the current allocation has room to send at least one
		// message, and it should last until the next tick, we keep it.
		if (info.limit == db.BroadcastUnlimited || atomic.LoadUint64(&info.sent) < info.limit) &&
			(info.expiry.IsZero() || info.expiry.After(ftime.Now().Add(2*m.basePollWait))) {
			r[info.bID] = true
		}
	}
	return r
}

// updateAllocs updates the map m.info.
//
// "keep" identifies records which should be preserved while "new" identifies
// records which should be updated. Any other record will be removed.  The
// return value lists those structs which were removed or replaced.
func (m *Manager) updateAllocs(keep map[ids.BroadcastID]bool, new map[ids.BroadcastID]*bInfo) []*bInfo {
	m.l.Lock()
	defer m.l.Unlock()

	if m.infos == nil { // occasionally happens when shutting down
		return nil
	}

	var ret []*bInfo

	// Make a pass through m.infos, deleting anything not in keep or new.
	for _, info := range m.infos {
		if keep[info.bID] {
			continue
		}
		if new[info.bID] == nil {
			ret = append(ret, info)
			delete(m.infos, info.bID)
			continue
		}
	}

	// Make a pass through new, updating m.infos.
	for id, info := range new {
		if m.infos[id] != nil {
			ret = append(ret, m.infos[id])
		}
		m.infos[id] = info
	}

	return ret
}

// Close attempts to shut down the Manager gracefully.
func (m *Manager) Close(ctx context.Context) error {
	close(m.done)
	m.l.Lock()
	defer m.l.Unlock()

	var errMsgs []string
	for _, i := range m.infos {
		i.useCount.Wait()
		if err := m.bs.CleanupAllocation(ctx, i.bID, i.aID); err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("[%v,%v]:\"%v\"", i.bID, i.aID, err))
		}
	}
	m.infos = nil

	if len(errMsgs) > 0 {
		return errors.New("errors cleaning up allocations - " + strings.Join(errMsgs, " "))
	}
	return nil
}
