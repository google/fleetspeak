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
	"crypto/sha256"
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

func calcClientCertFingerprint(derBytes []byte) (string) {
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

func makeTestClient(t *testing.T) (common.ClientID, *http.Client, []byte, string) {
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
	clientCertFingerprint := calcClientCertFingerprint(b)

	clientCert, err := tls.X509KeyPair(bc, bk)
	if err != nil {
		t.Fatal(err)
	}

	cl := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      cp,
				Certificates: []tls.Certificate{clientCert},
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
	return id, &cl, bc, clientCertFingerprint
}

func TestGetClientCert(t *testing.T) {
	tests := []struct {
		frontendMode cpb.FrontendMode
		clientCertHeader string
		clientCertChecksumHeader string
		wantCert bool
		wantErr bool
	}{
		{
			frontendMode: cpb.FrontendMode_MTLS,	// if the frontend mode is MTLS (original Fleetspeak design)
			clientCertHeader: "",			// and neither the client certification header,
			clientCertChecksumHeader: "",		// nor the client certification checksum header are set
			wantCert: true,				// GetClientCert() returns the certificate
			wantErr: false,				// and no error.
		},
		{
			frontendMode: cpb.FrontendMode_MTLS,	// if the frontend mode is MTLS (original Fleetspeak design)
			clientCertHeader: "ssl-client-cert",	// and the client certifcation header is set
			clientCertChecksumHeader: "",		// and the client certification checksum header is not set
			wantCert: false,			// GetClientCert() returns no certificate
			wantErr: true,				// but returns an error.
		},
		{
			frontendMode: cpb.FrontendMode_MTLS,			//if the fontend mode is MTLS (original Fleetspeak design)
			clientCertHeader: "",					// and the client certification header is not set
			clientCertChecksumHeader: "ssl-client-cert-checksum",	// but the client certification checksum header is set
			wantCert: false,					// GetClientCert() returns no certificate
			wantErr: true,						// but an error.
		},
		{
			frontendMode: cpb.FrontendMode_HEADER_TLS,	// if the frontend mode is HEADER_TLS (catering for L7 load balancer)
			clientCertHeader: "ssl-client-cert",		// and the client certification header is set
			clientCertChecksumHeader: "",			// and the client certification checksum header is not set
			wantCert: true,					// GetClientCert() returns the certificate
			wantErr: false,					// and no error.
		},
		{
			frontendMode: cpb.FrontendMode_HEADER_TLS,	// if the frontend mode is HEADER_TLS (catering for L7 load balancer)
			clientCertHeader: "",				// and neither the client certificate header is set
			clientCertChecksumHeader: "",			// nor the client certificate checksum header is set
			wantCert: false,				// GetClientCert() returns no certificate
			wantErr: true,					// but an error.
		},
		{
			frontendMode: cpb.FrontendMode_HEADER_TLS,		// if the frontend mode is HEADER_TLS (catering for L7 load balancer)
			clientCertHeader: "",					// and the client certificate header is not set
			clientCertChecksumHeader: "ssl-client-cert-checksum",	// and the client certificate checksum header is set
			wantCert: false,					// GetClientCert() return no certificate
			wantErr: true,						// but an error.
		},
		{
			frontendMode: cpb.FrontendMode_HEADER_TLS_CHECKSUM,	// if the frontend mode is HEADER_TLS_CHECKSUM (catering for L7 load balancer)
			clientCertHeader: "ssl-client-cert",			// and both the client certificate header
			clientCertChecksumHeader: "ssl-client-cert-checksum",	// and the client certificate checksum header are set
			wantCert: true,						// GetClientCert() returns teh certificate
			wantErr: false,						// and no error.
		},
		{
			frontendMode: cpb.FrontendMode_HEADER_TLS_CHECKSUM,	// if the frontend mode is HEADER_TLS_CHECKSUM (catering for L7 load balancer)
			clientCertHeader: "ssl-client-cert",			// and the client certificate header is set
			clientCertChecksumHeader: "",				// but the client certificate checksum header is not set
			wantCert: false,					// GetClientCert() returns no certificate
			wantErr: true,						// but an error.
		},
		{
			frontendMode: cpb.FrontendMode_HEADER_TLS_CHECKSUM,	//if the frontend mode is HEADER_TLS_CHECKSUM (catering for L7 load balancer)
			clientCertHeader: "",					// and the client certificate header is not set
			clientCertChecksumHeader: "ssl-client-cert-checksum",	// and the client certificate checksum header is set
			wantCert: false,					// GetClientCert() returns no certificate
			wantErr: true,						// but an error.
		},
	}

	for _, tc := range tests {
		ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			cert, err := GetClientCert(req, tc.clientCertHeader, tc.frontendMode, tc.clientCertChecksumHeader)
			if err != nil && !tc.wantErr {
				t.Errorf("GetClientCert(%s, %s, %s) = _, %v; wantErr %t", tc.clientCertHeader, tc.frontendMode, tc.clientCertChecksumHeader, err, tc.wantErr)
			}
			if cert != nil && !tc.wantCert {
				t.Errorf("GetClientCert(%s, %s, %s) = %v, _; wantCert %t", tc.clientCertHeader, tc.frontendMode, tc.clientCertChecksumHeader, cert, tc.wantCert)
			}
		}))
		ts.TLS = &tls.Config{
			ClientAuth: tls.RequireAnyClientCert,
		}
		ts.StartTLS()
		defer ts.Close()

		_, client, bc, clientCertChecksum := makeTestClient(t)

		req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
		if err != nil {
			t.Fatal(err)
		}

		if tc.clientCertHeader != "" {
			req.Header.Set(tc.clientCertHeader, url.PathEscape(string(bc)))
		}
		if tc.clientCertChecksumHeader != "" {
			req.Header.Set(tc.clientCertChecksumHeader, clientCertChecksum)
		}

		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()

		_, err = io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
	}

}
