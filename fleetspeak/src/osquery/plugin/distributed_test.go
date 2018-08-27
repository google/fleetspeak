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
		// What rows are expected to be sent back to FS, for each query.
		wantRows map[string]*ospb.Rows
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
			wantRows: map[string]*ospb.Rows{"basic": &ospb.Rows{Rows: []*ospb.Row{{Row: map[string]string{"iso_8601": "2017-07-10T22:08:40Z"}}}}},
		},
		{
			name: "multi-query",
			inQueries: []*ospb.Queries{
				{Queries: map[string]string{
					"multi1": "SELECT name FROM foobar WHERE idx=1",
					"multi2": "SELECT name FROM foobar WHERE idx=2"}},
				{Queries: map[string]string{
					"multi3": "SELECT name FROM foobar WHERE idx=3",
					"multi4": "SELECT name FROM foobar WHERE idx=4"}},
			},
			wantGet: osquery.ExtensionPluginResponse{
				{"results": `{"queries":{"multi1":"SELECT name FROM foobar WHERE idx=1","multi2":"SELECT name FROM foobar WHERE idx=2","multi3":"SELECT name FROM foobar WHERE idx=3","multi4":"SELECT name FROM foobar WHERE idx=4"}}`},
			},
			inResults: []osquery.ExtensionPluginRequest{
				{"action": "writeResults", "results": `{"queries":{"multi1":[{"name":"client1"}], "multi2":[{"name":"client2"}], "multi3":[{"name":"client3"}], "multi4":[{"name":"client4"}]}, "statuses":{"multi1":"0","multi2":"0","multi3":"0","multi4":"0"}}`},
			},
			wantRows: map[string]*ospb.Rows{
				"multi1": &ospb.Rows{Rows: []*ospb.Row{{Row: map[string]string{"name": "client1"}}}},
				"multi2": &ospb.Rows{Rows: []*ospb.Row{{Row: map[string]string{"name": "client2"}}}},
				"multi3": &ospb.Rows{Rows: []*ospb.Row{{Row: map[string]string{"name": "client3"}}}},
				"multi4": &ospb.Rows{Rows: []*ospb.Row{{Row: map[string]string{"name": "client4"}}}},
			},
		},
		{
			name: "multi-results",
			inQueries: []*ospb.Queries{{
				Queries: map[string]string{"basic": "SELECT * FROM foobar"},
			}},
			wantGet: osquery.ExtensionPluginResponse{
				{"results": `{"queries":{"basic":"SELECT * FROM foobar"}}`},
			},
			inResults: []osquery.ExtensionPluginRequest{
				{"action": "writeResults", "results": `{"queries":{"basic":[{"iso_8601":"2017-07-10T22:08:40Z"}, {"iso_8601":"2017-07-10T22:08:41Z"}, {"iso_8601":"2017-07-10T22:08:42Z"}]}, "statuses":{"basic":"0"}}`},
			},
			wantRows: map[string]*ospb.Rows{"basic": &ospb.Rows{Rows: []*ospb.Row{
				{Row: map[string]string{"iso_8601": "2017-07-10T22:08:40Z"}},
				{Row: map[string]string{"iso_8601": "2017-07-10T22:08:41Z"}},
				{Row: map[string]string{"iso_8601": "2017-07-10T22:08:42Z"}},
			}}},
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
	L:
		for {
			var msg *fspb.Message
			select {
			case msg = <-fromOSQuery:
			default:
				break L
			}

			if !proto.Equal(msg.Destination, &fspb.Address{ServiceName: serviceName}) {
				t.Errorf("%s: Unexpected Destination, wanted %v, got %v", tc.name, msg.Destination, &fspb.Address{ServiceName: serviceName})
			}
			if msg.MessageType != "DistributedResult" {
				t.Errorf("%s: Unexpected MessageType, wanted DistributedResult, got %s", tc.name, msg.MessageType)
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
			var rows ospb.Rows
			if err := proto.Unmarshal(b, &rows); err != nil {
				t.Errorf("%s: Unable to unmarshal rows: %v", tc.name, err)
				continue
			}
			if !proto.Equal(&rows, tc.wantRows[qr.QueryName]) {
				t.Errorf("%s: Unexpected rows returned for %s, wanted %+v, got %+v", tc.name, qr.QueryName, tc.wantRows[qr.QueryName], rows)
			}
			delete(tc.wantRows, qr.QueryName)
		}
		if len(tc.wantRows) > 0 {
			t.Errorf("Did not receive all expected rows, still waiting for: %v", tc.wantRows)
		}
	}
}
