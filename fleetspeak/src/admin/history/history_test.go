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

package history

import (
	"testing"
	"time"

	tspb "github.com/golang/protobuf/ptypes/timestamp"
	spb "github.com/google/fleetspeak/fleetspeak/src/server/proto/fleetspeak_server"
)

func TestSummary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      []*spb.ClientContact
		want    Summary
		wantErr bool
	}{
		{ // An empty list shouldn't break anything.
			name: "nil",
			in:   nil,
			want: Summary{},
		},
		{ // A single contact.
			name: "single",
			in: []*spb.ClientContact{
				{
					SentNonce:       1,
					ReceivedNonce:   0,
					ObservedAddress: "192.168.1.1:50000",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303404, Nanos: 0},
				},
			},
			want: Summary{
				Start:   time.Unix(1514303404, 0).UTC(),
				End:     time.Unix(1514303404, 0).UTC(),
				Count:   1,
				IPCount: 1,
			},
		},
		{ // Approximate startup sequence - nonces chained with increasing ts.
			name: "normalStartup",
			in: []*spb.ClientContact{
				{
					ReceivedNonce:   0,
					SentNonce:       1,
					ObservedAddress: "192.168.1.1:50000",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303404, Nanos: 0},
				},
				{
					ReceivedNonce:   1,
					SentNonce:       2,
					ObservedAddress: "192.168.1.1:50001",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303405, Nanos: 0},
				},
				{
					ReceivedNonce:   2,
					SentNonce:       3,
					ObservedAddress: "192.168.1.1:50002",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303406, Nanos: 0},
				},
				{
					ReceivedNonce:   3,
					SentNonce:       4,
					ObservedAddress: "192.168.1.1:50003",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303407, Nanos: 0},
				},
				{
					ReceivedNonce:   4,
					SentNonce:       5,
					ObservedAddress: "192.168.1.2:50004",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303408, Nanos: 0},
				},
				{
					ReceivedNonce:   5,
					SentNonce:       6,
					ObservedAddress: "192.168.1.2:50005",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303409, Nanos: 0},
				},
			},
			want: Summary{
				Start:   time.Unix(1514303404, 0).UTC(),
				End:     time.Unix(1514303409, 0).UTC(),
				Count:   6,
				IPCount: 2,
			},
		},
		{
			name: "duplicateTimestamp",
			in: []*spb.ClientContact{
				{
					ReceivedNonce:   0,
					SentNonce:       1,
					ObservedAddress: "192.168.1.1:50000",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303404, Nanos: 0},
				},
				{
					ReceivedNonce:   1,
					SentNonce:       2,
					ObservedAddress: "192.168.1.1:50001",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303404, Nanos: 0},
				},
			},
			wantErr: true,
		},
		{
			name: "duplicateNonce",
			in: []*spb.ClientContact{
				{
					ReceivedNonce:   0,
					SentNonce:       1,
					ObservedAddress: "192.168.1.1:50000",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303404, Nanos: 0},
				},
				{
					ReceivedNonce:   1,
					SentNonce:       1,
					ObservedAddress: "192.168.1.1:50001",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303405, Nanos: 0},
				},
			},
			wantErr: true,
		},
		{ // Client is restored twice from a backup.
			name: "backupRestore",
			in: []*spb.ClientContact{
				{
					ReceivedNonce:   0,
					SentNonce:       1,
					ObservedAddress: "192.168.1.1:50000",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303404, Nanos: 0},
				},
				{
					ReceivedNonce:   1,
					SentNonce:       2,
					ObservedAddress: "192.168.1.1:50001",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303405, Nanos: 0},
				},
				{
					ReceivedNonce:   2,
					SentNonce:       3,
					ObservedAddress: "192.168.1.1:50002",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303406, Nanos: 0},
				},
				{
					ReceivedNonce:   3,
					SentNonce:       4,
					ObservedAddress: "192.168.1.1:50003",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303407, Nanos: 0},
				},
				{
					ReceivedNonce:   2,
					SentNonce:       5,
					ObservedAddress: "192.168.1.1:50004",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303408, Nanos: 0},
				},
				{
					ReceivedNonce:   2,
					SentNonce:       6,
					ObservedAddress: "192.168.1.1:50005",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303409, Nanos: 0},
				},
			},
			want: Summary{
				Start:       time.Unix(1514303404, 0).UTC(),
				End:         time.Unix(1514303409, 0).UTC(),
				Count:       6,
				IPCount:     1,
				Splits:      2,
				SplitPoints: 1,
				Skips:       2,
			},
		},
		{ // Client is cloned 2 ways after second contact.
			name: "2wayClone",
			in: []*spb.ClientContact{
				{
					ReceivedNonce:   1,
					SentNonce:       2,
					ObservedAddress: "192.168.1.1:50000",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303000, Nanos: 0},
				},
				{
					ReceivedNonce:   2,
					SentNonce:       3,
					ObservedAddress: "192.168.1.1:50001",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303001, Nanos: 0},
				},
				{
					ReceivedNonce:   3,
					SentNonce:       4,
					ObservedAddress: "192.168.1.1:50002",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303002, Nanos: 10},
				},
				{
					ReceivedNonce:   3,
					SentNonce:       5,
					ObservedAddress: "192.168.1.2:50003",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303002, Nanos: 20},
				},
				{
					ReceivedNonce:   4,
					SentNonce:       6,
					ObservedAddress: "192.168.1.1:50004",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303003, Nanos: 10},
				},
				{
					ReceivedNonce:   5,
					SentNonce:       7,
					ObservedAddress: "192.168.1.2:50005",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303003, Nanos: 20},
				},
				{
					ReceivedNonce:   6,
					SentNonce:       8,
					ObservedAddress: "192.168.1.1:50000",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303004, Nanos: 10},
				},
				{
					ReceivedNonce:   7,
					SentNonce:       9,
					ObservedAddress: "192.168.1.2:50001",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303004, Nanos: 20},
				},
				{
					ReceivedNonce:   8,
					SentNonce:       10,
					ObservedAddress: "192.168.1.1:50002",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303005, Nanos: 10},
				},
				{
					ReceivedNonce:   9,
					SentNonce:       11,
					ObservedAddress: "192.168.1.2:50003",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303005, Nanos: 20},
				},
				{
					ReceivedNonce:   10,
					SentNonce:       12,
					ObservedAddress: "192.168.1.1:50004",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303006, Nanos: 10},
				},
				{
					ReceivedNonce:   11,
					SentNonce:       13,
					ObservedAddress: "192.168.1.2:50005",
					Timestamp:       &tspb.Timestamp{Seconds: 1514303006, Nanos: 20},
				},
			},
			want: Summary{
				Start:       time.Unix(1514303000, 0).UTC(),
				End:         time.Unix(1514303006, 20).UTC(),
				Count:       12,
				IPCount:     2,
				Splits:      1,
				SplitPoints: 1,
				Skips:       9,
			},
		},
	} {
		got, err := Summarize(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("Summarize(%s) succeeded, but want error.", tc.name)
			}
		} else {
			if err != nil {
				t.Errorf("Summarize(%s) returned error: %v", tc.name, err)
			}
			if *got != tc.want {
				t.Errorf("Summarize(%s) = %+v, but want %+v", tc.name, got, tc.want)
			}
		}
	}
}
