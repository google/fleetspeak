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
	"crypto/rand"
	"crypto/rsa"
	"testing"
)

func TestClientID(t *testing.T) {
	// A ClientID is either empty/nil, or is a sequence of 8 arbitrary
	// bytes.  It is displayed as 16 hex characters.
	for _, tc := range []struct {
		bytes     []byte
		want      string
		wantError bool
	}{
		{bytes: []byte{}, want: "nil"},
		{bytes: []byte("abc"), wantError: true},
		{bytes: []byte("\x01\x02\x03\x04\x05\x06\x07"), wantError: true},
		{bytes: []byte("\x00\x00\x00\x00\x00\x00\x00\x00"), want: "0000000000000000"},
		{bytes: []byte("\x00\x00\x00\xff\x00\x00\x00\x00"), want: "000000ff00000000"},
		{bytes: []byte("\x00\x01\x02\x03\x04\x05\x06\x07"), want: "0001020304050607"},
		{bytes: []byte("\x01\x02\x03\x04\x05\x06\x07\x08"), want: "0102030405060708"},
		{bytes: []byte("\x01\x02\x03\x04\x05\x06\x07\x08\x09"), wantError: true},
		{bytes: []byte("\xff\xfe\xfd\xfc\xfb\xfb\xfa\xf9"), want: "fffefdfcfbfbfaf9"},
	} {
		got, err := BytesToClientID(tc.bytes)
		switch {
		case err != nil && !tc.wantError:
			t.Errorf("BytesToClientID(%v) returned unexpected error: %v", tc.bytes, err)
		case err == nil && tc.wantError:
			t.Errorf("BytesToClientID(%v) returned [%v] expected error: %v", tc.bytes, got, err)
		case err == nil && !tc.wantError:
			if got.String() != tc.want {
				t.Errorf("BytesToClientID(%v) = %v want %v", tc.bytes, got, tc.want)
			}
		}
		if err != nil {
			h, err := StringToClientID(got.String())
			if err != nil {
				t.Errorf("Unable to convert %v back to client id: %v", got, err)
			}
			if h != got {
				t.Errorf("Mismatch after to and from string. Started with %v ended with %v", got, h)
			}
		}
	}

	// Verify that it works as a map key.
	id1, err := BytesToClientID([]byte("\x00\x00\x00\x00\x00\x00\x00\x01"))
	if err != nil {
		t.Fatal(err)
	}
	id2, err := BytesToClientID([]byte("\x00\x00\x00\x00\x00\x00\x00\x02"))
	if err != nil {
		t.Fatal(err)
	}
	id3, err := BytesToClientID([]byte("\x00\x00\x00\x00\x00\x00\x00\x03"))
	if err != nil {
		t.Fatal(err)
	}
	m := make(map[ClientID]int)
	m[id1] = 1
	m[id2] = 2
	m[id3] = 3
	if m[id1] != 1 {
		t.Errorf("id1 could not be used to retrieve 1, got %v", m[id1])
	}
	id2a, err := BytesToClientID([]byte("\x00\x00\x00\x00\x00\x00\x00\x02"))
	if err != nil {
		t.Fatal(err)
	}
	if m[id2a] != 2 {
		t.Errorf("Duplicate of id2 could not be used to retrieve 2, got %v", m[id2a])
	}
}

func TestMakeClientID(t *testing.T) {
	k1, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	id1, err := MakeClientID(k1.Public())
	if err != nil {
		t.Fatal(err)
	}
	id2, err := MakeClientID(k2.Public())
	if err != nil {
		t.Fatal(err)
	}
	if id1 == id2 {
		t.Errorf("Ids from different keys should be different, but both were [%v]", id1)
	}

	id1b, err := MakeClientID(k1.Public())
	if err != nil {
		t.Fatal(err)
	}
	if id1b != id1 {
		t.Errorf("Ids from key %v were different, should be identical: [%v], [%v]", k1, id1, id1b)
	}
}

func TestMessageID(t *testing.T) {
	// A MessageID is either empty/nil, or is a sequence of 32 arbitrary
	// bytes.  It is displayed in hex.
	for _, tc := range []struct {
		bytes     []byte
		want      string
		wantError bool
	}{
		{bytes: []byte{}, want: "nil"},
		{bytes: []byte("abc"), wantError: true},
		{bytes: []byte(
			"\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e"),
			wantError: true},
		{bytes: []byte(
			"\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f"),
			want: "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"},
		{bytes: []byte("\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f\x20"),
			wantError: true},
	} {
		got, err := BytesToMessageID(tc.bytes)
		switch {
		case err != nil && !tc.wantError:
			t.Errorf("BytesToMessageID(%v) returned unexpected error: %v", tc.bytes, err)
		case err == nil && tc.wantError:
			t.Errorf("BytesToMessageID(%v) returned [%v] expected error: %v", tc.bytes, got, err)
		case err == nil && !tc.wantError:
			if got.String() != tc.want {
				t.Errorf("BytesToMessageID(%v) = %v want %v", tc.bytes, got, tc.want)
			}
		}
	}

	// Verify that it works as a map key.
	id1, err := BytesToMessageID([]byte("\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01"))
	if err != nil {
		t.Fatal(err)
	}
	id2, err := BytesToMessageID([]byte("\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x02"))
	if err != nil {
		t.Fatal(err)
	}
	id3, err := BytesToMessageID([]byte("\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x03"))
	if err != nil {
		t.Fatal(err)
	}
	m := make(map[MessageID]int)
	m[id1] = 1
	m[id2] = 2
	m[id3] = 3
	if m[id1] != 1 {
		t.Errorf("id1 could not be used to retrieve 1, got %v", m[id1])
	}
	id2a, err := BytesToMessageID([]byte("\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x02"))
	if err != nil {
		t.Fatal(err)
	}
	if m[id2a] != 2 {
		t.Errorf("Duplicate of id2 could not be used to retrieve 2, got %v", m[id2a])
	}

}
