package dbtesting

import (
	"testing"

	"github.com/google/fleetspeak/fleetspeak/src/inttesting/integrationtest"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
)

func integrationTest(t *testing.T, ms db.Store) {
	integrationtest.FRRIntegrationTest(t, ms, false)
}

func cloneHandlingTest(t *testing.T, ms db.Store) {
	integrationtest.CloneHandlingTest(t, ms)
}

func integrationTestSuite(t *testing.T, env DbTestEnv) {
	t.Run("IntegrationTestSuite", func(t *testing.T) {
		runTestSuite(t, env, map[string]func(*testing.T, db.Store){
			//"IntegrationTest":   integrationTest,
			"CloneHandlingTest": cloneHandlingTest,
		})
	})
}
