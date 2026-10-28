package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
)

// cliSnapshot loads the real trimmed situation snapshot for the test server.
func cliSnapshot(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/feeds/situations/snap-1.xml")
	if err != nil {
		t.Fatalf("reading the situation snapshot: %v", err)
	}
	return data
}

// setCLIEnv isolates the XDG directories of the command in a temp dir so the
// tests never read the user's real configuration.
func setCLIEnv(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
}

// writeCLIConfig writes a config file enabling two feeds: one served by the
// test server and one pointing at a failing endpoint.
func writeCLIConfig(t *testing.T, srvURL string) {
	t.Helper()
	dir := os.Getenv("XDG_CONFIG_HOME")
	path := filepath.Join(dir, "trafico-cli", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the config directory: %v", err)
	}
	content := fmt.Sprintf(`[feeds.dgt-situations]
url = "%s/situations"
enabled = true

[feeds.dgt-radars]
url = "%s/broken"
enabled = true
`, srvURL, srvURL)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}
}

// newCLIServer serves a healthy situation feed on /situations and a 500 on
// /broken, and reports how many requests each path received.
func newCLIServer(t *testing.T) (*httptest.Server, func(string) int) {
	t.Helper()
	body := cliSnapshot(t)
	counts := make(map[string]int)
	mux := http.NewServeMux()
	mux.HandleFunc("/situations", func(w http.ResponseWriter, r *http.Request) { counts["/situations"]++; w.Write(body) })
	mux.HandleFunc("/broken", func(w http.ResponseWriter, r *http.Request) {
		counts["/broken"]++
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func(path string) int { return counts[path] }
}

// runCLI executes the root command with args and returns the exit code plus
// the captured stdout and stderr.
func runCLI(t *testing.T, args ...string) (errs.ExitCode, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	root := NewRootCommand()
	root.SetOut(stdout)
	root.SetErr(stderr)
	code := executeRoot(root, args)
	return code, stdout, stderr
}

// decodeJSON parses stdout into a generic document.
func decodeJSON(t *testing.T, stdout *bytes.Buffer) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("stdout is not one JSON value: %v\nstdout: %q", err, stdout.String())
	}
	return doc
}

// feedEntries extracts the feeds array of a JSON document.
func feedEntries(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()
	raw, ok := doc["feeds"].([]any)
	if !ok {
		t.Fatalf("the document carries no feeds array: %v", doc)
	}
	entries := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("feed entry is not an object: %v", item)
		}
		entries = append(entries, entry)
	}
	return entries
}

// seedFeedCache stores body as the cache entry of feedName in the XDG cache
// directory the command under test reads, with fetchedAt as its timestamp.
func seedFeedCache(t *testing.T, feedName, sourceURL string, body []byte, fetchedAt time.Time) {
	t.Helper()
	configPath := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "trafico-cli", "config.toml")
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("loading the test config to seed the cache: %v", err)
	}
	cacheDir := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "trafico-cli")
	fetcher := feeds.NewFetcher(cfg, cacheDir, "test")
	if _, err := fetcher.Cache.Store(feedName, sourceURL, bytes.NewReader(body), fetchedAt); err != nil {
		t.Fatalf("seeding the cache: %v", err)
	}
}

// writeDeadFeedConfig writes a config file with one enabled feed pointing at
// an address that refuses connections, so the fetch fails and the cache
// fallback (if any) kicks in.
func writeDeadFeedConfig(t *testing.T) {
	t.Helper()
	dir := os.Getenv("XDG_CONFIG_HOME")
	path := filepath.Join(dir, "trafico-cli", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the config directory: %v", err)
	}
	content := `[feeds.dgt-situations]
url = "http://127.0.0.1:1/situations"
enabled = true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}
}

func TestFeedsListPrintsContractJSONWithoutNetwork(t *testing.T) {
	srv, hits := newCLIServer(t)
	setCLIEnv(t)
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "feeds", "list")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if got := hits("/situations") + hits("/broken"); got != 0 {
		t.Fatalf("feeds list made %d HTTP requests, want 0", got)
	}
	if got := stderr.String(); got != "" {
		t.Errorf("stderr must stay empty on success, got %q", got)
	}
	doc := decodeJSON(t, stdout)
	if doc["schema_version"] != float64(1) {
		t.Errorf("schema_version = %v, want 1", doc["schema_version"])
	}
	attribution, ok := doc["attribution"].(map[string]any)
	if !ok || attribution["source"] == "" {
		t.Errorf("attribution block missing or empty: %v", doc["attribution"])
	}
	if _, ok := doc["meta"].(map[string]any); !ok {
		t.Errorf("meta block missing: %v", doc["meta"])
	}
	entries := feedEntries(t, doc)
	// The merged config keeps the six compiled feeds; the two test feeds must
	// appear with their test-server URLs and enabled state.
	if len(entries) != 6 {
		t.Errorf("feeds list returned %d entries, want the 6 configured feeds", len(entries))
	}
	byName := make(map[string]map[string]any, len(entries))
	for _, entry := range entries {
		byName[entry["name"].(string)] = entry
	}
	if got := byName["dgt-situations"]["url"]; got != srv.URL+"/situations" {
		t.Errorf("dgt-situations url = %v, want %v", got, srv.URL+"/situations")
	}
	if got := byName["dgt-situations"]["enabled"]; got != true {
		t.Errorf("dgt-situations enabled = %v, want true", got)
	}
	if got := byName["dgt-radars"]["enabled"]; got != true {
		t.Errorf("dgt-radars enabled = %v, want true", got)
	}
	if got := byName["dgt-signs"]["enabled"]; got != false {
		t.Errorf("dgt-signs enabled = %v, want false", got)
	}
}

func TestFeedsStatusExitZeroMarksFailingFeed(t *testing.T) {
	srv, _ := newCLIServer(t)
	setCLIEnv(t)
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "feeds", "status")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0: a diagnostic command must survive one sick feed (stderr: %s)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	byName := make(map[string]map[string]any)
	for _, entry := range feedEntries(t, doc) {
		byName[entry["name"].(string)] = entry
	}
	if got := byName["dgt-situations"]["status"]; got != "ok" {
		t.Errorf("dgt-situations status = %v, want ok", got)
	}
	if got := byName["dgt-radars"]["status"]; got != "error" {
		t.Errorf("dgt-radars status = %v, want error", got)
	}
	if got := byName["dgt-radars"]["error"]; got == nil || got == "" {
		t.Errorf("dgt-radars must carry the error message, got %v", got)
	}
	meta, ok := doc["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta block missing: %v", doc["meta"])
	}
	if meta["published_at"] == nil {
		t.Error("meta.published_at must be present for the healthy feed (AGENTS.md)")
	}
}

func TestFeedsPrettyEndsWithAttributionLine(t *testing.T) {
	srv, _ := newCLIServer(t)
	setCLIEnv(t)
	writeCLIConfig(t, srv.URL)

	code, stdout, _ := runCLI(t, "feeds", "list", "--pretty")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := stdout.String(); !strings.HasSuffix(got, "Fuente: DGT (CC BY)\n") {
		t.Errorf("pretty output must end with the attribution line, got %q", got)
	}
}

func TestFeedsListReportsEnabledAndDisabledStatusInJSON(t *testing.T) {
	srv, _ := newCLIServer(t)
	setCLIEnv(t)
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "feeds", "list")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	byName := make(map[string]map[string]any)
	for _, entry := range feedEntries(t, decodeJSON(t, stdout)) {
		byName[entry["name"].(string)] = entry
	}
	// feeds list never checks the feed, so it must not claim "ok": it reports
	// the enabled state instead.
	if got := byName["dgt-situations"]["status"]; got != "enabled" {
		t.Errorf("dgt-situations status = %v, want enabled", got)
	}
	if got := byName["dgt-signs"]["status"]; got != "disabled" {
		t.Errorf("dgt-signs status = %v, want disabled", got)
	}
}

func TestFeedsStatusCacheFallbackWarnsOnStderrAndKeepsStdoutJSON(t *testing.T) {
	setCLIEnv(t)
	writeDeadFeedConfig(t)
	// An expired cache entry holds a body the parser can read: the fetch
	// fails, the fallback serves it, and the warning must reach stderr
	// (ARRANQUE §5.6) while stdout stays one pure JSON value (P-01).
	seedFeedCache(t, "dgt-situations", "http://127.0.0.1:1/situations", cliSnapshot(t), time.Now().Add(-time.Hour))

	code, stdout, stderr := runCLI(t, "feeds", "status")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0: a fallback serve is still a success (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "aviso:") {
		t.Errorf("the cache fallback warning must reach stderr, got %q", stderr.String())
	}
	doc := decodeJSON(t, stdout)
	byName := make(map[string]map[string]any)
	for _, entry := range feedEntries(t, doc) {
		byName[entry["name"].(string)] = entry
	}
	entry := byName["dgt-situations"]
	if entry["from_cache"] != true {
		t.Errorf("from_cache = %v, want true: the body came from the cache", entry["from_cache"])
	}
	if entry["stale"] != true {
		t.Errorf("stale = %v, want true: a fallback serve is always stale", entry["stale"])
	}
	if entry["warning"] == nil || entry["warning"] == "" {
		t.Errorf("warning must travel in the status for stderr, got %v", entry["warning"])
	}
	meta, ok := doc["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta block missing: %v", doc["meta"])
	}
	if meta["stale"] != true {
		t.Errorf("meta.stale = %v, want true while a feed entry is stale", meta["stale"])
	}
	if meta["from_cache"] != true {
		t.Errorf("meta.from_cache = %v, want true while a feed entry is cached", meta["from_cache"])
	}
}

func TestFeedsStatusFetchedAtIsUTCInJSON(t *testing.T) {
	srv, _ := newCLIServer(t)
	setCLIEnv(t)
	writeCLIConfig(t, srv.URL)

	code, stdout, _ := runCLI(t, "feeds", "status")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0", code)
	}
	doc := decodeJSON(t, stdout)
	for _, entry := range feedEntries(t, doc) {
		raw, ok := entry["fetched_at"].(string)
		if !ok {
			continue // null: the feed was never fetched
		}
		ts, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			t.Fatalf("fetched_at %q is not RFC 3339: %v", raw, err)
		}
		if ts.Location() != time.UTC {
			t.Errorf("fetched_at of %v = %q, want ISO 8601 UTC (ends in Z)", entry["name"], raw)
		}
	}
}

func TestDoctorCacheFallbackWarnsOnStderr(t *testing.T) {
	setCLIEnv(t)
	writeDeadFeedConfig(t)
	seedFeedCache(t, "dgt-situations", "http://127.0.0.1:1/situations", cliSnapshot(t), time.Now().Add(-time.Hour))

	code, stdout, stderr := runCLI(t, "doctor")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0: doctor is a diagnostic (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "aviso:") {
		t.Errorf("the cache fallback warning must reach stderr, got %q", stderr.String())
	}
	decodeJSON(t, stdout) // stdout stays one pure JSON value
}

func TestVersionRejectsPrettyFlag(t *testing.T) {
	// --pretty is scoped to the envelope-rendering commands: version prints
	// plain text and must reject the flag (exit 2) instead of silently
	// ignoring it.
	setCLIEnv(t)

	code, stdout, stderr := runCLI(t, "version", "--pretty")

	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want 2 (usage)", code)
	}
	if got := stdout.String(); got != "" {
		t.Errorf("stdout must stay empty on failure, got %q", got)
	}
	if !strings.Contains(stderr.String(), `"type":"usage"`) {
		t.Errorf("stderr must carry a usage envelope, got %q", stderr.String())
	}
}

func TestFeedsWithoutSubcommandExitsTwo(t *testing.T) {
	// Help belongs on neither stdout nor exit 0 (P-01): the usage envelope
	// goes to stderr with exit code 2.
	setCLIEnv(t)

	code, stdout, stderr := runCLI(t, "feeds")

	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want 2 (usage)", code)
	}
	if got := stdout.String(); got != "" {
		t.Errorf("stdout must stay empty, got %q", got)
	}
	if !strings.Contains(stderr.String(), `"type":"usage"`) {
		t.Errorf("stderr must carry a usage envelope, got %q", stderr.String())
	}
}

func TestFeedsUnknownSubcommandExitsTwo(t *testing.T) {
	setCLIEnv(t)

	code, stdout, stderr := runCLI(t, "feeds", "bogus")

	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want 2 (usage)", code)
	}
	if got := stdout.String(); got != "" {
		t.Errorf("stdout must stay empty on failure, got %q", got)
	}
	if !strings.Contains(stderr.String(), `"type":"usage"`) {
		t.Errorf("stderr must carry a usage envelope, got %q", stderr.String())
	}
}

func TestDoctorCommandReportsWarningAndFailingFeed(t *testing.T) {
	srv, _ := newCLIServer(t)
	setCLIEnv(t)
	writeCLIConfig(t, srv.URL)

	code, stdout, stderr := runCLI(t, "doctor")

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want 0: doctor is a diagnostic (stderr: %s)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	warning, ok := doc["not_official"].(string)
	if !ok || warning == "" {
		t.Errorf("doctor must carry the not_official warning field, got %v", doc["not_official"])
	}
	if doc["ok"] != false {
		t.Errorf("ok = %v, want false while dgt-radars is failing", doc["ok"])
	}
	configSection, ok := doc["config"].(map[string]any)
	if !ok {
		t.Fatalf("config section missing: %v", doc["config"])
	}
	if configSection["loaded"] != true {
		t.Errorf("config.loaded = %v, want true", configSection["loaded"])
	}
	if _, ok := doc["attribution"].(map[string]any); !ok {
		t.Error("doctor output must carry attribution (P-05)")
	}
	feedsSection := feedEntries(t, doc)
	byName := make(map[string]map[string]any)
	for _, entry := range feedsSection {
		byName[entry["name"].(string)] = entry
	}
	if got := byName["dgt-radars"]["status"]; got != "error" {
		t.Errorf("dgt-radars status = %v, want error", got)
	}
}
