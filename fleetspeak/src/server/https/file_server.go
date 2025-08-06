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
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	log "github.com/golang/glog"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"
	"golang.org/x/time/rate"
)

// unauthorizedLogging is used to rate-limit logging of unauthorized file
// requests to avoid spam during potential DoS attacks.
var unauthorizedLogging = rate.Sometimes{Interval: time.Minute}

// fileServer wraps a Communicator in order to serve files.
type fileServer struct {
	*Communicator
}

// ServeHTTP implements http.Handler
func (s fileServer) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	if s.p.FileServerAuthorization {
		if err := s.authorizeFileRequest(req); err != nil {
			http.Error(res, "unauthorized", http.StatusUnauthorized)
			unauthorizedLogging.Do(func() {
				log.Warningf("Unauthorized file request: %v", err)
			})
			return
		}
	}

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

func (s fileServer) authorizeFileRequest(req *http.Request) error {
	addrPort, err := netip.ParseAddrPort(req.RemoteAddr)
	if err != nil {
		return err
	}
	addr := net.TCPAddrFromAddrPort(addrPort)
	if !s.fs.Authorizer().Allow1(addr) {
		return fmt.Errorf("unauthorized via Allow1 (addr: %v)", addr)
	}

	cert, err := GetClientCert(req, s.p.FrontendConfig)
	if err != nil {
		return err
	}
	id, err := common.MakeClientID(cert.PublicKey)
	if err != nil {
		return err
	}

	ci := authorizer.ContactInfo{
		ID:           id,
		ContactSize:  0,
		ClientLabels: req.Header["X-Fleetspeak-Labels"],
	}

	if !s.fs.Authorizer().Allow2(addr, ci) {
		return fmt.Errorf("unauthorized via Allow2 (addr: %v, contact: %v)", addr, ci)
	}
	return nil
}
