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

package https

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// fileServer wraps a Communicator in order to serve files.
type fileServer struct {
	*Communicator
}

// ServeHTTP implements http.Handler
func (s fileServer) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	path := strings.Split(strings.TrimPrefix(req.URL.EscapedPath(), "/"), "/")
	if len(path) != 3 || path[0] != "files" {
		http.Error(res, fmt.Sprintf("unable to parse files uri: %v", req.URL.EscapedPath()), http.StatusBadRequest)
		return
	}
	service, err := url.PathUnescape(path[1])
	if err != nil {
		http.Error(res, fmt.Sprintf("unable to parse path component [%v]: %v", path[1], err), http.StatusBadRequest)
		return
	}
	name, err := url.PathUnescape(path[2])
	if err != nil {
		http.Error(res, fmt.Sprintf("unable to parse path component [%v]: %v", path[2], err), http.StatusBadRequest)
		return
	}

	ctx := req.Context()
	data, modtime, err := s.fs.ReadFile(ctx, service, name)
	if err != nil {
		if s.fs.IsNotFound(err) {
			http.Error(res, "file not found", http.StatusNotFound)
			return
		}
		http.Error(res, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
		return
	}
	http.ServeContent(res, req, name, modtime, data)
	data.Close()
}
