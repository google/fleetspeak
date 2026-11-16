// Copyright 2026 Google Inc.
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

package signatures

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"math/big"
	"testing"

	"github.com/google/fleetspeak/fleetspeak/src/common"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

func makeTestCertAndSig(t *testing.T, data []byte) ([]byte, []byte) {
	t.Helper()
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() failed: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, privKey.Public(), privKey)
	if err != nil {
		t.Fatalf("x509.CreateCertificate() failed: %v", err)
	}
	digest := sha256.Sum256(data)
	sig, err := ecdsa.SignASN1(rand.Reader, privKey, digest[:])
	if err != nil {
		t.Fatalf("ecdsa.SignASN1() failed: %v", err)
	}
	return certDER, sig
}

func TestValidateWrappedContactData(t *testing.T) {
	contactData := []byte("test contact data")
	certDER, validSig := makeTestCertAndSig(t, contactData)

	testCases := []struct {
		name          string
		sigs          []*fspb.Signature
		wantLen       int
		wantValid     bool
		wantAlgorithm x509.SignatureAlgorithm
		wantCertCount int
	}{
		{
			name:    "no_signatures",
			sigs:    nil,
			wantLen: 0,
		},
		{
			name: "empty_certificates_valid_algorithm",
			sigs: []*fspb.Signature{
				{
					Certificate: nil,
					Algorithm:   int32(x509.SHA256WithRSA),
					Signature:   []byte("unverified"),
				},
			},
			wantLen:       1,
			wantValid:     false,
			wantAlgorithm: x509.SHA256WithRSA,
			wantCertCount: 0,
		},
		{
			name: "empty_certificates_unknown_algorithm",
			sigs: []*fspb.Signature{
				{
					Certificate: [][]byte{},
					Algorithm:   9999,
					Signature:   []byte("unverified"),
				},
			},
			wantLen:       1,
			wantValid:     false,
			wantAlgorithm: x509.UnknownSignatureAlgorithm,
			wantCertCount: 0,
		},
		{
			name: "valid_signature",
			sigs: []*fspb.Signature{
				{
					Certificate: [][]byte{certDER},
					Algorithm:   int32(x509.ECDSAWithSHA256),
					Signature:   validSig,
				},
			},
			wantLen:       1,
			wantValid:     true,
			wantAlgorithm: x509.ECDSAWithSHA256,
			wantCertCount: 1,
		},
		{
			name: "invalid_signature_bytes",
			sigs: []*fspb.Signature{
				{
					Certificate: [][]byte{certDER},
					Algorithm:   int32(x509.ECDSAWithSHA256),
					Signature:   []byte("corrupted signature"),
				},
			},
			wantLen:       1,
			wantValid:     false,
			wantAlgorithm: x509.ECDSAWithSHA256,
			wantCertCount: 1,
		},
		{
			name: "unknown_algorithm_with_certificate",
			sigs: []*fspb.Signature{
				{
					Certificate: [][]byte{certDER},
					Algorithm:   -1,
					Signature:   validSig,
				},
			},
			wantLen:       1,
			wantValid:     false,
			wantAlgorithm: x509.UnknownSignatureAlgorithm,
			wantCertCount: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			wcd := &fspb.WrappedContactData{
				ContactData: contactData,
				Signatures:  tc.sigs,
			}
			got, err := ValidateWrappedContactData(common.ClientID{}, wcd)
			if err != nil {
				t.Fatalf("ValidateWrappedContactData() unexpected error: %v", err)
			}
			if len(got) != tc.wantLen {
				t.Fatalf("len(ValidateWrappedContactData()) = %d, want %d", len(got), tc.wantLen)
			}
			if tc.wantLen == 0 {
				return
			}
			if got[0].Valid != tc.wantValid {
				t.Errorf("SignatureInfo.Valid = %v, want %v", got[0].Valid, tc.wantValid)
			}
			if got[0].Algorithm != tc.wantAlgorithm {
				t.Errorf("SignatureInfo.Algorithm = %v, want %v", got[0].Algorithm, tc.wantAlgorithm)
			}
			if len(got[0].Certificate) != tc.wantCertCount {
				t.Errorf("len(SignatureInfo.Certificate) = %d, want %d", len(got[0].Certificate), tc.wantCertCount)
			}
		})
	}
}

func TestValidateWrappedContactData_InvalidCertificate(t *testing.T) {
	wcd := &fspb.WrappedContactData{
		ContactData: []byte("test contact data"),
		Signatures: []*fspb.Signature{
			{
				Certificate: [][]byte{[]byte("invalid DER bytes")},
				Algorithm:   int32(x509.ECDSAWithSHA256),
				Signature:   []byte("sig"),
			},
		},
	}
	got, err := ValidateWrappedContactData(common.ClientID{}, wcd)
	if err == nil {
		t.Errorf("ValidateWrappedContactData() = %v, nil; want non-nil error for malformed certificate", got)
	}
}
