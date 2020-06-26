package main

import (
	"flag"
	"fmt"
	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/lib"
	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/tests"
	"os"
)

var (
	mysqlDatabase = flag.String("mysql_database", "", "MySQL database name to use")
	mysqlUsername = flag.String("mysql_username", "", "MySQL username to use")
	mysqlPassword = flag.String("mysql_password", "", "MySQL password to use")
)

func run() error {
	msPort := 6059
	var componentCmds setup.ComponentCmds
	err := componentCmds.ConfigureAndStart(setup.MysqlCredentials{Password: *mysqlPassword, Username: *mysqlUsername, Database: *mysqlDatabase}, msPort)
	defer componentCmds.KillAll()
	if err != nil {
		return fmt.Errorf("Failed to start components: %v", err)
	}

	err = endtoendtests.RunTest(msPort, componentCmds.StartedClientID)
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
