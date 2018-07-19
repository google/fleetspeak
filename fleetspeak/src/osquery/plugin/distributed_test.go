package plugin

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io/ioutil"
	"reflect"
	"testing"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/kolide/osquery-go/gen/osquery"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	ospb "github.com/google/fleetspeak/fleetspeak/src/osquery/proto/fleetspeak_osquery"
)

func decode(t ospb.CompressionType, b []byte) ([]byte, error) {
	switch t {
	case ospb.CompressionType_UNCOMPRESSED:
		return b, nil
	case ospb.CompressionType_ZCOMPRESSION:
		r, err := zlib.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return ioutil.ReadAll(r)
	}
	return nil, fmt.Errorf("unknown compression type: %d", t)
}

func TestDistributed(t *testing.T) {
	toOSQuery := make(chan *fspb.Message, 5)
	fromOSQuery := make(chan *fspb.Message, 20)

	dist := MakeDistributed("testDistributed", toOSQuery, fromOSQuery)
	for _, tc := range []struct {
		name string

		// The queries to throw at the extension as if they came through FS.
		inQueries []*ospb.Queries
		// What getQueries should return to osquery, after inQ are sent to the extension.
		wantGet osquery.ExtensionPluginResponse

		// What results should be given using writeResults after getQueries.
		inResults []osquery.ExtensionPluginRequest
		// What results are expected to be sent back to FS.
		wantResults []ospb.QueryResults
		// What rows are expected to be sent back to FS, should be same length as wantResults.
		wantRows []ospb.Rows
	}{
		{
			name: "basic",
			inQueries: []*ospb.Queries{{
				Queries: map[string]string{"basic": "SELECT * FROM foobar"},
			}},
			wantGet: osquery.ExtensionPluginResponse{
				{"results": `{"queries":{"basic":"SELECT * FROM foobar"}}`},
			},
			inResults: []osquery.ExtensionPluginRequest{
				{"action": "writeResults", "results": `{"queries":{"basic":[{"iso_8601":"2017-07-10T22:08:40Z"}]}, "statuses":{"basic":"0"}}`},
			},
			wantResults: []ospb.QueryResults{{
				QueryName: "basic",
				Status:    0,
			}},
			wantRows: []ospb.Rows{{
				Rows: []*ospb.Row{
					{Row: map[string]string{"iso_8601": "2017-07-10T22:08:40Z"}},
				},
			}},
		},
	} {
		serviceName := fmt.Sprintf("TestService-%s", tc.name)
		for _, q := range tc.inQueries {
			data, err := ptypes.MarshalAny(q)
			if err != nil {
				t.Errorf("%s: Failed to marshal ospb.Queries: %v", tc.name, err)
			}
			toOSQuery <- &fspb.Message{
				Source:      &fspb.Address{ServiceName: serviceName},
				MessageType: "Queries",
				Data:        data,
			}
		}
		res := dist.Call(context.Background(), osquery.ExtensionPluginRequest{"action": "getQueries"})
		if (*res.Status != osquery.ExtensionStatus{Code: 0, Message: "OK"}) {
			t.Errorf("%s: Unexpected ExtensionStatus, wanted OK, got: %v", tc.name, res.Status)
			continue
		}
		if !reflect.DeepEqual(res.Response, tc.wantGet) {
			t.Errorf("%s: Unexpected Response attributed, wanted %v, got %v.", tc.name, tc.wantGet, res.Response)
			continue
		}
		for _, r := range tc.inResults {
			res := dist.Call(context.Background(), r)
			if (*res.Status != osquery.ExtensionStatus{Code: 0, Message: "OK"}) {
				t.Errorf("%s: Unexpected ExtensionStatus, wanted OK, got: %v", tc.name, res.Status)
				continue
			}
		}
		for i, r := range tc.wantResults {
			var msg *fspb.Message
			select {
			case msg = <-fromOSQuery:
			default:
				t.Errorf("%s: Failed to read result message", tc.name)
				continue
			}
			if !proto.Equal(msg.Destination, &fspb.Address{ServiceName: serviceName}) {
				t.Errorf("%s: Unexpected Destination, for result %d wanted %v, got %v", tc.name, i, msg.Destination, &fspb.Address{ServiceName: serviceName})
			}
			if msg.MessageType != "DistributedResult" {
				t.Errorf("%s: Unexpected MessageType, for result %d wanted DistributedResult, got %s", tc.name, i, msg.MessageType)
			}
			var qr ospb.QueryResults
			if err := ptypes.UnmarshalAny(msg.Data, &qr); err != nil {
				t.Errorf("%s: Error unmarshalling data as QueryResults: %v", tc.name, err)
				continue
			}
			b, err := decode(qr.Compress, qr.Rows)
			if err != nil {
				t.Errorf("%s: Error decoding Rows: %v", tc.name, err)
			}
			qr.Compress = ospb.CompressionType_UNCOMPRESSED
			qr.Rows = nil
			if !proto.Equal(&qr, &r) {
				t.Errorf("%s: Unexpected QueryResults, wanted %+v, got %+v.", tc.name, r, qr)
			}
			var rows ospb.Rows
			if err := proto.Unmarshal(b, &rows); err != nil {
				t.Errorf("%s: Unable to unmarshal rows: %v", tc.name, err)
				continue
			}
			if !proto.Equal(&rows, &tc.wantRows[i]) {
				t.Errorf("%s: Unexpected rows returned, wanted %+v, got %+v", tc.name, tc.wantRows[i], rows)
			}
		}
		select {
		case msg := <-fromOSQuery:
			t.Errorf("%s: Unexpected message from OSQuery: %v", tc.name, msg)
		default:
		}

	}
}
