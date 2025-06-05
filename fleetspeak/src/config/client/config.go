package client

import (
	"fmt"
	"os"

	"google.golang.org/protobuf/encoding/prototext"

	gpb "github.com/google/fleetspeak/fleetspeak/src/client/generic/proto/fleetspeak_client_generic"
	cpb "github.com/google/fleetspeak/fleetspeak/src/config/proto/fleetspeak_config"
)

func WriteLinuxConfig(cfg *cpb.Config, trustedPEM []byte) error {
	if cfg.LinuxClientConfigurationFile == "" {
		return nil
	}
	out := &gpb.Config{
		TrustedCerts: string(trustedPEM),
		Server:       cfg.PublicHostPort,
		ServerName:   cfg.ServerName,
		ClientLabel:  []string{cfg.ComponentsConfig.RequiredLabel},
		PersistenceHandler: &gpb.Config_FilesystemHandler{
			FilesystemHandler: &gpb.FilesystemHandler{
				ConfigurationDirectory: "/etc/fleetspeak-client",
				StateFile:              "/var/lib/misc/fleetspeak-client.state",
			}},
		Streaming: !cfg.ComponentsConfig.HttpsConfig.DisableStreaming,
		Proxy: cfg.GetProxy(),
	}

	b, err := prototext.Marshal(out)
	if err != nil {
		return fmt.Errorf("failed to marshal linux client configuration: %v", err)
	}
	err = os.WriteFile(cfg.LinuxClientConfigurationFile, b, 0644)
	if err != nil {
		return fmt.Errorf("failed to write linux client configuration file [%s]: %v", cfg.LinuxClientConfigurationFile, err)
	}

	return nil
}

func WriteDarwinConfig(cfg *cpb.Config, trustedPEM []byte) error {
	if cfg.DarwinClientConfigurationFile == "" {
		return nil
	}
	out := &gpb.Config{
		TrustedCerts: string(trustedPEM),
		Server:       cfg.PublicHostPort,
		ServerName:   cfg.ServerName,
		ClientLabel:  []string{cfg.ComponentsConfig.RequiredLabel},
		PersistenceHandler: &gpb.Config_FilesystemHandler{
			FilesystemHandler: &gpb.FilesystemHandler{
				ConfigurationDirectory: "/etc/fleetspeak-client",
				StateFile:              "/etc/fleetspeak-client/state",
			}},
		Streaming: !cfg.ComponentsConfig.HttpsConfig.DisableStreaming,
		Proxy: cfg.GetProxy(),
	}

	b, err := prototext.Marshal(out)
	if err != nil {
		return fmt.Errorf("failed to marshal darwin client configuration: %v", err)
	}
	err = os.WriteFile(cfg.DarwinClientConfigurationFile, b, 0644)
	if err != nil {
		return fmt.Errorf("failed to write darwin client configuration file [%s]: %v", cfg.DarwinClientConfigurationFile, err)
	}

	return nil
}

func WriteWindowsConfig(cfg *cpb.Config, trustedPEM []byte) error {
	if cfg.WindowsClientConfigurationFile == "" {
		return nil
	}
	out := &gpb.Config{
		TrustedCerts: string(trustedPEM),
		Server:       cfg.PublicHostPort,
		ServerName:   cfg.ServerName,
		ClientLabel:  []string{cfg.ComponentsConfig.RequiredLabel},
		PersistenceHandler: &gpb.Config_RegistryHandler{
			RegistryHandler: &gpb.RegistryHandler{
				ConfigurationKey: `HKEY_LOCAL_MACHINE\SOFTWARE\FleetspeakClient`,
			}},
		Streaming: !cfg.ComponentsConfig.HttpsConfig.DisableStreaming,
		Proxy: cfg.GetProxy(),
	}

	b, err := prototext.Marshal(out)
	if err != nil {
		return fmt.Errorf("failed to marshal Windows client configuration: %v", err)
	}
	err = os.WriteFile(cfg.WindowsClientConfigurationFile, b, 0644)
	if err != nil {
		return fmt.Errorf("failed to write Windows client configuration file [%s]: %v", cfg.WindowsClientConfigurationFile, err)
	}

	return nil
}
