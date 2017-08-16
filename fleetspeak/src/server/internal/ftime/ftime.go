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

// Package ftime defines "fleetspeak time" as a global variable. This is the
// sense of time used by the fleetspeak server and it can be changed to support
// unit testing.
package ftime

import "time"

// Now is the time used by the Fleetspeak system. Variable to support unit testing.
var Now = time.Now
