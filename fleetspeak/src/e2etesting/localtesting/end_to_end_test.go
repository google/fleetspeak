package localtesting_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/setup"
	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/tests"
)

var (
	mysqlAddress  = flag.String("mysql_address", "", "MySQL server address")
	mysqlDatabase = flag.String("mysql_database", "", "MySQL database name to use")
	mysqlUsername = flag.String("mysql_username", "", "MySQL username to use")
	mysqlPassword = flag.String("mysql_password", "", "MySQL password to use")
	numClients    = flag.Int("num_clients", 3, "Number of clients to test")
	numServers    = flag.Int("num_servers", 2, "Number of servers to test")
)

func parseFlags() {
	flag.Parse()
	for flagVar, envVarName := range map[*string]string{
		mysqlAddress:  "MYSQL_TEST_ADDR",
		mysqlUsername: "MYSQL_TEST_USER",
		mysqlPassword: "MYSQL_TEST_PASS",
		mysqlDatabase: "MYSQL_TEST_E2E_DB",
	} {
		val := os.Getenv(envVarName)
		if len(val) > 0 {
			*flagVar = val
		}
	}
}

// Test end to end
func TestLocalEndToEnd(t *testing.T) {
	parseFlags()
	if *mysqlAddress == "" {
		t.Skip("Mysql address not provided")
	}
	if *mysqlUsername == "" {
		t.Skip("Mysql user not provided")
	}
	if *mysqlDatabase == "" {
		t.Skip("Mysql database for end-to-end testing not provided")
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	for i := 0; i < 4; i++ {
		wd = filepath.Dir(wd)
	}
	err = os.Chdir(wd)
	if err != nil {
		t.Fatalf("Failed to change directory: %v", err)
	}

	frontendAddress := "localhost:6000"
	msAddress := "localhost:6059"

	var componentsInfo setup.ComponentsInfo
	err = componentsInfo.ConfigureAndStart(setup.MysqlCredentials{Host: *mysqlAddress, Password: *mysqlPassword, Username: *mysqlUsername, Database: *mysqlDatabase}, frontendAddress, msAddress, *numServers, *numClients)
	defer componentsInfo.KillAll()
	if err != nil {
		t.Fatalf("Failed to start components: %v", err)
	}
	tests.RunTests(t, msAddress, componentsInfo.ClientIDs)
}
