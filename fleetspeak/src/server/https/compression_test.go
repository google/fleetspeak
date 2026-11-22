// Copyright 2026 Google LLC
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

package https

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func TestCompressionHandler(t *testing.T) {
	data := []byte(strings.Repeat("fleetspeak", 100))
	compressed := compress(t, data, fspb.CompressionAlgorithm_COMPRESSION_DEFLATE)

	testCases := []struct {
		name     string
		encoding string
		body     []byte
		// wantStatus is the status of the response. It is http.StatusOK if the
		// request was passed to the wrapped handler.
		wantStatus        int
		wantBody          []byte
		wantContentLength int64
	}{
		{
			name:              "uncompressed",
			body:              data,
			wantStatus:        http.StatusOK,
			wantBody:          data,
			wantContentLength: int64(len(data)),
		},
		{
			// The length of the decompressed body is unknown.
			name:              "deflate",
			encoding:          "deflate",
			body:              compressed,
			wantStatus:        http.StatusOK,
			wantBody:          data,
			wantContentLength: -1,
		},
		{
			name:       "invalid deflate data",
			encoding:   "deflate",
			body:       data,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unsupported encoding",
			encoding:   "gzip",
			body:       data,
			wantStatus: http.StatusUnsupportedMediaType,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var gotReq *http.Request
			var gotBody []byte
			h := &CompressionHandler{
				Wrapped: http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
					gotReq = req
					var err error
					if gotBody, err = io.ReadAll(req.Body); err != nil {
						t.Errorf("Error reading request body: %v", err)
					}
				}),
			}
			req := httptest.NewRequest(http.MethodPost, "/message", bytes.NewReader(tc.body))
			req.Header.Set("Content-Length", strconv.Itoa(len(tc.body)))
			if tc.encoding != "" {
				req.Header.Set("Content-Encoding", tc.encoding)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("ServeHTTP() returned status %d, want %d", rec.Code, tc.wantStatus)
			}
			if gotReq == nil {
				if tc.wantStatus == http.StatusOK {
					t.Error("ServeHTTP() didn't call the wrapped handler")
				}
				return
			}
			if tc.wantStatus != http.StatusOK {
				t.Fatal("ServeHTTP() called the wrapped handler")
			}
			if !bytes.Equal(gotBody, tc.wantBody) {
				t.Errorf("Wrapped handler got body %q, want %q", gotBody, tc.wantBody)
			}
			if gotReq.ContentLength != tc.wantContentLength {
				t.Errorf("Wrapped handler got ContentLength %d, want %d", gotReq.ContentLength, tc.wantContentLength)
			}
			// The headers have to describe the body that the wrapped handler gets.
			if got := gotReq.Header.Get("Content-Encoding"); got != "" {
				t.Errorf("Wrapped handler got Content-Encoding header %q, want none", got)
			}
			wantLengthHeader := ""
			if tc.wantContentLength >= 0 {
				wantLengthHeader = strconv.FormatInt(tc.wantContentLength, 10)
			}
			if got := gotReq.Header.Get("Content-Length"); got != wantLengthHeader {
				t.Errorf("Wrapped handler got Content-Length header %q, want %q", got, wantLengthHeader)
			}
		})
	}
}
