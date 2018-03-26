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
	"time"

	log "github.com/golang/glog"
	"golang.org/x/time/rate"

	"github.com/google/fleetspeak/fleetspeak/src/common"
	"github.com/google/fleetspeak/fleetspeak/src/server/authorizer"

	fspb "github.com/google/fleetspeak/fleetspeak/src/common/proto/fleetspeak"
)

var logLimiter = rate.NewLimiter(rate.Every(30*time.Second), 50)

// ValidateWrappedContactData examines the extra signatures included
// in WrappedContactData and attempts to validate them.
func ValidateWrappedContactData(id common.ClientID, cd *fspb.WrappedContactData) ([]authorizer.SignatureInfo, error) {
	if len(cd.Signatures) == 0 {
		return nil, nil
	}
	res := make([]authorizer.SignatureInfo, 0, len(cd.Signatures))

	for _, sig := range cd.Signatures {
		certs := make([]*x509.Certificate, len(sig.Certificate))
		var err error
		for i, c := range sig.Certificate {
			certs[i], err = x509.ParseCertificate(c)
			if err != nil {
				return nil, err
			}
		}

		alg := x509.UnknownSignatureAlgorithm
		if int(sig.Algorithm) > int(x509.UnknownSignatureAlgorithm) &&
			int(sig.Algorithm) <= int(x509.SHA512WithRSAPSS) {
			alg = x509.SignatureAlgorithm(sig.Algorithm)
		} else {
			if logLimiter.Allow() {
				log.Warningf("Client [%v] provided signature with unknown algorithm enum: %d", id, sig.Algorithm)
			}
		}

		var valid bool
		if alg != x509.UnknownSignatureAlgorithm {
			err := certs[0].CheckSignature(alg, cd.ContactData, sig.Signature)
			if err != nil && logLimiter.Allow() {
				log.Warningf("Client [%v] provided signature that could not be validated: %v", id, err)
			}
			valid = err == nil
		}

		res = append(res, authorizer.SignatureInfo{
			Certificate: certs,
			Algorithm:   alg,
			Valid:       valid,
		})
	}
	return res, nil
}
