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
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/stats"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

const (
	// MaxContactSize is the largest contact (in bytes) that we will accept.
	MaxContactSize = 20 * 1024 * 1024
)

// Communicator implements server.Communicator, and accepts client connections
// over HTTPS.
type Communicator struct {
	hs          http.Server
	l           net.Listener
	fs          comms.Context
	running     bool
	runningLock sync.RWMutex
	pending     sync.WaitGroup
}

type guardedListener struct {
	net.Listener
	auth authorizer.Authorizer
}

func (l guardedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.auth.Allow1(c.RemoteAddr()) {
			return c, err
		}
		c.Close()
	}
}

type listener struct {
	*net.TCPListener
}

func (l listener) Accept() (net.Conn, error) {
	tc, err := l.AcceptTCP()
	if err != nil {
		return nil, err
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(1 * time.Minute)
	tc.SetNoDelay(false)
	return tc, nil
}

// NewCommunicator creates a Communicator, which listens through l and identifies
// itself using certFile and keyFile.
func NewCommunicator(l net.Listener, cert, key []byte) (*Communicator, error) {
	mux := http.NewServeMux()
	c, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, err
	}
	h := Communicator{
		hs: http.Server{
			Handler: mux,
			TLSConfig: &tls.Config{
				ClientAuth:   tls.RequireAnyClientCert,
				Certificates: []tls.Certificate{c},
				CipherSuites: []uint16{
					// We may as well allow only the strongest (as far as we can guess)
					// ciphers. Note that TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 is
					// required by the https library.
					tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
					tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
					tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
					tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
				// Correctly implementing session tickets means sharing and rotating a
				// secret key between servers, with implications if it leaks. Simply
				// disable for the moment.
				SessionTicketsDisabled: true,
				MinVersion:             tls.VersionTLS12,
				NextProtos:             []string{"h2"},
			},
			ReadTimeout:       5 * time.Minute,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      5 * time.Minute,
			IdleTimeout:       30 * time.Second,
		},
	}
	mux.Handle("/message", messageServer{&h})
	mux.Handle("/files/", fileServer{&h})

	switch l := l.(type) {
	case *net.TCPListener:
		h.l = listener{l}
	default:
		h.l = l
	}

	return &h, nil
}

func (c *Communicator) serve(l net.Listener) {
	err := c.hs.Serve(l)
	log.Errorf("Serving finished with error: %v", err)
}

func (c *Communicator) Setup(fs comms.Context) error {
	c.fs = fs
	c.l = guardedListener{
		Listener: c.l,
		auth:     fs.Authorizer(),
	}
	return nil
}

func (c *Communicator) Start() error {
	go c.serve(tls.NewListener(c.l, c.hs.TLSConfig))

	c.runningLock.Lock()
	defer c.runningLock.Unlock()
	c.running = true
	return nil
}

func (c *Communicator) Stop() {
	// The most graceful way to shut down an http.Server is to close the associated listener.
	c.l.Close()
	c.runningLock.Lock()
	c.running = false
	c.runningLock.Unlock()
	c.pending.Wait()
}

// startProcessing returns if we are up and running. If we are up and running,
// it updates the pending operation count to support orderly shutdown.
func (c *Communicator) startProcessing() bool {
	c.runningLock.RLock()
	defer c.runningLock.RUnlock()
	if c.running {
		c.pending.Add(1)
	}
	return c.running
}

func (c *Communicator) stopProcessing() {
	c.pending.Done()
}

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

	contactInfo := authorizer.ContactInfo{
		ID:           id,
		ContactSize:  len(wcd.ContactData),
		ClientLabels: wcd.ClientLabels,
	}
	if !s.fs.Authorizer().Allow2(addr, contactInfo) {
		pi.Status = http.StatusServiceUnavailable
		http.Error(res, "not authorized", pi.Status)
		return
	}
	info, err := s.fs.GetClientInfo(ctx, id)
	if err != nil {
		log.Errorf("InternalServerError: GetClientInfo returned error: %v", err)
		pi.Status = http.StatusInternalServerError
		http.Error(res, fmt.Sprintf("internal error getting client info: %v", err), pi.Status)
		return
	}
	var clientInfo authorizer.ClientInfo
	if info == nil {
		clientInfo.New = true
	} else {
		clientInfo.Labels = info.Labels
		pi.CacheHit = info.Cached
	}
	if !s.fs.Authorizer().Allow3(addr, contactInfo, clientInfo) {
		pi.Status = http.StatusServiceUnavailable
		http.Error(res, "not authorized", pi.Status)
		return
	}
	if info == nil {
		info, err = s.fs.AddClient(ctx, id, cert.PublicKey)
		if err != nil {
			log.Errorf("InternalServerError: AddClient returned error: %v", err)
			pi.Status = http.StatusInternalServerError
			http.Error(res, fmt.Sprintf("internal error adding client info: %v", err), pi.Status)
			return
		}
	}

	toSend, err := s.fs.HandleClientContact(ctx, info, addr, &wcd)
	if err != nil {
		log.Errorf("InternalServerError: HandleClientContact returned error: %v", err)
		pi.Status = http.StatusInternalServerError
		http.Error(res, fmt.Sprintf("error processing contact: %v", err), pi.Status)
		return
	}

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

// messageServer wraps a Communicator in order to serve files.
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
