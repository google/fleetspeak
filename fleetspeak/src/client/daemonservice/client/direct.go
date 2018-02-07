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

// Package client is a go client library for daemonservice.
package client

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/client/channel"

	fcpb "github.com/google/fleetspeak/fleetspeak/src/client/channel/proto/fleetspeak_channel"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// Init initializes the library, assuming that we are in a process started by
// fleetspeak. If successful, it returns a channel.Channel which should be used
// to communicate with the fleetspeak system.
func Init(version string) (*channel.Channel, error) {
	strInFd := os.Getenv("FLEETSPEAK_COMMS_CHANNEL_INFD")
	if strInFd == "" {
		return nil, errors.New("environment variable FLEETSPEAK_COMMS_CHANNEL_INFD not set")
	}

	inFd, err := strconv.Atoi(strInFd)
	if err != nil {
		return nil, fmt.Errorf("unable to  parse FLEETSPEAK_COMMS_CHANNEL_INFD (%q): %v", strInFd, err)
	}

	pr := os.NewFile(uintptr(inFd), "-")

	strOutFd := os.Getenv("FLEETSPEAK_COMMS_CHANNEL_OUTFD")
	if strOutFd == "" {
		return nil, errors.New("environment variable FLEETSPEAK_COMMS_CHANNEL_OUTFD not set")
	}

	outFd, err := strconv.Atoi(strOutFd)
	if err != nil {
		return nil, fmt.Errorf("unable to  parse FLEETSPEAK_COMMS_CHANNEL_OUTFD (%q): %v", strOutFd, err)
	}

	pw := os.NewFile(uintptr(outFd), "-")
	c := channel.New(pr, pw)
	sd, err := ptypes.MarshalAny(&fcpb.StartupData{
		Pid:     int64(os.Getpid()),
		Version: version,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to marshal StartupData: %v", err)
	}
	c.Out <- &fspb.Message{
		MessageType: "StartupData",
		Destination: &fspb.Address{ServiceName: "system"},
		Data:        sd,
	}
	return c, nil
}
