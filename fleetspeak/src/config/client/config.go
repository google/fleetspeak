package client

import (
	"fmt"
	"os"

	"github.com/golang/protobuf/proto"

	gpb "github.com/google/fleetspeak/fleetspeak/src/client/generic/proto/fleetspeak_client_generic"
	cpb "github.com/google/fleetspeak/fleetspeak/src/config/proto/fleetspeak_config"
)

func WriteLinuxConfig(cfg cpb.Config, trustedPEM []byte) error {
	if cfg.LinuxClientConfigurationFile == "" {
		return nil
	}
	out := gpb.Config{
		TrustedCerts: string(trustedPEM),
		Server:       cfg.PublicHostPort,
		ClientLabel:  []string{cfg.ComponentsConfig.RequiredLabel},
		PersistenceHandler: &gpb.Config_FilesystemHandler{
			FilesystemHandler: &gpb.FilesystemHandler{
				ConfigurationDirectory: "/etc/fleetspeak-client",
				StateFile:              "/var/lib/misc/fleetspeak-client.state",
			}},
		Streaming: !cfg.ComponentsConfig.HttpsConfig.DisableStreaming,
	}

	f, err := os.OpenFile(cfg.LinuxClientConfigurationFile, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("unable to open server linux client configuration file [%s] for writing: %v", cfg.LinuxClientConfigurationFile, err)
	}
	if err := proto.MarshalText(f, &out); err != nil {
		return fmt.Errorf("failed to write linux client configuration file [%s]: %v", cfg.LinuxClientConfigurationFile, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to write linux client configuration file [%s]: %v", cfg.LinuxClientConfigurationFile, err)
	}

	return nil
}
