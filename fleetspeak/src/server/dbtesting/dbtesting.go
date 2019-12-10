package dbtesting

import (
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"testing"
)

// DbTestEnv has to be implemented for each datastore where data store tests are expected to run.
type DbTestEnv interface {
	// Creates the database testing environment. Called once per database test suite.
	Create() error
	// Cleans the database testing environment before every test and returns a new Store instance to be used by the test.
	Clean() (db.Store, error)
	// Destroys the database testing environment after all the tests have run. Called once per database test suite.
	Destroy() error
}

// Predefined client ids to be used in tests.
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
