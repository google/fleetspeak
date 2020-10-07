// +build windows

package generic

import (
	"errors"
	"fmt"

	"github.com/google/fleetspeak/fleetspeak/src/client/config"

	gpb "github.com/google/fleetspeak/fleetspeak/src/client/generic/proto/fleetspeak_client_generic"
)

func makePersistenceHandler(cfg *gpb.Config) (config.PersistenceHandler, error) {
	if cfg.PersistenceHandler == nil {
		return nil, errors.New("persistence_handler is required")
	}
	switch h := cfg.PersistenceHandler.(type) {
	case *gpb.Config_FilesystemHandler:
		return config.NewFilesystemPersistenceHandler(h.FilesystemHandler.ConfigurationDirectory, h.FilesystemHandler.StateFile)
	case *gpb.Config_RegistryHandler:
		return config.NewWindowsRegistryPersistenceHandler(h.RegistryHandler.ConfigurationKey, false)
	default:
		return nil, fmt.Errorf("persistence_handler has unsupported type: %T", cfg.PersistenceHandler)
	}
}
