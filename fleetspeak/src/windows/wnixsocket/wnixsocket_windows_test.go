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

// +build windows

package wnixsocket

import (
	"io"
	"io/ioutil"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/client/socketservice/checks"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
)

func TestWnixsocket(t *testing.T) {
	tmpDir, tmpDirCleanup := comtesting.GetTempDir("wnixsocket")
	defer tmpDirCleanup()
	socketPath := tmpDir + `\TestWnixsocketSocket`

	l, err := Listen(socketPath, 0600)
	if err != nil {
		t.Error(err)
	}

	if err = checks.CheckSocketFile(socketPath); err != nil {
		t.Error(err)
	}

	const (
		clientMsg = "Hello Wnixsocket server, Wnixsocket client here."
		serverMsg = "Hello Wnixsocket client, Wnixsocket server here."
	)

	done := make(chan struct{})
	go func() {
		conn, err := Dial(socketPath, time.Second)
		if err != nil {
			t.Error(err)
		}

		if n, err := conn.Write([]byte(clientMsg)); err != nil {
			t.Errorf("Wnixsocket .Write([]byte(%q)): (%d, %v)", clientMsg, n, err)
		}

		msgBuf, err := ioutil.ReadAll(conn)
		if err != nil {
			t.Error(err)
		}

		if err = conn.Close(); err != nil {
			t.Error(err)
		}

		if got, want := string(msgBuf), serverMsg; got != want {
			t.Errorf("Unexpected message from server; got [%q], want [%q]", got, want)
		}

		close(done)
	}()

	conn, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}

	if err = l.Close(); err != nil {
		t.Error(err)
	}

	msgBuf := make([]byte, len(clientMsg))
	n, err := io.ReadFull(conn, msgBuf)
	if err != nil {
		t.Errorf("io.ReadFull(...): (%d, %v)", n, err)
	}

	// Writes are blocking, so the order is important.
	if n, err := conn.Write([]byte(serverMsg)); err != nil {
		t.Errorf("Wnixsocket .Write([]byte(%q)): (%d, %v)", serverMsg, n, err)
	}

	if err = conn.Close(); err != nil {
		t.Error(err)
	}

	if got, want := string(msgBuf), clientMsg; got != want {
		t.Errorf("Unexpected message from client; got [%q], want [%q]", got, want)
	}

	<-done
}
