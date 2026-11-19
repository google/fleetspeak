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
