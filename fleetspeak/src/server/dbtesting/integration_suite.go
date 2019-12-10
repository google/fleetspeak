package dbtesting

import (
	"testing"

	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/inttesting/integrationtest"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
)

func integrationTest(t *testing.T, ms db.Store) {
	tmpDir, tmpDirCleanup := comtesting.GetTempDir("mysql_integration")
	defer tmpDirCleanup()

	integrationtest.FRRIntegrationTest(t, ms, tmpDir, false)
}

func cloneHandlingTest(t *testing.T, ms db.Store) {
	tmpDir, tmpDirCleanup := comtesting.GetTempDir("mysql_clone_handling")
	defer tmpDirCleanup()

	integrationtest.CloneHandlingTest(t, ms, tmpDir)
}

func integrationTestSuite(t *testing.T, env DbTestEnv) {
	t.Run("IntegrationTestSuite", func(t *testing.T) {
		runTestSuite(t, env, map[string]func(*testing.T, db.Store){
			"IntegrationTest":   integrationTest,
			"CloneHandlingTest": cloneHandlingTest,
		})
	})
}
