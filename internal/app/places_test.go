package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
)

// newPlaceStore returns an empty default config and the temp file path its
// mutations must persist to. No test ever writes the real user config
// (AGENTS.md): every store lives in t.TempDir().
func newPlaceStore(t *testing.T) (*config.Config, string) {
	t.Helper()
	return config.Default(), filepath.Join(t.TempDir(), "config.toml")
}

// TestAddPlacePersistsAndLists pins the full place round trip: the saved
// coordinates survive a config reload and Places lists them parsed and in
// name order.
func TestAddPlacePersistsAndLists(t *testing.T) {
	cfg, path := newPlaceStore(t)

	if err := AddPlace(cfg, path, "trabajo", 38.34, -0.49); err != nil {
		t.Fatalf("AddPlace(trabajo) error = %v", err)
	}
	if err := AddPlace(cfg, path, "casa", 38.3452, -0.4810); err != nil {
		t.Fatalf("AddPlace(casa) error = %v", err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load after AddPlace error = %v", err)
	}
	places, err := Places(reloaded)
	if err != nil {
		t.Fatalf("Places() error = %v", err)
	}
	if len(places) != 2 {
		t.Fatalf("Places() returned %d places, want 2: %+v", len(places), places)
	}
	// Name order, not insertion order.
	if places[0].Name != "casa" || places[1].Name != "trabajo" {
		t.Errorf("Places() order = %q, %q; want casa first (name order)", places[0].Name, places[1].Name)
	}
	if places[1].Lat != 38.34 || places[1].Lon != -0.49 {
		t.Errorf("place trabajo = (%g, %g), want (38.34, -0.49)", places[1].Lat, places[1].Lon)
	}
}

// TestAddPlaceRejectsInvalid pins the usage errors (exit 2) of place add:
// an unsafe name and out-of-range coordinates are rejected before anything
// is persisted, so the file on disk stays exactly as it was.
func TestAddPlaceRejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		add  func(cfg *config.Config, path string) error
	}{
		{
			name: "name with a space",
			add:  func(cfg *config.Config, path string) error { return AddPlace(cfg, path, "mi casa", 38.0, -0.5) },
		},
		{
			name: "empty name",
			add:  func(cfg *config.Config, path string) error { return AddPlace(cfg, path, "", 38.0, -0.5) },
		},
		{
			name: "latitude out of range",
			add:  func(cfg *config.Config, path string) error { return AddPlace(cfg, path, "casa", 95.0, -0.5) },
		},
		{
			name: "longitude out of range",
			add:  func(cfg *config.Config, path string) error { return AddPlace(cfg, path, "casa", 38.0, -200.0) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, path := newPlaceStore(t)
			err := tt.add(cfg, path)
			var target *errs.Error
			if !errors.As(err, &target) || target.ExitCode != errs.ExitUsage {
				t.Fatalf("AddPlace error = %v, want a usage error (exit 2)", err)
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Errorf("a rejected place add touched the config file (stat error = %v), want no file at all", statErr)
			}
		})
	}
}

// TestRemovePlaceMissingIsNotFound pins exit 4 for a place that does not
// exist, and that nothing is written when the removal refuses.
func TestRemovePlaceMissingIsNotFound(t *testing.T) {
	cfg, path := newPlaceStore(t)

	err := RemovePlace(cfg, path, "casa")
	var target *errs.Error
	if !errors.As(err, &target) || target.ExitCode != errs.ExitNotFound {
		t.Fatalf("RemovePlace(missing) error = %v, want not_found (exit 4)", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("a refused removal wrote the config file (stat error = %v)", statErr)
	}
}

// TestRemovePlacePersists pins that the removal deletes exactly the
// requested place and the deletion survives a reload.
func TestRemovePlacePersists(t *testing.T) {
	cfg, path := newPlaceStore(t)
	if err := AddPlace(cfg, path, "casa", 38.3452, -0.4810); err != nil {
		t.Fatalf("AddPlace(casa) error = %v", err)
	}
	if err := AddPlace(cfg, path, "trabajo", 38.34, -0.49); err != nil {
		t.Fatalf("AddPlace(trabajo) error = %v", err)
	}

	if err := RemovePlace(cfg, path, "casa"); err != nil {
		t.Fatalf("RemovePlace(casa) error = %v", err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load after RemovePlace error = %v", err)
	}
	places, err := Places(reloaded)
	if err != nil {
		t.Fatalf("Places() error = %v", err)
	}
	if len(places) != 1 || places[0].Name != "trabajo" {
		t.Fatalf("Places() after removal = %+v, want only trabajo", places)
	}
}

// TestPlacesRejectsNilConfig pins the internal error of a nil config.
func TestPlacesRejectsNilConfig(t *testing.T) {
	if _, err := Places(nil); err == nil {
		t.Fatal("Places(nil) = nil error, want an internal error")
	}
	if err := AddPlace(nil, "whatever", "casa", 0, 0); err == nil {
		t.Fatal("AddPlace(nil, ...) = nil error, want an internal error")
	}
}
