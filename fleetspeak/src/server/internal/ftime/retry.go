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

package ftime

import (
	"math"
	"math/rand"
	"time"
)

// ClientRetryTime determines how long to wait for an acknowledgement before
// sending a message to a client again. The normal implementation waits one
// hour. It is mutable primarily to support testing.
var ClientRetryTime = func() time.Time {
	return Now().Add(time.Hour)
}

// ServerRetryTime determines how long to wait before attempting to process a
// message again on the FS server. The normal implementation provides
// exponential backoff with jitter, with an initial wait of 1-2 min. It is
// mutable primarily to support testing.
var ServerRetryTime = func(retryCount uint32) time.Time {
	delay := float64(time.Minute) * math.Pow(1.1, float64(retryCount))
	delay *= 1.0 + rand.Float64()

	return Now().Add(time.Duration(delay))
}
