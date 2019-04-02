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
