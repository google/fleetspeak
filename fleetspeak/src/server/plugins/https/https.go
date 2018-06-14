// Copyright 2018 Google Inc.
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

// Package https contains a plugin which provides https based communication with
// clients.
package main

import (
	"fmt"
	"io/ioutil"
	"net"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/https"

	ppb "github.com/google/fleetspeak/fleetspeak/src/server/plugins/proto/plugins"
)

// HttpsFactory is a plugins.CommunicatorFactory
func HttpsFactory(configFile string) (comms.Communicator, error) {
	b, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Exitf("Unable to read component config file [%s]: %v", configFile, err)
	}
	var c ppb.HttpsConfig
	if err := proto.UnmarshalText(string(b), &c); err != nil {
		log.Exitf("Unable to parse component config file [%s]: %v", configFile, err)
	}
	l, err := net.Listen("tcp", c.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("unable to listen on [%s]: %v", c.ListenAddress, err)
	}
	return https.NewCommunicator(https.Params{
		Listener:  l,
		Cert:      []byte(c.Certificate),
		Key:       []byte(c.Key),
		Streaming: c.Streaming,
	})
}
