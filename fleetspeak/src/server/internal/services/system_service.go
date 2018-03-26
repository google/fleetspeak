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

package services

import (
	"context"
	"errors"
	"fmt"
	"sync"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/cache"
	"github.com/google/fleetspeak/fleetspeak/src/server/service"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	apb "github.com/golang/protobuf/ptypes/any"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	mpb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak_monitoring"
)

const (
	clientServiceName = "client"
)

// A systemService contains references to all the components we need to
// operate. It is populated directly by MakeServer as a special case, as the
// Datastore isn't provided to normal services.
type systemService struct {
	sctx      service.Context
	stats     stats.Collector
	datastore db.Store
	w         sync.WaitGroup
	cc        *cache.Clients
}

func (s *systemService) Start(sctx service.Context) error {
	s.sctx = sctx
	return nil
}

func (s *systemService) Stop() error {
	return nil
}

func (s *systemService) ProcessMessage(ctx context.Context, m *fspb.Message) error {
	mid, _ := common.BytesToMessageID(m.MessageId)
	if m.Source == nil {
		return errors.New("Source is nil")
	}
	cid, err := common.BytesToClientID(m.Source.ClientId)
	if err != nil || cid.IsNil() {
		return fmt.Errorf("invalid source client id[%v]: %v", m.Source.ClientId, err)
	}
	// all of our messages should have data
	if m.Data == nil {
		return errors.New("no Data present")
	}

	switch m.MessageType {
	case "MessageAck":
		return s.processMessageAck(ctx, mid, cid, m.Data)
	case "MessageError":
		return s.processMessageError(ctx, cid, m.Data)
	case "ClientInfo":
		return s.processClientInfo(ctx, cid, m.Data)
	case "ResourceUsage":
		return s.processResourceUsage(ctx, cid, m.Data, m.ValidationInfo)
	default:
	}

	return fmt.Errorf("unknown system message type: %v", m.MessageType)
}

// processMessageAck processes a message MessageAck from a client.
func (s *systemService) processMessageAck(ctx context.Context, mid common.MessageID, cid common.ClientID, d *apb.Any) error {
	var data fspb.MessageAckData
	if err := ptypes.UnmarshalAny(d, &data); err != nil {
		return fmt.Errorf("unable to unmarshal data as MessageAckData: %v", err)
	}

	ids := make([]common.MessageID, 0, len(data.MessageIds))
	for _, b := range data.MessageIds {
		id, err := common.BytesToMessageID(b)
		if err != nil {
			return fmt.Errorf("MessageAckData contains invalid message id[%v]: %v", b, err)
		}
		ids = append(ids, id)
	}

	msgs, err := s.datastore.GetMessages(ctx, ids, false)
	if err != nil {
		return service.TemporaryError{E: fmt.Errorf("unable to retrieve messages to ack: %v", err)}
	}

	for _, msg := range msgs {
		if msg.Result == nil {
			mmid, err := common.BytesToMessageID(msg.MessageId)
			if err != nil {
				log.Errorf("%v: retrieved message with bad message id[%v]: %v", mid, msg.MessageId, err)
				continue
			}
			mcid, err := common.BytesToClientID(msg.Destination.ClientId)
			if err != nil {
				log.Errorf("%v: retrieved message[%v] with bad client id[%v]: %v", mid, mmid, msg.Destination.ClientId, err)
				continue
			}
			if cid != mcid {
				if msg.Source != nil && msg.Source.ServiceName == "system" && msg.MessageType == "RekeyRequest" {
					// RekeyRequests are special - they are acked by the new client ID. Since
					// the mcid is a random number, we'll assume that this client really did
					// receive the RekeyRequest under its previous id.
					log.Infof("%v: client [%v] acked RekeyRequest sent to [%v] - rekey complete.", mid, cid, mcid)
				} else {
					log.Errorf("%v: attempt by client [%v] to ack a message meant for client [%v]", mid, cid, mcid)
					continue
				}
			}
			if err := s.datastore.SetMessageResult(ctx, mcid, mmid, &fspb.MessageResult{ProcessedTime: db.NowProto()}); err != nil {
				log.Errorf("%v: unable to mark message [%v] processed: %v", mid, mmid, err)
			}
		}
	}
	return nil
}

// processMessageError processes a MessageError message.
func (s *systemService) processMessageError(ctx context.Context, cid common.ClientID, d *apb.Any) error {
	var data fspb.MessageErrorData
	if err := ptypes.UnmarshalAny(d, &data); err != nil {
		return fmt.Errorf("unable to unmarshal data as MessageErrorData: %v", err)
	}

	id, err := common.BytesToMessageID(data.MessageId)
	if err != nil {
		return fmt.Errorf("MessageErr Data contains bad message id[%v]: %v", data.MessageId, err)
	}

	msgs, err := s.datastore.GetMessages(ctx, []common.MessageID{id}, false)
	if err != nil {
		return service.TemporaryError{E: fmt.Errorf("error from GetMessage([]{%v}): %v", id, err)}
	}
	if len(msgs) != 1 {
		return fmt.Errorf("expected one result from GetMessages, got %v", len(msgs))
	}
	msg := msgs[0]
	mcid, err := common.BytesToClientID(msg.Destination.ClientId)
	if err != nil {
		return fmt.Errorf("retrieved message [%v] has bad client id[%v]: %v", id, msg.Destination.ClientId, err)
	}
	if mcid != cid {
		return fmt.Errorf("attempt by client [%v] to ack a message meant for client [%v]", cid, mcid)
	}
	if err := s.datastore.SetMessageResult(ctx, mcid, id,
		&fspb.MessageResult{
			ProcessedTime: db.NowProto(),
			Failed:        true,
			FailedReason:  data.Error,
		}); err != nil {
		return service.TemporaryError{E: fmt.Errorf("unable to mark message [%v] as failed: %v", id, err)}
	}
	return nil
}

// processClientInfo processes a ClientInfo message.
func (s *systemService) processClientInfo(ctx context.Context, cid common.ClientID, d *apb.Any) error {
	var data fspb.ClientInfoData
	if err := ptypes.UnmarshalAny(d, &data); err != nil {
		return fmt.Errorf("unable to unmarshal data as ClientInfoData: %v", err)
	}
	cd, err := s.datastore.GetClientData(ctx, cid)
	if err != nil {
		return service.TemporaryError{E: fmt.Errorf("GetClientData(%v) failed: %v", cid, err)}
	}

	// We create a set of the new client labels.
	nl := make(map[string]bool)
	for _, l := range data.Labels {
		if l.ServiceName != clientServiceName {
			log.Errorf("attempt to set non-client label: %v", l)
			continue
		}
		nl[l.Label] = true
	}

	// Remove labels not in nl, remember labels already present.
	ol := make(map[string]bool)
	for _, l := range cd.Labels {
		if l.ServiceName == clientServiceName {
			if !nl[l.Label] {
				if err = s.datastore.RemoveClientLabel(ctx, cid, l); err != nil {
					return service.TemporaryError{E: fmt.Errorf("unable to remove label[%v]: %v", l, err)}
				}
			} else {
				ol[l.Label] = true
			}
		}
	}

	// Add labels from nl which are not yet present.
	for _, l := range data.Labels {
		if l.ServiceName != clientServiceName {
			continue
		}
		if !ol[l.Label] {
			if err = s.datastore.AddClientLabel(ctx, cid, l); err != nil {
				return service.TemporaryError{E: fmt.Errorf("unable to add label[%v]: %v", l, err)}
			}
		}
	}
	// Forget anything we know about this client. Other servers could have
	// now-stale data, but this client is likely to stick with us due to
	// connection reuse.
	s.cc.Update(cid, nil)
	return nil
}

// processResourceUsage processes a ResourceUsageData message.
func (s *systemService) processResourceUsage(ctx context.Context, cid common.ClientID, d *apb.Any, v *fspb.ValidationInfo) error {
	var rud mpb.ResourceUsageData
	if err := ptypes.UnmarshalAny(d, &rud); err != nil {
		return fmt.Errorf("unable to unmarshal data as ResourceUsageData: %v", err)
	}

	cd, err := s.sctx.GetClientData(ctx, cid)
	if err != nil {
		log.Errorf("Failed to get client data for %v: %v", cid, err)
	}
	s.stats.ResourceUsageDataReceived(cd, rud, v)
	if err := s.datastore.RecordResourceUsageData(ctx, cid, rud); err != nil {
		err = fmt.Errorf("failed to write resource-usage data: %v", err)
		return err
	}
	return nil
}
