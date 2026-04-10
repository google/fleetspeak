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
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"golang.org/x/time/rate"
)

// unauthorizedLogging is used to rate-limit logging of unauthorized file
// requests to avoid spam during potential DoS attacks.
var unauthorizedLogging = rate.Sometimes{Interval: time.Minute}

// fileServer uses a subset of Communicator in order to serve files.
type fileServer struct {
	fs func() comms.Context
	p  Params
}

// NewFileServer allows reuse of fileServer in non-HTTPS communicators.
func NewFileServer(fs func() comms.Context, p Params) fileServer {
	return fileServer{
		fs: fs,
		p:  p,
	}
}

// ServeHTTP implements http.Handler
func (s fileServer) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	clientID, idErr := s.extractClientID(req)
	if idErr == nil {
		ctx = comms.WithClientID(ctx, clientID)
	}

	labels := req.Header["X-Fleetspeak-Labels"]
	if len(labels) > 0 {
		ctx = comms.WithLabels(ctx, labels)
	}

	req = req.WithContext(ctx)

	if s.p.FileServerAuthorization {
		if idErr != nil {
			http.Error(res, "unauthorized", http.StatusUnauthorized)
			unauthorizedLogging.Do(func() {
				log.Warningf("Unauthorized file request: failed to extract client ID: %v", idErr)
			})
			return
		}

		if err := s.authorizeFileRequest(req, clientID, labels); err != nil {
			http.Error(res, "unauthorized", http.StatusUnauthorized)
			unauthorizedLogging.Do(func() {
				log.Warningf("Unauthorized file request: %v", err)
			})
			return
		}
	}

	service, name, err := parseFilePath(req.URL.Path)
	if err != nil {
		http.Error(res, err.Error(), http.StatusBadRequest)
		return
	}

	data, modtime, err := s.fs().ReadFile(ctx, service, name)
	if err != nil {
		if s.fs().IsNotFound(err) {
			http.Error(res, "file not found", http.StatusNotFound)
			return
		}
		http.Error(res, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
		return
	}
	http.ServeContent(res, req, name, modtime, data)
	data.Close()
}

func (s fileServer) extractClientID(req *http.Request) (common.ClientID, error) {
	cert, err := GetClientCert(req, s.p.FrontendConfig)
	if err != nil {
		return common.ClientID{}, err
	}
	return common.MakeClientID(cert.PublicKey)
}

func parseFilePath(pathStr string) (string, string, error) {
	path := strings.Split(strings.TrimPrefix(pathStr, "/"), "/")
	if len(path) != 3 || path[0] != "files" {
		return "", "", fmt.Errorf("unable to parse files URI: %q", pathStr)
	}
	service, err := url.PathUnescape(path[1])
	if err != nil {
		return "", "", fmt.Errorf("unable to parse path component %q: %w", path[1], err)
	}
	name, err := url.PathUnescape(path[2])
	if err != nil {
		return "", "", fmt.Errorf("unable to parse path component %q: %w", path[2], err)
	}
	return service, name, nil
}

func (s fileServer) authorizeFileRequest(req *http.Request, id common.ClientID, labels []string) error {
	addrPort, err := netip.ParseAddrPort(req.RemoteAddr)
	if err != nil {
		return err
	}
	addr := net.TCPAddrFromAddrPort(addrPort)
	if !s.fs().Authorizer().Allow1(addr) {
		return fmt.Errorf("unauthorized via Allow1 (addr: %v)", addr)
	}

	ci := authorizer.ContactInfo{
		ID:           id,
		ContactSize:  0,
		ClientLabels: labels,
	}

	if !s.fs().Authorizer().Allow2(addr, ci) {
		return fmt.Errorf("unauthorized via Allow2 (addr: %v, contact: %v)", addr, ci)
	}
	return nil
}
