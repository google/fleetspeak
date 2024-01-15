//go:build oss

package daemonservice

import (
	"runtime"
	"testing"
)

func testClient(t *testing.T) []string {
	if runtime.GOOS == "windows" {
		return []string{`testclient\testclient.exe`}
	}

	return []string{"testclient/testclient"}
}

func testClientPY(t *testing.T) []string {
	return []string{"python", "-m", "fleetspeak.client_connector.testing.testclient"}
}

func testClientLauncherPY(t *testing.T) []string {
	return []string{"python", "-m", "fleetspeak.client_connector.testing.testclient_launcher"}
}
