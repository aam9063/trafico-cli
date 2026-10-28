package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDefaultContainsAllConsumableFeeds(t *testing.T) {
	want := map[string]Feed{
		"dgt-situations": {
			URL:     "https://nap.dgt.es/datex2/v3/dgt/SituationPublication/datex2_v37.xml",
			Enabled: true,
		},
		"dgt-signs": {
			URL:     "https://nap.dgt.es/datex2/v3/dgt/DevicePublication/vms_datex2_v37.xml",
			Enabled: false,
		},
		"dgt-signs-messages": {
			URL:     "https://nap.dgt.es/datex2/v3/dgt/VmsPublication/datex2_v37.xml",
			Enabled: false,
		},
		"dgt-radars": {
			URL:     "https://nap.dgt.es/datex2/dgt/PredefinedLocationsPublication/radares/content.xml",
			Enabled: false,
		},
		"dgt-sections-invive": {
			URL:     "https://nap.dgt.es/datex2/dgt/PredefinedLocationsPublication/tramos_invive/content.xml",
			Enabled: false,
		},
		"dgt-sections-animals": {
			URL:     "https://nap.dgt.es/datex2/dgt/PredefinedLocationsPublication/tefiva/content.xml",
			Enabled: false,
		},
	}

	got := Default()
	if len(got.Feeds) != len(want) {
		t.Fatalf("Default() has %d feeds, want %d: %v", len(got.Feeds), len(want), got.Feeds)
	}
	for name, wantFeed := range want {
		gotFeed, ok := got.Feeds[name]
		if !ok {
			t.Errorf("Default() is missing feed %q", name)
			continue
		}
		if gotFeed != wantFeed {
			t.Errorf("feed %q = %+v, want %+v", name, gotFeed, wantFeed)
		}
	}
}

func TestDefaultOnlySituationsEnabled(t *testing.T) {
	cfg := Default()
	for name, feed := range cfg.Feeds {
		if name == "dgt-situations" {
			continue
		}
		if feed.Enabled {
			t.Errorf("feed %q is enabled by default, want disabled", name)
		}
	}
	if !cfg.Feeds["dgt-situations"].Enabled {
		t.Error("feed dgt-situations is disabled by default, want enabled")
	}
}

func TestDefaultValues(t *testing.T) {
	cfg := Default()
	if time.Duration(cfg.Stale.MaxAge) != 10*time.Minute {
		t.Errorf("Stale.MaxAge = %s, want 10m", cfg.Stale.MaxAge)
	}
	if len(cfg.Places) != 0 {
		t.Errorf("Default() has places %v, want none", cfg.Places)
	}
	if len(cfg.Sinks) != 0 {
		t.Errorf("Default() has sinks %v, want none", cfg.Sinks)
	}
}

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing config file: %v", err)
	}
	return path
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.toml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(missing file) error = %v, want defaults", err)
	}
	if !reflect.DeepEqual(cfg, Default()) {
		t.Errorf("Load(missing file) = %+v, want %+v", cfg, Default())
	}
}

func TestLoadMergesPartialConfigOverDefaults(t *testing.T) {
	path := writeConfigFile(t, `
[feeds.dgt-signs]
url = "https://example.com/signs.xml"
enabled = true

[feeds.dgt-situations]
enabled = false

[places]
casa = "38.3452,-0.4810"

[stale]
max_age = "15m"

[sinks.phone]
type = "ntfy"
url = "https://ntfy.sh"
topic = "long-random-topic"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Overridden feed keeps its default URL and takes the new enabled flag.
	signs := cfg.Feeds["dgt-signs"]
	if signs.URL != "https://example.com/signs.xml" || !signs.Enabled {
		t.Errorf("dgt-signs = %+v, want overridden url and enabled", signs)
	}
	// A feed table with only `enabled` keeps the compiled URL.
	situations := cfg.Feeds["dgt-situations"]
	if situations.URL != "https://nap.dgt.es/datex2/v3/dgt/SituationPublication/datex2_v37.xml" {
		t.Errorf("dgt-situations url = %q, want the compiled default", situations.URL)
	}
	if situations.Enabled {
		t.Error("dgt-situations is still enabled, want the explicit false from the file")
	}
	// The other five feeds keep their defaults untouched.
	wantUnchanged := map[string]string{
		"dgt-signs-messages":   "https://nap.dgt.es/datex2/v3/dgt/VmsPublication/datex2_v37.xml",
		"dgt-radars":           "https://nap.dgt.es/datex2/dgt/PredefinedLocationsPublication/radares/content.xml",
		"dgt-sections-invive":  "https://nap.dgt.es/datex2/dgt/PredefinedLocationsPublication/tramos_invive/content.xml",
		"dgt-sections-animals": "https://nap.dgt.es/datex2/dgt/PredefinedLocationsPublication/tefiva/content.xml",
	}
	// dgt-signs was overridden, dgt-situations kept its URL but was disabled:
	// five of the six keep their default URLs verbatim.
	for name, url := range wantUnchanged {
		if got := cfg.Feeds[name]; got.URL != url {
			t.Errorf("feed %q url = %q, want default %q", name, got.URL, url)
		}
	}

	if cfg.Stale.MaxAge != Duration(15*time.Minute) {
		t.Errorf("Stale.MaxAge = %s, want 15m", cfg.Stale.MaxAge)
	}
	if cfg.Places["casa"] != "38.3452,-0.4810" {
		t.Errorf("places = %v, want casa kept", cfg.Places)
	}
	sink := cfg.Sinks["phone"]
	if sink.Type != "ntfy" || sink.Topic != "long-random-topic" {
		t.Errorf("sink phone = %+v, want the ntfy sink from the file", sink)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "unknown top-level key",
			content: `feadz = []`,
		},
		{
			name: "unknown key inside a feed",
			content: `
[feeds.dgt-signs]
urll = "https://example.com/signs.xml"
`,
		},
		{
			name: "unknown key inside stale",
			content: `
[stale]
max_agee = "10m"
`,
		},
		{
			name: "unknown key inside a sink",
			content: `
[sinks.phone]
type = "ntfy"
topic = "x"
topik = "x"
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfigFile(t, tt.content)
			cfg, err := Load(path)
			if err == nil {
				t.Fatalf("Load() = %+v, want an error for an unknown key", cfg)
			}
		})
	}
}

func TestValidateFailures(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(cfg *Config)
		wantErr string
	}{
		{
			name: "enabled feed with empty URL",
			mutate: func(cfg *Config) {
				cfg.Feeds["dgt-signs"] = Feed{URL: "", Enabled: true}
			},
			wantErr: `feed "dgt-signs" is enabled but has no URL`,
		},
		{
			name: "enabled feed with whitespace URL",
			mutate: func(cfg *Config) {
				cfg.Feeds["dgt-signs"] = Feed{URL: "   ", Enabled: true}
			},
			wantErr: `feed "dgt-signs" is enabled but has no URL`,
		},
		{
			name: "place with a single coordinate",
			mutate: func(cfg *Config) {
				cfg.Places["casa"] = "38.3452"
			},
			wantErr: `place "casa"`,
		},
		{
			name: "place with non-numeric latitude",
			mutate: func(cfg *Config) {
				cfg.Places["casa"] = "north,-0.4810"
			},
			wantErr: `place "casa"`,
		},
		{
			name: "place with latitude out of range",
			mutate: func(cfg *Config) {
				cfg.Places["casa"] = "95.0,-0.4810"
			},
			wantErr: `place "casa"`,
		},
		{
			name: "place with longitude out of range",
			mutate: func(cfg *Config) {
				cfg.Places["casa"] = "38.3452,-200.0"
			},
			wantErr: `place "casa"`,
		},
		{
			name: "zero max_age",
			mutate: func(cfg *Config) {
				cfg.Stale.MaxAge = 0
			},
			wantErr: "stale.max_age",
		},
		{
			name: "negative max_age",
			mutate: func(cfg *Config) {
				cfg.Stale.MaxAge = Duration(-time.Minute)
			},
			wantErr: "stale.max_age",
		},
		{
			name: "unknown sink type",
			mutate: func(cfg *Config) {
				cfg.Sinks["phone"] = Sink{Type: "carrier-pigeon"}
			},
			wantErr: `sink "phone"`,
		},
		{
			name: "empty sink type",
			mutate: func(cfg *Config) {
				cfg.Sinks["phone"] = Sink{}
			},
			wantErr: `sink "phone"`,
		},
		{
			name: "ntfy sink without topic",
			mutate: func(cfg *Config) {
				cfg.Sinks["phone"] = Sink{Type: "ntfy", URL: "https://ntfy.sh"}
			},
			wantErr: `sink "phone"`,
		},
		{
			name: "ntfy sink with blank topic",
			mutate: func(cfg *Config) {
				cfg.Sinks["phone"] = Sink{Type: "ntfy", URL: "https://ntfy.sh", Topic: "  "}
			},
			wantErr: `sink "phone"`,
		},
		{
			name: "disabled feed with empty URL is fine",
			mutate: func(cfg *Config) {
				cfg.Feeds["dgt-signs"] = Feed{URL: "", Enabled: false}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(cfg)

			err := cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want an error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	cfg := Default()
	cfg.Places["casa"] = "38.3452,-0.4810"
	cfg.Sinks["phone"] = Sink{Type: "ntfy", URL: "https://ntfy.sh", Topic: "long-random-topic"}
	cfg.Sinks["hook"] = Sink{Type: "webhook", URL: "https://example.com/hook", Template: "discord"}
	cfg.Sinks["run"] = Sink{Type: "exec", URL: "/usr/local/bin/notify"}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestLoadValidatesPlaces(t *testing.T) {
	path := writeConfigFile(t, `
[places]
casa = "38.3452"
`)
	if _, err := Load(path); err == nil {
		t.Fatal("Load() with a malformed place, want an error")
	}
}

func TestLoadRejectsWidePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not exist on Windows")
	}
	path := writeConfigFile(t, `
[places]
casa = "38.3452,-0.4810"
`)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() of a 0644 config file, want a loud error")
	}
	if !strings.Contains(err.Error(), "0600") {
		t.Errorf("Load() error = %q, want it to mention 0600", err.Error())
	}
}

func TestLoadAcceptsTightPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not exist on Windows")
	}
	path := writeConfigFile(t, `
[stale]
max_age = "10m"
`)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if _, err := Load(path); err != nil {
		t.Fatalf("Load() of a 0600 config file error = %v", err)
	}
}
