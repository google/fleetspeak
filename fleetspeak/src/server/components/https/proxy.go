// Copyright 2019 Google Inc.
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

// Package https provides generic implementations and utility methods for
// Fleetspeak https communication component type.
package https

import (
	"bufio"
	"fmt"
	"net"

	log "github.com/golang/glog"

	proxyproto "github.com/pires/go-proxyproto"
)

// ProxyListener wraps a net.Listener and adapts the perceived source of the connection
// to be that provided by the PROXY protocol header string. Should be used only when the
// server is behind a load balancer which implements the PROXY protocol.
//
// https://www.haproxy.org/download/1.8/doc/proxy-protocol.txt
type ProxyListener struct {
	net.Listener
}

func (l *ProxyListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return c, err
	}
	r := bufio.NewReader(c)
	h, err := proxyproto.Read(r)
	if err == proxyproto.ErrNoProxyProtocol {
		log.Warningf("Received connection from %v without proxy header.", c.RemoteAddr())
		return &proxyConn{
			Conn:   c,
			reader: r,
			remote: c.RemoteAddr(),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error parsing proxy header: %v", err)
	}
	return &proxyConn{
		Conn:   c,
		reader: r,
		remote: &net.TCPAddr{IP: h.SourceAddress, Port: int(h.SourcePort)},
	}, nil
}

type proxyConn struct {
	net.Conn
	reader *bufio.Reader
	remote net.Addr
}

func (c *proxyConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

func (c *proxyConn) RemoteAddr() net.Addr {
	return c.remote
}
