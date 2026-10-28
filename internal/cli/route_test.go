package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
)

// writeRouteConfig writes a config with the test-server feeds and one saved
// two-segment route (the roads and pk ranges of the real snap-1 snapshot),
// and returns the config path.
func writeRouteConfig(t *testing.T, srvURL string) string {
	t.Helper()
	dir := os.Getenv("XDG_CONFIG_HOME")
	path := filepath.Join(dir, "trafico-cli", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the config directory: %v", err)
	}
	content := fmt.Sprintf(`[feeds.dgt-situations]
url = "%s/situations"
enabled = true

[routes.trabajo]
direction = "both"
segments = ["A-372:40-55", "CT-34:0-10"]
`, srvURL)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}
	return path
}

// TestRouteCheckFetchesOnceAndGroups pins the two ROUTE-01 constraints at
// the CLI boundary: a two-segment route check makes exactly ONE request to
// the feed (never one per segment), and the segments come back in
// declaration order (docs/ARRANQUE.md §5.4) with the summary block.
func TestRouteCheckFetchesOnceAndGroups(t *testing.T) {
	setCLIEnv(t)
	srv, hits := newCLIServer(t)
	writeRouteConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "route", "check", "trabajo")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if got := hits("/situations"); got != 1 {
		t.Errorf("route check made %d requests for a 2-segment route, want 1", got)
	}

	doc := decodeJSON(t, stdout)
	if doc["route"] != "trabajo" {
		t.Errorf("route = %v, want trabajo", doc["route"])
	}
	segments, _ := doc["segments"].([]any)
	if len(segments) != 2 {
		t.Fatalf("segments = %v, want 2 entries", doc["segments"])
	}
	first, _ := segments[0].(map[string]any)
	second, _ := segments[1].(map[string]any)
	if first["road"] != "A-372" {
		t.Errorf("segment 0 = %v, want A-372 (declaration order)", first["road"])
	}
	if second["road"] != "CT-34" {
		t.Errorf("segment 1 = %v, want CT-34 (declaration order)", second["road"])
	}
	if incidents, _ := first["incidents"].([]any); len(incidents) != 3 {
		t.Errorf("A-372 incidents = %v, want 3 (the snapshot publishes 3 on A-372 in that pk range)", incidents)
	}
	summary, _ := doc["summary"].(map[string]any)
	if summary == nil {
		t.Fatal("the response carries no summary block")
	}
	if summary["incidents"] != float64(5) {
		t.Errorf("summary.incidents = %v, want 5", summary["incidents"])
	}
	if _, ok := summary["high_severity"]; !ok {
		t.Error("summary carries no high_severity key")
	}
	if _, ok := summary["closures"]; !ok {
		t.Error("summary carries no closures key")
	}
	// Every response keeps the attribution contract (P-05).
	if _, ok := doc["attribution"]; !ok {
		t.Error("the response carries no attribution block")
	}
}

// TestRouteCheckMissingRoute pins exit 4 with a not_found envelope and an
// empty stdout.
func TestRouteCheckMissingRoute(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newCLIServer(t)
	writeRouteConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "route", "check", "desconocida")
	if code != errs.ExitNotFound {
		t.Fatalf("exit code = %d, want 4 (not_found)", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty, got %q", stdout.String())
	}
	expectEnvelope(t, stderr, "not_found")
}

// TestRouteRemoveRequiresConfirmation pins that `route remove` without
// --yes exits 2 with a confirmation_required envelope, names the flag, and
// leaves the file byte-identical.
func TestRouteRemoveRequiresConfirmation(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newCLIServer(t)
	path := writeRouteConfig(t, srv.URL)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the config before the removal: %v", err)
	}

	code, stdout, stderr := runCLI(t, "route", "remove", "trabajo")
	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want 2 (confirmation_required)", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty, got %q", stdout.String())
	}
	errObj := expectEnvelope(t, stderr, "confirmation_required")
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "--yes") {
		t.Errorf("error.message = %q, want it to name the --yes flag", msg)
	}

	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("the refused removal changed the config file on disk")
	}
}

// TestRouteRemoveWithYesDeletes pins that --yes removes the route.
func TestRouteRemoveWithYesDeletes(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newCLIServer(t)
	writeRouteConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "route", "remove", "trabajo", "--yes")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	if routes, _ := doc["routes"].([]any); len(routes) != 0 {
		t.Errorf("stdout routes = %v, want an empty list", doc["routes"])
	}
	reloaded, err := config.Load(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "trafico-cli", "config.toml"))
	if err != nil {
		t.Fatalf("reloading the config: %v", err)
	}
	if len(reloaded.Routes) != 0 {
		t.Errorf("config routes after removal = %v, want none", reloaded.Routes)
	}
}

// TestRouteAddPersists pins `route add`: the segments land in the config in
// the declaration order and the stdout envelope lists the parsed route.
func TestRouteAddPersists(t *testing.T) {
	setCLIEnv(t)
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "trafico-cli", "config.toml")

	code, stdout, stderr := runCLI(t, "route", "add", "trabajo",
		"--segment", "a7:100-140", "--segment", "N-332:50-70", "--direction", "positive")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	routes, _ := doc["routes"].([]any)
	if len(routes) != 1 {
		t.Fatalf("stdout routes = %v, want 1 entry", doc["routes"])
	}

	// Assert the declaration order through the parsed config, not raw
	// bytes: go-toml serialises with its own quoting style.
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("reloading the config: %v", err)
	}
	route, ok := reloaded.Routes["trabajo"]
	if !ok {
		t.Fatalf("the config carries no trabajo route: %+v", reloaded.Routes)
	}
	if route.Direction != "positive" {
		t.Errorf("saved direction = %q, want positive", route.Direction)
	}
	if want := []string{"a7:100-140", "N-332:50-70"}; len(route.Segments) != 2 || route.Segments[0] != want[0] || route.Segments[1] != want[1] {
		t.Errorf("saved segments = %v, want %v (declaration order)", route.Segments, want)
	}
}

// TestRouteAddRejectsInvalid pins exit 2 for a route add with no segments,
// a malformed segment and an unknown direction, with the file untouched.
func TestRouteAddRejectsInvalid(t *testing.T) {
	setCLIEnv(t)
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "trafico-cli", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the config directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("[places]\n"), 0o600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}
	before, _ := os.ReadFile(path)

	tests := []struct {
		name string
		args []string
	}{
		{name: "no segments", args: []string{"route", "add", "trabajo"}},
		{name: "malformed segment", args: []string{"route", "add", "trabajo", "--segment", "A-7"}},
		{name: "inverted range", args: []string{"route", "add", "trabajo", "--segment", "A-7:140-100"}},
		{name: "unknown direction", args: []string{"route", "add", "trabajo", "--segment", "A-7:100-140", "--direction", "north"}},
		{name: "unsafe name", args: []string{"route", "add", "mi ruta", "--segment", "A-7:100-140"}},
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
				t.Error("a rejected route add changed the config file on disk")
			}
		})
	}
}

// TestRouteListAndShow pins `route list` and `route show <name>`: list
// carries every route in name order, show carries one route, and a missing
// route on show is exit 4.
func TestRouteListAndShow(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newCLIServer(t)
	writeRouteConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "route", "list")
	if code != errs.ExitSuccess {
		t.Fatalf("route list exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	routes, _ := doc["routes"].([]any)
	if len(routes) != 1 {
		t.Errorf("route list = %v, want 1 entry", doc["routes"])
	}

	code, stdout, stderr = runCLI(t, "route", "show", "trabajo")
	if code != errs.ExitSuccess {
		t.Fatalf("route show exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc = decodeJSON(t, stdout)
	route, _ := doc["route"].(map[string]any)
	if route == nil || route["name"] != "trabajo" {
		t.Errorf("route show = %v, want the trabajo route", doc["route"])
	}

	code, stdout, stderr = runCLI(t, "route", "show", "desconocida")
	if code != errs.ExitNotFound {
		t.Fatalf("route show (missing) exit code = %d, want 4", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", stdout.String())
	}
	expectEnvelope(t, stderr, "not_found")
}
