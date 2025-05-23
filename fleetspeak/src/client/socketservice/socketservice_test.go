// Copyright 2017 Google Inc.
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

package socketservice

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/golang/glog"
	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/client/clitesting"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
	"github.com/google/fleetspeak/fleetspeak/src/common/anypbtest"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"

	sspb "github.com/google/fleetspeak/fleetspeak/src/client/socketservice/proto/fleetspeak_socketservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

type testStatsCollector struct {
	stats.NoopCollector
	socketsClosed atomic.Int64
}

func (sc *testStatsCollector) SocketServiceSocketClosed(_ string, _ error) {
	sc.socketsClosed.Add(1)
}

func SetUpSocketPath(t *testing.T, nameHint string) string {
	t.Helper()

	return filepath.Join(SetUpTempDir(t), nameHint)
}

func SetUpTempDir(t *testing.T) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		tmpDir, cleanUpFn := comtesting.GetTempDir("socketservice")
		t.Cleanup(cleanUpFn)
		return tmpDir
	}

	// We need unix socket paths to be short (see socketservice_unix.go), so we ignore the $TMPDIR env variable,
	// which could point to arbitrary directories, and use /tmp.
	tmpDir, err := os.MkdirTemp("/tmp", "socketservice_")
	if err != nil {
		t.Fatalf("os.MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	// Explicitly set the group owner for tempDir in case it was inherited from /tmp.
	if err := os.Chown(tmpDir, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("os.Chown(%q): %v", tmpDir, err)
	}
	return tmpDir
}

func isErrKilled(err error) bool {
	if runtime.GOOS == "windows" {
		return strings.HasSuffix(err.Error(), "exit status 1")
	}
	return strings.HasSuffix(err.Error(), "signal: killed")
}

// exerciseLoopback attempts to send messages through a testclient in loopback mode using
// socketPath.
func exerciseLoopback(t *testing.T, socketPath string) {
	t.Helper()

	s, err := Factory(&fspb.ClientServiceConfig{
		Name: "TestSocketService",
		Config: anypbtest.New(t, &sspb.Config{
			ApiProxyPath: socketPath,
		}),
	})
	if err != nil {
		t.Fatalf("Factory(...): %v", err)
	}

	stats := &testStatsCollector{}
	sc := clitesting.FakeServiceContext{
		OutChan:        make(chan *fspb.Message, 5),
		StatsCollector: stats,
	}
	if err := s.Start(&sc); err != nil {
		t.Fatalf("socketservice.Start(...): %v", err)
	}
	defer func() {
		// We are shutting down this service. Give the service a bit of time to ack
		// the messages to the testclient, so that they won't be repeated.
		time.Sleep(time.Second)
		if err := s.Stop(); err != nil {
			t.Errorf("Error stopping service: %v", err)
		}
	}()

	msgs := []*fspb.Message{
		{
			MessageId:   []byte("\000\000\000"),
			MessageType: "RequestTypeA",
		},
		{
			MessageId:   []byte("\000\000\001"),
			MessageType: "RequestTypeB",
		},
	}
	ctx := context.Background()
	go func() {
		for _, m := range msgs {
			log.Infof("Processing message [%x]", m.MessageId)
			err := s.ProcessMessage(ctx, m)
			if err != nil {
				t.Errorf("Error processing message: %v", err)
			}
			log.Info("Message Processed.")
		}
		log.Info("Done processing messages.")
	}()
	for _, m := range msgs {
		g := <-sc.OutChan
		got := proto.Clone(g).(*fspb.Message)
		got.SourceMessageId = nil

		want := proto.Clone(m).(*fspb.Message)
		want.MessageType = want.MessageType + "Response"

		if !proto.Equal(got, want) {
			t.Errorf("Unexpected message from loopback: got [%v], want [%v]", got, want)
		}
	}

	socketsClosed := stats.socketsClosed.Load()
	if want := int64(0); socketsClosed != want {
		t.Errorf("Got %d sockets closed, want %d", socketsClosed, want)
	}
	log.Infof("looped %d messages", len(msgs))
}

func TestLoopback(t *testing.T) {
	socketPath := SetUpSocketPath(t, "Loopback")
	cmd := exec.Command(testClient(t), "--mode=loopback", "--socket_path="+socketPath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() returned error: %v", err)
	}
	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Errorf("failed to kill testclient[%d]: %v", cmd.Process.Pid, err)
		}
		if err := cmd.Wait(); err != nil {
			if !isErrKilled(err) {
				t.Errorf("error waiting for testclient: %v", err)
			}
		}
	}()
	// Each call to exerciseLoopback closes and recreates the socket,
	// simulating a fs client restart. The testclient in loopback mode should
	// be able to handle this.
	exerciseLoopback(t, socketPath)
	exerciseLoopback(t, socketPath)
	exerciseLoopback(t, socketPath)
}

func TestAckLoopback(t *testing.T) {
	socketPath := SetUpSocketPath(t, "AckLoopback")
	cmd := exec.Command(testClient(t), "--mode=ackLoopback", "--socket_path="+socketPath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() returned error: %v", err)
	}
	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Errorf("failed to kill testclient[%d]: %v", cmd.Process.Pid, err)
		}
		if err := cmd.Wait(); err != nil {
			if !isErrKilled(err) {
				t.Errorf("error waiting for testclient: %v", err)
			}
		}
	}()
	exerciseLoopback(t, socketPath)
}

func TestStutteringLoopback(t *testing.T) {
	socketPath := SetUpSocketPath(t, "StutteringLoopback")
	cmd := exec.Command(testClient(t), "--mode=stutteringLoopback", "--socket_path="+socketPath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() returned error: %v", err)
	}
	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Errorf("failed to kill testclient[%d]: %v", cmd.Process.Pid, err)
		}
		if err := cmd.Wait(); err != nil {
			if !isErrKilled(err) {
				t.Errorf("error waiting for testclient: %v", err)
			}
		}
	}()

	s, err := Factory(&fspb.ClientServiceConfig{
		Name: "TestSocketService",
		Config: anypbtest.New(t, &sspb.Config{
			ApiProxyPath: socketPath,
		}),
	})
	if err != nil {
		t.Fatalf("Factory(...): %v", err)
	}

	starts := make(chan struct{}, 1)
	s.(*Service).newChan = func() {
		log.Infof("starting new channel")
		starts <- struct{}{}
	}

	stats := &testStatsCollector{}
	sc := clitesting.FakeServiceContext{
		OutChan:        make(chan *fspb.Message),
		StatsCollector: stats,
	}
	if err := s.Start(&sc); err != nil {
		t.Fatalf("socketservice.Start(...): %v", err)
	}
	ctx := context.Background()

	// Stuttering loopback will close the connection and reconnect after
	// every message.
	for i := range 5 {
		// Wait for our end of the connection to start/restart.
		<-starts
		err := s.ProcessMessage(ctx, &fspb.Message{
			MessageId:   []byte{0, 0, byte(i)},
			MessageType: "RequestType",
		})
		if err != nil {
			t.Errorf("Error processing message: %v", err)
		}
		// Wait for the looped message. After sending the message, the other end
		// will close and restart.
		m := <-sc.OutChan
		log.Infof("received looped back message: %x", m.MessageId)
	}
	if err := s.Stop(); err != nil {
		t.Errorf("Error stopping service: %v", err)
	}
	socketsClosed := stats.socketsClosed.Load()
	if want := int64(5); socketsClosed != want {
		t.Errorf("Got %d sockets closed, want %d", socketsClosed, want)
	}
}

func TestMaxSockLenUnix(t *testing.T) {
	var maxSockLen int
	if runtime.GOOS == "windows" {
		t.Skip("Not applicable to windows.")
	} else if runtime.GOOS == "darwin" {
		maxSockLen = 103
	} else {
		maxSockLen = 107
	}
	sockPath := "/tmp/This is a really really long path that definitely will not fit in the sockaddr_un struct on most unix platforms"
	want := fmt.Errorf("socket path is too long (120 chars) - max allowed length is %d: %s.tmp", maxSockLen, sockPath)
	_, got := listen(sockPath)
	if got.Error() != want.Error() {
		t.Errorf("Wrong error received. Want '%s'; got '%s'", want, got)
	}
}

func TestResourceMonitoring(t *testing.T) {
	socketPath := SetUpSocketPath(t, "Loopback")
	cmd := exec.Command(testClient(t), "--mode=loopback", "--socket_path="+socketPath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() returned error: %v", err)
	}
	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Errorf("failed to kill testclient[%d]: %v", cmd.Process.Pid, err)
		}
		if err := cmd.Wait(); err != nil {
			if !isErrKilled(err) {
				t.Errorf("error waiting for testclient: %v", err)
			}
		}
	}()

	s, err := Factory(&fspb.ClientServiceConfig{
		Name: "TestSocketService",
		Config: anypbtest.New(t, &sspb.Config{
			ApiProxyPath:                          socketPath,
			ResourceMonitoringSampleSize:          2,
			ResourceMonitoringSamplePeriodSeconds: 1,
		}),
	})
	if err != nil {
		t.Fatalf("Factory(...): %v", err)
	}

	sc := clitesting.FakeServiceContext{
		OutChan: make(chan *fspb.Message, 5),
	}
	if err := s.Start(&sc); err != nil {
		t.Fatalf("socketservice.Start(...): %v", err)
	}
	defer func() {
		if err := s.Stop(); err != nil {
			t.Errorf("Error stopping service: %v", err)
		}
	}()

	to := time.After(10 * time.Second)
	select {
	case <-to:
		t.Errorf("resource usage report not received")
		return
	case m := <-sc.OutChan:
		if m.MessageType != "ResourceUsage" {
			t.Errorf("expected ResourceUsage, got %+v", m)
		}
		rud := &mpb.ResourceUsageData{}
		if err := m.Data.UnmarshalTo(rud); err != nil {
			t.Fatalf("Unable to unmarshal ResourceUsageData: %v", err)
		}
		if rud.Pid != int64(cmd.Process.Pid) {
			t.Errorf("ResourceUsageData.Pid=%d, but test client has pid %d", rud.Pid, cmd.Process.Pid)
		}
		if rud.Version != "0.5" {
			t.Errorf("ResourceUsageData.Version=\"%s\" but expected \"0.5\"", rud.Version)
		}
	}
}

func TestStopDoesNotBlock(t *testing.T) {
	socketPath := SetUpSocketPath(t, "Loopback")
	stopTimeout := 10 * time.Second
	s, err := Factory(&fspb.ClientServiceConfig{
		Name: "TestSocketService",
		Config: anypbtest.New(t, &sspb.Config{
			ApiProxyPath: socketPath,
		}),
	})
	if err != nil {
		t.Fatalf("Factory(...): %v", err)
	}

	sc := clitesting.FakeServiceContext{
		OutChan: make(chan *fspb.Message, 5),
	}
	if err := s.Start(&sc); err != nil {
		t.Fatalf("socketservice.Start(...): %v", err)
	}

	timer := time.AfterFunc(stopTimeout, func() {
		panic(fmt.Sprintf("Socket service failed to stop after %v.", stopTimeout))
	})
	defer timer.Stop()

	// Make sure Fleetspeak does not block waiting for the (non-existent)
	// process on the other side of the socket to connect.
	err = s.Stop()

	if err != nil {
		t.Errorf("socketservice.Stop(): %v", err)
	}
}

func init() {
	// Hack so that socketservice logs get written to the right place.
	if d := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); d != "" {
		os.Setenv("GOOGLE_LOG_DIR", d)
	}
}
