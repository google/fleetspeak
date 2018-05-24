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

// Package https provides an client.Communicator which connects to the
// Fleetspeak server using https.
package https

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	mrand "math/rand"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/client/comms"
	"github.com/google/fleetspeak/fleetspeak/src/common"
)

const (
	sendBytesThreshold = 15 * 1024 * 1024
	sendCountThreshold = 100
	closeWaitThreshold = 30 * time.Second // Matches IdleTimeout in server/https.
)

func makeTransport(cctx comms.Context, dc func(ctx context.Context, network, addr string) (net.Conn, error)) (common.ClientID, *http.Transport, error) {
	ci, err := cctx.CurrentIdentity()
	if err != nil {
		return common.ClientID{}, nil, err
	}
	si, err := cctx.ServerInfo()
	if err != nil {
		return common.ClientID{}, nil, err
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
		return common.ClientID{}, nil, fmt.Errorf("unable to configure communicator, could not create client cert: %v", err)
	}

	if dc == nil {
		dc = (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext
	}

	return ci.ID, &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			RootCAs: si.TrustedCerts,
			Certificates: []tls.Certificate{{
				Certificate: [][]byte{certBytes},
				PrivateKey:  ci.Private,
			}},
			CipherSuites: []uint16{
				// We implement both endpoints, so we might as well require long keys and
				// perfect forward secrecy. Note that TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
				// is required by the https library.
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
			VerifyPeerCertificate: cv,
		},
		MaxIdleConns:          10,
		DialContext:           dc,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}, nil
}

// jitter adds up to 50% random jitter, and converts to time.Duration.
func jitter(seconds int32) time.Duration {
	return time.Duration((1.0 + 0.5*mrand.Float32()) * float32(seconds) * float32(time.Second))
}

func getFileIfModified(ctx context.Context, hosts []string, client *http.Client, service, name string, modSince time.Time) (io.ReadCloser, time.Time, error) {
	var lastErr error
	for _, h := range hosts {
		u := url.URL{Scheme: "https", Host: h,
			Path: "/files/" + url.PathEscape(service) + "/" + url.PathEscape(name)}

		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			lastErr = err
			continue
		}
		req = req.WithContext(ctx)

		if (modSince != time.Time{}) {
			req.Header.Set("If-Modified-Since", modSince.Format(http.TimeFormat))
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if ctx.Err() != nil {
				break
			}
			continue
		}

		switch resp.StatusCode {
		case http.StatusOK:
			return resp.Body, time.Time{}, nil
		case http.StatusNotModified:
			resp.Body.Close()
			return nil, time.Time{}, nil
		default:
			resp.Body.Close()
			lastErr = fmt.Errorf("failed with http response code: %v", resp.StatusCode)
			continue
		}
	}

	return nil, time.Time{}, fmt.Errorf("unable to retrieve file, last attempt failed with: %v", lastErr)
}
