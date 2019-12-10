package dbtesting

import (
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"testing"
)

// DbTestEnv has to be implemented for each datastore where data store tests are expected to run.
type DbTestEnv interface {
	Create() error
	Clean() (db.Store, error)
	Destroy() error
}

var clientID, _ = common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 1})
var clientID2, _ = common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 2})
var clientID3, _ = common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 3})

// RunTestSuite runs a suite of databases tests.
func runTestSuite(t *testing.T, env DbTestEnv, tests map[string]func(*testing.T, db.Store)) {
	for n, fn := range tests {
		ms, err := env.Clean()
		if err != nil {
			t.Fatalf("Can't clean the datastore for test '%v': %v", n, err)
		}
		t.Run(n, func(t *testing.T) {
			fn(t, ms)
		})
	}
}

// DataStoreTestSuite combines all test suites for datastore testing.
func DataStoreTestSuite(t *testing.T, env DbTestEnv) {
	if err := env.Create(); err != nil {
		t.Fatalf("Can't create the datastore: %v", err)
	}

	messageStoreTestSuite(t, env)
	clientStoreTestSuite(t, env)
	broadcastStoreTestSuite(t, env)
	fileStoreTestSuite(t, env)
	integrationTestSuite(t, env)

	if err := env.Destroy(); err != nil {
		t.Fatalf("Can't destroy the datastore: %v", err)
	}
}
