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
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// messageServer wraps a Communicator in order to handle clients polls.
type messageServer struct {
	*Communicator
}

type unknownAddr struct {
	address string
}

func (a unknownAddr) Network() string { return "unknown" }
func (a unknownAddr) String() string  { return a.address }

// addrFromString takes an address in string form, e.g. from
// http.Request.RemoteAddress and attempts to create an appropriate
// implementation of net.Addr.
//
// Currently it recognizes numeric TCP Addresses (e.g. 127.0.0.1:80,
// or [::1]:80) and puts them into a TCPAddr. Anything else is just
// wrapped in an unknownAddr.
func addrFromString(addr string) net.Addr {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return unknownAddr{address: addr}
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return unknownAddr{address: addr}
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		return unknownAddr{address: addr}
	}
	return &net.TCPAddr{
		IP:   ip,
		Port: p,
	}
}

// ServeHTTP implements http.Handler
func (s messageServer) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	ctx, fin := context.WithTimeout(req.Context(), 5*time.Minute)

	pi := stats.PollInfo{
		CTX:    req.Context(),
		Start:  db.Now(),
		Status: http.StatusTeapot, // Should never actually be returned
	}
	defer func() {
		fin()
		if pi.Status == http.StatusTeapot {
			log.Errorf("Forgot to set status.")
		}
		pi.End = db.Now()
		s.fs.StatsCollector().ClientPoll(pi)
	}()

	if !s.startProcessing() {
		log.Error("InternalServerError: server not ready.")
		pi.Status = http.StatusInternalServerError
		http.Error(res, "Server not ready.", pi.Status)
		return
	}
	defer s.stopProcessing()

	if req.Method != http.MethodPost {
		pi.Status = http.StatusBadRequest
		http.Error(res, fmt.Sprintf("%v not supported", req.Method), pi.Status)
		return
	}
	if req.ContentLength > MaxContactSize {
		pi.Status = http.StatusBadRequest
		http.Error(res, fmt.Sprintf("content length too large: %v", req.ContentLength), pi.Status)
		return
	}

	if req.TLS == nil {
		pi.Status = http.StatusBadRequest
		http.Error(res, "TLS information not found", pi.Status)
		return
	}
	if len(req.TLS.PeerCertificates) != 1 {
		pi.Status = http.StatusBadRequest
		http.Error(res, fmt.Sprintf("expected 1 client cert, received %v", len(req.TLS.PeerCertificates)), pi.Status)
		return
	}

	cert := req.TLS.PeerCertificates[0]
	if cert.PublicKey == nil {
		pi.Status = http.StatusBadRequest
		http.Error(res, "public key not present in client cert", pi.Status)
		return
	}
	id, err := common.MakeClientID(cert.PublicKey)
	if err != nil {
		pi.Status = http.StatusBadRequest
		http.Error(res, fmt.Sprintf("unable to create client id from public key: %v", err), pi.Status)
		return
	}
	pi.ID = id

	req.Body = http.MaxBytesReader(res, req.Body, MaxContactSize+1)
	st := time.Now()
	buf, err := ioutil.ReadAll(req.Body)
	pi.ReadTime = time.Since(st)
	pi.ReadBytes = len(buf)

	if len(buf) > MaxContactSize {
		pi.Status = http.StatusBadRequest
		http.Error(res, fmt.Sprintf("body can't be larger than %v bytes", MaxContactSize), pi.Status)
		return
	}
	if err != nil {
		pi.Status = http.StatusBadRequest
		http.Error(res, fmt.Sprintf("error reading body: %v", err), pi.Status)
		return
	}
	var wcd fspb.WrappedContactData
	if err = proto.Unmarshal(buf, &wcd); err != nil {
		pi.Status = http.StatusBadRequest
		http.Error(res, fmt.Sprintf("error parsing body: %v", err), pi.Status)
		return
	}
	addr := addrFromString(req.RemoteAddr)

	info, toSend, _, err := s.fs.InitializeConnection(ctx, addr, cert.PublicKey, &wcd, false)
	if err == comms.ErrNotAuthorized {
		pi.Status = http.StatusServiceUnavailable
		http.Error(res, "not authorized", pi.Status)
		return
	}
	if err != nil {
		log.Errorf("InternalServiceError: error processing contact: %v", err)
		pi.Status = http.StatusInternalServerError
		http.Error(res, fmt.Sprintf("error processing contact: %v", err), pi.Status)
		return
	}
	pi.CacheHit = info.Client.Cached

	bytes, err := proto.Marshal(toSend)
	if err != nil {
		log.Errorf("InternalServerError: proto.Marshal returned error: %v", err)
		pi.Status = http.StatusInternalServerError
		http.Error(res, fmt.Sprintf("error preparing messages: %v", err), pi.Status)
		return
	}

	res.Header().Set("Content-Type", "application/octet-stream")
	res.WriteHeader(http.StatusOK)
	st = time.Now()
	size, err := res.Write(bytes)
	if err != nil {
		log.Warning("Error writing body: %v", err)
		pi.Status = http.StatusBadRequest
		return
	}

	pi.WriteTime = time.Since(st)
	pi.WriteBytes = size
	pi.Status = http.StatusOK
}
