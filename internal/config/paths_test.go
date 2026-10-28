package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testLookup(env map[string]string) EnvLookup {
	return func(key string) string { return env[key] }
}

// baseEnv provides the fallback variables of every platform so that the cases
// below only need to set the XDG variable under test.
func baseEnv() map[string]string {
	return map[string]string{"AppData": `C:\AppData`, "LocalAppData": `C:\LocalAppData`}
}

// wrapBase merges extra environment entries over windowsBaseEnv so the
// fallback of either platform resolves without touching the real environment.
func wrapBase(extra map[string]string) map[string]string {
	env := baseEnv()
	for k, v := range extra {
		env[k] = v
	}
	return env
}

func TestResolvePathsHonoursXDGVariables(t *testing.T) {
	configBase := filepath.Join(t.TempDir(), "config")
	dataBase := filepath.Join(t.TempDir(), "data")
	cacheBase := filepath.Join(t.TempDir(), "cache")
	tests := []struct {
		name string
		env  map[string]string
		want Paths
	}{
		{
			name: "config",
			env:  wrapBase(map[string]string{"XDG_CONFIG_HOME": configBase}),
			want: Paths{ConfigPath: filepath.Join(configBase, "trafico-cli", "config.toml")},
		},
		{
			name: "data",
			env:  wrapBase(map[string]string{"XDG_DATA_HOME": dataBase}),
			want: Paths{DataPath: filepath.Join(dataBase, "trafico-cli", "trafico.db")},
		},
		{
			name: "cache",
			env:  wrapBase(map[string]string{"XDG_CACHE_HOME": cacheBase}),
			want: Paths{CacheDir: filepath.Join(cacheBase, "trafico-cli")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePaths(testLookup(tt.env))
			if err != nil {
				t.Fatalf("ResolvePaths() error = %v", err)
			}
			if tt.want.ConfigPath != "" && got.ConfigPath != tt.want.ConfigPath {
				t.Errorf("ConfigPath = %q, want %q", got.ConfigPath, tt.want.ConfigPath)
			}
			if tt.want.DataPath != "" && got.DataPath != tt.want.DataPath {
				t.Errorf("DataPath = %q, want %q", got.DataPath, tt.want.DataPath)
			}
			if tt.want.CacheDir != "" && got.CacheDir != tt.want.CacheDir {
				t.Errorf("CacheDir = %q, want %q", got.CacheDir, tt.want.CacheDir)
			}
		})
	}
}

func TestResolvePathsIgnoresRelativeXDGValues(t *testing.T) {
	// The XDG base directory specification says relative values must be ignored.
	home := t.TempDir()
	env := baseEnv()
	env["HOME"] = home
	env["XDG_CONFIG_HOME"] = "relative/path"
	env["XDG_DATA_HOME"] = "relative/path"
	env["XDG_CACHE_HOME"] = "relative/path"
	lookup := testLookup(env)
	got, err := ResolvePaths(lookup)
	if err != nil {
		t.Fatalf("ResolvePaths() error = %v", err)
	}
	for _, path := range []string{got.ConfigPath, got.DataPath, got.CacheDir} {
		if strings.Contains(path, "relative") {
			t.Errorf("path %q uses the relative XDG value, want it ignored", path)
		}
	}
	if runtime.GOOS != "windows" && !strings.HasPrefix(got.ConfigPath, home) {
		t.Errorf("ConfigPath = %q, want the %q fallback", got.ConfigPath, home)
	}
}

func TestResolvePathsFallsBackWhenXDGUnset(t *testing.T) {
	home := t.TempDir()
	env := baseEnv()
	env["HOME"] = home
	lookup := testLookup(env)

	got, err := ResolvePaths(lookup)
	if err != nil {
		t.Fatalf("ResolvePaths() error = %v", err)
	}

	if runtime.GOOS == "windows" {
		// On Windows the fallback is %AppData% / %LocalAppData% (the same
		// locations os.UserConfigDir and os.UserCacheDir resolve to); HOME
		// is irrelevant there. Covered by TestResolvePathsWindowsFallback.
		return
	}

	wantConfig := filepath.Join(home, ".config", "trafico-cli", "config.toml")
	wantData := filepath.Join(home, ".local", "share", "trafico-cli", "trafico.db")
	wantCache := filepath.Join(home, ".cache", "trafico-cli")
	if got.ConfigPath != wantConfig {
		t.Errorf("ConfigPath = %q, want %q", got.ConfigPath, wantConfig)
	}
	if got.DataPath != wantData {
		t.Errorf("DataPath = %q, want %q", got.DataPath, wantData)
	}
	if got.CacheDir != wantCache {
		t.Errorf("CacheDir = %q, want %q", got.CacheDir, wantCache)
	}
}

func TestResolvePathsWindowsFallback(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows fallback only applies on Windows")
	}
	appData := t.TempDir()
	localAppData := t.TempDir()
	lookup := testLookup(map[string]string{
		"AppData":       appData,
		"LocalAppData":  localAppData,
		"XDG_DATA_HOME": "", // never invented on Windows
	})

	got, err := ResolvePaths(lookup)
	if err != nil {
		t.Fatalf("ResolvePaths() error = %v", err)
	}

	wantConfig := filepath.Join(appData, "trafico-cli", "config.toml")
	wantData := filepath.Join(appData, "trafico-cli", "trafico.db")
	wantCache := filepath.Join(localAppData, "trafico-cli")
	if got.ConfigPath != wantConfig {
		t.Errorf("ConfigPath = %q, want %q", got.ConfigPath, wantConfig)
	}
	if got.DataPath != wantData {
		t.Errorf("DataPath = %q, want %q", got.DataPath, wantData)
	}
	if got.CacheDir != wantCache {
		t.Errorf("CacheDir = %q, want %q", got.CacheDir, wantCache)
	}
}

func TestResolvePathsFailsWithoutAnyBase(t *testing.T) {
	// No XDG variable and no HOME (or AppData on Windows) must be a loud error,
	// never a path relative to the working directory.
	lookup := testLookup(map[string]string{
		"HOME":         "",
		"AppData":      "",
		"LocalAppData": "",
	})
	if _, err := ResolvePaths(lookup); err == nil {
		t.Fatal("ResolvePaths() with no resolvable base directory, want error")
	}
}

func TestEnsureDirsCreatesDirectories(t *testing.T) {
	base := t.TempDir()
	lookup := testLookup(map[string]string{
		"XDG_CONFIG_HOME": filepath.Join(base, "config"),
		"XDG_DATA_HOME":   filepath.Join(base, "data"),
		"XDG_CACHE_HOME":  filepath.Join(base, "cache"),
	})
	paths, err := ResolvePaths(lookup)
	if err != nil {
		t.Fatalf("ResolvePaths() error = %v", err)
	}

	if err := paths.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}

	dirs := []string{
		filepath.Dir(paths.ConfigPath),
		filepath.Dir(paths.DataPath),
		paths.CacheDir,
	}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("directory %s was not created: %v", dir, err)
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
			t.Errorf("directory %s has mode %04o, want 0700", dir, info.Mode().Perm())
		}
	}
}
