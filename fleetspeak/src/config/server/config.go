package server

import (
	"errors"
	"fmt"
	"os"

	"github.com/golang/protobuf/proto"
	cpb "github.com/google/fleetspeak/fleetspeak/src/config/proto/fleetspeak_config"
)

// WriteConfig validates and then writes a server component configuration.
func WriteConfig(cfg cpb.Config, certPEM, keyPEM []byte) error {
	if cfg.ServerComponentConfigurationFile == "" {
		return errors.New("server_component_configuration_file is required")
	}

	cc := cfg.ComponentsConfig
	if cc == nil {
		return errors.New("components_config is required")
	}
	if cc.HttpsConfig == nil {
		return errors.New("component_config.https_config is required")
	}
	if cc.HttpsConfig.ListenAddress == "" {
		return errors.New("component_config.https_config.listen_address is required")
	}
	cc.HttpsConfig.Certificates = string(certPEM)
	cc.HttpsConfig.Key = string(keyPEM)

	f, err := os.OpenFile(cfg.ServerComponentConfigurationFile, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("unable to open server component configuration file for writing: %v", err)
	}
	if err := proto.MarshalText(f, cc); err != nil {
		return fmt.Errorf("failed to write server component configuration file [%s]: %v", cfg.ServerComponentConfigurationFile, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to write server component configuration file [%s]: %v", cfg.ServerComponentConfigurationFile, err)
	}
	return nil
}
