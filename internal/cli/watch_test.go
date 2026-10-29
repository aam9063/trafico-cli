package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/service"
	"github.com/aam9063/trafico-cli/internal/store"
)

// watchServer is the fixture server of the watch command tests: the
// situations feed with a body the test can swap between runs (the feed
// changes every minute in reality) and a notification endpoint a configured
// ntfy sink can deliver to, both on localhost.
type watchServer struct {
	*httptest.Server
	hits        func(string) int
	setBody     func([]byte)
	notifyPosts func() [][]byte
}

func newWatchServer(t *testing.T) *watchServer {
	t.Helper()
	var mu sync.Mutex
	body := cliSnapshot(t)
	posts := [][]byte{}
	counts := make(map[string]int)
	mux := http.NewServeMux()
	mux.HandleFunc("/situations", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		counts["/situations"]++
		w.Write(body)
	})
	mux.HandleFunc("/notify", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		posts = append(posts, buf[:n])
		counts["/notify"]++
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &watchServer{
		Server: srv,
		hits:   func(path string) int { return counts[path] },
		setBody: func(b []byte) {
			mu.Lock()
			defer mu.Unlock()
			body = b
		},
		notifyPosts: func() [][]byte {
			mu.Lock()
			defer mu.Unlock()
			return append([][]byte{}, posts...)
		},
	}
}

// writeWatchConfig writes a config with the test feed, one saved route (an
// A-372 segment the snapshot publishes incidents on) and one ntfy sink
// pointed at the test server, so a delivery never leaves localhost.
func writeWatchConfig(t *testing.T, srvURL string) string {
	t.Helper()
	dir := os.Getenv("XDG_CONFIG_HOME")
	path := filepath.Join(dir, "trafico-cli", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("creating the config directory: %v", err)
	}
	content := fmt.Sprintf(`[feeds.dgt-situations]
url = "%s/situations"
enabled = true

[places]
casa = "38.3452,-0.4810"

[routes.trabajo]
direction = "both"
segments = ["A-372:40-55"]

[sinks.phone]
type = "ntfy"
url = "%s"
topic = "notify"

[sinks.telegram]
type = "webhook"
template = "telegram"
url = "%s/telegram"
`, srvURL, srvURL, srvURL)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}
	return path
}

// watchWindowsNow returns two window specs around the CURRENT Europe/Madrid
// wall-clock minute: inWindow provably matches now (and stays matching one
// minute of rollover) and outWindow provably does not. Very close to
// midnight no expressible window matches, so the helper waits for the next
// minute instead of building a flaky window.
func watchWindowsNow(t *testing.T) (inWindow, outWindow string) {
	t.Helper()
	loc := madridLocation()
	for {
		now := time.Now().In(loc)
		m := now.Hour()*60 + now.Minute()
		if m > 1436 {
			next := now.Truncate(time.Minute).Add(time.Minute)
			time.Sleep(time.Until(next))
			continue
		}
		inWindow = fmt.Sprintf("lun-dom %02d:%02d-%02d:%02d", m/60, m%60, (m+2)/60, (m+2)%60)
		start, end := m+2, m+3
		if end > 1439 {
			start, end = m-3, m-2
		}
		outWindow = fmt.Sprintf("lun-dom %02d:%02d-%02d:%02d", start/60, start%60, end/60, end%60)
		return inWindow, outWindow
	}
}

// expectInternalEnvelope parses stderr as exactly one internal error
// envelope (exit 1): the shared helper pins exit 2/4, and the lock
// contention is the one documented internal failure of the watch commands.
func expectInternalEnvelope(t *testing.T, stderr *bytes.Buffer) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &doc); err != nil {
		t.Fatalf("stderr is not one JSON envelope: %v / stderr: %q", err, stderr.String())
	}
	errObj, _ := doc["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("stderr carries no error object: %v", doc)
	}
	if errObj["type"] != "internal" {
		t.Errorf("error.type = %v, want internal", errObj["type"])
	}
	if errObj["exit_code"] != float64(errs.ExitInternal) {
		t.Errorf("error.exit_code = %v, want 1", errObj["exit_code"])
	}
	return errObj
}

// watchDataDir returns the XDG data directory the commands store the
// database (and the lock file) in.
func watchDataDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(os.Getenv("XDG_DATA_HOME"), "trafico-cli")
}

// dropFeedCache empties the feed cache between two runs, simulating the
// elapsed 60 s TTL of a feed that changes every minute.
func dropFeedCache(t *testing.T) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(os.Getenv("XDG_CACHE_HOME"), "trafico-cli")); err != nil {
		t.Fatalf("dropping the feed cache: %v", err)
	}
}

// swapIncidentID replaces one A-372 incident id of the snapshot with a brand
// new one, so the next check sees it as incident.new.
func swapIncidentID(t *testing.T, oldID, newID string) []byte {
	t.Helper()
	body := cliSnapshot(t)
	swapped := strings.Replace(string(body), fmt.Sprintf("id=%q", oldID), fmt.Sprintf("id=%q", newID), 1)
	if swapped == string(body) {
		t.Fatalf("the snapshot carries no id %q to swap", oldID)
	}
	return []byte(swapped)
}

// TestWatchAddListShowThreeShapes adds each of the three target shapes and
// reads them back through list and show (docs/ARRANQUE.md §5.5).
func TestWatchAddListShowThreeShapes(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, _ := watchWindowsNow(t)

	code, stdout, stderr := runCLI(t, "watch", "add", "ruta", "route", "trabajo",
		"--window", inWindow, "--notify", "phone")
	if code != errs.ExitSuccess {
		t.Fatalf("watch add (route) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	if watches, _ := doc["watches"].([]any); len(watches) != 1 {
		t.Fatalf("watch add (route) watches = %v, want 1 entry", doc["watches"])
	}

	code, _, stderr = runCLI(t, "watch", "add", "autopista", "road", "ap-7",
		"--pk", "600-700", "--window", inWindow, "--min-severity", "medium")
	if code != errs.ExitSuccess {
		t.Fatalf("watch add (road) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}

	code, _, stderr = runCLI(t, "watch", "add", "casa", "near", "casa",
		"--radius", "15km", "--window", inWindow)
	if code != errs.ExitSuccess {
		t.Fatalf("watch add (near) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}

	// The road name must be normalised into the stored definition.
	code, stdout, stderr = runCLI(t, "watch", "list")
	if code != errs.ExitSuccess {
		t.Fatalf("watch list exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc = decodeJSON(t, stdout)
	entries, _ := doc["watches"].([]any)
	if len(entries) != 3 {
		t.Fatalf("watch list = %v, want the 3 watches", doc["watches"])
	}
	byName := make(map[string]map[string]any, len(entries))
	for _, entry := range entries {
		w, _ := entry.(map[string]any)
		byName[w["name"].(string)] = w
	}
	road, _ := byName["autopista"]["target"].(map[string]any)
	if road["road"] != "AP-7" {
		t.Errorf("stored road target = %v, want the normalised AP-7", road)
	}
	if pk, _ := road["pk"].(map[string]any); pk == nil || pk["from"] != float64(600) || pk["to"] != float64(700) {
		t.Errorf("stored road pk = %v, want 600-700", road["pk"])
	}
	if byName["autopista"]["min_severity"] != "medium" {
		t.Errorf("stored min_severity = %v, want medium", byName["autopista"]["min_severity"])
	}
	if sinks, _ := byName["ruta"]["sinks"].([]any); len(sinks) != 1 || sinks[0] != "phone" {
		t.Errorf("stored sinks = %v, want [phone]", byName["ruta"]["sinks"])
	}
	if windows, _ := byName["ruta"]["windows"].([]any); len(windows) != 1 || windows[0] != inWindow {
		t.Errorf("stored windows = %v, want [%s]", byName["ruta"]["windows"], inWindow)
	}

	// show reads one watch back, and a missing one is not_found (exit 4).
	code, stdout, stderr = runCLI(t, "watch", "show", "casa")
	if code != errs.ExitSuccess {
		t.Fatalf("watch show exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc = decodeJSON(t, stdout)
	watch, _ := doc["watch"].(map[string]any)
	if watch == nil || watch["kind"] != "near" {
		t.Errorf("watch show = %v, want the near watch", doc["watch"])
	}
	code, stdout, stderr = runCLI(t, "watch", "show", "desconocida")
	if code != errs.ExitNotFound {
		t.Fatalf("watch show (missing) exit code = %d, want 4", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", stdout.String())
	}
	expectEnvelope(t, stderr, "not_found")
}

// TestWatchAddRejectsInvalid pins exit 2 with the error envelope for a bad
// name, a missing window, a bad min severity, an unknown sink and an
// interval under two minutes — and that nothing is stored.
func TestWatchAddRejectsInvalid(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, _ := watchWindowsNow(t)

	tests := []struct {
		name string
		args []string
	}{
		{"bad name", []string{"watch", "add", "mi ruta", "route", "trabajo", "--window", inWindow}},
		{"missing window", []string{"watch", "add", "ruta", "route", "trabajo"}},
		{"bad min severity", []string{"watch", "add", "ruta", "route", "trabajo", "--window", inWindow, "--min-severity", "critical"}},
		{"unknown sink", []string{"watch", "add", "ruta", "route", "trabajo", "--window", inWindow, "--notify", "reloj"}},
		{"interval under two minutes", []string{"watch", "add", "ruta", "route", "trabajo", "--window", inWindow, "--interval", "1m"}},
		{"bad interval", []string{"watch", "add", "ruta", "route", "trabajo", "--window", inWindow, "--interval", "pronto"}},
		{"unknown kind", []string{"watch", "add", "ruta", "zone", "trabajo", "--window", inWindow}},
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
		})
	}

	// Nothing was stored by any rejected add.
	code, stdout, stderr := runCLI(t, "watch", "list")
	if code != errs.ExitSuccess {
		t.Fatalf("watch list after rejected adds exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if watches, _ := decodeJSON(t, stdout)["watches"].([]any); len(watches) != 0 {
		t.Errorf("watch list after rejected adds = %v, want empty", watches)
	}
}

// TestWatchAddRejectsUnbuildableSink pins the definition-time sink check:
// a sink the delivery layer cannot construct (a telegram webhook, which the
// config schema cannot carry a chat id for) is rejected by `watch add` with
// a usage error (exit 2) instead of being accepted and silently dropped at
// every delivery.
func TestWatchAddRejectsUnbuildableSink(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, _ := watchWindowsNow(t)

	code, stdout, stderr := runCLI(t, "watch", "add", "rota", "route", "trabajo",
		"--window", inWindow, "--notify", "telegram")
	if code != errs.ExitUsage {
		t.Fatalf("watch add with an unbuildable sink exit code = %d, want 2 (stderr: %q)", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", stdout.String())
	}
	errObj := expectEnvelope(t, stderr, "usage")
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "telegram") {
		t.Errorf("error.message = %q, want it to name the telegram sink problem", msg)
	}

	// The rejected add stored nothing.
	_, stdout, _ = runCLI(t, "watch", "list")
	if watches, _ := decodeJSON(t, stdout)["watches"].([]any); len(watches) != 0 {
		t.Errorf("watch list after the rejected add = %v, want empty", watches)
	}
}

// TestWatchRemoveRequiresConfirmation pins that `watch remove` without --yes
// exits 2 with a confirmation_required envelope and leaves the stored
// definition unchanged (asserted by reading it back).
func TestWatchRemoveRequiresConfirmation(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, _ := watchWindowsNow(t)
	if code, _, stderr := runCLI(t, "watch", "add", "ruta", "route", "trabajo", "--window", inWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}

	code, stdout, stderr := runCLI(t, "watch", "remove", "ruta")
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

	// The definition is still there, unchanged.
	code, stdout, stderr = runCLI(t, "watch", "show", "ruta")
	if code != errs.ExitSuccess {
		t.Fatalf("watch show after refused removal exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	watch, _ := decodeJSON(t, stdout)["watch"].(map[string]any)
	if watch == nil || watch["name"] != "ruta" || watch["kind"] != "route" {
		t.Errorf("watch after refused removal = %v, want the intact ruta watch", watch)
	}

	// With --yes it goes away.
	code, _, _ = runCLI(t, "watch", "remove", "ruta", "--yes")
	if code != errs.ExitSuccess {
		t.Fatalf("watch remove --yes exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	_, stdout, _ = runCLI(t, "watch", "list")
	if watches, _ := decodeJSON(t, stdout)["watches"].([]any); len(watches) != 0 {
		t.Errorf("watch list after removal = %v, want empty", watches)
	}
}

// TestWatchCheckOutsideWindow pins the manual check outside the window (S4):
// skipped, nothing fetched, nothing emitted.
func TestWatchCheckOutsideWindow(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	_, outWindow := watchWindowsNow(t)
	if code, _, stderr := runCLI(t, "watch", "add", "ruta", "route", "trabajo", "--window", outWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}

	code, stdout, stderr := runCLI(t, "watch", "check", "ruta")
	if code != errs.ExitSuccess {
		t.Fatalf("watch check exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	if doc["watch"] != "ruta" || doc["skipped"] != true {
		t.Errorf("watch check = %v, want skipped for ruta", doc)
	}
	if doc["baseline"] == true || doc["events"] != float64(0) {
		t.Errorf("watch check reported work it did not do: %v", doc)
	}
	if got := srv.hits("/situations"); got != 0 {
		t.Errorf("the skipped check fetched the feed %d times, want 0 (S4)", got)
	}
}

// TestWatchRunOnlyChecksDueWatches pins the run scheduling rule: with two
// watches, only the one inside its window is checked (one feed fetch), the
// other is reported skipped.
func TestWatchRunOnlyChecksDueWatches(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, outWindow := watchWindowsNow(t)
	if code, _, stderr := runCLI(t, "watch", "add", "activa", "route", "trabajo", "--window", inWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add (activa) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if code, _, stderr := runCLI(t, "watch", "add", "nocturna", "route", "trabajo", "--window", outWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add (nocturna) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}

	code, stdout, stderr := runCLI(t, "watch", "run")
	if code != errs.ExitSuccess {
		t.Fatalf("watch run exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	runs, _ := doc["runs"].([]any)
	if len(runs) != 2 {
		t.Fatalf("watch run runs = %v, want one entry per watch", doc["runs"])
	}
	byName := make(map[string]map[string]any, len(runs))
	for _, entry := range runs {
		w, _ := entry.(map[string]any)
		byName[w["watch"].(string)] = w
	}
	if byName["activa"]["skipped"] == true || byName["activa"]["baseline"] != true {
		t.Errorf("activa run = %v, want a baseline check", byName["activa"])
	}
	if byName["nocturna"]["skipped"] != true {
		t.Errorf("nocturna run = %v, want skipped", byName["nocturna"])
	}
	if doc["delivered"] != float64(0) {
		t.Errorf("delivered = %v, want 0", doc["delivered"])
	}
	if got := srv.hits("/situations"); got != 1 {
		t.Errorf("watch run fetched the feed %d times for 2 watches (1 due), want 1", got)
	}
}

// backdateLastCheck moves the stored last-check instant of one watch back
// into the past, so the next `watch run` finds its interval elapsed. It goes
// through the store API, never a raw SQL write.
func backdateLastCheck(t *testing.T, name string) {
	t.Helper()
	st, err := store.Open(filepath.Join(watchDataDir(t), "trafico.db"))
	if err != nil {
		t.Fatalf("opening the watch database to backdate the last check: %v", err)
	}
	defer st.Close()
	if err := st.MarkWatchChecked(name, time.Now().Add(-10*time.Minute)); err != nil {
		t.Fatalf("MarkWatchChecked(%s): %v", name, err)
	}
}

// TestWatchRunSkipsWithinInterval pins the interval gate (S6): a watch whose
// interval has not elapsed since its last real check is reported skipped FOR
// THE INTERVAL — distinctly from the window skip — without a feed fetch, and
// it is checked again once the interval has elapsed.
func TestWatchRunSkipsWithinInterval(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, _ := watchWindowsNow(t)
	if code, _, stderr := runCLI(t, "watch", "add", "ruta", "route", "trabajo", "--window", inWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}

	// The first run checks the watch (its silent baseline) and fetches once.
	code, stdout, stderr := runCLI(t, "watch", "run")
	if code != errs.ExitSuccess {
		t.Fatalf("watch run (baseline) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	runs, _ := decodeJSON(t, stdout)["runs"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["baseline"] != true {
		t.Fatalf("baseline run = %v, want one baseline entry", runs)
	}

	// The second run, seconds later, is inside the window but inside the
	// interval too: skipped for the interval, no fetch, nothing delivered.
	code, stdout, stderr = runCLI(t, "watch", "run")
	if code != errs.ExitSuccess {
		t.Fatalf("watch run (inside the interval) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	runs, _ = doc["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("interval run runs = %v, want one entry", runs)
	}
	entry := runs[0].(map[string]any)
	if entry["skipped_interval"] != true {
		t.Errorf("interval run entry = %v, want skipped_interval = true", entry)
	}
	if _, claimed := entry["skipped"]; claimed {
		t.Errorf("interval run entry = %v, want no window skip", entry)
	}
	if doc["delivered"] != float64(0) {
		t.Errorf("delivered = %v, want 0", doc["delivered"])
	}
	if got := srv.hits("/situations"); got != 1 {
		t.Errorf("the interval-skipped run fetched the feed %d times in total, want 1", got)
	}

	// Once the interval has elapsed the watch is checked again.
	backdateLastCheck(t, "ruta")
	code, stdout, stderr = runCLI(t, "watch", "run")
	if code != errs.ExitSuccess {
		t.Fatalf("watch run (interval elapsed) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	runs, _ = decodeJSON(t, stdout)["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("elapsed run runs = %v, want one entry", runs)
	}
	entry = runs[0].(map[string]any)
	if _, skipped := entry["skipped_interval"]; skipped {
		t.Errorf("elapsed run entry = %v, want no interval skip", entry)
	}
	if _, skipped := entry["skipped"]; skipped {
		t.Errorf("elapsed run entry = %v, want no window skip", entry)
	}
	// The interval-skipped run never requested the feed; the elapsed run may
	// serve it from cache, so the total stays at the baseline's one fetch.
	if got := srv.hits("/situations"); got != 1 {
		t.Errorf("the cycle fetched the feed %d times in total, want 1", got)
	}
}

// TestWatchRunLockContention pins the single-instance lock at the command
// boundary: with a live lock file in the data directory, `watch run` fails
// cleanly (internal envelope, exit 1) and the stored watches are intact.
func TestWatchRunLockContention(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, _ := watchWindowsNow(t)
	if code, _, stderr := runCLI(t, "watch", "add", "activa", "route", "trabajo", "--window", inWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}

	lockPath := filepath.Join(watchDataDir(t), "watch.lock")
	if err := os.WriteFile(lockPath, []byte("424242\n"), 0o600); err != nil {
		t.Fatalf("creating the live lock: %v", err)
	}

	code, stdout, stderr := runCLI(t, "watch", "run")
	if code != errs.ExitInternal {
		t.Fatalf("exit code = %d, want 1 (internal)", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", stdout.String())
	}
	errObj := expectInternalEnvelope(t, stderr)
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "otro proceso") {
		t.Errorf("error.message = %q, want it to explain the contention", msg)
	}

	// The stored definitions are intact and the command works once the lock
	// is gone (the contended run must not have wedged anything).
	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("removing the live lock: %v", err)
	}
	code, stdout, stderr = runCLI(t, "watch", "list")
	if code != errs.ExitSuccess {
		t.Fatalf("watch list after the contended run exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if watches, _ := decodeJSON(t, stdout)["watches"].([]any); len(watches) != 1 {
		t.Errorf("watch list after the contended run = %v, want the intact watch", watches)
	}
}

// TestWatchRunStaleLockProceeds pins that a stale lock file — the remains of
// a crashed process — does not block `watch run` forever: the run takes the
// lock over and succeeds.
func TestWatchRunStaleLockProceeds(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, _ := watchWindowsNow(t)
	if code, _, stderr := runCLI(t, "watch", "add", "activa", "route", "trabajo", "--window", inWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}

	lockPath := filepath.Join(watchDataDir(t), "watch.lock")
	if err := os.WriteFile(lockPath, []byte("424242\n"), 0o600); err != nil {
		t.Fatalf("creating the stale lock: %v", err)
	}
	stale := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(lockPath, stale, stale); err != nil {
		t.Fatalf("backdating the stale lock: %v", err)
	}

	code, stdout, stderr := runCLI(t, "watch", "run")
	if code != errs.ExitSuccess {
		t.Fatalf("watch run over a stale lock exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if runs, _ := decodeJSON(t, stdout)["runs"].([]any); len(runs) != 1 {
		t.Errorf("watch run runs = %v, want the one watch checked", runs)
	}
}

// TestWatchRunDeliversExactlyOneEvent pins the WATCH-01 acceptance end to
// end at the CLI boundary (S3, S5, S7, S10): the baseline run emits nothing,
// the second run — with a brand new incident on the watched road — emits
// exactly one incident.new and delivers it to the configured sink, and the
// notification carries no route or watch name.
func TestWatchRunDeliversExactlyOneEvent(t *testing.T) {
	setCLIEnv(t)
	srv := newWatchServer(t)
	writeWatchConfig(t, srv.URL)
	inWindow, _ := watchWindowsNow(t)
	if code, _, stderr := runCLI(t, "watch", "add", "ruta", "route", "trabajo", "--window", inWindow, "--notify", "phone"); code != errs.ExitSuccess {
		t.Fatalf("watch add exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}

	// Run 1: the silent baseline (S3).
	code, stdout, stderr := runCLI(t, "watch", "run")
	if code != errs.ExitSuccess {
		t.Fatalf("watch run (baseline) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	runs, _ := doc["runs"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["baseline"] != true {
		t.Fatalf("baseline run = %v, want one baseline entry", doc["runs"])
	}
	if doc["delivered"] != float64(0) || len(srv.notifyPosts()) != 0 {
		t.Fatalf("baseline run delivered %v (%d posts), want none", doc["delivered"], len(srv.notifyPosts()))
	}

	// Run 2: the feed changed (cache TTL simulated by dropping it) and one
	// brand new incident appears on the watched road. The watch's interval
	// is backdated through the store, so this run is really due (S6).
	dropFeedCache(t)
	backdateLastCheck(t, "ruta")
	srv.setBody(swapIncidentID(t, "22365494", "9999999"))
	code, stdout, stderr = runCLI(t, "watch", "run")
	if code != errs.ExitSuccess {
		t.Fatalf("watch run (second) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc = decodeJSON(t, stdout)
	runs, _ = doc["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("second run runs = %v, want one entry", doc["runs"])
	}
	entry := runs[0].(map[string]any)
	if entry["events"] != float64(1) {
		t.Fatalf("second run entry = %v, want exactly one event", entry)
	}
	if kinds, _ := entry["kinds"].([]any); len(kinds) != 1 || kinds[0] != "incident.new" {
		t.Errorf("second run kinds = %v, want [incident.new]", entry["kinds"])
	}
	if doc["delivered"] != float64(1) {
		t.Errorf("delivered = %v, want 1", doc["delivered"])
	}
	posts := srv.notifyPosts()
	if len(posts) != 1 {
		t.Fatalf("sink received %d notifications, want exactly one", len(posts))
	}
	message := string(posts[0])
	if !strings.Contains(message, "A-372") {
		t.Errorf("notification %q does not name the road", message)
	}
	if strings.Contains(message, "trabajo") || strings.Contains(message, "ruta") {
		t.Errorf("notification %q leaks a route or watch name (P-07)", message)
	}
	if !strings.HasSuffix(message, "Fuente: DGT (CC BY)") {
		t.Errorf("notification %q lacks the attribution line (P-05)", message)
	}
}

// fakeServiceRunner records every scheduler command the service commands run
// and can be told to fail, so no test ever invokes a real systemctl or
// launchctl.
type fakeServiceRunner struct {
	calls []string
	fail  error
}

func (r *fakeServiceRunner) Run(name string, args ...string) error {
	call := name
	for _, arg := range args {
		call += " " + arg
	}
	r.calls = append(r.calls, call)
	return r.fail
}

// setServiceContext replaces the injectable service context of the CLI with
// one rooted at a temp home and driven by the given fake runner, so the
// command tests never touch the real scheduler or the user's home.
func setServiceContext(t *testing.T, runner *fakeServiceRunner) string {
	t.Helper()
	home := t.TempDir()
	restore := serviceContext
	serviceContext = func() (service.Env, service.Platform, error) {
		return service.Env{
			HomeDir: home,
			BinPath: filepath.Join(home, "bin", "trafico"),
			Runner:  runner,
		}, service.PlatformLinux, nil
	}
	t.Cleanup(func() { serviceContext = restore })
	return home
}

// TestWatchServiceInstallStatusUninstall walks the whole lifecycle of the
// scheduler integration through the CLI: install writes the units and
// enables the timer, status reads the state back, uninstall removes the
// files and disables the timer, and every step exits 0 with one envelope.
func TestWatchServiceInstallStatusUninstall(t *testing.T) {
	setCLIEnv(t)
	runner := &fakeServiceRunner{}
	home := setServiceContext(t, runner)

	code, stdout, stderr := runCLI(t, "watch", "service", "install")
	if code != errs.ExitSuccess {
		t.Fatalf("watch service install exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc := decodeJSON(t, stdout)
	svc, _ := doc["service"].(map[string]any)
	if svc == nil {
		t.Fatalf("install envelope carries no service object: %v", doc)
	}
	if svc["platform"] != "linux" || svc["scheduler"] != "systemd" {
		t.Errorf("install platform/scheduler = %v/%v, want linux/systemd", svc["platform"], svc["scheduler"])
	}
	if svc["installed"] != true {
		t.Errorf("install installed = %v, want true", svc["installed"])
	}
	files, _ := svc["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("install files = %v, want the two unit files", svc["files"])
	}
	for _, f := range files {
		if _, err := os.Stat(f.(string)); err != nil {
			t.Errorf("unit file %v was not written: %v", f, err)
		}
	}
	if !runnerHas(runner, "systemctl --user daemon-reload") || !runnerHas(runner, "systemctl --user enable --now trafico-watch.timer") {
		t.Errorf("install ran commands %v, want daemon-reload and enable --now", runner.calls)
	}

	// The default interval of the generated timer is 5 minutes (S6).
	timerBytes, err := os.ReadFile(filepath.Join(home, ".config", "systemd", "user", "trafico-watch.timer"))
	if err != nil {
		t.Fatalf("reading the generated timer: %v", err)
	}
	if !strings.Contains(string(timerBytes), "OnUnitActiveSec=5m") {
		t.Errorf("generated timer does not carry the default 5m interval:\n%s", timerBytes)
	}

	code, stdout, stderr = runCLI(t, "watch", "service", "status")
	if code != errs.ExitSuccess {
		t.Fatalf("watch service status exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc = decodeJSON(t, stdout)
	svc = doc["service"].(map[string]any)
	if svc["installed"] != true || svc["active"] != true {
		t.Errorf("status installed/active = %v/%v, want true/true", svc["installed"], svc["active"])
	}

	code, stdout, stderr = runCLI(t, "watch", "service", "uninstall")
	if code != errs.ExitSuccess {
		t.Fatalf("watch service uninstall exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	doc = decodeJSON(t, stdout)
	svc = doc["service"].(map[string]any)
	if svc["installed"] != false {
		t.Errorf("uninstall installed = %v, want false", svc["installed"])
	}
	removed, _ := svc["files"].([]any)
	if len(removed) != 2 {
		t.Errorf("uninstall removed files = %v, want the two unit files", svc["files"])
	}
	for _, f := range removed {
		if _, err := os.Stat(f.(string)); !os.IsNotExist(err) {
			t.Errorf("unit file %v still exists after uninstall", f)
		}
	}
	if !runnerHas(runner, "systemctl --user disable --now trafico-watch.timer") {
		t.Errorf("uninstall ran commands %v, want the disable of the timer", runner.calls)
	}

	code, stdout, _ = runCLI(t, "watch", "service", "status")
	if code != errs.ExitSuccess {
		t.Fatalf("watch service status (after uninstall) exit code = %d, want 0", code)
	}
	doc = decodeJSON(t, stdout)
	svc = doc["service"].(map[string]any)
	if svc["installed"] != false || svc["active"] != false {
		t.Errorf("status after uninstall installed/active = %v/%v, want false/false", svc["installed"], svc["active"])
	}
}

// TestWatchServiceStatusNotInstalledExitsZero pins the not-installed report:
// exit 0 with an installed=false envelope that says "no instalado", never an
// error envelope.
func TestWatchServiceStatusNotInstalledExitsZero(t *testing.T) {
	setCLIEnv(t)
	runner := &fakeServiceRunner{}
	setServiceContext(t, runner)

	code, stdout, stderr := runCLI(t, "watch", "service", "status")
	if code != errs.ExitSuccess {
		t.Fatalf("watch service status (not installed) exit code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr must stay empty, got %q", stderr.String())
	}
	doc := decodeJSON(t, stdout)
	svc, _ := doc["service"].(map[string]any)
	if svc == nil {
		t.Fatalf("status envelope carries no service object: %v", doc)
	}
	if svc["installed"] != false || svc["active"] != false {
		t.Errorf("status installed/active = %v/%v, want false/false", svc["installed"], svc["active"])
	}
	if message, _ := svc["message"].(string); !strings.Contains(message, "no instalado") {
		t.Errorf("status message = %q, want it to say no instalado", message)
	}
	if files, _ := svc["files"].([]any); len(files) != 0 {
		t.Errorf("status files = %v, want none", files)
	}
	if len(runner.calls) != 0 {
		t.Errorf("status of an empty home ran commands %v, want none", runner.calls)
	}
}

// TestWatchServiceRejectsInvalidInterval pins exit 2 with a usage envelope
// for an unparseable interval and one below the two-minute minimum (S6),
// without writing anything.
func TestWatchServiceRejectsInvalidInterval(t *testing.T) {
	setCLIEnv(t)
	runner := &fakeServiceRunner{}
	home := setServiceContext(t, runner)

	for _, interval := range []string{"pronto", "1m"} {
		code, stdout, stderr := runCLI(t, "watch", "service", "install", "--interval", interval)
		if code != errs.ExitUsage {
			t.Fatalf("install --interval %s exit code = %d, want 2 (usage)", interval, code)
		}
		if stdout.Len() != 0 {
			t.Errorf("stdout must stay empty, got %q", stdout.String())
		}
		expectEnvelope(t, stderr, "usage")
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("reading the home directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("a rejected install wrote %v into the home", entries)
	}
	if len(runner.calls) != 0 {
		t.Errorf("a rejected install ran commands %v, want none", runner.calls)
	}
}

// TestWatchServiceUnknownSubcommand pins exit 2 for an unknown subcommand of
// the service group instead of a silent help print (exit 0).
func TestWatchServiceUnknownSubcommand(t *testing.T) {
	setCLIEnv(t)
	runner := &fakeServiceRunner{}
	setServiceContext(t, runner)

	code, stdout, stderr := runCLI(t, "watch", "service", "bogus")
	if code != errs.ExitUsage {
		t.Fatalf("watch service bogus exit code = %d, want 2 (usage)", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", stdout.String())
	}
	expectEnvelope(t, stderr, "usage")
}

// TestWatchServiceWindowsUnsupported pins that on Windows every service
// subcommand fails with the clear usage envelope instead of pretending, and
// writes nothing anywhere.
func TestWatchServiceWindowsUnsupported(t *testing.T) {
	setCLIEnv(t)
	home := t.TempDir()
	restore := serviceContext
	serviceContext = func() (service.Env, service.Platform, error) {
		return service.Env{
			HomeDir: home,
			BinPath: filepath.Join(home, "bin", "trafico.exe"),
			Runner:  &fakeServiceRunner{},
		}, service.PlatformWindows, nil
	}
	t.Cleanup(func() { serviceContext = restore })

	for _, args := range [][]string{
		{"watch", "service", "install"},
		{"watch", "service", "uninstall"},
		{"watch", "service", "status"},
	} {
		code, stdout, stderr := runCLI(t, args...)
		if code != errs.ExitUsage {
			t.Fatalf("%v exit code = %d, want 2 (usage)", args, code)
		}
		if stdout.Len() != 0 {
			t.Errorf("%v: stdout must stay empty, got %q", args, stdout.String())
		}
		envelope := expectEnvelope(t, stderr, "usage")
		if message, _ := envelope["message"].(string); !strings.Contains(message, "no está soportado") {
			t.Errorf("%v: message = %q, want it to say the platform is not supported", args, message)
		}
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("reading the home directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("a rejected windows install wrote %v into the home", entries)
	}
}

// TestWatchServiceRunnerFailureSurfaces pins that a scheduler command that
// fails surfaces as an internal error envelope (exit 1), never swallowed.
func TestWatchServiceRunnerFailureSurfaces(t *testing.T) {
	setCLIEnv(t)
	runner := &fakeServiceRunner{fail: errors.New("boom")}
	setServiceContext(t, runner)

	code, stdout, stderr := runCLI(t, "watch", "service", "install")
	if code != errs.ExitInternal {
		t.Fatalf("install with a failing runner exit code = %d, want 1 (internal)", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay empty, got %q", stdout.String())
	}
	expectInternalEnvelope(t, stderr)
}

// runnerHas reports whether the fake runner executed the exact command.
func runnerHas(runner *fakeServiceRunner, call string) bool {
	for _, c := range runner.calls {
		if c == call {
			return true
		}
	}
	return false
}
