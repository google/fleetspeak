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
	"fmt"
	"os/exec"
	"strings"

	"log"
	"context"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/client/internal/monitoring"
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
	err := ptypes.UnmarshalAny(conf.Config, ssConf)
	if err != nil {
		return nil, err
	}

	return &StdinService{
		conf:   conf,
		ssConf: ssConf,
	}, nil
}

// StdinService implements Service.
type StdinService struct {
	conf   *fspb.ClientServiceConfig
	ssConf *sspb.Config
	sc     service.Context
}

// Start implements Service.
func (s *StdinService) Start(sc service.Context) error {
	s.sc = sc

	return nil
}

// ProcessMessage implements Service.
func (s *StdinService) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	om := &sspb.OutputMessage{}

	if m.MessageType != "StdinServiceInputMessage" {
		return fmt.Errorf("unrecognized common.Message.message_type: %q", m.MessageType)
	}

	im := &sspb.InputMessage{}
	if err := ptypes.UnmarshalAny(m.Data, im); err != nil {
		return fmt.Errorf("error while unmarshalling common.Message.data: %v", err)
	}

	cmd := exec.Command(s.ssConf.Cmd, im.Args...)

	inBuf := bytes.NewBuffer(im.Input)
	var outBuf, errBuf bytes.Buffer

	cmd.Stdin = inBuf
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error while starting a process: %v", err)
	}

	ruf := monitoring.ResourceUsageFetcher{}
	initialRU, ruErr := ruf.ResourceUsageForPID(cmd.Process.Pid)
	if ruErr != nil {
		log.Printf("Failed to get resource usage for process: %v", ruErr)
	}

	waitChan := make(chan error)
	go func() {
		// Note that using cmd.Run() here triggers panics.
		waitChan <- cmd.Wait()
	}()

	var err error
	select {
	case err = <-waitChan:
	case <-ctx.Done():
		err = fmt.Errorf("context done: %v", ctx.Err())

		// The error message string literal is a copypaste from exec_unix.go .
		if e := cmd.Process.Kill(); e != nil && e.Error() != "os: process already finished" {
			err = fmt.Errorf("%v; also, an error occured while killing the process: %v", err, e)
		}

		e, ok := <-waitChan

		const (
			killedMessage    = "signal: killed"
			killedMessageWin = "exit status " // + number representing the return code.
		)

		if !ok {
			err = fmt.Errorf("%v; also, .Wait hasn't returned after killing the process", err)
		} else if e != nil &&
			e.Error() != killedMessage &&
			!strings.HasPrefix(e.Error(), killedMessageWin) {
			err = fmt.Errorf("%v; also, .Wait returned an error: %v", err, e)
		}
	}
	if err != nil {
		return fmt.Errorf("error while running a process: %v", err)
	}

	om.Stdout = outBuf.Bytes()
	om.Stderr = errBuf.Bytes()

	finalRU := ruf.ResourceUsageFromFinishedCmd(cmd)
	aggRU, ruErr := monitoring.AggregateResourceUsageForFinishedCmd(initialRU, finalRU)
	if ruErr != nil {
		log.Printf("Aggregation of resource-usage data failed: %v", ruErr)
	} else {
		om.ResourceUsage = aggRU
	}

	om.Timestamp = ptypes.TimestampNow()
	if err := s.respond(ctx, om); err != nil {
		return fmt.Errorf("error while trying to send a response to the requesting server: %v", err)
	}

	return nil
}

// Stop implements Service.
func (s *StdinService) Stop() error {
	return nil
}

func (s *StdinService) respond(ctx context.Context, om *sspb.OutputMessage) error {
	// TODO: Chunk the response into ~1MB parts.
	m := &fspb.Message{
		Destination: &fspb.Address{
			ServiceName: s.conf.Name,
		},
		MessageType: "StdinServiceOutputMessage",
	}

	var err error
	m.Data, err = ptypes.MarshalAny(om)
	if err != nil {
		return err
	}

	return s.sc.Send(ctx, service.AckMessage{M: m})
}
