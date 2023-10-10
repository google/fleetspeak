package cloudtesting_test

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/setup"
	"github.com/google/fleetspeak/fleetspeak/src/e2etesting/tests"
)

var (
	numClients          = flag.Int("num_clients", 0, "Number of clients")
	masterServerAddress = flag.String("ms_address", "", "Address of master server")
	serversFile         = flag.String("servers_file", "", "File with server hosts")
)

func TestCloudEndToEnd(t *testing.T) {
	flag.Parse()

	if *numClients == 0 {
		t.Skip("num_clients flag is required to run this test.")
	}
	if *masterServerAddress == "" {
		t.Skip("ms_address flag is required to run this test.")
	}
	if *serversFile == "" {
		t.Skip("servers_file flag is required to run this test.")
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	err = os.Chdir(filepath.Dir(filepath.Dir(wd)))
	if err != nil {
		t.Fatal("Failed to cd to fleetspeak directory")
	}

	dat, err := ioutil.ReadFile(*serversFile)
	if err != nil {
		t.Fatalf("Failed to read serversFile: %v", err)
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
			t.Fatalf("Not all clients connected (connected: %v, expected: %v, connected clients: %v): %v", len(clientIDs), *numClients, clientIDs, err)
		}
	}

	tests.RunTests(t, *masterServerAddress, clientIDs)
}
