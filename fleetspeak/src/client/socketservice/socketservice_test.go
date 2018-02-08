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
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"testing"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/client/clitesting"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"

	sspb "github.com/google/fleetspeak/fleetspeak/src/client/socketservice/proto/fleetspeak_socketservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

func getTempDir() (string, func(), error) {
	if runtime.GOOS == "windows" {
		tempDir, cleanUpFn := comtesting.GetTempDir("socketservice")
		return tempDir, cleanUpFn, nil
	}
	// We need unix socket paths to be short (see socketservice_unix.go), so we ignore the $TMPDIR env variable,
	// which could point to arbitrary directories, and use /tmp.
	tempDir, err := ioutil.TempDir("/tmp", "socketservice_")
	// Explicitly set the group owner for tempDir in case it was inherited from /tmp.
	if err := os.Chown(tempDir, os.Getuid(), os.Getgid()); err != nil {
		return "", func() {}, fmt.Errorf("os.Chown(%q) failed: %v", tempDir, err)
	}
	return tempDir, func() {}, err
}

func testClient() string {
	return "testclient/testclient"
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
	ssc := sspb.Config{
		ApiProxyPath: socketPath,
	}
	sscAny, err := ptypes.MarshalAny(&ssc)
	if err != nil {
		t.Fatalf("ptypes.MarshalAny(*socketservice.Config): %v", err)
	}
	s, err := Factory(&fspb.ClientServiceConfig{
		Name:   "TestSocketService",
		Config: sscAny,
	})
	if err != nil {
		t.Fatalf("Factory(...): %v", err)
	}

	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message, 5),
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
	log.Infof("looped %d messages", len(msgs))
}

func TestLoopback(t *testing.T) {
	tmpDir, cleanUpFn, err := getTempDir()
	if err != nil {
		t.Fatalf("Failed to create tempdir for test: %v", err)
	}
	defer cleanUpFn()
	socketPath := path.Join(tmpDir, "Loopback")

	cmd := exec.Command(testClient(), "--mode=loopback", "--socket_path="+socketPath)
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
	tmpDir, cleanUpFn, err := getTempDir()
	if err != nil {
		t.Fatalf("Failed to create tempdir for test: %v", err)
	}
	defer cleanUpFn()
	socketPath := path.Join(tmpDir, "AckLoopback")

	cmd := exec.Command(testClient(), "--mode=ackLoopback", "--socket_path="+socketPath)
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
	tmpDir, cleanUpFn, err := getTempDir()
	if err != nil {
		t.Fatalf("Failed to create tempdir for test: %v", err)
	}
	defer cleanUpFn()
	socketPath := path.Join(tmpDir, "StutteringLoopback")

	cmd := exec.Command(testClient(), "--mode=stutteringLoopback", "--socket_path="+socketPath)
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

	ssc := sspb.Config{
		ApiProxyPath: socketPath,
	}
	sscAny, err := ptypes.MarshalAny(&ssc)
	if err != nil {
		t.Fatalf("ptypes.MarshalAny(*socketservice.Config): %v", err)
	}
	s, err := Factory(&fspb.ClientServiceConfig{
		Name:   "TestSocketService",
		Config: sscAny,
	})
	if err != nil {
		t.Fatalf("Factory(...): %v", err)
	}

	starts := make(chan struct{}, 1)
	s.(*Service).newChan = func() {
		log.Infof("starting new channel")
		starts <- struct{}{}
	}

	sc := clitesting.MockServiceContext{
		OutChan: make(chan *fspb.Message),
	}
	if err := s.Start(&sc); err != nil {
		t.Fatalf("socketservice.Start(...): %v", err)
	}
	ctx := context.Background()

	// Stuttering loopback will close the connection and reconnect after
	// every message.
	for i := 0; i < 5; i++ {
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
	tmpDir, cleanUpFn, err := getTempDir()
	if err != nil {
		t.Fatalf("Failed to create tempdir for test: %v", err)
	}
	defer cleanUpFn()
	socketPath := path.Join(tmpDir, "Loopback")

	cmd := exec.Command(testClient(), "--mode=loopback", "--socket_path="+socketPath)
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

	ssc := sspb.Config{
		ApiProxyPath:                          socketPath,
		ResourceMonitoringSampleSize:          2,
		ResourceMonitoringSamplePeriodSeconds: 1,
	}
	sscAny, err := ptypes.MarshalAny(&ssc)
	if err != nil {
		t.Fatalf("ptypes.MarshalAny(*socketservice.Config): %v", err)
	}
	s, err := Factory(&fspb.ClientServiceConfig{
		Name:   "TestSocketService",
		Config: sscAny,
	})
	if err != nil {
		t.Fatalf("Factory(...): %v", err)
	}

	sc := clitesting.MockServiceContext{
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
		if err := ptypes.UnmarshalAny(m.Data, rud); err != nil {
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
