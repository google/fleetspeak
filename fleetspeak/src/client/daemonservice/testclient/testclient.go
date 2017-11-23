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

// Package testclient implements a daemonservice client meant for testing.
package main

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"time"

	"flag"
	"log"

	"github.com/google/fleetspeak/fleetspeak/src/client/daemonservice/client"
)

var mode = flag.String("mode", "loopback", "Mode of operation. Options are: loopback, garbage, freeze, freezeHard, die, stdSpam, subprocess")

func main() {
	flag.Parse()

	switch *mode {
	case "loopback":
		loopback()
	case "garbage":
		garbage()
	case "freeze":
		freeze(false)
	case "die":
		os.Exit(42)
	case "stdSpam":
		stdSpam()
	case "freezeHard":
		freeze(true)
	case "subprocess":
		subprocess()
	default:
		log.Fatalf("unknown  mode: %v", *mode)
	}
}

// loopback sends received messages to the server, appending "Response" to the
// MessageType field.
func loopback() {
	log.Print("starting loopback")

	ch, err := client.Init()
	if err != nil {
		log.Fatalf("Unable to initialize client: %v", err)
	}

	for {
		select {
		case m, ok := <-ch.In:
			if !ok {
				return // normal shutdown
			}
			m.MessageType = m.MessageType + "Response"
			ch.Out <- m
		case e := <-ch.Err:
			log.Fatalf("Channel reported error: %v", e)
		}
	}
}

// garbage writes random data to the server.
func garbage() {
	log.Print("starting garbage")

	strOutFd := os.Getenv("FLEETSPEAK_COMMS_CHANNEL_OUTFD")
	if strOutFd == "" {
		log.Fatal("Environment variable FLEETSPEAK_COMMS_CHANNEL_OUTFD not set")
	}

	outFd, err := strconv.Atoi(strOutFd)
	if err != nil {
		log.Fatalf("Unable to parse FLEETSPEAK_COMMS_CHANNEL_OUTFD (%q): %v", strOutFd, err)
	}

	pw := os.NewFile(uintptr(outFd), "-")

	for {
		buf := make([]byte, 1024)
		rand.Read(buf)
		_, err := pw.Write(buf)
		if err != nil {
			log.Fatalf("Garbage write failed: %v", err)
		}
	}
}

// freeze does nothing for a period of time.
func freeze(hard bool) {
	log.Print("starting freeze")
	if hard {
		signal.Ignore(os.Interrupt)
	}
	time.Sleep(time.Hour)
}

// stdSpam sets up the interface, but then just writes ~5mb of set text to stdout and ~5mb to stderr.
func stdSpam() {
	_, err := client.Init()
	if err != nil {
		log.Fatalf("Unable to initialize client: %v", err)
	}

	for i := 0; i < 128*1024; i++ {
		fmt.Println("The quick brown fox jumped over the lazy dogs.")
		fmt.Fprintln(os.Stderr, "The brown quick fox jumped over some lazy dogs.")
	}
}

// subprocess starts a subprocess tree, writes a leaf pid to stdout, and waits
func subprocess() {
	log.Print("starting subprocess")

	_, err := client.Init()
	if err != nil {
		log.Fatalf("Unable to initialize client: %v", err)
	}

	cmd := exec.Command("/bin/bash", "-c", `
    /bin/bash -c '
      /bin/echo $$
      /bin/sleep infinity
    '
  `)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Fatalf("Unable to spawn tree: %v", err)
	}
	cmd.Wait()
}
