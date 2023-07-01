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

// Package https implements an https-based server Communicator. It is the
// primary way for clients to communicate with the FS server.
package https

import (
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"time"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
)

const (
	// MaxContactSize is the largest contact (in bytes) that we will accept.
	MaxContactSize = 20 * 1024 * 1024
)

// Communicator implements server.Communicator, and accepts client connections
// over HTTPS.
type Communicator struct {
	p           Params
	hs          http.Server
	l           net.Listener
	fs          comms.Context
	stopping    chan struct{}
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

// Params wraps the parameters required to create an https communicator.
type Params struct {
	Listener           net.Listener  // Where to listen for connections, required.
	Cert, Key          []byte        // x509 encoded certificate and matching private key, required.
	Streaming          bool          // Whether to enable streaming communications.
	ClientCertHeader   string        // Where to locate the client certificate from the request header, if not provided use TLS request.
	StreamingLifespan  time.Duration // Maximum time to keep a streaming connection open, defaults to 10 min.
	StreamingCloseTime time.Duration // How much of StreamingLifespan to allocate to an orderly stream close, defaults to 30 sec.
	StreamingJitter    time.Duration // Maximum amount of jitter to add to StreamingLifespan.
}

// NewCommunicator creates a Communicator, which listens through l and identifies
// itself using certFile and keyFile.
func NewCommunicator(p Params) (*Communicator, error) {
	if p.StreamingLifespan == 0 {
		p.StreamingLifespan = 10 * time.Minute
	}
	if p.StreamingCloseTime == 0 {
		p.StreamingCloseTime = 30 * time.Second
	}
	mux := http.NewServeMux()
	c, err := tls.X509KeyPair(p.Cert, p.Key)
	if err != nil {
		return nil, err
	}
	h := Communicator{
		p: p,
		hs: http.Server{
			Handler: mux,
			TLSConfig: &tls.Config{
				ClientAuth:   tls.RequestClientCert,
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
			ReadTimeout:       20 * time.Minute,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      20 * time.Minute,
			IdleTimeout:       30 * time.Second,
		},
		stopping: make(chan struct{}),
	}
	mux.Handle("/message", messageServer{&h})
	if p.Streaming {
		mux.Handle("/streaming-message", streamingMessageServer{&h})
	}
	mux.Handle("/files/", fileServer{&h})

	switch l := h.p.Listener.(type) {
	case *net.TCPListener:
		h.p.Listener = listener{l}
	default:
	}

	return &h, nil
}

func (c *Communicator) serve(l net.Listener) {
	err := c.hs.Serve(l)
	log.Errorf("Serving finished with error: %v", err)
}

func (c *Communicator) Setup(fs comms.Context) error {
	c.fs = fs
	c.p.Listener = guardedListener{
		Listener: c.p.Listener,
		auth:     fs.Authorizer(),
	}
	return nil
}

func (c *Communicator) Start() error {
	go c.serve(tls.NewListener(c.p.Listener, c.hs.TLSConfig))

	c.runningLock.Lock()
	defer c.runningLock.Unlock()
	c.running = true
	return nil
}

func (c *Communicator) Stop() {
	// The most graceful way to shut down an http.Server is to close the associated listener.
	c.p.Listener.Close()
	c.runningLock.Lock()
	c.running = false
	c.runningLock.Unlock()
	close(c.stopping)
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
