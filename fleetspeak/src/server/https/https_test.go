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
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"log"
	"context"

	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/sqlite"
	"github.com/google/fleetspeak/fleetspeak/src/server/testserver"
)

var (
	serverCert []byte
)

func makeServer(t *testing.T, caseName string) (*server.Server, *sqlite.Datastore, string) {
	cert, key, err := comtesting.ServerCert()
	if err != nil {
		t.Fatal(err)
	}
	serverCert = cert

	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}
	tl, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	com, err := NewCommunicator(tl, cert, key)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	log.Printf("Communicator listening to: %v", tl.Addr())
	ts := testserver.Make(t, "https", caseName, []comms.Communicator{com})

	return ts.S, ts.DS, tl.Addr().String()
}

func makeClient(t *testing.T) *http.Client {
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
				RootCAs:      cp,
				Certificates: []tls.Certificate{clientCert},
			},
			Dial: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	return &cl
}

func TestNormal(t *testing.T) {
	ctx := context.Background()

	s, ds, addr := makeServer(t, "Normal")
	cl := makeClient(t)
	defer s.Stop()

	u := url.URL{Scheme: "https", Host: addr, Path: "/message"}
	resp, err := cl.Post(u.String(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// We were able to contact the server without any errors getting back to
	// us.
	//
	// TODO: Add more tests.

	data := []byte("The quick sly fox jumped over the lazy dogs.")
	if err := ds.StoreFile(ctx, "testService", "testFile", bytes.NewReader(data)); err != nil {
		t.Errorf("Error from StoreFile(testService, testFile): %v", err)
	}

	u = url.URL{Scheme: "https", Host: addr, Path: "/files/testService/testFile"}
	resp, err = cl.Get(u.String())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected response code when reading file got [%v] want [%v].", resp.StatusCode, http.StatusOK)
	}
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Unexpected error reading file body: %v", err)
	}
	if !bytes.Equal(data, b) {
		t.Errorf("Unexpected file body, got [%v], want [%v]", string(b), string(data))
	}
	resp.Body.Close()

}
