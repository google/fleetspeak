package main

import (
	"flag"
	"fmt"
	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/setup"
	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/tests"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

var (
	numClients          = flag.Int("num_clients", 1, "Number of clients")
	masterServerAddress = flag.String("ms_address", "", "Address of master server")
	serversFile         = flag.String("servers_file", "", "File with server hosts")
)

func run() error {
	dat, err := ioutil.ReadFile(*serversFile)
	if err != nil {
		return fmt.Errorf("Failed to read serversFile: %v", err)
	}
	serverHosts := strings.Fields(string(dat))

	startTime := time.Now()
	var clientIDs []string

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

	err = tests.RunTests(*masterServerAddress, clientIDs)
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
