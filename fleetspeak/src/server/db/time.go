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

package db

import (
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/ftime"

	tpb "github.com/golang/protobuf/ptypes/timestamp"
)

// Now is the clock used by the server. Normally just time.Now, but can be replaced to support testing. It should be used by db.Store implementations to determine the time.
func Now() time.Time {
	return ftime.Now()
}

// NowProto returns a proto representation of Now().
func NowProto() *tpb.Timestamp {
	n, err := ptypes.TimestampProto(Now())
	if err != nil {
		// Really shouldn't happen; the most likely situation is that we
		// are in a test using a badly broken Now.
		log.Fatalf("Unable to convert Now() to a protocol buffer: %s", err)
	}
	return n
}

// ClientRetryTime returns when a client message, being sent to the client
// approximately Now(), will be considered timed out and eligible to be sent
// again. It should be used by MessageStore implementations to determine when a
// message can next be provided by ClientMessagesForProcessing.
func ClientRetryTime() time.Time {
	return ftime.ClientRetryTime()
}

// ServerRetryTime returns when server message, whose processing is about to
// start, will be considered timed out and eligible to be processed again.
// It should be used by MessageStore implementations to determine when a message
// can next be provided to a MessageProcessor.
func ServerRetryTime(retryCount uint32) time.Time {
	return ftime.ServerRetryTime(retryCount)
}
