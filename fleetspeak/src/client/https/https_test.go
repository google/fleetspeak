// Copyright 2024 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package https

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
)

type testStatsCollector struct {
	stats.CommunicatorCollector
	fetches atomic.Int64
}

func (c *testStatsCollector) AfterGetFileRequest(_, _, _ string, didFetch bool, err error) {
	if err == nil && didFetch {
		c.fetches.Add(1)
	}
}

func (*testStatsCollector) OutboundContactData(string, int, error) {}

func (*testStatsCollector) InboundContactData(string, int, error) {}

func createFakeServer(lastModified time.Time) (*httptest.Server, []string) {
	fakeServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := strings.NewReader("test")
		http.ServeContent(w, r, "test.txt", lastModified, content)
	}))
	hosts := []string{fakeServer.Listener.Addr().String()}
	fakeServer.Client().Timeout = time.Second
	return fakeServer, hosts
}

func doRequest(t *testing.T, hosts []string, client *http.Client, lastModifiedOnClient time.Time, stats stats.CommunicatorCollector) (string, time.Time) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reader, modTime, err := getFileIfModified(ctx, hosts, client, "TestService", "test.txt", lastModifiedOnClient, stats)
	if err != nil {
		t.Fatalf("getFileIfModified() failed: %v", err)
	}
	if reader != nil {
		defer reader.Close()
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("io.ReadAll() failed: %v", err)
		}
		return string(body), modTime
	}
	return "", modTime
}

func TestGetFileIfModified(t *testing.T) {
	stats := &testStatsCollector{}
	lastModifiedOnServer := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	lastModifiedOnClient := lastModifiedOnServer.Add(-time.Hour)
	fakeServer, hosts := createFakeServer(lastModifiedOnServer)
	defer fakeServer.Close()

	body, modTime := doRequest(t, hosts, fakeServer.Client(), lastModifiedOnClient, stats)
	if !modTime.Equal(lastModifiedOnServer) {
		t.Errorf("Unexpected modTime, got: %v, want: %v", modTime, lastModifiedOnServer)
	}
	if want := "test"; body != want {
		t.Errorf("Unexpected body, got: %q, want: %q", body, want)
	}
	fetches := stats.fetches.Load()
	if want := int64(1); fetches != want {
		t.Errorf("Unexpected fetch count, got: %d, want: %d", fetches, want)
	}
}

func TestGetFileIfNotModified(t *testing.T) {
	stats := &testStatsCollector{}
	lastModifiedOnServer := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	lastModifiedOnClient := lastModifiedOnServer
	fakeServer, hosts := createFakeServer(lastModifiedOnServer)
	defer fakeServer.Close()

	body, _ := doRequest(t, hosts, fakeServer.Client(), lastModifiedOnClient, stats)
	if want := ""; body != want {
		t.Errorf("Unexpected response body, got: %q, want: %q", body, want)
	}
	fetches := stats.fetches.Load()
	if want := int64(0); fetches != want {
		t.Errorf("Unexpected fetch count, got: %d, want: %d", fetches, want)
	}
}

func TestGetFileUnreachableHost(t *testing.T) {
	lastModifiedOnServer := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	lastModifiedOnClient := lastModifiedOnServer.Add(-time.Hour)
	fakeServer, hosts := createFakeServer(lastModifiedOnServer)
	defer fakeServer.Close()

	// prepend an unreachable host to the list of hosts, the operation
	// should still succeed by trying the next one in the list.
	hosts = append([]string{"unreachable_host"}, hosts...)

	body, modTime := doRequest(t, hosts, fakeServer.Client(), lastModifiedOnClient, stats.NoopCollector{})
	if !modTime.Equal(lastModifiedOnServer) {
		t.Errorf("Unexpected modTime, got: %v, want: %v", modTime, lastModifiedOnServer)
	}
	if want := "test"; body != want {
		t.Errorf("Unexpected body, got: %q, want: %q", body, want)
	}
}
