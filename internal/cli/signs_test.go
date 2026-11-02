package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aam9063/trafico-cli/internal/errs"
)

// vmsCLIFixture reads one of the trimmed vms snapshots for the test server.
func vmsCLIFixture(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return data
}

// newSignsCLIServer serves both real sign snapshots and reports how many
// requests each path received.
func newSignsCLIServer(t *testing.T) (*httptest.Server, func(string) int) {
	t.Helper()
	locations := vmsCLIFixture(t, "../../testdata/feeds/vms-locations/snap-1.xml")
	messages := vmsCLIFixture(t, "../../testdata/feeds/vms-messages/snap-1.xml")
	counts := make(map[string]int)
	mux := http.NewServeMux()
	mux.HandleFunc("/vms-locations", func(w http.ResponseWriter, r *http.Request) {
		counts["/vms-locations"]++
		w.Write(locations)
	})
	mux.HandleFunc("/vms-messages", func(w http.ResponseWriter, r *http.Request) {
		counts["/vms-messages"]++
		w.Write(messages)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func(path string) int { return counts[path] }
}

// writeSignsCLIConfig writes a config file with both sign feeds enabled and
// pointed at the test server.
func writeSignsCLIConfig(t *testing.T, srvURL string) {
	t.Helper()
	dir := os.Getenv("XDG_CONFIG_HOME")
	path := filepath.Join(dir, "trafico-cli", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the config directory: %v", err)
	}
	content := fmt.Sprintf(`[feeds.dgt-signs]
url = "%s/vms-locations"
enabled = true

[feeds.dgt-signs-messages]
url = "%s/vms-messages"
enabled = true
`, srvURL, srvURL)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}
}

// TestSignsCommandOutputsEnvelope pins `trafico signs` end to end over the
// real fixtures: exit 0, one JSON envelope on stdout with the five joined
// signs, the shared schema_version, attribution and the published_at of the
// messages feed (docs/feeds.md §9).
func TestSignsCommandOutputsEnvelope(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newSignsCLIServer(t)
	writeSignsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "signs")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	if doc["schema_version"] != float64(1) {
		t.Errorf("schema_version = %v, want 1", doc["schema_version"])
	}
	signs, ok := doc["signs"].([]any)
	if !ok {
		t.Fatalf("the document carries no signs array: %v", doc)
	}
	if len(signs) != 5 {
		t.Fatalf("signs = %d entries, want 5", len(signs))
	}
	meta, _ := doc["meta"].(map[string]any)
	if meta == nil {
		t.Fatalf("the document carries no meta block: %v", doc)
	}
	if meta["published_at"] != "2026-10-28T12:04:37Z" {
		t.Errorf("meta.published_at = %v, want 2026-10-28T12:04:37Z", meta["published_at"])
	}
	if meta["feed"] != "dgt-signs-messages" {
		t.Errorf("meta.feed = %v, want dgt-signs-messages", meta["feed"])
	}
	attribution, _ := doc["attribution"].(map[string]any)
	if attribution == nil || attribution["license"] != "CC BY" {
		t.Errorf("attribution = %v, want the CC BY block", doc["attribution"])
	}
}

// TestSignsCommandRoadFilter pins `trafico signs --road A-54`: one sign, the
// panel on the A-54.
func TestSignsCommandRoadFilter(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newSignsCLIServer(t)
	writeSignsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "signs", "--road", "A-54")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	signs, _ := doc["signs"].([]any)
	if len(signs) != 1 {
		t.Fatalf("signs = %d entries, want 1", len(signs))
	}
	sign, _ := signs[0].(map[string]any)
	if sign["id"] != "dgt:167938" || sign["road"] != "A-54" {
		t.Errorf("sign = %v, want dgt:167938 on A-54", sign)
	}
}

// TestSignsCommandUnknownDirectionIsUsage pins ARRANQUE §6.3: an unknown
// --direction exits 2 with a usage envelope on stderr and an empty stdout,
// and it spends no request on either feed.
func TestSignsCommandUnknownDirectionIsUsage(t *testing.T) {
	setCLIEnv(t)
	srv, counts := newSignsCLIServer(t)
	writeSignsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "signs", "--direction", "north")
	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want %d (usage; stderr: %s)", code, errs.ExitUsage, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty on a usage error, got %q", stdout.String())
	}
	doc := decodeJSON(t, stderr)
	errObj, _ := doc["error"].(map[string]any)
	if errObj == nil || errObj["type"] != "usage" {
		t.Errorf("error envelope = %v, want type usage", doc["error"])
	}
	if got := counts("/vms-locations") + counts("/vms-messages"); got != 0 {
		t.Errorf("invalid criteria made %d requests, want 0", got)
	}
}

// TestSignsCommandLimit pins --limit end to end.
func TestSignsCommandLimit(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newSignsCLIServer(t)
	writeSignsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "signs", "--limit", "2")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	signs, _ := doc["signs"].([]any)
	if len(signs) != 2 {
		t.Fatalf("signs = %d entries, want 2", len(signs))
	}
}
