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

// Package messagesaver implements a Fleetspeak server.ServiceFactory which
// saves all messages received as files in a configured directory.
package messagesaver

import (
	"errors"
	"io/ioutil"
	"os"
	"path"

	"context"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/demo/proto/fleetspeak_messagesaver"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

// Factory is a server.ServiceFactory which creates a service which saves all
// received messages to configured directory.
func Factory(cfg *spb.ServiceConfig) (service.Service, error) {
	var c mpb.Config
	if err := ptypes.UnmarshalAny(cfg.Config, &c); err != nil {
		return nil, err
	}
	return &serv{path: c.Path}, nil
}

type serv struct {
	sctx service.Context
	path string
}

func (s *serv) Start(sctx service.Context) error {
	s.sctx = sctx
	if err := os.MkdirAll(s.path, 0700); err != nil {
		return err
	}
	return nil
}

func (s *serv) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	mid, err := common.BytesToMessageID(m.MessageId)
	if err != nil {
		return err
	}
	if len(m.Source.ClientId) == 0 {
		return errors.New("message has no source ClientID")
	}
	cid, err := common.BytesToClientID(m.Source.ClientId)
	if err != nil {
		return err
	}

	clientDir := path.Join(s.path, cid.String())
	if err := os.MkdirAll(clientDir, 0700); err != nil {
		return err
	}

	t, err := ptypes.Timestamp(m.CreationTime)
	if err != nil {
		return err
	}
	fileName := path.Join(clientDir, t.Format("2006.01.02_15:04:05.000_")+mid.String())
	if err := ioutil.WriteFile(fileName, []byte(proto.MarshalTextString(m)), 0700); err != nil {
		return err
	}
	return nil
}

func (s *serv) Stop() error {
	return nil
}
