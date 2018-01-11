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

package broadcasts

import (
	"context"
	"path"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/ids"
	"github.com/google/fleetspeak/fleetspeak/src/server/internal/cache"
	"github.com/google/fleetspeak/fleetspeak/src/server/sqlite"

	apb "github.com/golang/protobuf/ptypes/any"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

func TestManager(t *testing.T) {
	tmpDir, tmpDirCleanup := comtesting.GetTempDir("broadcasts")
	defer tmpDirCleanup()
	ds, err := sqlite.MakeDatastore(path.Join(tmpDir, "TestManager.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	var bid []ids.BroadcastID
	for _, s := range []string{"0000000000000000", "0000000000000001", "0000000000000002"} {
		b, err := ids.StringToBroadcastID(s)
		if err != nil {
			t.Fatalf("BroadcastID(%v) failed: %v", s, err)
		}
		bid = append(bid, b)
	}

	for i, tc := range []struct {
		br  *spb.Broadcast
		lim uint64
	}{
		{
			br: &spb.Broadcast{
				BroadcastId:    bid[0].Bytes(),
				Source:         &fspb.Address{ServiceName: "testService"},
				MessageType:    "WindowsBroadcast1",
				RequiredLabels: []*fspb.Label{{ServiceName: "system", Label: "Windows"}},
				Data: &apb.Any{
					TypeUrl: "message proto name 1",
					Value:   []byte("message data 1"),
				},
			},
			lim: 8,
		},
		{
			br: &spb.Broadcast{
				BroadcastId:    bid[1].Bytes(),
				Source:         &fspb.Address{ServiceName: "testService"},
				MessageType:    "LinuxBroadcast",
				RequiredLabels: []*fspb.Label{{ServiceName: "system", Label: "Linux"}}},
			lim: 8,
		},
	} {
		if err := ds.CreateBroadcast(ctx, tc.br, tc.lim); err != nil {
			t.Fatalf("%v: Unable to CreateBroadcast(%v): %v", i, tc.br, err)
		}
	}

	bm, err := MakeManager(ctx, ds, 100*time.Millisecond, cache.NewClients())
	if err != nil {
		t.Fatal(err)
	}

	clientID, err := common.BytesToClientID([]byte{0, 0, 0, 0, 0, 0, 0, 1})
	if err != nil {
		t.Fatal(err)
	}

	cd := &db.ClientData{
		Key: []byte("A binary client key \x00\xff\x01\xfe"),
		Labels: []*fspb.Label{
			{ServiceName: "system", Label: "Windows"},
			{ServiceName: "system", Label: "client-version-0.01"}},
	}
	if err := ds.AddClient(ctx, clientID, cd); err != nil {
		t.Fatal(err)
	}

	msgs, err := bm.MakeBroadcastMessagesForClient(ctx, clientID, cd.Labels)
	if err != nil {
		t.Error(err)
	} else {
		if len(msgs) != 1 {
			t.Errorf("Expected 1 broadcast messages, got: %v", msgs)
		} else {
			if msgs[0].MessageType != "WindowsBroadcast1" {
				t.Errorf("Expected message of type [WindowsBroadcast1], got: [%v]", msgs[0])
			}
			if msgs[0].Data.TypeUrl != "message proto name 1" {
				t.Errorf("Expected TypeUrl of [message proto name 1], got: [%v]", msgs[0].Data.TypeUrl)
			}
			if msgs[0].CreationTime == nil {
				t.Error("Expected Creation time to be populated, was nil.")
			}
		}
	}

	msgs, err = bm.MakeBroadcastMessagesForClient(ctx, clientID, cd.Labels)
	if err != nil {
		t.Error(err)
	} else {
		if len(msgs) != 0 {
			t.Errorf("Expected 0 broadcast messages on second run, got: %v", msgs)
		}
	}

	br2 := &spb.Broadcast{
		BroadcastId:    bid[2].Bytes(),
		Source:         &fspb.Address{ServiceName: "testService"},
		MessageType:    "WindowsBroadcast2",
		RequiredLabels: []*fspb.Label{{ServiceName: "system", Label: "Windows"}},
		Data: &apb.Any{
			TypeUrl: "message proto name 2",
			Value:   []byte("message data 2"),
		}}
	if err := ds.CreateBroadcast(ctx, br2, 8); err != nil {
		t.Fatalf("Unable to CreateBroadcast(%v): %v", br2, err)
	}

	time.Sleep(300 * time.Millisecond)

	if err := bm.Close(ctx); err != nil {
		t.Error(err)
	}
}
