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

package client

import (
	"net"
	"time"

	log "github.com/golang/glog"
	"github.com/google/fleetspeak/fleetspeak/src/client/channel"
	"github.com/google/fleetspeak/fleetspeak/src/client/socketservice/checks"
	"github.com/google/fleetspeak/fleetspeak/src/windows/wnixsocket"
)

// buildChannel is almost a channel.Builder, meant to be wrapped to become one
// once socketPath is known.
func buildChannel(socketPath string) (*channel.Channel, func()) {
	// Try once per second until the fleetspeak server is available.
	var err error
	var conn net.Conn

	timeout := time.Second
	for {
		if err = checks.CheckSocketFile(socketPath); err != nil {
			log.Warningf("Failure checking perms of [%s], will retry: %v", socketPath, err)
			time.Sleep(timeout)
		} else if conn, err = wnixsocket.Dial(socketPath, timeout); err != nil {
			log.Warningf("Failed to connect to [%s], will retry: %v", socketPath, err)
		} else {
			log.Infof("Connected to [%s]", socketPath)

			return channel.New(conn, conn), func() {
				// Because it is a socket, this is sufficient to terminate any pending
				// I/O operations.
				conn.Close()
			}
		}
	}
}
