package main

import (
	"flag"
	"fmt"
	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/lib"
	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/tests"
	"os"
)

var (
	mysqlAddress  = flag.String("mysql_address", "", "MySQL server address")
	mysqlDatabase = flag.String("mysql_database", "", "MySQL database name to use")
	mysqlUsername = flag.String("mysql_username", "", "MySQL username to use")
	mysqlPassword = flag.String("mysql_password", "", "MySQL password to use")
	numClients    = flag.Int("num_clients", 1, "Number of clients to test")
	numServers    = flag.Int("num_servers", 1, "Number of servers to test")
)

func run() error {
	msAddress := "localhost:6059"

	var componentsInfo setup.ComponentsInfo
	err := componentsInfo.ConfigureAndStart(setup.MysqlCredentials{Host: *mysqlAddress, Password: *mysqlPassword, Username: *mysqlUsername, Database: *mysqlDatabase}, msAddress, *numServers, *numClients)
	defer componentsInfo.KillAll()
	if err != nil {
		return fmt.Errorf("Failed to start components: %v", err)
	}
	err = endtoendtests.RunTest(msAddress, componentsInfo.ClientIDs)
	if err != nil {
		return fmt.Errorf("Failed to run tests: %v", err)
	}

	return nil
}

func main() {
	flag.Parse()
	err := run()
	if err != nil {
		fmt.Printf("FAIL: %v\n", err)
		os.Exit(1)
	} else {
		fmt.Println("OK: End to end tests passed")
	}
}
