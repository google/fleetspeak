package https

import (
	"compress/zlib"
	"io"
	"net/http"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// BodyWriter is an interface that groups Write, Flush, and Close functions.
type BodyWriter interface {
	io.WriteCloser
	Flush() error
}

// CompressingWriter returns a BodyWriter that will compress the data written to
// it with the given compression algorithm and write it to w.
func CompressingWriter(w io.Writer, compression fspb.CompressionAlgorithm) BodyWriter {
	switch compression {
	case fspb.CompressionAlgorithm_COMPRESSION_DEFLATE:
		return zlib.NewWriter(w)
	default:
		return nopWriteCloseFlusherTo{w}
	}
}

type nopWriteCloseFlusherTo struct {
	io.Writer
}

func (nopWriteCloseFlusherTo) Flush() error {
	return nil
}

func (nopWriteCloseFlusherTo) Close() error {
	return nil
}

// SetContentEncoding sets the Content-Encoding header on req to the appropriate
// value for the given compression algorithm.
func SetContentEncoding(h http.Header, compression fspb.CompressionAlgorithm) {
	switch compression {
	case fspb.CompressionAlgorithm_COMPRESSION_DEFLATE:
		h.Set("Content-Encoding", "deflate")
	}
}
