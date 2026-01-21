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

//go:build !google_internal

// Package should lets callers indicate impossible conditions from the code.
//
// It is a lightweight way to make these impossible conditions observable,
// when it is hooked up with an appropriate monitoring framework.
package should

// Never indicates that the given condition should never (or seldom) happen.
//
// Callers should only pass a fixed set of strings to Never(),
// to avoid a state explosion in monitoring counters.
func Never(condition string) {
	// Unfortunately empty in the open source version.
}
