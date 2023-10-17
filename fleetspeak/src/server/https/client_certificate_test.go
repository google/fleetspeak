// Copyright 2023 Google Inc.
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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	cpb "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
)

func makeTestClient(t *testing.T) (common.ClientID, *http.Client, []byte) {
	serverCert, _, err := comtesting.ServerCert()
	if err != nil {
		t.Fatal(err)
	}
	// Populate a CertPool with the server's certificate.
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM(serverCert) {
		t.Fatal("Unable to parse server pem.")
	}

	// Create a key for the client.
	privKey, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		t.Fatal(err)
	}
	bk := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b})

	id, err := common.MakeClientID(privKey.Public())
	if err != nil {
		t.Fatal(err)
	}

	// Create a self signed cert for client key.
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(42),
	}
	b, err = x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, privKey.Public(), privKey)
	if err != nil {
		t.Fatal(err)
	}
	bc := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: b})

	clientCert, err := tls.X509KeyPair(bc, bk)
	if err != nil {
		t.Fatal(err)
	}

	cl := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:            cp,
				Certificates:       []tls.Certificate{clientCert},
				InsecureSkipVerify: true,
			},
			Dial: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	return id, &cl, bc
}

func TestFrontendMode_MTLS(t *testing.T) {
	// These test cases should all make the frontend use mTLS mode
	testCases := []struct {
		config *cpb.FrontendConfig
	}{
		{
			config: &cpb.FrontendConfig{
				FrontendMode: &cpb.FrontendConfig_MtlsConfig{
					MtlsConfig: &cpb.MTlsConfig{},
				},
			},
		},
		{
			config: &cpb.FrontendConfig{
				FrontendMode: nil,
			},
		},
		{
			config: nil,
		},
	}

	for _, tc := range testCases {
		ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// test the valid frontend mode combination of receiving the client cert in the req
			cert, err := GetClientCert(req, tc.config)
			if err != nil {
				t.Fatal(err)
			}
			// make sure we received the client cert in the req
			if cert == nil {
				t.Error("Expected client certificate but received none")
			}
			fmt.Fprintln(w, "Testing Frontend Mode: MTLS")
		}))
		ts.TLS = &tls.Config{
			ClientAuth: tls.RequireAnyClientCert,
		}
		ts.StartTLS()
		defer ts.Close()

		_, client, _ := makeTestClient(t)

		res, err := client.Get(ts.URL)
		if err != nil {
			t.Fatal(err)
		}

		_, err = io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFrontendMode_HEADER_TLS(t *testing.T) {
	clientCertHeader := "ssl-client-cert"
	frontendConfig := &cpb.FrontendConfig{
		FrontendMode: &cpb.FrontendConfig_HttpsHeaderConfig{
			HttpsHeaderConfig: &cpb.HttpsHeaderConfig{
				ClientCertificateHeader: clientCertHeader,
			},
		},
	}
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// test the valid frontend mode combination of receiving the client cert in the header
		cert, err := GetClientCert(req, frontendConfig)
		if err != nil {
			t.Fatal(err)
		}
		// make sure we received the client cert in the header
		if cert == nil {
			t.Error("Expected client certificate but received none")
		}
		fmt.Fprintln(w, "Testing Frontend Mode: HEADER_TLS")
	}))
	ts.TLS = &tls.Config{
		ClientAuth: tls.RequireAnyClientCert,
	}
	ts.StartTLS()
	defer ts.Close()

	_, client, bc := makeTestClient(t)

	clientCert := url.PathEscape(string(bc))
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(clientCertHeader, clientCert)

	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	_, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
}
