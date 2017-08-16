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

// Package client is a go client library for socketservice.Service.
package client

import (
	"net"
	"time"

	"log"
	"github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/channel"
	"github.com/google/fleetspeak/fleetspeak/src/client/socketservice/checks"
)

// OpenChannel creates a channel.RelentlessChannel to a fleetspeak server
// through an agreed upon unix domain socket.
func OpenChannel(socketPath string) *channel.RelentlessChannel {
	return channel.NewRelentlessChannel(
		func() (*channel.Channel, func()) {
			return buildChannel(socketPath)
		})
}

// buildChannel is almost a channel.Builder, meant to be wrapped to become one
// once socketPath is known.
func buildChannel(socketPath string) (*channel.Channel, func()) {
	// Try once per second until the fleetspeak server is available.
	var err error
	var conn *net.UnixConn

	for {
		if err = checks.CheckUnixSocket(socketPath); err != nil {
			log.Printf("failure checking perms of [%s], will retry: %v", socketPath, err)
		} else if conn, err = net.DialUnix("unix", nil, &net.UnixAddr{socketPath, "unix"}); err != nil {
			log.Printf("failed to connect to [%s], will retry: %v", socketPath, err)
		} else {
			log.Printf("connected to [%s]", socketPath)
			return channel.New(conn, conn), func() {
				// Because it is a socket, this is sufficient to terminate any pending
				// I/O operations.
				conn.Close()
			}
		}
		time.Sleep(time.Second)
	}
}
