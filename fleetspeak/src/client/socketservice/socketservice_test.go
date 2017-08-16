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
	"os/exec"
	"path"
	"strings"
	"testing"
	"time"

	"log"

	"context"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/fleetspeak/fleetspeak/src/client/clitesting"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"

	sspb "github.com/google/fleetspeak/fleetspeak/src/client/socketservice/proto/fleetspeak_socketservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func testClient() string {
	return "testclient/testclient"
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
			log.Printf("Processing message [%x]", m.MessageId)
			err := s.ProcessMessage(ctx, m)
			if err != nil {
				t.Errorf("Error processing message: %v", err)
			}
			log.Print("Message Processed.")
		}
		log.Print("Done processing messages.")
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
	log.Printf("looped %d messages", len(msgs))
}

func TestLoopback(t *testing.T) {
	tmpDir := comtesting.GetTempDir("socketservice")
	socketPath := path.Join(tmpDir, "sockets/TestLoopbackSocket")

	cmd := exec.Command(testClient(), "--mode=loopback", "--socket_path="+socketPath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() returned error: %v", err)
	}
	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Errorf("failed to kill testclient[%d]: %v", cmd.Process.Pid, err)
		}
		if err := cmd.Wait(); err != nil {
			if !strings.HasSuffix(err.Error(), "signal: killed") {
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
	tmpDir := comtesting.GetTempDir("socketservice")
	socketPath := path.Join(tmpDir, "sockets/TestAckLoopbackSocket")

	cmd := exec.Command(testClient(), "--mode=ackLoopback", "--socket_path="+socketPath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() returned error: %v", err)
	}
	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Errorf("failed to kill testclient[%d]: %v", cmd.Process.Pid, err)
		}
		if err := cmd.Wait(); err != nil {
			if !strings.HasSuffix(err.Error(), "signal: killed") {
				t.Errorf("error waiting for testclient: %v", err)
			}
		}
	}()
	exerciseLoopback(t, socketPath)
}

func TestStutteringLoopback(t *testing.T) {
	tmpDir := comtesting.GetTempDir("socketservice")
	socketPath := path.Join(tmpDir, "sockets/TestStutteringLoopbackSocket")

	cmd := exec.Command(testClient(), "--mode=stutteringLoopback", "--socket_path="+socketPath)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cmd.Start() returned error: %v", err)
	}
	defer func() {
		if err := cmd.Process.Kill(); err != nil {
			t.Errorf("failed to kill testclient[%d]: %v", cmd.Process.Pid, err)
		}
		if err := cmd.Wait(); err != nil {
			if !strings.HasSuffix(err.Error(), "signal: killed") {
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
		log.Printf("starting new channel")
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
		log.Printf("received looped back message: %x", m.MessageId)
	}
	if err := s.Stop(); err != nil {
		t.Errorf("Error stopping service: %v", err)
	}
}
