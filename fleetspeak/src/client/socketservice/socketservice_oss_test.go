//go:build !google_internal

package socketservice

import "testing"

func testClient(t *testing.T) string {
	_ = t // intentionally unused
	return "testclient/testclient"
}
