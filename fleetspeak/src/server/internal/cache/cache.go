package cache

import (
	"sync"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
)

var (
	// How long client data should be considered valid for. Variable to support
	// unit testing.
	MaxAge = 30 * time.Second

	// We occasionally expunge old client data records, to be tidy
	// with RAM and prevent what would effectively be a slow memory leak as
	// clients come and go. Variable to support unit testing.
	expireInterval = 5 * time.Minute
)

// Clients is a cache of recently connected clients.
type Clients struct {
	m    map[common.ClientID]*clientEntry
	l    sync.RWMutex
	stop chan struct{}
}

type clientEntry struct {
	u time.Time
	d *db.ClientData
}

// NewClients returns a new cache of client data.
func NewClients() *Clients {
	ret := &Clients{
		m:    make(map[common.ClientID]*clientEntry),
		stop: make(chan struct{}),
	}
	go ret.expireLoop()
	return ret
}

// Get returns the cached client data, if there is sufficiently fresh data in
// the cache, otherwise nil.
func (c *Clients) Get(id common.ClientID) *db.ClientData {
	c.l.RLock()
	defer c.l.RUnlock()

	e := c.m[id]
	if e == nil || db.Now().Sub(e.u) > MaxAge {
		return nil
	}
	return e.d
}

// Update updates or sets the cached data for a particular client. If data is
// nil, it clears the data for the client.
func (c *Clients) Update(id common.ClientID, data *db.ClientData) {
	c.l.Lock()
	defer c.l.Unlock()

	if data == nil {
		delete(c.m, id)
	} else {
		c.m[id] = &clientEntry{
			u: db.Now(),
			d: data,
		}
	}
}

// Clear empties the cache, removing all entries.
func (c *Clients) Clear() {
	c.l.Lock()
	defer c.l.Unlock()

	c.m = make(map[common.ClientID]*clientEntry)
}

// Stop releases the resources required for background cache maintenence. The
// cache should not be used once Stop has been called.
func (c *Clients) Stop() {
	close(c.stop)
}

// Size returns the current size taken up by the cache, this is a count of
// client records, some of which may no longer be up to date.
func (c *Clients) Size() int {
	c.l.RLock()
	defer c.l.RUnlock()
	return len(c.m)
}

func (c *Clients) expireLoop() {
	t := time.NewTicker(expireInterval)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			c.expire()
		case <-c.stop:
			return
		}
	}
}

// expire prunes the cache to clean out clients that are no longer up to date.
func (c *Clients) expire() {
	c.l.Lock()
	defer c.l.Unlock()

	for k, e := range c.m {
		if db.Now().Sub(e.u) > MaxAge {
			delete(c.m, k)
		}
	}
}
