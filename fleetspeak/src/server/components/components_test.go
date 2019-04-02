// Copyright 2019 Google Inc.
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

package components

import (
	"testing"

	cpb "github.com/google/fleetspeak/fleetspeak/src/server/components/proto/fleetspeak_components"
)

func TestValidation(t *testing.T) {
	for _, tc := range []struct {
		cfg     cpb.Config
		wantErr string
	}{
		{
			cfg:     cpb.Config{},
			wantErr: "mysql_data_source_name is required",
		},
		{
			cfg: cpb.Config{
				MysqlDataSourceName: "fs:seecret@tcp(localhost:1234)/fsdb",
			},
			wantErr: "https_config is required",
		},
		{
			cfg: cpb.Config{
				MysqlDataSourceName: "fs:seecret@tcp(localhost:1234)/fsdb",
				HttpsConfig: &cpb.HttpsConfig{
					ListenAddress: "localhost:443",
				},
			},
			wantErr: "https_config requires listen_address, certificates and key",
		},
	} {
		_, err := MakeComponents(tc.cfg)
		if err == nil || err.Error() != tc.wantErr {
			t.Errorf("Expected error message [%v], got [%v]", tc.wantErr, err)
		}
	}
}
