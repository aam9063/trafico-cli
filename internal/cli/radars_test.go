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

// newRadarsCLIServer serves the real radars snapshot and reports how many
// requests it received.
func newRadarsCLIServer(t *testing.T) (*httptest.Server, func(string) int) {
	t.Helper()
	body := vmsCLIFixture(t, "../../testdata/feeds/radars/snap-1.xml")
	counts := make(map[string]int)
	mux := http.NewServeMux()
	mux.HandleFunc("/radars", func(w http.ResponseWriter, r *http.Request) {
		counts["/radars"]++
		w.Write(body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func(path string) int { return counts[path] }
}

// writeRadarsCLIConfig writes a config file with the radars feed enabled and
// pointed at the test server.
func writeRadarsCLIConfig(t *testing.T, srvURL string) {
	t.Helper()
	dir := os.Getenv("XDG_CONFIG_HOME")
	path := filepath.Join(dir, "trafico-cli", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the config directory: %v", err)
	}
	content := fmt.Sprintf(`[feeds.dgt-radars]
url = "%s/radars"
enabled = true
`, srvURL)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}
}

// TestRadarsCommandOutputsEnvelope pins `trafico radars` end to end over the
// real fixture: exit 0, one JSON envelope on stdout with the 12 radars, the
// shared schema_version, attribution and — docs/feeds.md §9 —
// meta.published_at null, never an invented instant.
func TestRadarsCommandOutputsEnvelope(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newRadarsCLIServer(t)
	writeRadarsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "radars")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	if doc["schema_version"] != float64(1) {
		t.Errorf("schema_version = %v, want 1", doc["schema_version"])
	}
	radars, ok := doc["radars"].([]any)
	if !ok {
		t.Fatalf("the document carries no radars array: %v", doc)
	}
	if len(radars) != 12 {
		t.Fatalf("radars = %d entries, want 12", len(radars))
	}
	meta, _ := doc["meta"].(map[string]any)
	if meta == nil {
		t.Fatalf("the document carries no meta block: %v", doc)
	}
	if published, ok := meta["published_at"]; !ok || published != nil {
		t.Errorf("meta.published_at = %v, want null (docs/feeds.md §9)", meta["published_at"])
	}
	if meta["feed"] != "dgt-radars" {
		t.Errorf("meta.feed = %v, want dgt-radars", meta["feed"])
	}
	attribution, _ := doc["attribution"].(map[string]any)
	if attribution == nil || attribution["license"] != "CC BY" {
		t.Errorf("attribution = %v, want the CC BY block", doc["attribution"])
	}
}

// TestRadarsCommandProvinceFilter pins `trafico radars --province zaragoza`
// end to end: the INE table resolves code 50 to Zaragoza and exactly the one
// Z-40 radar of the fixture matches.
func TestRadarsCommandProvinceFilter(t *testing.T) {
	setCLIEnv(t)
	srv, _ := newRadarsCLIServer(t)
	writeRadarsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "radars", "--province", "zaragoza")
	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	radars, _ := doc["radars"].([]any)
	if len(radars) != 1 {
		t.Fatalf("radars = %d entries, want 1", len(radars))
	}
	radar, _ := radars[0].(map[string]any)
	if radar["province"] != "Zaragoza" || radar["province_code"] != "50" || radar["road"] != "Z-40" {
		t.Errorf("radar = %v, want the Zaragoza radar with province_code 50", radar)
	}
}

// TestRadarsCommandUnknownDirectionIsUsage pins ARRANQUE §6.3: an unknown
// --direction exits 2 with a usage envelope on stderr and an empty stdout,
// and it spends no request on the feed.
func TestRadarsCommandUnknownDirectionIsUsage(t *testing.T) {
	setCLIEnv(t)
	srv, counts := newRadarsCLIServer(t)
	writeRadarsCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "radars", "--direction", "north")
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
	if got := counts("/radars"); got != 0 {
		t.Errorf("invalid criteria made %d requests, want 0", got)
	}
}
