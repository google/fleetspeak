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
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"io"
	"io/ioutil"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	log "github.com/golang/glog"
	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
	"github.com/google/fleetspeak/fleetspeak/src/comtesting"
	"github.com/google/fleetspeak/fleetspeak/src/server"
	"github.com/google/fleetspeak/fleetspeak/src/server/comms"
	"github.com/google/fleetspeak/fleetspeak/src/server/db"
	"github.com/google/fleetspeak/fleetspeak/src/server/sqlite"
	"github.com/google/fleetspeak/fleetspeak/src/server/testserver"

	cpb "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
)

var (
	serverCert []byte
)

func makeServer(t *testing.T, caseName string, frontendConfig *cpb.FrontendConfig) (*server.Server, *sqlite.Datastore, string) {
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
	com, err := NewCommunicator(Params{Listener: tl, Cert: cert, Key: key, Streaming: true, FrontendConfig: frontendConfig})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	log.Infof("Communicator listening to: %v", tl.Addr())
	ts := testserver.Make(t, "https", caseName, []comms.Communicator{com})

	return ts.S, ts.DS, tl.Addr().String()
}

func makeClient(t *testing.T) (common.ClientID, *http.Client, []byte) {
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
	return id, &cl, bc
}

func TestNormalPoll(t *testing.T) {
	ctx := context.Background()

	s, ds, addr := makeServer(t, "Normal", nil)
	id, cl, _ := makeClient(t)
	defer s.Stop()

	u := url.URL{Scheme: "https", Host: addr, Path: "/message"}

	// An empty body is a valid, though atypical initial request.
	resp, err := cl.Post(u.String(), "", nil)
	if err != nil {
		t.Fatal(err)
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Error(err)
	}
	resp.Body.Close()

	var cd fspb.ContactData
	if err := proto.Unmarshal(b, &cd); err != nil {
		t.Errorf("Unable to parse returned data as ContactData: %v", err)
	}
	if cd.SequencingNonce == 0 {
		t.Error("Expected SequencingNonce in returned ContactData")
	}

	// The client should now exist in the datastore.
	_, err = ds.GetClientData(ctx, id)
	if err != nil {
		t.Errorf("Error getting client data after poll: %v", err)
	}
}

func TestFile(t *testing.T) {
	ctx := context.Background()

	s, ds, addr := makeServer(t, "File", nil)
	_, cl, _ := makeClient(t)
	defer s.Stop()

	data := []byte("The quick sly fox jumped over the lazy dogs.")
	if err := ds.StoreFile(ctx, "testService", "testFile", bytes.NewReader(data)); err != nil {
		t.Errorf("Error from StoreFile(testService, testFile): %v", err)
	}

	u := url.URL{Scheme: "https", Host: addr, Path: "/files/testService/testFile"}
	resp, err := cl.Get(u.String())
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

func makeWrapped() []byte {
	cd := &fspb.ContactData{
		ClientClock: db.NowProto(),
	}
	b, err := proto.Marshal(cd)
	if err != nil {
		log.Fatal(err)
	}
	wcd := &fspb.WrappedContactData{
		ContactData:  b,
		ClientLabels: []string{"linux", "test"},
	}

	buf, err := proto.Marshal(wcd)
	if err != nil {
		log.Fatal(err)
	}
	sizeBuf := make([]byte, 0, 16)
	sizeBuf = binary.AppendUvarint(sizeBuf, uint64(len(buf)))

	return append(sizeBuf, buf...)
}

func readContact(body *bufio.Reader) (*fspb.ContactData, error) {
	size, err := binary.ReadUvarint(body)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, size)
	_, err = io.ReadFull(body, buf)
	if err != nil {
		return nil, err
	}
	var cd fspb.ContactData
	if err := proto.Unmarshal(buf, &cd); err != nil {
		return nil, err
	}
	return &cd, nil
}

func TestStreaming(t *testing.T) {
	ctx := context.Background()

	s, _, addr := makeServer(t, "Streaming", nil)
	_, cl, _ := makeClient(t)
	defer s.Stop()

	br, bw := io.Pipe()
	go func() {
		// First exchange - these writes must happen during the http.Client.Do call
		// below, because the server writes headers at the end of the first message
		// exchange.

		// Start with the magic number:
		binary.Write(bw, binary.LittleEndian, magic)

		if _, err := bw.Write(makeWrapped()); err != nil {
			t.Error(err)
		}
	}()

	u := url.URL{Scheme: "https", Host: addr, Path: "/streaming-message"}
	req, err := http.NewRequest("POST", u.String(), br)
	req.ContentLength = -1
	req.Close = true
	req.Header.Set("Expect", "100-continue")
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(ctx)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatalf("Streaming post failed (%v): %v", resp, err)
	}
	// Read ContactData for first exchange.
	body := bufio.NewReader(resp.Body)
	cd, err := readContact(body)
	if err != nil {
		t.Error(err)
	}
	if cd.AckIndex != 0 {
		t.Errorf("AckIndex of initial exchange should be unset, got %d", cd.AckIndex)
	}

	for i := uint64(1); i < 10; i++ {
		// Write another WrappedContactData.
		if _, err := bw.Write(makeWrapped()); err != nil {
			t.Error(err)
		}
		cd, err := readContact(body)
		if err != nil {
			t.Error(err)
		}
		if cd.AckIndex != i {
			t.Errorf("Received ack for contact %d, but expected %d", cd.AckIndex, i)
		}
	}

	bw.Close()
	resp.Body.Close()
}

func TestHeaderNormalPoll(t *testing.T) {
	ctx := context.Background()
	clientCertHeader := "ssl-client-cert"
	frontendConfig := &cpb.FrontendConfig{
		FrontendMode: &cpb.FrontendConfig_HttpsHeaderConfig{
			HttpsHeaderConfig: &cpb.HttpsHeaderConfig{
				ClientCertificateHeader: clientCertHeader,
			},
		},
	}

	s, ds, addr := makeServer(t, "Normal", frontendConfig)
	id, cl, bc := makeClient(t)
	defer s.Stop()

	u := url.URL{Scheme: "https", Host: addr, Path: "/message"}

	req, err := http.NewRequest("POST", u.String(), nil)
	req.Close = true
	cc := url.PathEscape(string(bc))
	req.Header.Set(clientCertHeader, cc)
	if err != nil {
		t.Fatal(err)
	}

	// An empty body is a valid, though atypical initial request.
	req = req.WithContext(ctx)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Error(err)
	}
	resp.Body.Close()

	var cd fspb.ContactData
	if err := proto.Unmarshal(b, &cd); err != nil {
		t.Errorf("Unable to parse returned data as ContactData: %v", err)
	}
	if cd.SequencingNonce == 0 {
		t.Error("Expected SequencingNonce in returned ContactData")
	}

	// The client should now exist in the datastore.
	_, err = ds.GetClientData(ctx, id)
	if err != nil {
		t.Errorf("Error getting client data after poll: %v", err)
	}
}

func TestHeaderStreaming(t *testing.T) {
	ctx := context.Background()
	clientCertHeader := "ssl-client-cert"
	frontendConfig := &cpb.FrontendConfig{
		FrontendMode: &cpb.FrontendConfig_HttpsHeaderConfig{
			HttpsHeaderConfig: &cpb.HttpsHeaderConfig{
				ClientCertificateHeader: clientCertHeader,
			},
		},
	}

	s, _, addr := makeServer(t, "Streaming", frontendConfig)
	_, cl, bc := makeClient(t)
	defer s.Stop()

	br, bw := io.Pipe()
	go func() {
		// First exchange - these writes must happen during the http.Client.Do call
		// below, because the server writes headers at the end of the first message
		// exchange.

		// Start with the magic number:
		binary.Write(bw, binary.LittleEndian, magic)

		if _, err := bw.Write(makeWrapped()); err != nil {
			t.Error(err)
		}
	}()

	u := url.URL{Scheme: "https", Host: addr, Path: "/streaming-message"}
	req, err := http.NewRequest("POST", u.String(), br)
	req.ContentLength = -1
	req.Close = true
	req.Header.Set("Expect", "100-continue")

	cc := url.PathEscape(string(bc))
	req.Header.Set(clientCertHeader, cc)
	if err != nil {
		t.Fatal(err)
	}
	req = req.WithContext(ctx)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatalf("Streaming post failed (%v): %v", resp, err)
	}
	// Read ContactData for first exchange.
	body := bufio.NewReader(resp.Body)
	cd, err := readContact(body)
	if err != nil {
		t.Error(err)
	}
	if cd.AckIndex != 0 {
		t.Errorf("AckIndex of initial exchange should be unset, got %d", cd.AckIndex)
	}

	for i := uint64(1); i < 10; i++ {
		// Write another WrappedContactData.
		if _, err := bw.Write(makeWrapped()); err != nil {
			t.Error(err)
		}
		cd, err := readContact(body)
		if err != nil {
			t.Error(err)
		}
		if cd.AckIndex != i {
			t.Errorf("Received ack for contact %d, but expected %d", cd.AckIndex, i)
		}
	}

	bw.Close()
	resp.Body.Close()
}
