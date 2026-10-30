package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/store"
	"github.com/aam9063/trafico-cli/internal/watch"
	"github.com/aam9063/trafico-cli/internal/window"
)

// watchClock is the injected clock of the run-path tests: Wednesday
// 2026-10-28 14:00 UTC. The window helpers derive the Europe/Madrid wall
// minute from it, so the windows match or do not match deterministically
// whatever the machine runs on.
var watchClock = time.Date(2026, 10, 28, 14, 0, 0, 0, time.UTC)

// watchWindowAround builds a window spec covering the Europe/Madrid
// wall-clock minute of clock, from padBefore minutes before to padAfter
// minutes after, on every weekday. The tests use it so a window provably
// matches (or provably does not match) the injected clock.
func watchWindowAround(clock time.Time, padBefore, padAfter int) string {
	local := clock.In(window.Madrid())
	minute := local.Hour()*60 + local.Minute()
	start := minute - padBefore
	end := minute + padAfter
	return fmt.Sprintf("lun-dom %02d:%02d-%02d:%02d", start/60, start%60, end/60, end%60)
}

// watchWindowAway builds a window spec that provably does NOT cover the
// Europe/Madrid wall-clock minute of clock: a one-minute window two hours
// earlier, on every weekday.
func watchWindowAway(clock time.Time) string {
	local := clock.In(window.Madrid())
	minute := local.Hour()*60 + local.Minute()
	start := (minute - 120 + 1440) % 1440
	return fmt.Sprintf("lun-dom %02d:%02d-%02d:%02d", start/60, start%60, start/60, (start+1)%60)
}

// watchServer is the fixture server of the run-path tests: the situations
// feed with a body the test can swap between runs (the feed changes every
// minute in reality) and a notification endpoint a sink can deliver to.
type watchServer struct {
	srv  *httptest.Server
	hits func(string) int

	setBody      func([]byte)
	notifyPosts  func() [][]byte
	setNotifyErr func(int) // make /notify answer with this status (200 restores)
}

// newWatchServer serves the snap-1 snapshot on /situations, accepts sink
// POSTs on /notify and reports per-path request counts.
func newWatchServer(t *testing.T) *watchServer {
	t.Helper()
	var mu sync.Mutex
	body := snapshotFeed(t)
	notifyErr := 0
	posts := [][]byte{}
	counts := make(map[string]int)
	srv, hits := newFeedServer(t, map[string]http.HandlerFunc{
		"/situations": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			counts["/situations"]++
			w.Write(body)
		},
		"/notify": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			buf := make([]byte, 4096)
			n, _ := r.Body.Read(buf)
			posts = append(posts, buf[:n])
			counts["/notify"]++
			if notifyErr != 0 {
				w.WriteHeader(notifyErr)
				return
			}
			w.WriteHeader(http.StatusOK)
		},
	})
	return &watchServer{
		srv:  srv,
		hits: hits,
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
		setNotifyErr: func(status int) {
			mu.Lock()
			defer mu.Unlock()
			notifyErr = status
		},
	}
}

// runConfig builds the configuration of the run-path tests: the test feed,
// one saved route (an A-372 segment the fixture publishes incidents on) and
// one ntfy sink pointed at the test server, so a delivery never leaves
// localhost (AGENTS.md: no test touches the network).
func runConfig(srvURL string) *config.Config {
	cfg := testConfig(srvURL)
	cfg.Routes = map[string]config.Route{"trabajo": {Segments: []string{"A-372:40-55"}}}
	cfg.Sinks = map[string]config.Sink{"phone": {Type: "ntfy", URL: srvURL, Topic: "notify"}}
	return cfg
}

// newWatchFetcher builds a fetcher over cfg whose clock is fixed at now.
func newWatchFetcher(t *testing.T, cfg *config.Config, now time.Time) *feeds.Fetcher {
	t.Helper()
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")
	fetcher.Now = func() time.Time { return now }
	return fetcher
}

// saveTestWatch parses, saves and returns a watch through the same path the
// CLI uses, failing the test on any error.
func saveTestWatch(t *testing.T, st *store.Store, cfg *config.Config, name string, target WatchTarget, windowSpec string, sinks []string) WatchSpec {
	t.Helper()
	spec, err := ParseWatchSpec(name, target, []string{windowSpec}, "", sinks, 5*time.Minute, false, cfg)
	if err != nil {
		t.Fatalf("ParseWatchSpec(%s): %v", name, err)
	}
	spec.CreatedAt = watchClock
	if err := SaveWatch(st, spec); err != nil {
		t.Fatalf("SaveWatch(%s): %v", name, err)
	}
	return spec
}

// swapIncidentID replaces one A-372 incident id of the fixture with a brand
// new one, so the next check sees it as incident.new.
func swapIncidentID(t *testing.T, body []byte, oldID, newID string) []byte {
	t.Helper()
	swapped := strings.Replace(string(body), fmt.Sprintf("id=%q", oldID), fmt.Sprintf("id=%q", newID), 1)
	if swapped == string(body) {
		t.Fatalf("the fixture carries no id %q to swap", oldID)
	}
	return []byte(swapped)
}

// TestValidateSinksUsableRejectsUnbuildableSink pins the unit behind the
// `watch add` check: a sink that exists in the config but cannot be
// constructed (a telegram webhook, which the config schema cannot carry a
// chat id for) is a usage error; a constructible one passes.
func TestValidateSinksUsableRejectsUnbuildableSink(t *testing.T) {
	cfg := runConfig("http://127.0.0.1:1")
	cfg.Sinks["telegram"] = config.Sink{Type: "webhook", Template: "telegram", URL: "http://127.0.0.1:1"}

	if err := ValidateSinksUsable([]string{"phone"}, cfg); err != nil {
		t.Fatalf("ValidateSinksUsable(ntfy sink) = %v, want nil", err)
	}
	err := ValidateSinksUsable([]string{"telegram"}, cfg)
	var errsErr *errs.Error
	if !errors.As(err, &errsErr) || errsErr.Type != "usage" {
		t.Fatalf("ValidateSinksUsable(telegram sink) = %v, want a usage error", err)
	}
	if !strings.Contains(errsErr.Message, "telegram") {
		t.Errorf("error message %q does not name the telegram sink problem", errsErr.Message)
	}
	if !strings.Contains(errsErr.Message, "chat_id") {
		t.Errorf("error message %q does not name the missing chat_id field", errsErr.Message)
	}
}

// TestSinkFromConfigBuildsTelegramWithChatID pins the sink construction of
// the telegram webhook template: with a chat_id the sink builds and carries
// it into the delivery body; without one it is an error naming the field,
// because a telegram sink without a chat_id can never succeed.
func TestSinkFromConfigBuildsTelegramWithChatID(t *testing.T) {
	sink, err := sinkFromConfig("telegram", config.Sink{Type: "webhook", Template: "telegram", URL: "http://127.0.0.1:1", ChatID: "12345678"})
	if err != nil {
		t.Fatalf("sinkFromConfig(telegram with chat_id) = %v, want nil", err)
	}
	webhook, ok := sink.(*watch.WebhookSink)
	if !ok {
		t.Fatalf("sinkFromConfig(telegram) = %T, want *watch.WebhookSink", sink)
	}
	if webhook.Style != watch.StyleTelegram || webhook.ChatID != "12345678" {
		t.Errorf("built sink = %+v, want the telegram style with the configured chat_id", webhook)
	}

	_, err = sinkFromConfig("telegram", config.Sink{Type: "webhook", Template: "telegram", URL: "http://127.0.0.1:1"})
	if err == nil {
		t.Fatalf("sinkFromConfig(telegram without chat_id) = nil error, want an error naming chat_id")
	}
	if !strings.Contains(err.Error(), "chat_id") {
		t.Errorf("error %q does not name the chat_id field", err.Error())
	}
}

// TestRunAllDeliversThroughTelegramSinkWithChatID pins the wiring end to
// end: a telegram sink with chat_id is constructible from the config, and a
// pending event delivered through it POSTs the chat_id + text body the
// Telegram API expects. The baseline stays silent (S3).
func TestRunAllDeliversThroughTelegramSinkWithChatID(t *testing.T) {
	srv := newWatchServer(t)
	cfg := runConfig(srv.srv.URL)
	cfg.Sinks["telegram"] = config.Sink{Type: "webhook", Template: "telegram", URL: srv.srv.URL + "/notify", ChatID: "12345678"}
	st, err := store.Open(filepath.Join(t.TempDir(), "trafico.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	saveTestWatch(t, st, cfg, "tg", WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		watchWindowAround(watchClock, 30, 60), []string{"telegram"})

	// First cycle: silent baseline, nothing delivered.
	if _, err := RunAll(context.Background(), cfg, newWatchFetcher(t, cfg, watchClock), st); err != nil {
		t.Fatalf("RunAll (baseline): %v", err)
	}
	if posts := srv.notifyPosts(); len(posts) != 0 {
		t.Fatalf("baseline delivered %d posts, want 0", len(posts))
	}

	// Second cycle: a new incident is announced through the telegram sink.
	srv.setBody(swapIncidentID(t, snapshotFeed(t), "22365494", "9999999"))
	result, err := RunAll(context.Background(), cfg, newWatchFetcher(t, cfg, watchClock.Add(6*time.Minute)), st)
	if err != nil {
		t.Fatalf("RunAll (second): %v", err)
	}
	if result.Delivered != 1 {
		t.Fatalf("delivered = %d, want 1", result.Delivered)
	}
	posts := srv.notifyPosts()
	if len(posts) != 1 {
		t.Fatalf("telegram sink received %d posts, want 1", len(posts))
	}
	var body map[string]string
	if err := json.Unmarshal(posts[0], &body); err != nil {
		t.Fatalf("telegram body is not JSON: %v (%q)", err, posts[0])
	}
	if body["chat_id"] != "12345678" {
		t.Errorf("body chat_id = %q, want 12345678", body["chat_id"])
	}
	if body["text"] == "" {
		t.Errorf("body text is empty, want the rendered notification")
	}
}

// TestRunAllReportsUnbuildableSinkPerWatch pins the run-time half of the
// sink rule: a watch whose sink can no longer be constructed (hand-edited
// config, sink removed later) gets its events reported as a PER-WATCH error
// and the events stay queued for retry — never a silent MarkSkipped — while
// the cycle keeps running and delivering for the other watches.
func TestRunAllReportsUnbuildableSinkPerWatch(t *testing.T) {
	srv := newWatchServer(t)
	cfg := runConfig(srv.srv.URL)
	// The broken sink exists in the config; only its construction fails.
	cfg.Sinks["telegram"] = config.Sink{Type: "webhook", Template: "telegram", URL: srv.srv.URL}
	st, err := store.Open(filepath.Join(t.TempDir(), "trafico.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	saveTestWatch(t, st, cfg, "buena", WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		watchWindowAround(watchClock, 30, 60), []string{"phone"})
	// The broken watch bypasses the definition-time check the way a
	// hand-edited config or a later sink removal would.
	saveTestWatch(t, st, cfg, "rota", WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		watchWindowAround(watchClock, 30, 60), []string{"telegram"})

	// The first cycle records both baselines silently.
	if _, err := RunAll(context.Background(), cfg, newWatchFetcher(t, cfg, watchClock), st); err != nil {
		t.Fatalf("RunAll (baseline): %v", err)
	}

	// Second cycle, interval elapsed, one new incident for both watches.
	srv.setBody(swapIncidentID(t, snapshotFeed(t), "22365494", "9999999"))
	result, err := RunAll(context.Background(), cfg, newWatchFetcher(t, cfg, watchClock.Add(6*time.Minute)), st)
	if err != nil {
		t.Fatalf("RunAll (second): %v", err)
	}
	byName := make(map[string]RunResult, len(result.Runs))
	for _, run := range result.Runs {
		byName[run.Check.Name] = run
	}
	if run := byName["buena"]; run.Err != nil || run.Delivered != 0 {
		t.Errorf("run of buena = %+v, want no error (its delivery happens in the cycle total)", run)
	}
	if run := byName["rota"]; run.Err == nil {
		t.Errorf("run of rota = %+v, want a per-watch error naming the unusable sink", run)
	} else if !strings.Contains(run.Err.Error(), "telegram") {
		t.Errorf("error of rota = %q, want it to name the telegram sink", run.Err)
	}
	if result.Delivered != 1 {
		t.Errorf("delivered = %d, want 1 (the cycle kept running for buena)", result.Delivered)
	}
	pending, err := st.UndeliveredEvents(0)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 1 || pending[0].WatchName != "rota" {
		t.Errorf("pending events = %+v, want rota's event still queued for retry", pending)
	}
}

// TestRunWatchEnforcesInterval pins the interval gate (S6) at the
// application layer: a watch whose interval has not elapsed since its last
// real check is skipped FOR THE INTERVAL — distinctly from the window skip
// — without a fetch and without touching state, and it is checked for real
// once the interval has elapsed. RunAll reports the same skip per watch.
func TestRunWatchEnforcesInterval(t *testing.T) {
	srv := newWatchServer(t)
	cfg := runConfig(srv.srv.URL)
	st, err := store.Open(filepath.Join(t.TempDir(), "trafico.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	saveTestWatch(t, st, cfg, "ruta", WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		watchWindowAround(watchClock, 30, 60), []string{"phone"})

	// Run 1: the silent baseline, recorded as the watch's last check.
	r1, err := RunWatch(context.Background(), cfg, newWatchFetcher(t, cfg, watchClock), st, "ruta")
	if err != nil {
		t.Fatalf("RunWatch (baseline): %v", err)
	}
	if r1.Check.Skipped || r1.Check.SkippedForInterval || !r1.Check.Baseline {
		t.Fatalf("baseline run = %+v, want a real baseline check", r1.Check)
	}

	// Run 2, inside the interval: skipped for the interval, no fetch, no
	// state change.
	r2, err := RunWatch(context.Background(), cfg, newWatchFetcher(t, cfg, watchClock.Add(2*time.Minute)), st, "ruta")
	if err != nil {
		t.Fatalf("RunWatch (inside the interval): %v", err)
	}
	if !r2.Check.SkippedForInterval || r2.Check.Skipped || len(r2.Check.Events) != 0 {
		t.Fatalf("interval run = %+v, want skipped for the interval with no events", r2.Check)
	}
	pending, err := st.UndeliveredEvents(0)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("the interval run queued %d events, want none", len(pending))
	}

	// Run 3, interval elapsed with a new incident: checked for real.
	srv.setBody(swapIncidentID(t, snapshotFeed(t), "22365494", "9999999"))
	r3, err := RunWatch(context.Background(), cfg, newWatchFetcher(t, cfg, watchClock.Add(6*time.Minute)), st, "ruta")
	if err != nil {
		t.Fatalf("RunWatch (interval elapsed): %v", err)
	}
	if r3.Check.SkippedForInterval || len(r3.Check.Events) != 1 {
		t.Fatalf("elapsed run = %+v, want a real check with one event", r3.Check)
	}
	if got := srv.hits("/situations"); got != 2 {
		t.Errorf("the feed was fetched %d times (baseline, elapsed run), want 2", got)
	}

	// RunAll right after: the interval has not elapsed again, and the skip
	// is reported distinctly from the window skip, without a third fetch.
	all, err := RunAll(context.Background(), cfg, newWatchFetcher(t, cfg, watchClock.Add(7*time.Minute)), st)
	if err != nil {
		t.Fatalf("RunAll (inside the interval): %v", err)
	}
	if len(all.Runs) != 1 || !all.Runs[0].Check.SkippedForInterval || all.Runs[0].Check.Skipped {
		t.Fatalf("RunAll inside the interval = %+v, want one interval-skipped entry", all.Runs)
	}
	if got := srv.hits("/situations"); got != 2 {
		t.Errorf("the interval-skipped cycle fetched the feed again (%d in total), want 2", got)
	}
}

// TestRunWatchBaselineThenExactlyOneNew pins the WATCH-01 acceptance (S3,
// S5, S10) end to end at the application layer: the first run records the
// baseline silently, the second run — with one brand new incident on the
// watched road — emits exactly one incident.new and delivers it to the
// configured sink, with no route name in the message (S7, P-07).
func TestRunWatchBaselineThenExactlyOneNew(t *testing.T) {
	srv := newWatchServer(t)
	cfg := runConfig(srv.srv.URL)
	st, err := store.Open(filepath.Join(t.TempDir(), "trafico.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	saveTestWatch(t, st, cfg, "ruta", WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		watchWindowAround(watchClock, 30, 60), []string{"phone"})

	// Run 1: the silent baseline (S3).
	fetcher := newWatchFetcher(t, cfg, watchClock)
	result, err := RunWatch(context.Background(), cfg, fetcher, st, "ruta")
	if err != nil {
		t.Fatalf("RunWatch (baseline): %v", err)
	}
	if result.Check.Skipped || result.Check.Baseline != true || len(result.Check.Events) != 0 {
		t.Fatalf("baseline run = %+v, want a non-skipped baseline with no events", result.Check)
	}
	if result.Delivered != 0 || len(srv.notifyPosts()) != 0 {
		t.Fatalf("baseline run delivered %d notifications (%d posts), want none", result.Delivered, len(srv.notifyPosts()))
	}
	prev, err := st.PreviousState("ruta")
	if err != nil {
		t.Fatalf("PreviousState after baseline: %v", err)
	}
	if len(prev) == 0 {
		t.Fatal("the baseline left no stored state")
	}

	// Run 2, six minutes later (interval elapsed, cache TTL elapsed) with
	// one new incident on the watched road.
	srv.setBody(swapIncidentID(t, snapshotFeed(t), "22365494", "9999999"))
	fetcher = newWatchFetcher(t, cfg, watchClock.Add(6*time.Minute))
	result, err = RunWatch(context.Background(), cfg, fetcher, st, "ruta")
	if err != nil {
		t.Fatalf("RunWatch (second): %v", err)
	}
	if len(result.Check.Events) != 1 {
		t.Fatalf("second run emitted %d events (%+v), want exactly one", len(result.Check.Events), result.Check.Events)
	}
	event := result.Check.Events[0]
	if event.Kind != store.EventIncidentNew || !strings.Contains(event.IncidentID, "9999999") {
		t.Fatalf("event = %s on %s, want incident.new of the new incident", event.Kind, event.IncidentID)
	}
	if result.Delivered != 1 || len(srv.notifyPosts()) != 1 {
		t.Fatalf("delivered = %d with %d sink posts, want exactly one of each", result.Delivered, len(srv.notifyPosts()))
	}
	// S7/P-07: the notification carries the road, never the route name.
	message := string(srv.notifyPosts()[0])
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

// TestRunWatchOutsideWindowSkipsWithoutFetch pins S4 at the application
// layer: outside the window the watch is not even fetched, reports skipped
// and writes nothing.
func TestRunWatchOutsideWindowSkipsWithoutFetch(t *testing.T) {
	srv := newWatchServer(t)
	cfg := runConfig(srv.srv.URL)
	st, err := store.Open(filepath.Join(t.TempDir(), "trafico.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	saveTestWatch(t, st, cfg, "ruta", WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		watchWindowAway(watchClock), nil)

	result, err := RunWatch(context.Background(), cfg, newWatchFetcher(t, cfg, watchClock), st, "ruta")
	if err != nil {
		t.Fatalf("RunWatch: %v", err)
	}
	if !result.Check.Skipped || len(result.Check.Events) != 0 {
		t.Fatalf("run outside the window = %+v, want skipped with no events", result.Check)
	}
	if got := srv.hits("/situations"); got != 0 {
		t.Errorf("the skipped run fetched the feed %d times, want 0 (S4)", got)
	}
	prev, _ := st.PreviousState("ruta")
	if len(prev) != 0 {
		t.Errorf("the skipped run stored state (%+v), want none", prev)
	}
}

// TestCheckWatchReportsWithoutWriting pins the manual-check rule: `check`
// shows what WOULD be notified (the engine decides, on a scratch copy of the
// state) without appending events and without replacing the stored state —
// the next real run performs the detection and delivery for itself.
func TestCheckWatchReportsWithoutWriting(t *testing.T) {
	srv := newWatchServer(t)
	cfg := runConfig(srv.srv.URL)
	st, err := store.Open(filepath.Join(t.TempDir(), "trafico.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	saveTestWatch(t, st, cfg, "ruta", WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		watchWindowAround(watchClock, 30, 60), []string{"phone"})

	// A real run establishes the baseline.
	fetcher := newWatchFetcher(t, cfg, watchClock)
	if _, err := RunWatch(context.Background(), cfg, fetcher, st, "ruta"); err != nil {
		t.Fatalf("RunWatch (baseline): %v", err)
	}

	// The manual check sees the new incident and REPORTS it...
	srv.setBody(swapIncidentID(t, snapshotFeed(t), "22365494", "9999999"))
	fetcher = newWatchFetcher(t, cfg, watchClock.Add(2*time.Minute))
	report, err := CheckWatch(context.Background(), cfg, fetcher, st, "ruta")
	if err != nil {
		t.Fatalf("CheckWatch: %v", err)
	}
	if report.Skipped || report.Baseline {
		t.Fatalf("report = %+v, want a non-skipped non-baseline report", report)
	}
	if len(report.Events) != 1 || report.Events[0].Kind != store.EventIncidentNew {
		t.Fatalf("report events = %+v, want exactly one incident.new", report.Events)
	}

	// ...but nothing was written: no event is pending and the new incident
	// is still absent from the state.
	pending, err := st.UndeliveredEvents(0)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("the manual check queued %d events, want none", len(pending))
	}
	state, err := st.PreviousState("ruta")
	if err != nil {
		t.Fatalf("PreviousState: %v", err)
	}
	for id := range state {
		if strings.Contains(id, "9999999") {
			t.Errorf("the manual check replaced the stored state: %q is now tracked", id)
		}
	}

	// The next real run — after the watch's interval has elapsed — detects
	// and delivers the incident for real.
	fetcher = newWatchFetcher(t, cfg, watchClock.Add(6*time.Minute))
	result, err := RunWatch(context.Background(), cfg, fetcher, st, "ruta")
	if err != nil {
		t.Fatalf("RunWatch after check: %v", err)
	}
	if len(result.Check.Events) != 1 || result.Delivered != 1 {
		t.Fatalf("run after check = %+v (delivered %d), want one event delivered", result.Check, result.Delivered)
	}
}

// TestCheckWatchOutsideWindowSkips pins that the manual check honours the
// window gate too (S4): skipped, nothing fetched, nothing reported.
func TestCheckWatchOutsideWindowSkips(t *testing.T) {
	srv := newWatchServer(t)
	cfg := runConfig(srv.srv.URL)
	st, err := store.Open(filepath.Join(t.TempDir(), "trafico.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	saveTestWatch(t, st, cfg, "ruta", WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		watchWindowAway(watchClock), []string{"phone"})

	report, err := CheckWatch(context.Background(), cfg, newWatchFetcher(t, cfg, watchClock), st, "ruta")
	if err != nil {
		t.Fatalf("CheckWatch: %v", err)
	}
	if !report.Skipped || len(report.Events) != 0 {
		t.Fatalf("check outside the window = %+v, want skipped with no events", report)
	}
	if got := srv.hits("/situations"); got != 0 {
		t.Errorf("the skipped check fetched the feed %d times, want 0 (S4)", got)
	}
}

// TestRunAllChecksOnlyDueWatches pins the `watch run` scheduling rule: with
// two watches, only the one whose window matches now is checked (one fetch),
// the other is reported skipped, and nothing is delivered.
func TestRunAllChecksOnlyDueWatches(t *testing.T) {
	srv := newWatchServer(t)
	cfg := runConfig(srv.srv.URL)
	st, err := store.Open(filepath.Join(t.TempDir(), "trafico.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	saveTestWatch(t, st, cfg, "dentro", WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		watchWindowAround(watchClock, 30, 60), []string{"phone"})
	saveTestWatch(t, st, cfg, "fuera", WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		watchWindowAway(watchClock), []string{"phone"})

	result, err := RunAll(context.Background(), cfg, newWatchFetcher(t, cfg, watchClock), st)
	if err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if len(result.Runs) != 2 {
		t.Fatalf("RunAll reported %d runs, want one per stored watch", len(result.Runs))
	}
	if result.Runs[0].Check.Name != "dentro" || result.Runs[0].Check.Skipped || !result.Runs[0].Check.Baseline {
		t.Errorf("run of the in-window watch = %+v, want a baseline check", result.Runs[0])
	}
	if result.Runs[1].Check.Name != "fuera" || !result.Runs[1].Check.Skipped {
		t.Errorf("run of the out-of-window watch = %+v, want skipped", result.Runs[1])
	}
	if got := srv.hits("/situations"); got != 1 {
		t.Errorf("RunAll fetched the feed %d times for 2 watches (1 due), want 1", got)
	}
	if result.Delivered != 0 {
		t.Errorf("RunAll delivered %d notifications, want 0", result.Delivered)
	}
}

// TestDeliverPendingSkipsEventsWithoutUsableSink pins the zero-sink rule at
// delivery time: a watch without sinks only records history, so its events
// are stamped out of the retry queue (undeliverable by configuration) and
// never block the queue.
func TestDeliverPendingSkipsEventsWithoutUsableSink(t *testing.T) {
	srv := newWatchServer(t)
	cfg := runConfig(srv.srv.URL)
	st, err := store.Open(filepath.Join(t.TempDir(), "trafico.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	saveTestWatch(t, st, cfg, "historial", WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		watchWindowAround(watchClock, 30, 60), nil)

	// A first real run sets the baseline with the unmodified snapshot.
	fetcher := newWatchFetcher(t, cfg, watchClock)
	if _, err := RunWatch(context.Background(), cfg, fetcher, st, "historial"); err != nil {
		t.Fatalf("RunWatch (baseline): %v", err)
	}
	// A second run, once the watch's interval has elapsed (new incident),
	// emits an event that no sink will ever claim; its own drain stamps it
	// out of the queue.
	srv.setBody(swapIncidentID(t, snapshotFeed(t), "22365494", "9999999"))
	fetcher = newWatchFetcher(t, cfg, watchClock.Add(6*time.Minute))
	if _, err := RunWatch(context.Background(), cfg, fetcher, st, "historial"); err != nil {
		t.Fatalf("RunWatch (second): %v", err)
	}

	delivered, _, err := DeliverPending(context.Background(), cfg, st)
	if err != nil {
		t.Fatalf("DeliverPending: %v", err)
	}
	if delivered != 0 {
		t.Errorf("DeliverPending delivered %d events, want 0 (no sinks configured)", delivered)
	}
	pending, err := st.UndeliveredEvents(0)
	if err != nil {
		t.Fatalf("UndeliveredEvents after delivery: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("pending events after delivery = %d, want 0 (stamped out of the queue)", len(pending))
	}
}

// TestDeliverPendingRetriesAfterSinkFailure pins the retry semantics: a sink
// that refuses the delivery fails the drain, leaves the event pending, and a
// later drain with a healthy sink delivers it.
func TestDeliverPendingRetriesAfterSinkFailure(t *testing.T) {
	srv := newWatchServer(t)
	cfg := runConfig(srv.srv.URL)
	st, err := store.Open(filepath.Join(t.TempDir(), "trafico.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()
	saveTestWatch(t, st, cfg, "ruta", WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		watchWindowAround(watchClock, 30, 60), []string{"phone"})

	fetcher := newWatchFetcher(t, cfg, watchClock)
	if _, err := RunWatch(context.Background(), cfg, fetcher, st, "ruta"); err != nil {
		t.Fatalf("RunWatch (baseline): %v", err)
	}
	srv.setBody(swapIncidentID(t, snapshotFeed(t), "22365494", "9999999"))
	srv.setNotifyErr(http.StatusInternalServerError)
	fetcher = newWatchFetcher(t, cfg, watchClock.Add(6*time.Minute))
	// The run itself reports the failed delivery (its check succeeded; the
	// drain stops and leaves the event queued).
	if _, err := RunWatch(context.Background(), cfg, fetcher, st, "ruta"); err == nil {
		t.Fatal("RunWatch with a failing sink succeeded, want a delivery error")
	}
	pending, err := st.UndeliveredEvents(0)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending events after a failing sink = %d, want 1 (still queued)", len(pending))
	}

	// The sink recovers; the next drain delivers the queued event.
	srv.setNotifyErr(0)
	delivered, _, err := DeliverPending(context.Background(), cfg, st)
	if err != nil {
		t.Fatalf("DeliverPending after recovery: %v", err)
	}
	if delivered != 1 || len(srv.notifyPosts()) != 2 {
		// Two posts in total: the failed attempt during the run, then the
		// successful retry.
		t.Fatalf("recovered drain delivered %d (%d posts), want one delivery after one failed attempt", delivered, len(srv.notifyPosts()))
	}
}

// TestWithWatchLockStaleTakeoverIsExclusive pins the exclusivity guarantee
// of the single-instance lock: when several processes reach for the lock at
// once, exactly ONE of them may run its cycle — the rest fail cleanly with
// the contention error instead of executing the critical section
// concurrently, which is what the lock exists to prevent.
//
// The name is historical: the lock used to be an mtime-based file whose
// stale remains were taken over. It is now an OS advisory lock, which the
// kernel releases when the holder dies, so there is no staleness heuristic
// and no takeover — but the guarantee this test checks is unchanged, and
// it must hold when every contender starts at the same instant.
func TestWithWatchLockStaleTakeoverIsExclusive(t *testing.T) {
	const workers = 16
	const rounds = 30
	for round := 0; round < rounds; round++ {
		dir := t.TempDir()
		dataPath := filepath.Join(dir, "trafico.db")

		type interval struct{ start, end time.Time }
		var (
			mu        sync.Mutex
			intervals []interval
		)
		failures := make([]error, workers)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				failures[i] = WithWatchLock(dataPath, 0, func() error {
					mu.Lock()
					from := time.Now()
					mu.Unlock()
					time.Sleep(10 * time.Millisecond)
					mu.Lock()
					intervals = append(intervals, interval{start: from, end: time.Now()})
					mu.Unlock()
					return nil
				})
			}(i)
		}
		close(start)
		wg.Wait()

		if len(intervals) != 1 {
			t.Fatalf("round %d: %d concurrent cycles executed the critical section, want exactly 1", round, len(intervals))
		}
		for i, err := range failures {
			if err == nil {
				continue
			}
			var errsErr *errs.Error
			if !errors.As(err, &errsErr) || errsErr.Type != "internal" {
				t.Fatalf("round %d: worker %d failed with %v, want a clean internal contention error", round, i, err)
			}
		}
	}
}

// TestWithWatchLock pins the single-instance lock: a second acquisition
// while one is held fails cleanly (internal error explaining the
// contention), and once the first holder releases, a new acquisition
// succeeds.
func TestWithWatchLock(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "trafico.db")

	acquired := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan struct{})
	var holderErr error
	go func() {
		defer close(holderDone)
		holderErr = WithWatchLock(dataPath, 0, func() error {
			close(acquired)
			<-release
			return nil
		})
	}()
	<-acquired

	err := WithWatchLock(dataPath, 0, func() error { return nil })
	if err == nil {
		t.Fatal("a second WithWatchLock succeeded while the lock was held, want a clean failure")
	}
	var errsErr *errs.Error
	if !errors.As(err, &errsErr) || errsErr.Type != "internal" {
		t.Fatalf("error = %v, want an internal errs.Error", err)
	}
	if !strings.Contains(errsErr.Message, "otro proceso") {
		t.Errorf("error message %q does not explain the contention", errsErr.Message)
	}

	close(release)
	<-holderDone
	if holderErr != nil {
		t.Fatalf("the lock holder failed: %v", holderErr)
	}

	// Once the holder released, a new acquisition succeeds.
	if err := WithWatchLock(dataPath, 0, func() error { return nil }); err != nil {
		t.Fatalf("WithWatchLock after the holder released: %v", err)
	}
}

// TestWithWatchLockReleasesOnPanicOrError pins that the lock is released
// even when fn fails or panics: the error is returned as-is, the panic
// propagates, and in both cases a later acquisition succeeds (the kernel
// lock was really unlocked, not left behind by the failed cycle).
func TestWithWatchLockReleasesOnPanicOrError(t *testing.T) {
	dir := t.TempDir()
	dataPath := filepath.Join(dir, "trafico.db")

	sentinel := errors.New("el ciclo falló")
	if err := WithWatchLock(dataPath, 0, func() error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("WithWatchLock with a failing fn = %v, want the fn error", err)
	}
	if err := WithWatchLock(dataPath, 0, func() error { return nil }); err != nil {
		t.Fatalf("WithWatchLock after a failing fn: %v", err)
	}

	recovered := func() (value any) {
		defer func() { value = recover() }()
		_ = WithWatchLock(dataPath, 0, func() error { panic("boom") })
		return nil
	}()
	if recovered != "boom" {
		t.Fatalf("recovered %v from WithWatchLock, want the panic of fn to propagate", recovered)
	}
	if err := WithWatchLock(dataPath, 0, func() error { return nil }); err != nil {
		t.Fatalf("WithWatchLock after a panicking fn: %v", err)
	}
}
