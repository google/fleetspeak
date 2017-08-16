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

package common

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// ClientID identifies a client using 8 arbitrary bytes and can be used as a map
// key.
type ClientID struct {
	id string
}

// BytesToClientID creates a ClientID based on the provided bytes.
func BytesToClientID(b []byte) (ClientID, error) {
	if len(b) == 0 {
		return ClientID{}, nil
	}
	if len(b) != 8 {
		return ClientID{}, errors.New("ClientID must be 8 bytes")
	}
	return ClientID{id: string(b)}, nil
}

// StringToClientID creates a ClientID based on the provided hex string.
func StringToClientID(s string) (ClientID, error) {
	if s == "" || s == "nil" {
		return ClientID{}, nil
	}
	if len(s) != 16 {
		return ClientID{}, errors.New("ClientID must be 16 characters")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return ClientID{}, err
	}
	return BytesToClientID(b)
}

// MakeClientID generates a ClientID from the provided public key.
func MakeClientID(pk crypto.PublicKey) (ClientID, error) {
	b, err := x509.MarshalPKIXPublicKey(pk)
	if err != nil {
		return ClientID{}, err
	}
	h := sha256.Sum256(b)
	return BytesToClientID(h[0:8])
}

// IsNil returns a client id represents the nil/empty id.
func (i ClientID) IsNil() bool {
	return i.id == ""
}

// String implements fmt.Stringer.
func (i ClientID) String() string {
	if i.IsNil() {
		return "nil"
	}
	return hex.EncodeToString([]byte(i.id))
}

// Bytes returns bytes stored by ClientID
func (i ClientID) Bytes() []byte {
	if i.id == "" {
		return nil
	}
	return []byte(i.id)
}

// MessageID identifies a message using 32 arbitrary bytes and can be used as a
// map key.
type MessageID struct {
	id string
}

// BytesToMessageID converts a slice of 32 bytes to a message id.
func BytesToMessageID(b []byte) (MessageID, error) {
	if len(b) == 0 {
		return MessageID{}, nil
	}
	if len(b) != 32 {
		return MessageID{}, errors.New("MessageID must be 32 bytes")
	}
	return MessageID{id: string(b)}, nil
}

// MakeMessageID generates a MessageID from the provided Origin and
// OriginMessageID. It is deterministic, so that if a client needs to send a
// message twice, we can deduplicate the message by ID. It is a good hash so
// that we can be sure that IDs are uniformly distributed, even if the clients
// are miscoded or hostile.
func MakeMessageID(origin *fspb.Address, originID []byte) MessageID {
	b := bytes.NewBuffer(make([]byte, 0, len(originID)+len(origin.ClientId)+len(origin.ServiceName)+2))
	b.Write(origin.ClientId)
	b.WriteByte(0)
	b.WriteString(origin.ServiceName)
	b.WriteByte(0)
	b.Write(originID)
	hash := sha256.Sum256(b.Bytes())
	return MessageID{id: string(hash[:32])}
}

// RandomMessageID generates a MessageID from random bytes. This is appropriate
// for messages created on the server, client messages should use MakeMessageID.
func RandomMessageID() (MessageID, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return MessageID{}, err
	}
	return BytesToMessageID(b)
}

// StringToMessageID creates a MessageID based on the provided hex string.
func StringToMessageID(s string) (MessageID, error) {
	if s == "" || s == "nil" {
		return MessageID{}, nil
	}
	if len(s) != 64 {
		return MessageID{}, errors.New("MessageID must be 64 characters")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return MessageID{}, err
	}
	return BytesToMessageID(b)
}

// String implements fmt.Stringer.
func (i MessageID) String() string {
	if i.id == "" {
		return "nil"
	}
	return hex.EncodeToString([]byte(i.id))
}

// Bytes returns bytes stored by MessageID
func (i MessageID) Bytes() []byte {
	if i.IsNil() {
		return nil
	}
	return []byte(i.id)
}

// IsNil return whether MessageID represents the nil/empty id.
func (i MessageID) IsNil() bool {
	return i.id == ""
}
