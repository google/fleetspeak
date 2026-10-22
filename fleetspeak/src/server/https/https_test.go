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
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	log "github.com/golang/glog"
	"google.golang.org/protobuf/proto"

	"github.com/google/fleetspeak/fleetspeak/src/server/sertesting"

	"github.com/google/fleetspeak/fleetspeak/src/client/https"
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

func makeServer(t *testing.T, caseName string, frontendConfig *cpb.FrontendConfig, fileStores map[string]db.FileStore) (*server.Server, *sqlite.Datastore, string) {
	t.Helper()
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

	ts := testserver.MakeWithFileStores(t, "https", caseName, []comms.Communicator{com}, fileStores)

	return ts.S, ts.DS, tl.Addr().String()
}

func clientCertFingerprint(derBytes []byte) string {
	// Calculate the SHA-256 digest of the DER certificate
	sha256Digest := sha256.Sum256(derBytes)

	// Convert the SHA-256 digest to a hexadecimal string
	sha256HexStr := fmt.Sprintf("%x", sha256Digest)

	sha256Binary, err := hex.DecodeString(sha256HexStr)
	if err != nil {
		log.Errorf("error decoding hexdump: %v\n", err)
		return ""
	}

	// Convert the hexadecimal string to a base64 encoded string
	// It also removes trailing "=" padding characters
	base64EncodedStr := strings.TrimRight(base64.StdEncoding.EncodeToString(sha256Binary), "=")

	// Return the base64 encoded string
	return base64EncodedStr
}

func makeClient(t *testing.T, doTLS bool) (common.ClientID, *http.Client, []byte, string) {
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
	fp := clientCertFingerprint(b)
	bc := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: b})

	clientCert, err := tls.X509KeyPair(bc, bk)
	if err != nil {
		t.Fatal(err)
	}
	var cl http.Client
	if doTLS {
		cl = http.Client{
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
	} else {
		cl = http.Client{
			Transport: &http.Transport{
				Dial: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).Dial,
				ExpectContinueTimeout: 1 * time.Second,
			},
		}
	}
	return id, &cl, bc, fp
}

func TestNormalPoll(t *testing.T) {
	ctx := context.Background()

	s, ds, addr := makeServer(t, "Normal", nil, nil)
	id, cl, _, _ := makeClient(t, true)
	defer s.Stop()

	u := url.URL{Scheme: "https", Host: addr, Path: "/message"}

	// An empty body is a valid, though atypical initial request.
	resp, err := cl.Post(u.String(), "", nil)
	if err != nil {
		t.Fatal(err)
	}

	b, err := io.ReadAll(resp.Body)
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
	s, ds, addr := makeServer(t, "File", nil, nil)
	_, cl, _, _ := makeClient(t, true)
	defer s.Stop()

	service, name := "testService", "testsFiles/foo"
	data := []byte("The quick sly fox jumped over the lazy dogs.")
	if err := ds.StoreFile(ctx, service, name, bytes.NewReader(data)); err != nil {
		t.Errorf("Error from StoreFile(%s, %s): %v", service, name, err)
	}

	u := url.URL{
		Scheme: "https",
		Host:   addr,
		Path:   path.Join("/files", url.PathEscape(service), url.PathEscape(name)),
	}
	resp, err := cl.Get(u.String())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected response code when reading file got [%v] want [%v].", resp.StatusCode, http.StatusOK)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("Unexpected error reading file body: %v", err)
	}
	if !bytes.Equal(data, b) {
		t.Errorf("Unexpected file body, got [%v], want [%v]", string(b), string(data))
	}
	resp.Body.Close()
}

func TestParseFilePath(t *testing.T) {
	testCases := []struct {
		name        string
		path        string
		wantService string
		wantName    string
		wantErr     bool
	}{
		{
			name:        "valid path",
			path:        "/files/service/name",
			wantService: "service",
			wantName:    "name",
		},
		{
			name:        "valid path with escaped characters",
			path:        "/files/service%20name/file%2Fname",
			wantService: "service name",
			wantName:    "file/name",
		},
		{
			name:        "valid path with empty name",
			path:        "/files/service/",
			wantService: "service",
			wantName:    "",
		},
		{
			name:    "invalid path - too short",
			path:    "/files/service",
			wantErr: true,
		},
		{
			name:    "invalid path - too long",
			path:    "/files/service/name/extra",
			wantErr: true,
		},
		{
			name:    "invalid path - wrong prefix",
			path:    "/notfiles/service/name",
			wantErr: true,
		},
		{
			name:    "invalid path - malformed escape",
			path:    "/files/service/name%2",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotService, gotName, err := parseFilePath(tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseFilePath(%q) returned error %v, wantErr %v", tc.path, err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if gotService != tc.wantService {
				t.Errorf("parseFilePath(%q) gotService = %q, want %q", tc.path, gotService, tc.wantService)
			}
			if gotName != tc.wantName {
				t.Errorf("parseFilePath(%q) gotName = %q, want %q", tc.path, gotName, tc.wantName)
			}
		})
	}
}

func TestServiceSpecificFile(t *testing.T) {
	mockFS := sertesting.NewMapFileStore()
	mockFS.SetFile("testService", "foo", []byte("bar"), time.Now())

	s, _, addr := makeServer(t, "ServiceSpecificFile", nil, map[string]db.FileStore{"testService": mockFS})
	_, cl, _, _ := makeClient(t, true)
	defer s.Stop()

	service, name := "testService", "foo"

	u := url.URL{
		Scheme: "https",
		Host:   addr,
		Path:   path.Join("/files", url.PathEscape(service), url.PathEscape(name)),
	}
	resp, err := cl.Get(u.String())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Unexpected response code when reading file got %v want %v.", resp.StatusCode, http.StatusOK)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Unexpected error reading file body: %v", err)
	}
	expectedData := []byte("bar")
	if !bytes.Equal(expectedData, b) {
		t.Errorf("Unexpected file body, got %s, want %s", string(b), string(expectedData))
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
	testCases := []struct {
		name        string
		compression fspb.CompressionAlgorithm
	}{
		{
			name:        "uncompressed",
			compression: fspb.CompressionAlgorithm_COMPRESSION_NONE,
		},
		{
			name:        "deflate",
			compression: fspb.CompressionAlgorithm_COMPRESSION_DEFLATE,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, _, addr := makeServer(t, "Streaming", nil, nil)
			_, cl, _, _ := makeClient(t, true)
			defer s.Stop()

			pr, pw := io.Pipe()
			defer pr.Close()

			bw := https.CompressingWriter(pw, tc.compression)
			defer bw.Close()

			wg := sync.WaitGroup{}
			wg.Add(1)
			go func() {
				// First exchange - these writes must happen during the http.Client.Do call
				// below, because the server writes headers at the end of the first message
				// exchange.

				// Start with the magic number:
				binary.Write(bw, binary.LittleEndian, magic)

				if _, err := bw.Write(makeWrapped()); err != nil {
					t.Error(err)
				}
				bw.Flush()
				wg.Done()
			}()

			u := url.URL{Scheme: "https", Host: addr, Path: "/streaming-message"}
			req, err := http.NewRequest("POST", u.String(), pr)
			if err != nil {
				t.Fatal(err)
			}
			req.ContentLength = -1
			req.Close = true
			req.Header.Set("Expect", "100-continue")
			https.SetContentEncoding(req.Header, tc.compression)
			req = req.WithContext(ctx)
			resp, err := cl.Do(req)
			if err != nil {
				t.Fatalf("Streaming post failed (%v): %v", resp, err)
			}
			defer resp.Body.Close()
			// Read ContactData for first exchange.
			body := bufio.NewReader(resp.Body)
			cd, err := readContact(body)
			if err != nil {
				t.Fatal(err)
			}
			if cd.AckIndex != 0 {
				t.Errorf("AckIndex of initial exchange should be unset, got %d", cd.AckIndex)
			}
			wg.Wait()

			for i := uint64(1); i < 10; i++ {
				// Write another WrappedContactData.
				if _, err := bw.Write(makeWrapped()); err != nil {
					t.Error(err)
				}
				bw.Flush()
				cd, err := readContact(body)
				if err != nil {
					t.Fatal(err)
				}
				if cd.AckIndex != i {
					t.Errorf("Received ack for contact %d, but expected %d", cd.AckIndex, i)
				}
			}
		})
	}
}

// marshalWrapped returns a serialized WrappedContactData with the given client
// labels, wrapping a ContactData that only has its client clock set.
func marshalWrapped(t *testing.T, labels ...string) []byte {
	t.Helper()
	cd, err := proto.Marshal(&fspb.ContactData{ClientClock: db.NowProto()})
	if err != nil {
		t.Fatal(err)
	}
	b, err := proto.Marshal(&fspb.WrappedContactData{ContactData: cd, ClientLabels: labels})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// compress returns b compressed with the given algorithm, as the client sends
// it.
func compress(t *testing.T, b []byte, compression fspb.CompressionAlgorithm) []byte {
	t.Helper()
	var buf bytes.Buffer
	bw := https.CompressingWriter(&buf, compression)
	if _, err := bw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := bw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// postMessage sends body to the /message endpoint of the server at addr,
// announcing the given compression. If hideLength is set, the body is sent
// without a Content-Length header, i.e. chunked.
func postMessage(t *testing.T, cl *http.Client, addr string, body []byte, compression fspb.CompressionAlgorithm, hideLength bool) *http.Response {
	t.Helper()
	u := url.URL{Scheme: "https", Host: addr, Path: "/message"}
	var r io.Reader = bytes.NewReader(body)
	if hideLength {
		// Hide the type of the reader, so that its length is unknown.
		r = io.MultiReader(r)
	}
	req, err := http.NewRequest(http.MethodPost, u.String(), r)
	if err != nil {
		t.Fatal(err)
	}
	if hideLength {
		req.ContentLength = -1
	}
	req.Close = true
	https.SetContentEncoding(req.Header, compression)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatalf("POST to /message failed: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// checkPollResponse checks that resp is a successful response to a poll.
func checkPollResponse(t *testing.T, resp *http.Response) {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Error reading response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Poll returned status %d (%q), want %d", resp.StatusCode, b, http.StatusOK)
	}
	var cd fspb.ContactData
	if err := proto.Unmarshal(b, &cd); err != nil {
		t.Fatalf("Unable to parse returned data as ContactData: %v", err)
	}
	if cd.SequencingNonce == 0 {
		t.Error("Expected SequencingNonce in returned ContactData")
	}
}

// checkClientLabels checks that the client with the given id exists in the
// datastore and has exactly the given labels.
func checkClientLabels(t *testing.T, ds *sqlite.Datastore, id common.ClientID, want []string) {
	t.Helper()
	cd, err := ds.GetClientData(context.Background(), id)
	if err != nil {
		t.Fatalf("Error getting client data after poll: %v", err)
	}
	var got []string
	for _, l := range cd.Labels {
		got = append(got, l.GetLabel())
	}
	slices.Sort(got)
	want = slices.Sorted(slices.Values(want))
	if !slices.Equal(got, want) {
		t.Errorf("Client has labels %q, want %q", got, want)
	}
}

func TestReadWrappedContactData(t *testing.T) {
	wcdBytes := marshalWrapped(t, "linux", "test")
	wcd := &fspb.WrappedContactData{}
	if err := proto.Unmarshal(wcdBytes, wcd); err != nil {
		t.Fatal(err)
	}
	const next = "next frame"

	testCases := []struct {
		name          string
		input         []byte
		size          int
		want          *fspb.WrappedContactData
		wantReadBytes int
		wantUnread    string
		wantErr       bool
	}{
		{
			name:          "exact size",
			input:         append(bytes.Clone(wcdBytes), next...),
			size:          len(wcdBytes),
			want:          wcd,
			wantReadBytes: len(wcdBytes),
			wantUnread:    next,
		},
		{
			name:          "zero size",
			input:         []byte(next),
			size:          0,
			want:          &fspb.WrappedContactData{},
			wantReadBytes: 0,
			wantUnread:    next,
		},
		{
			name:          "unknown size",
			input:         wcdBytes,
			size:          -1,
			want:          wcd,
			wantReadBytes: len(wcdBytes),
		},
		{
			name:          "empty input of unknown size",
			input:         nil,
			size:          -1,
			want:          &fspb.WrappedContactData{},
			wantReadBytes: 0,
		},
		{
			name:          "truncated input",
			input:         wcdBytes[:len(wcdBytes)-1],
			size:          len(wcdBytes),
			wantReadBytes: len(wcdBytes) - 1,
			wantErr:       true,
		},
		{
			name:          "invalid input",
			input:         []byte(next),
			size:          len(next),
			wantReadBytes: len(next),
			wantErr:       true,
		},
	}
	for _, tc := range testCases {
		// Cover both pooled and freshly allocated buffers.
		for _, maxReusableBufferSize := range []int{0, 1024 * 1024} {
			t.Run(fmt.Sprintf("%s/maxReusableBufferSize=%d", tc.name, maxReusableBufferSize), func(t *testing.T) {
				r := bytes.NewReader(tc.input)
				got, readBytes, err := readWrappedContactData(r, tc.size, maxReusableBufferSize)
				if (err != nil) != tc.wantErr {
					t.Errorf("readWrappedContactData(size=%d) returned error %v, want error: %t", tc.size, err, tc.wantErr)
				}
				if !tc.wantErr && !proto.Equal(got, tc.want) {
					t.Errorf("readWrappedContactData(size=%d) = %v, want %v", tc.size, got, tc.want)
				}
				if readBytes != tc.wantReadBytes {
					t.Errorf("readWrappedContactData(size=%d) read %d bytes, want %d", tc.size, readBytes, tc.wantReadBytes)
				}
				unread, err := io.ReadAll(r)
				if err != nil {
					t.Fatal(err)
				}
				if string(unread) != tc.wantUnread {
					t.Errorf("readWrappedContactData(size=%d) left %q unread, want %q", tc.size, unread, tc.wantUnread)
				}
			})
		}
	}
}

func TestCompressedPoll(t *testing.T) {
	s, ds, addr := makeServer(t, "CompressedPoll", nil, nil)
	defer s.Stop()

	testCases := []struct {
		name       string
		labels     []string
		hideLength bool
	}{
		{
			// The compressed body is much shorter than the WrappedContactData.
			name:   "compressible",
			labels: []string{"linux", strings.Repeat("compressible", 100)},
		},
		{
			// With the zlib header and checksum, the compressed body is longer than
			// the WrappedContactData.
			name:   "small",
			labels: []string{"linux"},
		},
		{
			name:       "without content length",
			labels:     []string{"linux", strings.Repeat("compressible", 100)},
			hideLength: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			id, cl, _, _ := makeClient(t, true)
			body := compress(t, marshalWrapped(t, tc.labels...), fspb.CompressionAlgorithm_COMPRESSION_DEFLATE)
			resp := postMessage(t, cl, addr, body, fspb.CompressionAlgorithm_COMPRESSION_DEFLATE, tc.hideLength)
			checkPollResponse(t, resp)
			// The labels show that the server read the whole WrappedContactData.
			checkClientLabels(t, ds, id, tc.labels)
		})
	}
}

func TestPollWithoutContentLength(t *testing.T) {
	s, ds, addr := makeServer(t, "PollWithoutContentLength", nil, nil)
	defer s.Stop()
	id, cl, _, _ := makeClient(t, true)

	labels := []string{"linux", "test"}
	resp := postMessage(t, cl, addr, marshalWrapped(t, labels...), fspb.CompressionAlgorithm_COMPRESSION_NONE, true)
	checkPollResponse(t, resp)
	checkClientLabels(t, ds, id, labels)
}

func TestPollTooLarge(t *testing.T) {
	ctx := context.Background()
	s, ds, addr := makeServer(t, "PollTooLarge", nil, nil)
	defer s.Stop()

	// A valid WrappedContactData of MaxContactSize+1 bytes: the contact_data
	// field takes 1 byte for its tag and 4 bytes for its length.
	tooLarge, err := proto.Marshal(&fspb.WrappedContactData{ContactData: make([]byte, MaxContactSize+1-5)})
	if err != nil {
		t.Fatal(err)
	}
	if len(tooLarge) != MaxContactSize+1 {
		t.Fatalf("WrappedContactData has %d bytes, want %d", len(tooLarge), MaxContactSize+1)
	}

	// The sizes of these bodies are unknown until the server has read them.
	testCases := []struct {
		name        string
		compression fspb.CompressionAlgorithm
	}{
		{
			name:        "uncompressed",
			compression: fspb.CompressionAlgorithm_COMPRESSION_NONE,
		},
		{
			name:        "deflate",
			compression: fspb.CompressionAlgorithm_COMPRESSION_DEFLATE,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			id, cl, _, _ := makeClient(t, true)
			body := compress(t, tooLarge, tc.compression)
			resp := postMessage(t, cl, addr, body, tc.compression, true)
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("Poll returned status %d, want %d", resp.StatusCode, http.StatusBadRequest)
			}
			if _, err := ds.GetClientData(ctx, id); err == nil {
				t.Error("Client exists in the datastore after a poll that was too large")
			}
		})
	}
}

func TestStreamingEmptyContacts(t *testing.T) {
	s, _, addr := makeServer(t, "StreamingEmptyContacts", nil, nil)
	defer s.Stop()

	testCases := []struct {
		name        string
		compression fspb.CompressionAlgorithm
	}{
		{
			name:        "uncompressed",
			compression: fspb.CompressionAlgorithm_COMPRESSION_NONE,
		},
		{
			name:        "deflate",
			compression: fspb.CompressionAlgorithm_COMPRESSION_DEFLATE,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// The stream stays open, so the server must not wait for its end.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, cl, _, _ := makeClient(t, true)

			pr, pw := io.Pipe()
			defer pr.Close()
			// Ending the request body is the only way to unblock the client if the
			// server doesn't respond.
			stop := context.AfterFunc(ctx, func() { pr.CloseWithError(context.Cause(ctx)) })
			defer stop()
			bw := https.CompressingWriter(pw, tc.compression)
			defer bw.Close()
			emptyFrame := binary.AppendUvarint(nil, 0)

			initialContactWritten := make(chan struct{})
			go func() {
				defer close(initialContactWritten)
				// An empty WrappedContactData is a valid, though atypical, initial
				// contact. It has to be written during the http.Client.Do call below,
				// because the server writes headers at the end of the first exchange.
				binary.Write(bw, binary.LittleEndian, magic)
				if _, err := bw.Write(emptyFrame); err != nil {
					t.Error(err)
				}
				bw.Flush()
			}()

			u := url.URL{Scheme: "https", Host: addr, Path: "/streaming-message"}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), pr)
			if err != nil {
				t.Fatal(err)
			}
			req.ContentLength = -1
			req.Close = true
			req.Header.Set("Expect", "100-continue")
			https.SetContentEncoding(req.Header, tc.compression)
			resp, err := cl.Do(req)
			if err != nil {
				t.Fatalf("Streaming post failed: %v", err)
			}
			defer resp.Body.Close()
			body := bufio.NewReader(resp.Body)
			cd, err := readContact(body)
			if err != nil {
				t.Fatalf("Error reading ContactData of the initial exchange: %v", err)
			}
			if cd.AckIndex != 0 {
				t.Errorf("AckIndex of initial exchange should be unset, got %d", cd.AckIndex)
			}
			<-initialContactWritten

			// Empty WrappedContactData on the stream are acked like any other.
			if _, err := bw.Write(emptyFrame); err != nil {
				t.Fatal(err)
			}
			bw.Flush()
			cd, err = readContact(body)
			if err != nil {
				t.Fatalf("Error reading ack of an empty WrappedContactData: %v", err)
			}
			if cd.AckIndex != 1 {
				t.Errorf("Received ack for contact %d, but expected 1", cd.AckIndex)
			}
		})
	}
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

	s, ds, addr := makeServer(t, "Normal", frontendConfig, nil)
	id, cl, bc, _ := makeClient(t, true)
	defer s.Stop()

	u := url.URL{Scheme: "https", Host: addr, Path: "/message"}

	req, err := http.NewRequest("POST", u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Close = true
	cc := url.PathEscape(string(bc))
	req.Header.Set(clientCertHeader, cc)

	// An empty body is a valid, though atypical initial request.
	req = req.WithContext(ctx)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}

	b, err := io.ReadAll(resp.Body)
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

	s, _, addr := makeServer(t, "Streaming", frontendConfig, nil)
	_, cl, bc, _ := makeClient(t, true)
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
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = -1
	req.Close = true
	req.Header.Set("Expect", "100-continue")

	cc := url.PathEscape(string(bc))
	req.Header.Set(clientCertHeader, cc)
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

func TestHeaderStreamingChecksum(t *testing.T) {
	ctx := context.Background()
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

	s, _, addr := makeServer(t, "Streaming", frontendConfig, nil)
	_, cl, bc, fp := makeClient(t, true)
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
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = -1
	req.Close = true
	req.Header.Set("Expect", "100-continue")

	cc := url.PathEscape(string(bc))
	req.Header.Set(clientCertHeader, cc)
	req.Header.Set(clientCertChecksumHeader, fp)
	req = req.WithContext(ctx)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatalf("Streaming post failed (%v): %v", resp, err)
	}
	// Read ContactData for first exchange.
	body := bufio.NewReader(resp.Body)
	cd, err := readContact(body)
	if cd == nil {
		t.Fatalf("Read Contact returned nil: %v", err)
	}
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

func TestCleartextHeaderStreaming(t *testing.T) {
	ctx := context.Background()
	clientCertHeader := "ssl-client-cert"
	frontendConfig := &cpb.FrontendConfig{
		FrontendMode: &cpb.FrontendConfig_CleartextHeaderConfig{
			CleartextHeaderConfig: &cpb.CleartextHeaderConfig{
				ClientCertificateHeader: clientCertHeader,
			},
		},
	}

	s, _, addr := makeServer(t, "Streaming", frontendConfig, nil)
	_, cl, bc, _ := makeClient(t, false)
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

	u := url.URL{Scheme: "http", Host: addr, Path: "/streaming-message"}
	req, err := http.NewRequest("POST", u.String(), br)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = -1
	req.Close = true
	req.Header.Set("Expect", "100-continue")

	cc := url.PathEscape(string(bc))
	req.Header.Set(clientCertHeader, cc)
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

func TestCleartextHeaderStreamingChecksum(t *testing.T) {
	ctx := context.Background()
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

	s, _, addr := makeServer(t, "Streaming", frontendConfig, nil)
	_, cl, bc, fp := makeClient(t, false)
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

	u := url.URL{Scheme: "http", Host: addr, Path: "/streaming-message"}
	req, err := http.NewRequest("POST", u.String(), br)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = -1
	req.Close = true
	req.Header.Set("Expect", "100-continue")

	cc := url.PathEscape(string(bc))
	req.Header.Set(clientCertHeader, cc)
	req.Header.Set(clientCertChecksumHeader, fp)
	req = req.WithContext(ctx)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatalf("Streaming post failed (%v): %v", resp, err)
	}
	// Read ContactData for first exchange.
	body := bufio.NewReader(resp.Body)
	cd, err := readContact(body)
	if cd == nil {
		t.Fatalf("Read Contact returned nil: %v", err)
	}
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

func TestCleartextXfccStreaming(t *testing.T) {
	ctx := context.Background()
	clientCertHeader := "ssl-client-cert"
	frontendConfig := &cpb.FrontendConfig{
		FrontendMode: &cpb.FrontendConfig_CleartextXfccConfig{
			CleartextXfccConfig: &cpb.CleartextXfccConfig{
				ClientCertificateHeader: clientCertHeader,
			},
		},
	}

	s, _, addr := makeServer(t, "Streaming", frontendConfig, nil)
	_, cl, bc, _ := makeClient(t, false)
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

	u := url.URL{Scheme: "http", Host: addr, Path: "/streaming-message"}
	req, err := http.NewRequest("POST", u.String(), br)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = -1
	req.Close = true
	req.Header.Set("Expect", "100-continue")

	cc := url.PathEscape(string(bc))
	req.Header.Set(clientCertHeader, `Cert=`+cc)
	req = req.WithContext(ctx)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatalf("Streaming post failed (%v): %v", resp, err)
	}
	// Read ContactData for first exchange.
	body := bufio.NewReader(resp.Body)
	cd, err := readContact(body)
	if cd == nil {
		t.Fatalf("Read Contact returned nil: %v", err)
	}
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
