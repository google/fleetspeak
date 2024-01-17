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

var (
	// Used by mock server as the last modified date of the requested file
	lastModifiedOnServer = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
)

type statsCollector struct {
	stats.HTTPSCollector
	okCount atomic.Int64
}

func (c *statsCollector) AfterGetFileRequest(host, service, name string, modSince time.Time, response *http.Response, err error) {
	if response != nil && response.StatusCode == http.StatusOK {
		c.okCount.Add(1)
	}
}

func createMockServer() (*httptest.Server, []string) {
	mockServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := strings.NewReader("test")
		http.ServeContent(w, r, "test.txt", lastModifiedOnServer, content)
	}))
	hosts := []string{mockServer.Listener.Addr().String()}
	mockServer.Client().Timeout = time.Second
	return mockServer, hosts
}

func doRequest(t *testing.T, hosts []string, client *http.Client, lastModifiedOnClient time.Time, stats stats.HTTPSCollector) (string, time.Time) {
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
	stats := &statsCollector{}
	lastModifiedOnClient := lastModifiedOnServer.Add(-time.Hour)
	mockServer, hosts := createMockServer()
	defer mockServer.Close()

	body, modTime := doRequest(t, hosts, mockServer.Client(), lastModifiedOnClient, stats)
	if !modTime.Equal(lastModifiedOnServer) {
		t.Errorf("Unexpected modTime, got: %v, want: %v", modTime, lastModifiedOnServer)
	}
	if want := "test"; body != want {
		t.Errorf("Unexpected body, got: %q, want: %q", body, want)
	}
	okCount := stats.okCount.Load()
	if want := int64(1); okCount != want {
		t.Errorf("Unexpected status OK count, got: %d, want: %d", okCount, want)
	}
}

func TestGetFileIfNotModified(t *testing.T) {
	stats := &statsCollector{}
	lastModifiedOnClient := lastModifiedOnServer
	mockServer, hosts := createMockServer()
	defer mockServer.Close()

	body, _ := doRequest(t, hosts, mockServer.Client(), lastModifiedOnClient, stats)
	if want := ""; body != want {
		t.Errorf("Unexpected response body, got: %q, want: %q", body, want)
	}
	okCount := stats.okCount.Load()
	if want := int64(0); okCount != want {
		t.Errorf("Unexpected status OK count, got: %d, want: %d", okCount, want)
	}
}

func TestGetFileUnreachableHost(t *testing.T) {
	lastModifiedOnClient := lastModifiedOnServer.Add(-time.Hour)
	mockServer, hosts := createMockServer()
	defer mockServer.Close()

	// prepend an unreachable host to the list of hosts, the operation
	// should still succeed by trying the next one in the list.
	hosts = append([]string{"unreachable_host"}, hosts...)

	body, modTime := doRequest(t, hosts, mockServer.Client(), lastModifiedOnClient, stats.NoopCollector{})
	if !modTime.Equal(lastModifiedOnServer) {
		t.Errorf("Unexpected modTime, got: %v, want: %v", modTime, lastModifiedOnServer)
	}
	if want := "test"; body != want {
		t.Errorf("Unexpected body, got: %q, want: %q", body, want)
	}
}
