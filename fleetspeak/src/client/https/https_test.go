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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
	"github.com/google/fleetspeak/fleetspeak/src/common"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

type testStatsCollector struct {
	stats.Collector
	fetches atomic.Int64
}

func (c *testStatsCollector) AfterGetFileRequest(_, _, _ string, didFetch bool, err error) {
	if err == nil && didFetch {
		c.fetches.Add(1)
	}
}

// testCommsContext is a minimal comms.Context for tests that run a Communicator
// or StreamingCommunicator without a client.Client.
type testCommsContext struct {
	comms.Context
	id                 common.ClientID
	priv               *ecdsa.PrivateKey
	clientLabels       []string
	si                 comms.ServerInfo
	conf               *clpb.CommunicatorConfig
	outbox             <-chan comms.MessageInfo
	beacon             <-chan struct{}
	stats              stats.Collector
	processContactData func(ctx context.Context, cd *fspb.ContactData, streaming bool) error
}

func (c *testCommsContext) Outbox() <-chan comms.MessageInfo  { return c.outbox }
func (c *testCommsContext) ProcessingBeacon() <-chan struct{} { return c.beacon }
func (c *testCommsContext) CurrentID() common.ClientID        { return c.id }
func (c *testCommsContext) CurrentIdentity() (comms.ClientIdentity, error) {
	var pub crypto.PublicKey
	if c.priv != nil {
		pub = c.priv.Public()
	}
	return comms.ClientIdentity{ID: c.id, Private: c.priv, Public: pub, Labels: c.clientLabels}, nil
}
func (c *testCommsContext) ServerInfo() (comms.ServerInfo, error)        { return c.si, nil }
func (c *testCommsContext) CommunicatorConfig() *clpb.CommunicatorConfig { return c.conf }
func (c *testCommsContext) ChainRevoked([]*x509.Certificate) bool        { return false }
func (c *testCommsContext) Stats() stats.Collector {
	if c.stats != nil {
		return c.stats
	}
	return stats.NoopCollector{}
}
func (c *testCommsContext) MakeContactData(msgs []*fspb.Message, _ map[string]uint64) (*fspb.WrappedContactData, map[string]uint64, error) {
	b, err := proto.Marshal(&fspb.ContactData{Messages: msgs})
	if err != nil {
		return nil, nil, err
	}
	return &fspb.WrappedContactData{ContactData: b}, nil, nil
}
func (c *testCommsContext) ProcessContactData(ctx context.Context, cd *fspb.ContactData, streaming bool) error {
	if c.processContactData != nil {
		return c.processContactData(ctx, cd, streaming)
	}
	return nil
}

func newTestIdentity(t *testing.T) (common.ClientID, *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() failed: %v", err)
	}
	id, err := common.MakeClientID(priv.Public())
	if err != nil {
		t.Fatalf("MakeClientID() failed: %v", err)
	}
	return id, priv
}

func newTestCommsContext(t *testing.T, si comms.ServerInfo, conf *clpb.CommunicatorConfig) *testCommsContext {
	t.Helper()
	id, priv := newTestIdentity(t)
	return &testCommsContext{
		id:   id,
		priv: priv,
		si:   si,
		conf: conf,
	}
}

func startCommunicator(t *testing.T, c comms.Communicator, cctx comms.Context) {
	t.Helper()
	if err := c.Setup(cctx); err != nil {
		t.Fatalf("Setup() failed: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	t.Cleanup(c.Stop)
}

func createFakeServer(lastModified time.Time, blockedLabels ...string) (*httptest.Server, []string) {
	blocked := make(map[string]bool)
	for _, label := range blockedLabels {
		blocked[label] = true
	}
	fakeServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, label := range r.Header.Values("X-Fleetspeak-Labels") {
			if blocked[label] {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		content := strings.NewReader("test")
		http.ServeContent(w, r, "test.txt", lastModified, content)
	}))
	hosts := []string{fakeServer.Listener.Addr().String()}
	fakeServer.Client().Timeout = time.Second
	return fakeServer, hosts
}

func doRequest(t *testing.T, cctx comms.Context, hosts []string, client *http.Client, lastModifiedOnClient time.Time) (string, time.Time) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	reader, modTime, err := getFileIfModified(ctx, cctx, nil, hosts, client, "TestService", "test.txt", lastModifiedOnClient)
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
	cctx := &testCommsContext{stats: stats}
	lastModifiedOnServer := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	lastModifiedOnClient := lastModifiedOnServer.Add(-time.Hour)
	fakeServer, hosts := createFakeServer(lastModifiedOnServer)
	defer fakeServer.Close()

	body, modTime := doRequest(t, cctx, hosts, fakeServer.Client(), lastModifiedOnClient)
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
	cctx := &testCommsContext{stats: stats}
	lastModifiedOnServer := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	lastModifiedOnClient := lastModifiedOnServer
	fakeServer, hosts := createFakeServer(lastModifiedOnServer)
	defer fakeServer.Close()

	body, _ := doRequest(t, cctx, hosts, fakeServer.Client(), lastModifiedOnClient)
	if want := ""; body != want {
		t.Errorf("Unexpected response body, got: %q, want: %q", body, want)
	}
	fetches := stats.fetches.Load()
	if want := int64(0); fetches != want {
		t.Errorf("Unexpected fetch count, got: %d, want: %d", fetches, want)
	}
}

func TestGetFileUnreachableHost(t *testing.T) {
	cctx := &testCommsContext{stats: &stats.NoopCollector{}}
	lastModifiedOnServer := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	lastModifiedOnClient := lastModifiedOnServer.Add(-time.Hour)
	fakeServer, hosts := createFakeServer(lastModifiedOnServer)
	defer fakeServer.Close()

	// prepend an unreachable host to the list of hosts, the operation
	// should still succeed by trying the next one in the list.
	hosts = append([]string{"unreachable_host"}, hosts...)

	body, modTime := doRequest(t, cctx, hosts, fakeServer.Client(), lastModifiedOnClient)
	if !modTime.Equal(lastModifiedOnServer) {
		t.Errorf("Unexpected modTime, got: %v, want: %v", modTime, lastModifiedOnServer)
	}
	if want := "test"; body != want {
		t.Errorf("Unexpected body, got: %q, want: %q", body, want)
	}
}

func TestGetFileUnauthorizedClient(t *testing.T) {
	stats := &testStatsCollector{}
	cctx := &testCommsContext{stats: stats, clientLabels: []string{"label1"}}
	lastModifiedOnServer := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	lastModifiedOnClient := lastModifiedOnServer.Add(-time.Hour)
	fakeServer, hosts := createFakeServer(lastModifiedOnServer, "label1")
	defer fakeServer.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, _, err := getFileIfModified(ctx, cctx, nil, hosts, fakeServer.Client(), "TestService", "test.txt", lastModifiedOnClient)

	if err == nil {
		t.Errorf("getFileIfModified() succeeded, want error")
	}
}

func TestBackoff(t *testing.T) {
	testCases := []struct {
		name                string
		baseSeconds         int32
		maxSeconds          int32
		consecutiveFailures int
		wantSec             int32
	}{
		{
			name:                "first attempt (0 failures)",
			baseSeconds:         1,
			maxSeconds:          60,
			consecutiveFailures: 0,
			wantSec:             1,
		},
		{
			name:                "second attempt (1 failure)",
			baseSeconds:         1,
			maxSeconds:          60,
			consecutiveFailures: 1,
			wantSec:             2,
		},
		{
			name:                "third attempt (2 failures)",
			baseSeconds:         1,
			maxSeconds:          60,
			consecutiveFailures: 2,
			wantSec:             4,
		},
		{
			name:                "fourth attempt (3 failures)",
			baseSeconds:         1,
			maxSeconds:          60,
			consecutiveFailures: 3,
			wantSec:             8,
		},
		{
			name:                "fifth attempt (4 failures)",
			baseSeconds:         1,
			maxSeconds:          60,
			consecutiveFailures: 4,
			wantSec:             16,
		},
		{
			name:                "sixth attempt (5 failures)",
			baseSeconds:         1,
			maxSeconds:          60,
			consecutiveFailures: 5,
			wantSec:             32,
		},
		{
			name:                "clamped to maxSeconds",
			baseSeconds:         1,
			maxSeconds:          60,
			consecutiveFailures: 6,
			wantSec:             60,
		},
		{
			name:                "clamped to maxSeconds on high failures",
			baseSeconds:         1,
			maxSeconds:          60,
			consecutiveFailures: 10,
			wantSec:             60,
		},
		{
			name:                "capped shift beyond 16",
			baseSeconds:         1,
			maxSeconds:          100000,
			consecutiveFailures: 20,
			wantSec:             65536,
		},
		{
			name:                "zero baseSeconds defaults to 1",
			baseSeconds:         0,
			maxSeconds:          60,
			consecutiveFailures: 0,
			wantSec:             1,
		},
		{
			name:                "negative baseSeconds defaults to 1",
			baseSeconds:         -5,
			maxSeconds:          60,
			consecutiveFailures: 0,
			wantSec:             1,
		},
		{
			name:                "zero maxSeconds defaults to 300",
			baseSeconds:         1,
			maxSeconds:          0,
			consecutiveFailures: 10,
			wantSec:             300,
		},
		{
			name:                "negative maxSeconds defaults to 300",
			baseSeconds:         1,
			maxSeconds:          -10,
			consecutiveFailures: 10,
			wantSec:             300,
		},
		{
			name:                "maxSeconds less than baseSeconds clamps to baseSeconds",
			baseSeconds:         10,
			maxSeconds:          5,
			consecutiveFailures: 2,
			wantSec:             10,
		},
		{
			name:                "negative consecutiveFailures treated as 0",
			baseSeconds:         1,
			maxSeconds:          60,
			consecutiveFailures: -1,
			wantSec:             1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := backoff(tc.baseSeconds, tc.maxSeconds, tc.consecutiveFailures)
			if got != tc.wantSec {
				t.Errorf("backoff(%d, %d, %d) = %d, want %d", tc.baseSeconds, tc.maxSeconds, tc.consecutiveFailures, got, tc.wantSec)
			}
		})
	}
}

func TestBackoffWithJitter(t *testing.T) {
	testCases := []struct {
		name                string
		baseSeconds         int32
		maxSeconds          int32
		consecutiveFailures int
		wantMin             time.Duration
		wantMax             time.Duration
	}{
		{
			name:                "attempt 0 (1s to 1.5s)",
			baseSeconds:         1,
			maxSeconds:          60,
			consecutiveFailures: 0,
			wantMin:             time.Second,
			wantMax:             1500 * time.Millisecond,
		},
		{
			name:                "attempt 1 (2s to 3s)",
			baseSeconds:         1,
			maxSeconds:          60,
			consecutiveFailures: 1,
			wantMin:             2 * time.Second,
			wantMax:             3 * time.Second,
		},
		{
			name:                "attempt 2 (4s to 6s)",
			baseSeconds:         1,
			maxSeconds:          60,
			consecutiveFailures: 2,
			wantMin:             4 * time.Second,
			wantMax:             6 * time.Second,
		},
		{
			name:                "clamped to maxSec (60s to 90s)",
			baseSeconds:         1,
			maxSeconds:          60,
			consecutiveFailures: 10,
			wantMin:             60 * time.Second,
			wantMax:             90 * time.Second,
		},
		{
			name:                "zero and negative arguments fallback",
			baseSeconds:         0,
			maxSeconds:          0,
			consecutiveFailures: -1,
			wantMin:             time.Second,
			wantMax:             1500 * time.Millisecond,
		},
		{
			name:                "maxSec < baseSec clamps to baseSec",
			baseSeconds:         10,
			maxSeconds:          5,
			consecutiveFailures: 1,
			wantMin:             10 * time.Second,
			wantMax:             15 * time.Second,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var minD, maxD time.Duration
			for i := 0; i < 50; i++ {
				d := backoffWithJitter(tc.baseSeconds, tc.maxSeconds, tc.consecutiveFailures)
				if d < tc.wantMin || d > tc.wantMax {
					t.Errorf("backoffWithJitter(%d, %d, %d) = %v, want between %v and %v",
						tc.baseSeconds, tc.maxSeconds, tc.consecutiveFailures, d, tc.wantMin, tc.wantMax)
				}
				if i == 0 || d < minD {
					minD = d
				}
				if i == 0 || d > maxD {
					maxD = d
				}
			}
			if minD == maxD {
				t.Errorf("backoffWithJitter(%d, %d, %d) did not jitter: min == max == %v",
					tc.baseSeconds, tc.maxSeconds, tc.consecutiveFailures, minD)
			}
		})
	}
}

func TestJitterZero(t *testing.T) {
	if got := jitter(0); got != 0 {
		t.Errorf("jitter(0) = %v, want 0", got)
	}
}
