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

// Package https provides comms.Communicator implementations which connect to the
// Fleetspeak server using HTTPS.
package https

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	mrand "math/rand"
	"net"
	"net/http"
	"net/url"
	"path"
	"time"

	log "github.com/golang/glog"
	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/common"
	"golang.org/x/net/http2"
)

const (
	sendBytesThreshold    = 15 * 1024 * 1024
	minSendBytesThreshold = 256 * 1024
	sendCountThreshold    = 100
	closeWaitThreshold    = 30 * time.Second // Matches IdleTimeout in server/https.
)

func makeTransport(cctx comms.Context, dc func(ctx context.Context, network, addr string) (net.Conn, error)) (common.ClientID, *http.Transport, []byte, error) {
	ci, err := cctx.CurrentIdentity()
	if err != nil {
		return common.ClientID{}, nil, nil, err
	}
	si, err := cctx.ServerInfo()
	if err != nil {
		return common.ClientID{}, nil, nil, err
	}

	cv := func(_ [][]byte, chains [][]*x509.Certificate) error {
		for _, chain := range chains {
			if !cctx.ChainRevoked(chain) {
				return nil
			}
		}
		return errors.New("certificate revoked")
	}

	tmpl := x509.Certificate{
		Issuer:       pkix.Name{Organization: []string{"GRR Client"}},
		Subject:      pkix.Name{Organization: []string{ci.ID.String()}},
		SerialNumber: big.NewInt(1),
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, ci.Public, ci.Private)
	if err != nil {
		return common.ClientID{}, nil, nil, fmt.Errorf("unable to configure communicator, could not create client cert: %v", err)
	}

	if dc == nil {
		dc = (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext
	}

	var proxy func(*http.Request) (*url.URL, error)
	if si.Proxy == nil {
		// TODO: This path is not tested in unit tests, it will have to be tested in an integration test.
		// - ProxyFromEnvironment caches the value of the environment variable, so it can't be overridden in a unit test.
		// - ProxyFromEnvironment doesn't use the proxy for requests to localhost.
		proxy = http.ProxyFromEnvironment
	} else {
		proxy = http.ProxyURL(si.Proxy)
	}

	// We'll make the Transport configurable so we can be both backwards compatible but also forward looking
	nextProtos := []string{"http/1.1"}
	preferHttp2 := false
	if cctx.CommunicatorConfig() != nil {
		preferHttp2 = cctx.CommunicatorConfig().PreferHttp2
	}
	if preferHttp2 {
		nextProtos = []string{"h2", "http/1.1"}
	}

	tr := &http.Transport{
		Proxy: proxy,
		TLSClientConfig: &tls.Config{
			RootCAs: si.TrustedCerts,
			GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				return &tls.Certificate{
					Certificate: [][]byte{certBytes},
					PrivateKey:  ci.Private,
				}, nil
			},
			CipherSuites: []uint16{
				// We implement both endpoints, so we might as well require long keys and
				// perfect forward secrecy. Note that TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
				// is required by the https library.
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
			VerifyPeerCertificate: cv,
			ServerName:            si.ServerName,
			NextProtos:            nextProtos,
		},
		MaxIdleConns:          10,
		DialContext:           dc,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	if preferHttp2 {
		err = http2.ConfigureTransport(tr)
	}
	return ci.ID, tr, certBytes, err
}

// jitter adds up to 50% random jitter, and converts to time.Duration.
func jitter(seconds int32) time.Duration {
	return time.Duration((1.0 + 0.5*mrand.Float32()) * float32(seconds) * float32(time.Second))
}

// getFileIfModified fetches the file with the given name from the file server
// with the HTTP GET method. The hosts are sequentially dialed until one of them
// successfully responds. If no proper response was received, the function
// returns the most recent error.
func getFileIfModified(ctx context.Context, cctx comms.Context, clientCert []byte, hosts []string, client *http.Client, service, name string, modSince time.Time) (io.ReadCloser, time.Time, error) {
	var lastErr error
	for _, h := range hosts {
		body, modSince, err := getFileIfModifiedFromHost(ctx, cctx, clientCert, h, client, service, name, modSince)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				break
			}
			continue
		}
		return body, modSince, nil
	}

	return nil, time.Time{}, fmt.Errorf("unable to retrieve file, last attempt failed with: %v", lastErr)
}

func getFileIfModifiedFromHost(ctx context.Context, cctx comms.Context, clientCert []byte, host string, client *http.Client, service, name string, modSince time.Time) (io.ReadCloser, time.Time, error) {
	var didFetch bool
	var err error
	defer func() {
		cctx.Stats().AfterGetFileRequest(host, service, name, didFetch, err)
	}()

	u := url.URL{
		Scheme: "https",
		Host:   host,
		Path:   path.Join("/files", url.PathEscape(service), url.PathEscape(name)),
	}

	var req *http.Request
	req, err = http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, time.Time{}, err
	}
	req = req.WithContext(ctx)

	if !modSince.IsZero() {
		req.Header.Set("If-Modified-Since", modSince.Format(http.TimeFormat))
	}

	if ci, err := cctx.CurrentIdentity(); err == nil {
		for _, label := range ci.Labels {
			req.Header.Add("X-Fleetspeak-Labels", label)
		}
	} else {
		log.Errorf("Failed to get current identity: %v", err)
	}

	if si, err := cctx.ServerInfo(); err == nil {
		if si.ClientCertificateHeader != "" && clientCert != nil {
			bc := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCert})
			cc := url.PathEscape(string(bc))
			req.Header.Set(si.ClientCertificateHeader, cc)
		}
	} else {
		log.Errorf("Failed to get server info: %v", err)
	}

	var resp *http.Response
	resp, err = client.Do(req)
	if err != nil {
		return nil, time.Time{}, err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		didFetch = true
		modtime, err := http.ParseTime(resp.Header.Get("Last-Modified"))
		if err != nil {
			return resp.Body, time.Time{}, nil
		}
		return resp.Body, modtime, nil
	case http.StatusNotModified:
		resp.Body.Close()
		return nil, time.Time{}, nil
	default:
		resp.Body.Close()
		err = fmt.Errorf("failed with http response code: %v", resp.StatusCode)
		return nil, time.Time{}, err
	}
}
