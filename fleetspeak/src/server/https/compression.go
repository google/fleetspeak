package https

import (
	"compress/zlib"
	"fmt"
	"net/http"
)

// CompressionHandler is a http.Handler that transparently decompresses the
// request body if it is compressed and forwards the request to the wrapped
// handler.
type CompressionHandler struct {
	Wrapped http.Handler
}

// ServeHTTP implements http.Handler.ServeHTTP by transparently decompressing
// the request body if it is compressed and forwarding the call to the wrapped
// handler.
func (h *CompressionHandler) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	encoding := req.Header.Get("Content-Encoding")
	switch encoding {
	case "":
		// No compression.
	case "deflate":
		// "deflate" is the commonly used directive for deflate compressed data in
		// zlib format (https://www.rfc-editor.org/rfc/rfc9110#name-deflate-coding).
		zr, err := zlib.NewReader(req.Body)
		if err != nil {
			http.Error(res, fmt.Sprintf("failed to create zlib reader: %v", err), http.StatusBadRequest)
			return
		}
		defer zr.Close()
		req.Body = zr
		req.Header.Del("Content-Encoding")
	default:
		http.Error(res, fmt.Sprintf("unsupported content encoding: %s", encoding), http.StatusUnsupportedMediaType)
		return
	}
	h.Wrapped.ServeHTTP(res, req)
}
