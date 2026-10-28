package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSaveWritesConfigAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := Default()

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading saved config: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"[feeds.dgt-situations]",
		`url = 'https://nap.dgt.es/datex2/v3/dgt/SituationPublication/datex2_v37.xml'`,
		"enabled = true",
		"[feeds.dgt-signs]",
		"[stale]",
		`max_age = '10m'`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("saved config does not contain %q;\n%s", want, content)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading directory: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory contains %v, want only config.toml (no temp file left behind)", names)
	}
}

func TestSaveFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not exist on Windows")
	}
	path := filepath.Join(t.TempDir(), "config.toml")

	if err := Save(path, Default()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("saved file mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestSaveCreatesMissingDirectories(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "nested", "deeper", "config.toml")

	if err := Save(path, Default()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved file missing: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Errorf("created directory mode = %04o, want 0700", info.Mode().Perm())
		}
	}
}

func TestSaveOverwritesExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	first := Default()
	first.Stale.MaxAge = Duration(5 * time.Minute)
	if err := Save(path, first); err != nil {
		t.Fatalf("first Save() error = %v", err)
	}

	second := Default()
	second.Stale.MaxAge = Duration(20 * time.Minute)
	second.Places["casa"] = "38.3452,-0.4810"
	if err := Save(path, second); err != nil {
		t.Fatalf("second Save() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Stale.MaxAge != Duration(20*time.Minute) {
		t.Errorf("Stale.MaxAge = %s, want 20m from the second save", got.Stale.MaxAge)
	}
	if got.Places["casa"] != "38.3452,-0.4810" {
		t.Errorf("places = %v, want the second save's place", got.Places)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading directory: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("directory has %d entries, want only config.toml", len(entries))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	want := Default()
	want.Places["casa"] = "38.3452,-0.4810"
	want.Places["trabajo"] = "38.9100,-0.1800"
	want.Sinks["phone"] = Sink{Type: "ntfy", URL: "https://ntfy.sh", Topic: "long-random-topic"}
	want.Sinks["hook"] = Sink{Type: "webhook", URL: "https://example.com/hook", Template: "slack"}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(got.Feeds) != len(want.Feeds) {
		t.Fatalf("round trip has %d feeds, want %d", len(got.Feeds), len(want.Feeds))
	}
	for name, wantFeed := range want.Feeds {
		if gotFeed := got.Feeds[name]; gotFeed != wantFeed {
			t.Errorf("feed %q round trip = %+v, want %+v", name, gotFeed, wantFeed)
		}
	}
	for name, wantPlace := range want.Places {
		if gotPlace := got.Places[name]; gotPlace != wantPlace {
			t.Errorf("place %q round trip = %q, want %q", name, gotPlace, wantPlace)
		}
	}
	if got.Stale.MaxAge != want.Stale.MaxAge {
		t.Errorf("Stale.MaxAge round trip = %s, want %s", got.Stale.MaxAge, want.Stale.MaxAge)
	}
	for name, wantSink := range want.Sinks {
		if gotSink := got.Sinks[name]; gotSink != wantSink {
			t.Errorf("sink %q round trip = %+v, want %+v", name, gotSink, wantSink)
		}
	}
}
