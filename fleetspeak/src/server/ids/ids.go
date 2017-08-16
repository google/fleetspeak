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

// Package ids defines identifier types and utility methods specific to the
// fleetspeak server and server components.
package ids

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
)

// BroadcastID identifies a broadcast using 8 arbitrary bytes and can be used as
// a map key.
type BroadcastID struct {
	id string
}

// BytesToBroadcastID creates a BroadcastID based on the provided bytes.
func BytesToBroadcastID(b []byte) (BroadcastID, error) {
	if len(b) == 0 {
		return BroadcastID{}, nil
	}
	if len(b) != 8 {
		return BroadcastID{}, errors.New("BroadcastID must be 8 bytes")
	}
	return BroadcastID{id: string(b)}, nil
}

// RandomBroadcastID creates a new random BroadcastID.
func RandomBroadcastID() (BroadcastID, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return BroadcastID{""}, err
	}
	return BroadcastID{string(b)}, nil
}

// StringToBroadcastID creates a BroadcastID based on the provided hex string.
func StringToBroadcastID(s string) (BroadcastID, error) {
	if len(s) != 16 {
		return BroadcastID{}, errors.New("BroadcastID must be 16 characters")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return BroadcastID{}, err
	}
	return BytesToBroadcastID(b)
}

// String implements fmt.Stringer.
func (i BroadcastID) String() string {
	if i.id == "" {
		return "nil"
	}
	return hex.EncodeToString([]byte(i.id))
}

// Bytes returns bytes stored by BroadcastID
func (i BroadcastID) Bytes() []byte {
	if i.id == "" {
		return nil
	}
	return []byte(i.id)
}

// AllocationID identifies an allocation from a broadcast.
type AllocationID struct {
	id string
}

// RandomAllocationID creates a new random AllocationID.
func RandomAllocationID() (AllocationID, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return AllocationID{""}, err
	}
	return AllocationID{string(b)}, nil
}

// Bytes returns bytes stored by AllocationID
func (i AllocationID) Bytes() []byte {
	return []byte(i.id)
}

// String implements fmt.Stringer
func (i AllocationID) String() string {
	return hex.EncodeToString(i.Bytes())
}
