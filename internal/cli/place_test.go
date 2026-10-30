package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
)

// writePlaceConfig writes a config with one saved place and returns its
// path, so the tests can assert byte-level effects on disk. It relies on
// setCLIEnv having isolated XDG_CONFIG_HOME.
func writePlaceConfig(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("XDG_CONFIG_HOME")
	path := filepath.Join(dir, "trafico-cli", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the config directory: %v", err)
	}
	content := "[places]\ncasa = \"38.3452,-0.4810\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}
	return path
}

// expectEnvelope parses stderr as exactly one error envelope and returns
// the error object after checking its type and exit code.
func expectEnvelope(t *testing.T, stderr *bytes.Buffer, wantType string) map[string]any {
	t.Helper()
	if stderr.Len() == 0 {
		t.Fatal("stderr carries no error envelope")
	}
	var doc map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &doc); err != nil {
		t.Fatalf("stderr is not one JSON envelope: %v\nstderr: %q", err, stderr.String())
	}
	errObj, _ := doc["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("stderr carries no error object: %v", doc)
	}
	if errObj["type"] != wantType {
		t.Errorf("error.type = %v, want %q", errObj["type"], wantType)
	}
	wantCode := float64(errs.ExitUsage)
	if wantType == "not_found" {
		wantCode = float64(errs.ExitNotFound)
	}
	if errObj["exit_code"] != wantCode {
		t.Errorf("error.exit_code = %v, want %v", errObj["exit_code"], wantCode)
	}
	return errObj
}

// TestPlaceRemoveRequiresConfirmation pins ARRANQUE §6.3: `place remove`
// without --yes exits 2 with a confirmation_required envelope on stderr and
// an empty stdout, and the config file on disk stays byte-identical: a
// non-interactive invocation never deletes anything and never prompts.
func TestPlaceRemoveRequiresConfirmation(t *testing.T) {
	setCLIEnv(t)
	path := writePlaceConfig(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the config before the removal: %v", err)
	}

	code, stdout, stderr := runCLI(t, "place", "remove", "casa")
	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want %d (confirmation_required)", code, errs.ExitUsage)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty on a refused removal, got %q", stdout.String())
	}
	errObj := expectEnvelope(t, stderr, "confirmation_required")
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "--yes") {
		t.Errorf("error.message = %q, want it to name the --yes flag", msg)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the config after the refused removal: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the refused removal changed the config file on disk")
	}
}

// TestPlaceRemoveWithYesDeletes pins that --yes removes exactly the
// requested place and nothing else.
func TestPlaceRemoveWithYesDeletes(t *testing.T) {
	setCLIEnv(t)
	writePlaceConfig(t)

	code, stdout, stderr := runCLI(t, "place", "remove", "casa", "--yes")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	if places, _ := doc["places"].([]any); len(places) != 0 {
		t.Errorf("stdout places = %v, want an empty list", doc["places"])
	}
	reloaded, err := config.Load(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "trafico-cli", "config.toml"))
	if err != nil {
		t.Fatalf("reloading the config: %v", err)
	}
	if len(reloaded.Places) != 0 {
		t.Errorf("config places after removal = %v, want none", reloaded.Places)
	}
}

// TestPlaceAddPersists pins `place add`: the coordinates land in the config
// file in the "lat,lon" shape --near resolves, and the stdout envelope
// lists the saved place.
func TestPlaceAddPersists(t *testing.T) {
	setCLIEnv(t)
	writePlaceConfig(t)

	code, stdout, stderr := runCLI(t, "place", "add", "trabajo", "--lat", "38.34", "--lon", "-0.49")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	places, _ := doc["places"].([]any)
	if len(places) != 2 {
		t.Fatalf("stdout places = %v, want 2 entries", doc["places"])
	}

	reloaded, err := config.Load(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "trafico-cli", "config.toml"))
	if err != nil {
		t.Fatalf("reloading the config: %v", err)
	}
	if reloaded.Places["trabajo"] != "38.34,-0.49" {
		t.Errorf("saved place = %q, want %q", reloaded.Places["trabajo"], "38.34,-0.49")
	}
	if reloaded.Places["casa"] != "38.3452,-0.4810" {
		t.Errorf("place add lost the pre-existing place: %v", reloaded.Places)
	}
}

// TestPlaceAddRejectsInvalid pins exit 2 for missing coordinates, an
// out-of-range latitude and an unsafe name, with the file untouched.
func TestPlaceAddRejectsInvalid(t *testing.T) {
	setCLIEnv(t)
	path := writePlaceConfig(t)
	before, _ := os.ReadFile(path)

	tests := []struct {
		name string
		args []string
	}{
		{name: "missing coordinates", args: []string{"place", "add", "trabajo"}},
		{name: "latitude out of range", args: []string{"place", "add", "trabajo", "--lat", "95", "--lon", "-0.49"}},
		{name: "unsafe name", args: []string{"place", "add", "mi casa", "--lat", "38.34", "--lon", "-0.49"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, tt.args...)
			if code != errs.ExitUsage {
				t.Fatalf("exit code = %d, want 2 (usage)", code)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout must stay empty, got %q", stdout.String())
			}
			expectEnvelope(t, stderr, "usage")
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Error("a rejected place add changed the config file on disk")
			}
		})
	}
}

// TestPlaceListAndMissingRemove pins `place list` and exit 4 for a place
// that does not exist.
func TestPlaceListAndMissingRemove(t *testing.T) {
	setCLIEnv(t)
	writePlaceConfig(t)

	code, stdout, _ := runCLI(t, "place", "list")
	if code != errs.ExitSuccess {
		t.Fatalf("place list exit code = %d, want 0", code)
	}
	doc := decodeJSON(t, stdout)
	places, _ := doc["places"].([]any)
	if len(places) != 1 {
		t.Errorf("place list = %v, want one entry", doc["places"])
	}

	code, stdout, stderr := runCLI(t, "place", "remove", "no-existe", "--yes")
	if code != errs.ExitNotFound {
		t.Fatalf("missing place exit code = %d, want 4", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", stdout.String())
	}
	expectEnvelope(t, stderr, "not_found")
}
