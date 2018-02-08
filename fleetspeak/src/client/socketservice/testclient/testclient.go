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

// Package testclient implements a socketservice client meant for testing.
package main

import (
	"flag"
	"os"
	"sync"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/client/channel"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/socketservice/client"
)

var mode = flag.String("mode", "loopback", "Mode of operation.")
var socketPath = flag.String("socket_path", "", "Location of socket to contact.")

func main() {
	if d := os.Getenv("TEST_UNDECLARED_OUTPUTS_DIR"); d != "" {
		os.Args = append(os.Args, "--log_dir="+d)
	}

	flag.Parse()

	// All of the modes require interaction with the socket.
	if *socketPath == "" {
		log.Exit("--socket_path is required")
	}

	switch *mode {
	case "loopback":
		loopback()
	case "ackLoopback":
		ackLoopback()
	case "stutteringLoopback":
		stutteringLoopback()
	default:
		log.Exitf("unknown mode: %s", *mode)
	}
}

func openChannel() *channel.RelentlessChannel {
	log.Infof("opening relentless channel to %s", *socketPath)
	return client.OpenChannel(*socketPath, "0.5")
}

func loopback() {
	log.Info("starting loopback")

	ch := openChannel()
	for m := range ch.In {
		log.Infof("Looping message of type [%s]", m.MessageType)
		m.MessageType = m.MessageType + "Response"
		ch.Out <- service.AckMessage{M: m}
		log.Infof("Message %x looped.", m.MessageId)
	}
}

func ackLoopback() {
	log.Info("starting acknowledging loopback")

	ch := openChannel()
	var w sync.WaitGroup
	for m := range ch.In {
		log.Infof("Looping message of type [%s]", m.MessageType)
		m.MessageType = m.MessageType + "Response"
		w.Add(1)
		ch.Out <- service.AckMessage{M: m, Ack: w.Done}
		log.Infof("Message %x looped.", m.MessageId)
		w.Wait()
		log.Infof("Message %x ack'd.", m.MessageId)
	}
}

func stutteringLoopback() {
	log.Info("starting stuttering loopback")

	ch := openChannel()
	var w sync.WaitGroup
	for {
		m, ok := <-ch.In
		if !ok {
			log.Fatal("RelentlessChannel unexpectedly closed.")
		}
		log.Infof("Looping message of type [%s]", m.MessageType)
		m.MessageType = m.MessageType + "Response"
		w.Add(1)
		ch.Out <- service.AckMessage{M: m, Ack: w.Done}
		log.Infof("Message %x looped.", m.MessageId)
		w.Wait()
		log.Infof("Message %x ack'd.", m.MessageId)

		close(ch.Out)

		ch = openChannel()
	}
}
