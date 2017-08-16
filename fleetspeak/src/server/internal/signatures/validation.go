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

// Package signatures implements utility methods for checking
// cryptographic signatures.
package signatures

import (
	"crypto/x509"

	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

// ValidateWrappedContactData examines the extra signatures included
// in WrappedContactData and attempts to validate them.
func ValidateWrappedContactData(cd *fspb.WrappedContactData) ([]authorizer.SignatureInfo, error) {
	if len(cd.Signatures) == 0 {
		return nil, nil
	}
	res := make([]authorizer.SignatureInfo, 0, len(cd.Signatures))

	for _, sig := range cd.Signatures {
		cert, err := x509.ParseCertificate(sig.Certificate)
		if err != nil {
			return nil, err
		}

		alg := x509.UnknownSignatureAlgorithm
		if int(sig.Algorithm) > int(x509.UnknownSignatureAlgorithm) &&
			int(sig.Algorithm) <= int(x509.SHA512WithRSAPSS) {
			alg = x509.SignatureAlgorithm(sig.Algorithm)
		}

		var valid bool
		if alg != x509.UnknownSignatureAlgorithm {
			err = cert.CheckSignature(alg, cd.ContactData, sig.Signature)
			valid = err == nil
		}

		res = append(res, authorizer.SignatureInfo{
			Certificate: cert,
			Algorithm:   alg,
			Valid:       valid,
		})
	}
	return res, nil
}
