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
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	cpb "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
)

func calcClientCertChecksum(derBytes []byte) string {
	// Calculate the SHA-256 digest of the DER certificate
	sha256Digest := sha256.Sum256(derBytes)

	// Convert the SHA-256 digest to a hexadecimal string
	sha256HexStr := fmt.Sprintf("%x", sha256Digest)

	sha256Binary, err := hex.DecodeString(sha256HexStr)
	if err != nil {
		fmt.Sprintf("error decoding hexdump: %v\n", err)
		return ""
	}

	// Convert the hexadecimal string to a base64 encoded string
	// It also removes trailing "=" padding characters
	base64EncodedStr := strings.TrimRight(base64.StdEncoding.EncodeToString(sha256Binary), "=")

	// Return the base64 encoded string
	return base64EncodedStr
}

func makeTestClient(t *testing.T, clearText bool) (common.ClientID, *http.Client, []byte, string) {
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
	clientCertChecksum := calcClientCertChecksum(b)

	clientCert, err := tls.X509KeyPair(bc, bk)
	if err != nil {
		t.Fatal(err)
	}

	httpTransport := http.Transport{
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
	}
	if clearText {
		httpTransport = http.Transport{
			Dial: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}
	cl := http.Client{
		Transport: &httpTransport,
	}
	return id, &cl, bc, clientCertChecksum
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

		_, client, _, _ := makeTestClient(t, false)

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

	_, client, bc, _ := makeTestClient(t, false)

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
	defer res.Body.Close()
	_, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestFrontendMode_HEADER_TLS_CHECKSUM(t *testing.T) {
	clientCertHeader := "ssl-client-cert"
	clientCertChecksumHeader := "ssl-client-cert-checksum"
	frontendConfig := &cpb.FrontendConfig{
		FrontendMode: &cpb.FrontendConfig_HttpsHeaderChecksumConfig{
			HttpsHeaderChecksumConfig: &cpb.HttpsHeaderChecksumConfig{
				ClientCertificateHeader:         clientCertHeader,
				ClientCertificateChecksumHeader: clientCertChecksumHeader,
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
		fmt.Fprintln(w, "Testing Frontend Mode: HEADER_TLS_CHECKSUM")
	}))
	ts.TLS = &tls.Config{
		ClientAuth: tls.RequireAnyClientCert,
	}
	ts.StartTLS()
	defer ts.Close()

	_, client, bc, clientCertChecksum := makeTestClient(t, false)

	clientCert := url.PathEscape(string(bc))
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(clientCertHeader, clientCert)
	req.Header.Set(clientCertChecksumHeader, clientCertChecksum)

	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	_, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestFrontendMode_HEADER_CLEARTEXT(t *testing.T) {
	clientCertHeader := "ssl-client-cert"
	frontendConfig := &cpb.FrontendConfig{
		FrontendMode: &cpb.FrontendConfig_CleartextHeaderConfig{
			CleartextHeaderConfig: &cpb.CleartextHeaderConfig{
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
		fmt.Fprintln(w, "Testing Frontend Mode: HEADER_HEADER")
	}))
	ts.Start()
	defer ts.Close()

	_, client, bc, _:= makeTestClient(t, false)

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
	defer res.Body.Close()
	_, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestFrontendMode_HEADER_CLEARTEXT_CHECKSUM(t *testing.T) {
	clientCertHeader := "ssl-client-cert"
	clientCertChecksumHeader := "ssl-client-cert-checksum"
	frontendConfig := &cpb.FrontendConfig{
		FrontendMode: &cpb.FrontendConfig_CleartextHeaderChecksumConfig{
			CleartextHeaderChecksumConfig: &cpb.CleartextHeaderChecksumConfig{
				ClientCertificateHeader:         clientCertHeader,
				ClientCertificateChecksumHeader: clientCertChecksumHeader,
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
		fmt.Fprintln(w, "Testing Frontend Mode: HEADER_CHECKSUM")
	}))
	ts.Start()
	defer ts.Close()

	_, client, bc, clientCertChecksum := makeTestClient(t, true)

	clientCert := url.PathEscape(string(bc))
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(clientCertHeader, clientCert)
	req.Header.Set(clientCertChecksumHeader, clientCertChecksum)

	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	_, err = io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
}
