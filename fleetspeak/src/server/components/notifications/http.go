// Copyright 2019 Google LLC.
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

// Package notifications defines basic Listener/Notification support for generic
// Fleetspeak servers.
package notifications

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"sync"

	log "github.com/golang/glog"

	"github.com/google/fleetspeak/fleetspeak/src/common"
)

// HttpListener is an implementation of the fleetspeak notifications.Listener
// interface which listens for notifications over HTTP.
//
// The port opened by this listener should not be exposed to public. Only other
// Fleetspeak servers will need to be able to reach it.
type HttpListener struct {
	// The address that HttpListner should bind to.
	BindAddress string

	// The address that other servers can find this one at. If unset, a best guess
	// will be set by Start().
	AdvertisedAddress string

	listener net.Listener
	working  sync.WaitGroup
	out      chan<- common.ClientID
}

func (l *HttpListener) Start() (<-chan common.ClientID, error) {
	li, err := net.Listen("tcp", l.BindAddress)
	if err != nil {
		return nil, err
	}

	l.listener = li
	if l.AdvertisedAddress == "" {
		l.AdvertisedAddress = li.Addr().String()
	}

	c := make(chan common.ClientID)
	l.out = c

	l.working.Add(1)
	go l.runServer()
	return c, nil

}

func (l *HttpListener) Stop() {
	l.listener.Close()
	l.working.Wait()
}

func (l *HttpListener) Address() string {
	return l.AdvertisedAddress
}

func (l *HttpListener) runServer() {
	defer l.working.Done()

	log.Infof("Starting http notification listener at: %s", l.listener.Addr().String())
	err := http.Serve(l.listener, http.HandlerFunc(l.handle))
	log.Infof("Http notification listener stopped: %v", err)
	close(l.out)
}

func (l *HttpListener) handle(w http.ResponseWriter, r *http.Request) {
	dir, name := path.Split(r.URL.EscapedPath())
	if dir != "/client/" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	id, err := common.StringToClientID(name)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse client id [%s]: %v", name, err), http.StatusBadRequest)
		return
	}
	if id.IsNil() {
		http.Error(w, fmt.Sprintf("Client id is required."), http.StatusBadRequest)
		return
	}
	if r.Method != "POST" {
		http.Error(w, fmt.Sprintf("Unexpected method: %s", r.Method), http.StatusBadRequest)
		return
	}

	l.out <- id
	w.WriteHeader(http.StatusOK)
}

// HttpNotifier is an implementation of the fleetspeak notifications.Notifier
// interface which is compatible with HttpListener.
type HttpNotifier struct {
	c http.Client
}

func (n *HttpNotifier) NewMessageForClient(ctx context.Context, target string, id common.ClientID) error {
	req, err := http.NewRequest("POST", (&url.URL{
		Scheme: "http",
		Host:   target,
		Path:   fmt.Sprintf("/client/%s", id.String()),
	}).String(), nil)
	if err != nil {
		return err
	}

	_, err = n.c.Do(req.WithContext(ctx))
	return err
}
