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

// Package stdinservice implements a service which, on request, executes a
// command on the client, passes data to its stdin and returns the result.
package stdinservice

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	anypb "google.golang.org/protobuf/types/known/anypb"
	tspb "google.golang.org/protobuf/types/known/timestamppb"

	"github.com/google/fleetspeak/fleetspeak/src/client/service"

	sspb "github.com/google/fleetspeak/fleetspeak/src/client/stdinservice/proto/fleetspeak_stdinservice"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// Factory is a service.Factory for StdinServices.
//
// In response to every received sspb.InputMessage, a StdinService fires
// up a process, pipes the given input message to its stdin, waits for it to
// finish and sends the contents of stdout and stderr back to the server. The
// process is expected to finish relatively quickly.
//
// conf.Config should be of proto type DaemonServiceConfig.
func Factory(conf *fspb.ClientServiceConfig) (service.Service, error) {
	ssConf := &sspb.Config{}
	err := conf.Config.UnmarshalTo(ssConf)
	if err != nil {
		return nil, err
	}

	return &StdinService{
		conf:   conf,
		ssConf: ssConf,
	}, nil
}

// StdinService implements a service.Service which runs a command one for each
// message received, passing data to it through stdin and getting the results
// through stdout/stderr.
type StdinService struct {
	conf   *fspb.ClientServiceConfig
	ssConf *sspb.Config
	sc     service.Context
}

// Start starts the StdinService.
func (s *StdinService) Start(sc service.Context) error {
	s.sc = sc

	return nil
}

// ProcessMessage processes an incoming message from the server side.
func (s *StdinService) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	if m.MessageType != "StdinServiceInputMessage" {
		return fmt.Errorf("unrecognized common.Message.message_type: %q", m.MessageType)
	}

	im := &sspb.InputMessage{}
	if err := m.Data.UnmarshalTo(im); err != nil {
		return fmt.Errorf("error while unmarshalling common.Message.data: %v", err)
	}

	var stdout, stderr bytes.Buffer

	var args []string
	args = append(args, s.ssConf.Args...)
	args = append(args, im.Args...)

	cmd := exec.CommandContext(ctx, s.ssConf.Cmd, args...)
	cmd.Stdin = bytes.NewBuffer(im.Input)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return err
	}

	om := &sspb.OutputMessage{
		Stdout:    stdout.Bytes(),
		Stderr:    stderr.Bytes(),
		Timestamp: tspb.Now(),
	}
	if err := s.respond(ctx, om); err != nil {
		return fmt.Errorf("error while trying to send a response to the requesting server: %v", err)
	}

	return nil
}

// Stop stops the StdinService.
func (s *StdinService) Stop() error {
	return nil
}

func (s *StdinService) respond(ctx context.Context, om *sspb.OutputMessage) error {
	// TODO: Chunk the response into ~1MB parts.
	data, err := anypb.New(om)
	if err != nil {
		return err
	}

	m := &fspb.Message{
		Destination: &fspb.Address{
			ServiceName: s.conf.Name,
		},
		MessageType: "StdinServiceOutputMessage",
		Data:        data,
	}

	return s.sc.Send(ctx, service.AckMessage{M: m})
}
