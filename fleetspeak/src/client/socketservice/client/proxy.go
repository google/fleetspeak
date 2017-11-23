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
	"github.com/google/fleetspeak/fleetspeak/src/client/channel"
)

// OpenChannel creates a channel.RelentlessChannel to a fleetspeak server
// through an agreed upon unix domain socket.
func OpenChannel(socketPath string) *channel.RelentlessChannel {
	return channel.NewRelentlessChannel(
		func() (*channel.Channel, func()) {
			return buildChannel(socketPath)
		})
}
