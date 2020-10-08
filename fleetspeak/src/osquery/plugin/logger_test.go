package plugin

import (
	"context"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/kolide/osquery-go/gen/osquery"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	ospb "github.com/google/fleetspeak/fleetspeak/src/osquery/proto/fleetspeak_osquery"
)

func TestLogger(t *testing.T) {
	logged := make(chan *fspb.Message, 5)
	logger := MakeLogger("testLogger", "TestService", logged)

	for _, tc := range []struct {
		name string
		req  osquery.ExtensionPluginRequest
		want []*ospb.LoggedResult
	}{
		{
			name: "string",
			req:  osquery.ExtensionPluginRequest{"string": "logged string"},
			want: []*ospb.LoggedResult{{
				Type:     ospb.LoggedResult_STRING,
				Compress: ospb.CompressionType_UNCOMPRESSED,
				Data:     []byte("logged string"),
			}},
		},
		{
			name: "snapshot",
			req:  osquery.ExtensionPluginRequest{"snapshot": "A long query result with lots and lots and lots of redundancy."},
			want: []*ospb.LoggedResult{{
				Type:     ospb.LoggedResult_SNAPSHOT,
				Compress: ospb.CompressionType_ZCOMPRESSION,
				Data:     zcompress([]byte("A long query result with lots and lots and lots of redundancy.")),
			}},
		},
		{
			name: "health",
			req:  osquery.ExtensionPluginRequest{"health": "health log"},
			want: []*ospb.LoggedResult{{
				Type:     ospb.LoggedResult_HEALTH,
				Compress: ospb.CompressionType_UNCOMPRESSED,
				Data:     []byte("health log"),
			}},
		},
		{
			name: "init",
			req:  osquery.ExtensionPluginRequest{"init": "init log"},
			want: []*ospb.LoggedResult{{
				Type:     ospb.LoggedResult_INIT,
				Compress: ospb.CompressionType_UNCOMPRESSED,
				Data:     []byte("init log"),
			}},
		},
		{
			name: "status",
			req:  osquery.ExtensionPluginRequest{"status": "true", "log": `{"":{"s":"0","f":"events.cpp","i":"828","m":"Event publisher failed setup: kernel: Cannot access \/dev\/osquery"},"":{"s":"0","f":"events.cpp","i":"828","m":"Event publisher failed setup: scnetwork: Publisher not used"},"":{"s":"0","f":"scheduler.cpp","i":"74","m":"Executing scheduled query macos_kextstat: SELECT * FROM time"}}`},
			want: []*ospb.LoggedResult{
				{
					Type:     ospb.LoggedResult_STATUS,
					Compress: ospb.CompressionType_ZCOMPRESSION,
					Data:     zcompress([]byte(`{"s":"0","f":"events.cpp","i":"828","m":"Event publisher failed setup: kernel: Cannot access \/dev\/osquery"}`)),
				},
				{
					Type:     ospb.LoggedResult_STATUS,
					Compress: ospb.CompressionType_ZCOMPRESSION,
					Data:     zcompress([]byte(`{"s":"0","f":"events.cpp","i":"828","m":"Event publisher failed setup: scnetwork: Publisher not used"}`)),
				},
				{
					Type:     ospb.LoggedResult_STATUS,
					Compress: ospb.CompressionType_UNCOMPRESSED,
					Data:     []byte(`{"s":"0","f":"scheduler.cpp","i":"74","m":"Executing scheduled query macos_kextstat: SELECT * FROM time"}`),
				},
			},
		},
	} {
		res := logger.Call(context.Background(), tc.req)
		if (*res.Status != osquery.ExtensionStatus{Code: 0, Message: "OK"}) {
			t.Errorf("%s: Unexpected ExtensionStatus, wanted OK, got: %v", tc.name, res.Status)
			continue
		}

		for _, w := range tc.want {
			var got *fspb.Message
			d := time.NewTimer(time.Second)
			select {
			case got = <-logged:
				d.Stop()
			case <-d.C:
				t.Fatalf("%s: Message read timed out", tc.name)
			}

			if got.MessageType != "LoggedResult" {
				t.Errorf("%s: Unexpected MessageType, want LoggedResult, got: %v", tc.name, got.MessageType)
			}
			if !proto.Equal(got.Destination, &fspb.Address{ServiceName: "TestService"}) {
				t.Errorf("%s: Unexpected Destination, want TestService, got: %v", tc.name, got.Destination)
			}
			var gotResult ospb.LoggedResult
			if err := ptypes.UnmarshalAny(got.Data, &gotResult); err != nil {
				t.Errorf("%s: Error parsing Data as LoggedResult: %v", tc.name, err)
				continue
			}
			if !proto.Equal(&gotResult, w) {
				t.Errorf("%s: Read unexpected LoggedResult, want %v, got %v", tc.name, w, gotResult.String())
			}
		}
	}
	select {
	case extra := <-logged:
		t.Errorf("Logged extra message: %v", extra)
		var extraData ospb.LoggedResult
		if err := ptypes.UnmarshalAny(extra.Data, &extraData); err != nil {
			t.Errorf("Error decoding data: %v", err)
		} else {
			t.Errorf("Extra message Data decoded: %v", extraData.String())
		}
	default:
	}
}
