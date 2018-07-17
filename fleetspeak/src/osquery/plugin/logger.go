package plugin

import (
	"bytes"
	"compress/zlib"
	"context"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/ptypes"
	"github.com/kolide/osquery-go/plugin/logger"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	ospb "github.com/google/fleetspeak/fleetspeak/src/osquery/proto/fleetspeak_osquery"
)

// MakeLogger returns a logger.Plugin which reports whatever is logged as Fleetspeak
// messages through output. The messages will be addressed to the server service dest.
func MakeLogger(name string, dest string, output chan<- *fspb.Message) *logger.Plugin {
	return logger.NewPlugin(name, func(ctx context.Context, t logger.LogType, ll string) error {
		c, b := encodeResult(ll)
		res := ospb.LoggedResult{
			Type:     toProtoType(t),
			Compress: c,
			Data:     b,
		}
		data, err := ptypes.MarshalAny(&res)
		if err != nil {
			return err
		}
		ret := fspb.Message{
			Destination: &fspb.Address{ServiceName: dest},
			MessageType: "LoggedResult",
			Data:        data,
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case output <- &ret:
		}
		return nil
	})
}

func toProtoType(t logger.LogType) ospb.LoggedResult_Type {
	switch t {
	case logger.LogTypeString:
		return ospb.LoggedResult_STRING
	case logger.LogTypeSnapshot:
		return ospb.LoggedResult_SNAPSHOT
	case logger.LogTypeHealth:
		return ospb.LoggedResult_HEALTH
	case logger.LogTypeInit:
		return ospb.LoggedResult_INIT
	case logger.LogTypeStatus:
		return ospb.LoggedResult_STATUS
	}
	log.Warningf("Unknown log type: %d", t)
	return ospb.LoggedResult_UNKNOWN
}

func encodeResult(s string) (ospb.CompressionType, []byte) {
	sb := []byte(s)
	if len(sb) < 8 {
		return ospb.CompressionType_UNCOMPRESSED, sb
	}
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	w.Write([]byte(sb))
	w.Close()
	if len(b.Bytes()) > len(sb) {
		return ospb.CompressionType_UNCOMPRESSED, sb
	}
	return ospb.CompressionType_ZCOMPRESSION, b.Bytes()

}