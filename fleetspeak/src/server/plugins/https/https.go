// Copyright 2018 Google Inc.
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

// Package https contains a plugin which provides https based communication with
// clients.
package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	proxyproto "github.com/pires/go-proxyproto"

	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/https"

	ppb "github.com/google/fleetspeak/fleetspeak/src/server/plugins/proto/plugins"
)

// HTTPSFactory is a plugins.CommunicatorFactory which produces a communicator which expects to receive
// direct https connections over TCP.
func HTTPSFactory(configFile string) (comms.Communicator, error) {
	b, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Exitf("Unable to read component config file [%s]: %v", configFile, err)
	}
	var c ppb.HttpsConfig
	if err := proto.UnmarshalText(string(b), &c); err != nil {
		log.Exitf("Unable to parse component config file [%s]: %v", configFile, err)
	}
	l, err := net.Listen("tcp", c.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("unable to listen on [%s]: %v", c.ListenAddress, err)
	}
	return https.NewCommunicator(https.Params{
		Listener:  l,
		Cert:      []byte(c.Certificate),
		Key:       []byte(c.Key),
		Streaming: c.Streaming,
	})
}

// ProxyHTTPSFactory is a plugins.CommunicatorFactory which produces a
// communicator which expects to receive https connections through a load
// balancer implementing the Proxy protocol.
//
// Note that if this communicator's port is directly exposed to clients it would
// be possible for them to effectively spoof their IP.
func ProxyHTTPSFactory(configFile string) (comms.Communicator, error) {
	b, err := ioutil.ReadFile(configFile)
	if err != nil {
		log.Exitf("Unable to read component config file [%s]: %v", configFile, err)
	}
	var c ppb.HttpsConfig
	if err := proto.UnmarshalText(string(b), &c); err != nil {
		log.Exitf("Unable to parse component config file [%s]: %v", configFile, err)
	}
	l, err := net.Listen("tcp", c.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("unable to listen on [%s]: %v", c.ListenAddress, err)
	}
	return https.NewCommunicator(https.Params{
		Listener:  &proxyListener{l},
		Cert:      []byte(c.Certificate),
		Key:       []byte(c.Key),
		Streaming: c.Streaming,
	})
}

type proxyListener struct {
	net.Listener
}

func (l *proxyListener) Accept() (net.Conn, error) {
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

func main() {
	log.Exitf("unimplemented")
}
