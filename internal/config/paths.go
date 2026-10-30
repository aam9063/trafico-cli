package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// EnvLookup returns the value of an environment variable, or the empty string
// when the variable is unset. It exists so tests can drive path resolution
// with an injected table instead of the real process environment.
type EnvLookup func(key string) string

// OSEnv is the default EnvLookup backed by the process environment.
func OSEnv(key string) string { return os.Getenv(key) }

// Paths holds the local storage locations of trafico-cli, per docs/ARRANQUE.md §7:
//
//	config: $XDG_CONFIG_HOME/trafico-cli/config.toml
//	data:   $XDG_DATA_HOME/trafico-cli/trafico.db
//	cache:  $XDG_CACHE_HOME/trafico-cli/
type Paths struct {
	// ConfigPath is the path of the TOML configuration file.
	ConfigPath string
	// DataPath is the path of the local database (watch state, events).
	DataPath string
	// CacheDir is the directory that holds downloaded feed snapshots.
	CacheDir string
}

// ResolvePaths resolves the storage locations honouring the XDG base directory
// variables. Relative XDG values are ignored, as the XDG specification demands.
//
// Fallbacks when a variable is unset or relative:
//
//   - On Windows, %AppData% for config and data and %LocalAppData% for cache,
//     the same locations os.UserConfigDir and os.UserCacheDir resolve to.
//     XDG-style dot directories under the home directory are never invented there.
//   - Otherwise $HOME with the conventional suffixes .config, .local/share and
//     .cache.
//
// ResolvePaths never touches the filesystem; use EnsureDirs to create the
// directories.
func ResolvePaths(lookup EnvLookup) (Paths, error) {
	if lookup == nil {
		lookup = OSEnv
	}

	var (
		configBase, dataBase, cacheBase string
		err                             error
	)
	switch runtime.GOOS {
	case "windows":
		if configBase, err = windowsBase(lookup, "XDG_CONFIG_HOME", "AppData"); err != nil {
			return Paths{}, err
		}
		if dataBase, err = windowsBase(lookup, "XDG_DATA_HOME", "AppData"); err != nil {
			return Paths{}, err
		}
		if cacheBase, err = windowsBase(lookup, "XDG_CACHE_HOME", "LocalAppData"); err != nil {
			return Paths{}, err
		}
	default:
		if configBase, err = unixBase(lookup, "XDG_CONFIG_HOME", ".config"); err != nil {
			return Paths{}, err
		}
		if dataBase, err = unixBase(lookup, "XDG_DATA_HOME", filepath.Join(".local", "share")); err != nil {
			return Paths{}, err
		}
		if cacheBase, err = unixBase(lookup, "XDG_CACHE_HOME", ".cache"); err != nil {
			return Paths{}, err
		}
	}

	return Paths{
		ConfigPath: filepath.Join(configBase, "trafico-cli", "config.toml"),
		DataPath:   filepath.Join(dataBase, "trafico-cli", "trafico.db"),
		CacheDir:   filepath.Join(cacheBase, "trafico-cli"),
	}, nil
}

// unixBase resolves one base directory on Unix-like systems: the XDG variable
// when it holds an absolute path, otherwise the home directory plus suffix.
func unixBase(lookup EnvLookup, xdgVar, suffix string) (string, error) {
	if base := absolute(lookup(xdgVar)); base != "" {
		return base, nil
	}
	home := lookup("HOME")
	if home == "" {
		return "", fmt.Errorf("config: cannot determine the base directory: %s is unset and HOME is not set", xdgVar)
	}
	return filepath.Join(home, suffix), nil
}

// windowsBase resolves one base directory on Windows: the XDG variable when it
// holds an absolute path, otherwise the given well-known variable (%AppData% or
// %LocalAppData%), which is where os.UserConfigDir and os.UserCacheDir point.
func windowsBase(lookup EnvLookup, xdgVar, shellVar string) (string, error) {
	if base := absolute(lookup(xdgVar)); base != "" {
		return base, nil
	}
	base := lookup(shellVar)
	if base == "" {
		return "", fmt.Errorf("config: cannot determine the base directory: %s is unset and %%%s%% is not set", xdgVar, shellVar)
	}
	return base, nil
}

// absolute returns dir only when it is a non-empty absolute path; relative XDG
// values must be ignored per the XDG base directory specification.
func absolute(dir string) string {
	if dir == "" || !filepath.IsAbs(dir) {
		return ""
	}
	return dir
}

// EnsureDirs creates the directories behind every path (config, data and
// cache) with permission 0700, because they hold personal locations. It is
// idempotent and does nothing when the directories already exist.
func (p Paths) EnsureDirs() error {
	dirs := []string{
		filepath.Dir(p.ConfigPath),
		filepath.Dir(p.DataPath),
		p.CacheDir,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("config: cannot create directory %s: %w", dir, err)
		}
	}
	return nil
}
