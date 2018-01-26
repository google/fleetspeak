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

// +build windows

package regutil

import (
	"bytes"
	"testing"
)

func TestVerifyPath(t *testing.T) {
	if err := VerifyPath(""); err == nil {
		t.Fatal("Error expected, but err == nil, verification failed - path shouldn't be empty.")
	}

	if err := VerifyPath(`path\to\a\registry\key\without\hkey\prefix`); err == nil {
		t.Fatalf("Error expected, but err == nil, verification failed - path should start with %s.", hkeyPrefix)
	}

	if err := VerifyPath(`HKEY_LOCAL_MACHINE\path\to\a\registry\key\with\hkey\prefix`); err != nil {
		t.Fatalf("Verification of a valid path failed: %v", err)
	}
}

const tempRegPrefix = `HKEY_LOCAL_MACHINE\SOFTWARE\FleetspeakDevelopment\regutil_test`

func tearDown() {
	DeleteKey(tempRegPrefix)
}

func TestWriteBinaryValue(t *testing.T) {
	defer tearDown()

	if err := WriteBinaryValue(tempRegPrefix, "TestWriteBinaryValue", []byte("TestWriteBinaryValue value")); err != nil {
		t.Fatalf("WriteBinaryValue: %v", err)
	}
}

func TestReadBinaryValue(t *testing.T) {
	defer tearDown()

	value := []byte("TestReadBinaryValue value")

	if err := WriteBinaryValue(tempRegPrefix, "TestReadBinaryValue", value); err != nil {
		t.Fatalf("WriteBinaryValue: %v", err)
	}

	b, err := ReadBinaryValue(tempRegPrefix, "TestReadBinaryValue")
	if err != nil {
		t.Fatalf("ReadBinaryValue: %v", err)
	}

	if g, w := b, value; !bytes.Equal(g, w) {
		t.Fatalf("got %q, want %q", g, w)
	}
}

func TestWriteStringValue(t *testing.T) {
	defer tearDown()

	if err := WriteStringValue(tempRegPrefix, "TestWriteStringValue", "TestWriteStringValue value"); err != nil {
		t.Fatalf("WriteStringValue: %v", err)
	}
}

func TestReadStringValue(t *testing.T) {
	defer tearDown()

	value := "TestReadStringValue value"

	if err := WriteStringValue(tempRegPrefix, "TestReadStringValue", value); err != nil {
		t.Fatalf("WriteStringValue: %v", err)
	}

	s, err := ReadStringValue(tempRegPrefix, "TestReadStringValue")
	if err != nil {
		t.Fatalf("ReadStringValue: %v", err)
	}

	if g, w := s, value; g != w {
		t.Fatalf("got %q, want %q", g, w)
	}
}

func TestDeleteKey(t *testing.T) {
	if err := WriteStringValue(tempRegPrefix, "TestDeleteKey", "TestDeleteKey value"); err != nil {
		t.Fatalf("WriteStringValue: %v", err)
	}

	if err := DeleteKey(tempRegPrefix); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
}

func TestLs(t *testing.T) {
	defer tearDown()

	valueName := "TestLs"

	if err := WriteStringValue(tempRegPrefix, valueName, "TestLs value"); err != nil {
		t.Fatalf("WriteStringValue: %v", err)
	}

	vs, err := Ls(tempRegPrefix)
	if err != nil {
		t.Fatalf("Ls: %v", err)
	}

	if len(vs) != 1 {
		t.Fatalf("wrong number of elements: got %q, want []string{%q}", vs, valueName)
	}

	if vs[0] != valueName {
		t.Fatalf("got %q, want []string{%q}", vs, valueName)
	}
}
