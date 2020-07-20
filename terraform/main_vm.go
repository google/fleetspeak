package main

import (
	"flag"
	"fmt"
	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/lib"
	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/tests"
	"os"
	"time"
)

var (
	configDir           = flag.String("config_dir", "", "Directory to put config files")
	numClients          = flag.Int("num_clients", 1, "Number of clients")
	numServers          = flag.Int("num_servers", 1, "Number of servers")
	mysqlAddress        = flag.String("mysql_address", "", "MySQL server address")
	mysqlDatabase       = flag.String("mysql_database", "", "MySQL database name to use")
	mysqlUsername       = flag.String("mysql_username", "", "MySQL username to use")
	mysqlPassword       = flag.String("mysql_password", "", "MySQL password to use")
	masterServerAddress = flag.String("ms_address", "", "Address of master server")
)

func run() error {
	serverHosts := make([]string, *numServers)
	for i := 0; i < *numServers; i++ {
		fmt.Scanf("%s", &serverHosts[i])
	}

	err := setup.BuildConfigurations(*configDir, serverHosts, *numClients,
		setup.MysqlCredentials{
			Host:     *mysqlAddress,
			Password: *mysqlPassword,
			Username: *mysqlUsername,
			Database: *mysqlDatabase,
		})
	if err != nil {
		return fmt.Errorf("Failed to build configs: %v", err)
	}

	startTime := time.Now()
	var clientIDs []string

	for i := 0; i < 20; i++ {
		clientIDs, err = setup.WaitForNewClientIDs(fmt.Sprintf("%v:6061", serverHosts[0]), startTime, *numClients)
		if err == nil {
			break
		}
		if i == 19 {
			return fmt.Errorf("Not all clients connected: %v", err)
		}
	}

	err = endtoendtests.RunTest(*masterServerAddress, clientIDs)
	if err != nil {
		return fmt.Errorf("test failed: %v", err)
	}

	return nil
}

func main() {
	flag.Parse()
	startTime := time.Now()
	fmt.Println("Start time: ", startTime.Format("2006-01-02 15:04:05"))
	err := run()
	if err != nil {
		fmt.Printf("FAIL: %v", err)
		os.Exit(1)
	} else {
		dur := time.Now().Sub(startTime)
		fmt.Printf("OK: End to end tests passed in %v seconds\n", dur.Seconds())
	}
}
