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

package integrationtest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"sync/atomic"
	"testing"

	log "github.com/golang/glog"
	"github.com/google/fleetspeak/fleetspeak/src/client/signer"
	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

type testAuthorizer struct {
	authorizer.PermissiveAuthorizer
	authCount   int64
	t           *testing.T
	expectCount int
	expectValid map[int64]bool
	expectType  map[int64]x509.SignatureAlgorithm
}

func (a *testAuthorizer) Allow4(_ net.Addr, _ authorizer.ContactInfo, _ authorizer.ClientInfo, sigs []authorizer.SignatureInfo) (accept bool, validationInfo *fspb.ValidationInfo) {
	atomic.AddInt64(&a.authCount, 1)
	if len(sigs) != a.expectCount {
		a.t.Errorf("expected %d sigs, got %d", a.expectCount, len(sigs))
	}
	for _, s := range sigs {
		serial := s.Certificate[0].SerialNumber.Int64()
		if a.expectType[serial] != s.Algorithm {
			a.t.Errorf("Expected %v to have signature of type %v, got %v", serial, a.expectType[serial], s.Algorithm)
		}
		if a.expectValid[serial] != s.Valid {
			a.t.Errorf("Expected %v to have valid=%t got valid=%t", serial, a.expectValid[serial], s.Valid)
		}
	}
	return true, &fspb.ValidationInfo{Tags: map[string]string{"result": "Valid"}}
}

type testSigner struct {
	cert   *x509.Certificate
	alg    x509.SignatureAlgorithm
	hash   crypto.Hash
	signer crypto.Signer
	t      *testing.T
}

func (s *testSigner) SignContact(data []byte) *fspb.Signature {
	h := s.hash.New()
	h.Write(data)
	hashed := h.Sum(nil)
	sig, err := s.signer.Sign(rand.Reader, hashed, s.hash)
	if err != nil {
		log.Exitf("Unable to sign hashed of length %d with (%v): %v", len(hashed), s.hash, err)
	}
	return &fspb.Signature{
		Certificate: [][]byte{s.cert.Raw},
		Algorithm:   int32(s.alg),
		Signature:   sig,
	}
}

func makeAuthorizerSigners(t *testing.T) (*testAuthorizer, []signer.Signer) {
	var sigs []signer.Signer
	cases := []struct {
		serial  int64
		ka      x509.PublicKeyAlgorithm
		hash    crypto.Hash
		sa      x509.SignatureAlgorithm
		invalid bool
	}{
		{
			serial: 42,
			ka:     x509.RSA,
			hash:   crypto.SHA256,
			sa:     x509.SHA256WithRSA,
		},
		{
			serial: 49,
			ka:     x509.ECDSA,
			hash:   crypto.SHA256,
			sa:     x509.ECDSAWithSHA256,
		},
		{
			serial:  50,
			ka:      x509.ECDSA,
			hash:    crypto.SHA1, // broken sig - wrong hash used to generate it
			sa:      x509.ECDSAWithSHA256,
			invalid: true,
		},
	}
	auth := testAuthorizer{
		t:           t,
		expectCount: len(cases),
		expectValid: make(map[int64]bool),
		expectType:  make(map[int64]x509.SignatureAlgorithm),
	}
	for _, c := range cases {
		cert, key := makeCert(c.serial, c.ka)
		sigs = append(sigs, &testSigner{
			cert:   cert,
			alg:    c.sa,
			hash:   c.hash,
			signer: key.(crypto.Signer),
			t:      t,
		})
		auth.expectValid[c.serial] = !c.invalid
		auth.expectType[c.serial] = c.sa
	}
	return &auth, sigs
}

func makeCert(serial int64, alg x509.PublicKeyAlgorithm) (*x509.Certificate, crypto.PrivateKey) {
	var k crypto.PrivateKey
	var pk crypto.PublicKey
	switch alg {
	case x509.RSA:
		rk, err := rsa.GenerateKey(rand.Reader, 4096)
		if err != nil {
			log.Fatalf("Unable to generate RSA key: %v", err)
		}
		k = rk
		pk = rk.Public()
	case x509.ECDSA:
		ek, err := ecdsa.GenerateKey(elliptic.P224(), rand.Reader)
		if err != nil {
			log.Fatalf("Unable to generate ECDSA key: %v", err)
		}
		k = ek
		pk = ek.Public()
	default:
		log.Fatalf("Unknown public key algorithm type: %v", alg)
	}

	tmpl := x509.Certificate{
		Subject:               pkix.Name{CommonName: "Test client"},
		SerialNumber:          big.NewInt(serial),
		BasicConstraintsValid: true,
	}

	b, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, pk, k)
	if err != nil {
		log.Fatalf("Unable to create x509 cert: %v", err)
	}
	res, err := x509.ParseCertificate(b)
	if err != nil {
		log.Fatalf("Unable to parse newly created cert: %v", err)
	}
	return res, k
}
