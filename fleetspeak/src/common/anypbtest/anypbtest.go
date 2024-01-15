// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package anypbtest offers test helpers for working with AnyPb protos.
package anypbtest

import (
	"testing"

	"google.golang.org/protobuf/proto"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

// New creates a new anypb.Any, failing the test on error.
func New(t *testing.T, msg proto.Message) *anypb.Any {
	t.Helper()

	a, err := anypb.New(msg)
	if err != nil {
		t.Fatalf("anypb.New(%+v): %v", msg, err)
	}
	return a
}
