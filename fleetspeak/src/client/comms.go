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

package client

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"fmt"

	log "github.com/golang/glog"
	"google.golang.org/protobuf/proto"
	tspb "google.golang.org/protobuf/types/known/timestamppb"

	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/client/service"
	"github.com/google/fleetspeak/fleetspeak/src/client/stats"
	"github.com/google/fleetspeak/fleetspeak/src/common"

	clpb "github.com/google/fleetspeak/fleetspeak/src/client/proto/fleetspeak_client"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// commsContext is the default implementation for comms.Context.
type commsContext struct {
	c *Client
}

// Outbox implements comms.Context.
func (c commsContext) Outbox() <-chan comms.MessageInfo {
	return c.c.outbox
}

// ProcessingBeacon implements comms.Context.
func (c commsContext) ProcessingBeacon() <-chan struct{} {
	return c.c.processingBeacon
}

// MakeContactData implements comms.Context.
func (c commsContext) MakeContactData(toSend []*fspb.Message, baseCount map[string]uint64) (wcd *fspb.WrappedContactData, processedMessages map[string]uint64, err error) {
	defer func() {
		c.c.stats.ContactDataCreated(wcd, err)
	}()

	am, pm := c.c.sc.Counts()
	allowedMessages := make(map[string]uint64)
	for k, a := range am {
		if b, ok := baseCount[k]; !ok {
			allowedMessages[k] = inboxSize - (a - pm[k])
		} else {
			allowedMessages[k] = pm[k] - b
		}
	}
	log.V(2).Infof("Creating ContactData with %d messages and giving tokens: %v", len(toSend), allowedMessages)

	// Create the bytes transferred with this contact.
	cd := &fspb.ContactData{
		SequencingNonce: c.c.config.SequencingNonce(),
		Messages:        toSend,
		ClientClock:     tspb.Now(),
		AllowedMessages: allowedMessages,
	}
	b, err := proto.Marshal(cd)
	if err != nil {
		return nil, nil, err
	}

	// Pick the non-repetitive part out of the config manager's labels.
	labels := c.c.config.Labels()
	stringLabels := make([]string, 0, len(labels))
	for _, l := range labels {
		stringLabels = append(stringLabels, l.Label)
	}
	// Create extra sigs.
	sigs := make([]*fspb.Signature, 0, len(c.c.signers))
	for _, signer := range c.c.signers {
		if sig := signer.SignContact(b); sig != nil {
			sigs = append(sigs, sig)
		}
	}

	return &fspb.WrappedContactData{
		ContactData:  b,
		Signatures:   sigs,
		ClientLabels: stringLabels,
	}, pm, nil
}

// ProcessContactData implements comms.Context.
func (c commsContext) ProcessContactData(ctx context.Context, cd *fspb.ContactData, streaming bool) (err error) {
	defer func() {
		c.c.stats.ContactDataProcessed(cd, streaming, err)
	}()

	if !streaming {
		c.c.config.SetSequencingNonce(cd.SequencingNonce)
	}
	for _, m := range cd.Messages {
		if err := c.c.ProcessMessage(ctx, service.AckMessage{M: m}); err != nil {
			log.Warningf("Unable to process message[%v] from server: %v", hex.EncodeToString(m.MessageId), err)
		}
	}
	return nil
}

// ChainRevoked implements comms.Context.
func (c commsContext) ChainRevoked(chain []*x509.Certificate) bool {
	return c.c.config.ChainRevoked(chain)
}

// CurrentID implements comms.Context.
func (c commsContext) CurrentID() common.ClientID {
	return c.c.config.ClientID()
}

// CurrentIdentity implements comms.Context.
func (c commsContext) CurrentIdentity() (comms.ClientIdentity, error) {
	p := c.c.config.CurrentState()
	if len(p.ClientKey) == 0 {
		return comms.ClientIdentity{}, fmt.Errorf("ClientKey not set")
	}

	k, err := x509.ParseECPrivateKey(p.ClientKey)
	if err != nil {
		return comms.ClientIdentity{}, fmt.Errorf("failed to parse ClientKey: %v", err)
	}
	id, err := common.MakeClientID(k.Public())
	if err != nil {
		return comms.ClientIdentity{}, fmt.Errorf("failed to create ClientID: %v", err)
	}

	return comms.ClientIdentity{
		ID:      id,
		Private: k,
		Public:  k.Public(),
	}, nil
}

// CommunicatorConfig implements comms.Context.
func (c commsContext) ServerInfo() (comms.ServerInfo, error) {
	cfg := c.c.config.Configuration()

	return comms.ServerInfo{
		TrustedCerts:            cfg.TrustedCerts,
		Servers:                 cfg.Servers,
		Proxy:                   cfg.Proxy,
		ClientCertificateHeader: cfg.ClientCertificateHeader,
		ServerName:              cfg.ServerName,
		PreferHttp2:             cfg.PreferHttp2,
	}, nil
}

// CommunicatorConfig implements comms.Context.
func (c commsContext) CommunicatorConfig() *clpb.CommunicatorConfig {
	return c.c.config.CommunicatorConfig()
}

// Stats implements comms.Context.
func (c commsContext) Stats() stats.Collector {
	return c.c.stats
}
