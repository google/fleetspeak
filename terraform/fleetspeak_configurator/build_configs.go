package main

import (
	"flag"
	"fmt"
	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/setup"
	"io/ioutil"
	"os"
	"strings"
)

var (
	configDir     = flag.String("config_dir", "", "Directory to put config files")
	numClients    = flag.Int("num_clients", 1, "Number of clients")
	serversFile   = flag.String("servers_file", "", "File with server hosts")
	mysqlAddress  = flag.String("mysql_address", "", "MySQL server address")
	mysqlDatabase = flag.String("mysql_database", "", "MySQL database name to use")
	mysqlUsername = flag.String("mysql_username", "", "MySQL username to use")
	mysqlPassword = flag.String("mysql_password", "", "MySQL password to use")
)

func run() error {
	dat, err := ioutil.ReadFile(*serversFile)
	if err != nil {
		return fmt.Errorf("Failed to read serversFile: %v", err)
	}
	serverHosts := strings.Fields(string(dat))

	err = setup.BuildConfigurations(*configDir, serverHosts, *numClients,
		setup.MysqlCredentials{
			Host:     *mysqlAddress,
			Password: *mysqlPassword,
			Username: *mysqlUsername,
			Database: *mysqlDatabase,
		})
	if err != nil {
		return fmt.Errorf("Failed to build configs: %v", err)
	}

	return nil
}

func main() {
	flag.Parse()
	err := run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
