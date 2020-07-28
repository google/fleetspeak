package main

import (
	"flag"
	"fmt"
	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/setup"
	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/tests"
	"os"
	"time"
)

var (
	numClients          = flag.Int("num_clients", 1, "Number of clients")
	numServers          = flag.Int("num_servers", 1, "Number of servers")
	masterServerAddress = flag.String("ms_address", "", "Address of master server")
)

func run() error {
	serverHosts := make([]string, *numServers)
	for i := 0; i < *numServers; i++ {
		fmt.Scanf("%s", &serverHosts[i])
	}

	startTime := time.Now()
	var clientIDs []string
	var err error

	for i := 0; i < 20; i++ {
		// All clients that connected more than 30 minutes ago are considered inactive (not newly connected)
		clientIDs, err = setup.WaitForNewClientIDs(fmt.Sprintf("%v:6061", serverHosts[0]), startTime.Add(-time.Minute*30), *numClients)
		if err == nil {
			break
		}
		if time.Now().After(startTime.Add(time.Minute * 10)) {
			return fmt.Errorf("Not all clients connected (connected: %v, expected: %v, connected clients: %v): %v", len(clientIDs), *numClients, clientIDs, err)
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
	fmt.Println("Test start time: ", startTime.Format("2006-01-02 15:04:05"))
	err := run()
	if err != nil {
		fmt.Printf("FAIL: %v", err)
		os.Exit(1)
	} else {
		dur := time.Now().Sub(startTime)
		fmt.Printf("OK: End to end tests passed in %v seconds\n", dur.Seconds())
	}
}
