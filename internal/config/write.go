package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// Save writes cfg to path atomically: it serialises to a temporary file in the
// same directory, fsyncs, closes, sets 0600 and renames over the target. A
// crash mid-write therefore leaves either the old file or the new one, never
// half of either, and no temporary file is ever left behind.
func Save(path string, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config: cannot save a nil config to %s", path)
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: cannot serialise config for %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	// The directory holds personal locations, so it must be private (P-07).
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: cannot create directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("config: cannot create temporary file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	// Best effort cleanup: after a successful rename this file no longer
	// exists and the removal fails silently.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("config: cannot write temporary file for %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("config: cannot flush temporary file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: cannot close temporary file for %s: %w", path, err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("config: cannot set permissions on the temporary file for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("config: cannot replace %s atomically: %w", path, err)
	}
	return nil
}
