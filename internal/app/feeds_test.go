package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
)

// snapshotFeed loads a real trimmed DGT situation snapshot (public CC BY
// data) so the healthy-feed tests exercise the actual DATEX II dialect,
// including its com:publicationTime (docs/feeds.md §9).
func snapshotFeed(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/feeds/situations/snap-1.xml")
	if err != nil {
		t.Fatalf("reading the situation snapshot: %v", err)
	}
	return data
}

// newFeedServer starts an httptest server with one handler per path and
// returns a function that reports how many requests each path received, so
// tests can prove which feeds were (never) fetched. No test ever touches the
// real network (AGENTS.md).
func newFeedServer(t *testing.T, handlers map[string]http.HandlerFunc) (*httptest.Server, func(string) int) {
	t.Helper()
	var mu sync.Mutex
	counts := make(map[string]int)
	mux := http.NewServeMux()
	for path, handler := range handlers {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			counts[r.URL.Path]++
			mu.Unlock()
			handler(w, r)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return counts[path]
	}
}

// testConfig builds a two-feed configuration pointed at the test server.
func testConfig(srvURL string) *config.Config {
	return &config.Config{
		Feeds: map[string]config.Feed{
			config.FeedSituations: {URL: srvURL + "/situations", Enabled: true},
			config.FeedSigns:      {URL: srvURL + "/signs", Enabled: false},
		},
		Stale: config.Stale{MaxAge: config.DefaultMaxAge},
	}
}

// statusByName indexes a status list by feed name.
func statusByName(t *testing.T, list []FeedStatus) map[string]FeedStatus {
	t.Helper()
	byName := make(map[string]FeedStatus, len(list))
	for _, status := range list {
		if _, seen := byName[status.Name]; seen {
			t.Fatalf("duplicate feed %q in the status list", status.Name)
		}
		byName[status.Name] = status
	}
	return byName
}

func TestFeedsListReturnsEveryFeedWithoutHTTP(t *testing.T) {
	body := snapshotFeed(t)
	srv, hits := newFeedServer(t, map[string]http.HandlerFunc{
		"/situations": func(w http.ResponseWriter, r *http.Request) { w.Write(body) },
		"/signs":      func(w http.ResponseWriter, r *http.Request) { w.Write(body) },
	})
	cfg := testConfig(srv.URL)
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")

	list, err := FeedsList(context.Background(), cfg, fetcher)
	if err != nil {
		t.Fatalf("FeedsList failed: %v", err)
	}
	if got := hits("/situations") + hits("/signs"); got != 0 {
		t.Fatalf("feeds list made %d HTTP requests, want 0", got)
	}
	if len(list) != 2 {
		t.Fatalf("FeedsList returned %d feeds, want 2", len(list))
	}
	byName := statusByName(t, list)

	situations := byName[config.FeedSituations]
	if !situations.Enabled {
		t.Error("dgt-situations must report enabled = true")
	}
	if situations.URL != srv.URL+"/situations" {
		t.Errorf("dgt-situations URL = %q, want %q", situations.URL, srv.URL+"/situations")
	}
	if situations.Status != FeedStatusEnabled {
		t.Errorf("dgt-situations status = %q, want %q: feeds list never checks the feed, so it must not claim ok", situations.Status, FeedStatusEnabled)
	}

	signs := byName[config.FeedSigns]
	if signs.Enabled {
		t.Error("dgt-signs must report enabled = false")
	}
	if signs.Status != FeedStatusDisabled {
		t.Errorf("dgt-signs status = %q, want %q", signs.Status, FeedStatusDisabled)
	}
}

func TestFeedsStatusReportsHealthyAndFailingFeedsWithoutFailing(t *testing.T) {
	body := snapshotFeed(t)
	srv, _ := newFeedServer(t, map[string]http.HandlerFunc{
		"/situations": func(w http.ResponseWriter, r *http.Request) { w.Write(body) },
		"/broken":     func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
	})
	cfg := &config.Config{
		Feeds: map[string]config.Feed{
			config.FeedSituations: {URL: srv.URL + "/situations", Enabled: true},
			config.FeedRadars:     {URL: srv.URL + "/broken", Enabled: true},
		},
		Stale: config.Stale{MaxAge: config.DefaultMaxAge},
	}
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")

	list, err := FeedsStatus(context.Background(), cfg, fetcher)
	if err != nil {
		t.Fatalf("FeedsStatus must not fail because one feed is down: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("FeedsStatus returned %d feeds, want 2", len(list))
	}
	byName := statusByName(t, list)

	healthy := byName[config.FeedSituations]
	if healthy.Status != FeedStatusOK {
		t.Errorf("dgt-situations status = %q, want %q", healthy.Status, FeedStatusOK)
	}
	if healthy.Error != nil {
		t.Errorf("dgt-situations must carry no error, got %q", *healthy.Error)
	}
	if healthy.PublishedAt == nil {
		t.Error("dgt-situations must report its publication time")
	}
	if healthy.AgeSeconds == nil {
		t.Error("dgt-situations must report its age in seconds")
	}
	if healthy.FetchedAt == nil {
		t.Error("dgt-situations must report when it was fetched")
	}

	failing := byName[config.FeedRadars]
	if failing.Status != FeedStatusError {
		t.Errorf("dgt-radars status = %q, want %q", failing.Status, FeedStatusError)
	}
	if failing.Error == nil || *failing.Error == "" {
		t.Error("dgt-radars must carry the error message of the failed fetch")
	}
}

func TestFeedsStatusNeverFetchesDisabledFeeds(t *testing.T) {
	body := snapshotFeed(t)
	srv, hits := newFeedServer(t, map[string]http.HandlerFunc{
		"/situations": func(w http.ResponseWriter, r *http.Request) { w.Write(body) },
		"/signs":      func(w http.ResponseWriter, r *http.Request) { w.Write(body) },
	})
	cfg := testConfig(srv.URL)
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")

	list, err := FeedsStatus(context.Background(), cfg, fetcher)
	if err != nil {
		t.Fatalf("FeedsStatus failed: %v", err)
	}
	if got := hits("/signs"); got != 0 {
		t.Fatalf("the disabled feed was fetched %d times, want 0", got)
	}
	byName := statusByName(t, list)
	signs := byName[config.FeedSigns]
	if signs.Status != FeedStatusDisabled {
		t.Errorf("dgt-signs status = %q, want %q", signs.Status, FeedStatusDisabled)
	}
	if signs.FetchedAt != nil {
		t.Error("a disabled feed must not report a fetch time")
	}
}

func TestFeedsListReportsEnabledAndDisabledStatus(t *testing.T) {
	// feeds list makes no requests (CMD-02): "ok" would read as verified
	// health, so enabled feeds report "enabled" and disabled ones "disabled".
	srv, hits := newFeedServer(t, map[string]http.HandlerFunc{
		"/situations": func(w http.ResponseWriter, r *http.Request) { w.Write(snapshotFeed(t)) },
	})
	cfg := testConfig(srv.URL)
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")

	list, err := FeedsList(context.Background(), cfg, fetcher)
	if err != nil {
		t.Fatalf("FeedsList failed: %v", err)
	}
	if got := hits("/situations") + hits("/signs"); got != 0 {
		t.Fatalf("feeds list made %d HTTP requests, want 0", got)
	}
	byName := statusByName(t, list)
	if got := byName[config.FeedSituations].Status; got != FeedStatusEnabled {
		t.Errorf("dgt-situations status = %q, want %q", got, FeedStatusEnabled)
	}
	if got := byName[config.FeedSigns].Status; got != FeedStatusDisabled {
		t.Errorf("dgt-signs status = %q, want %q", got, FeedStatusDisabled)
	}
}

func TestFeedStatusForParseErrorAfterFallbackReportsCacheServe(t *testing.T) {
	// The fetch fails (no network) and the cache fallback serves a body no
	// parser can read: the per-feed error must say the data came from the
	// cache, keep from_cache/stale/warning set, and never claim the feed
	// "respondió" (ARRANQUE §5.6, P-08).
	cfg := &config.Config{
		Feeds: map[string]config.Feed{
			config.FeedSituations: {URL: "http://127.0.0.1:1/situations", Enabled: true},
		},
		Stale: config.Stale{MaxAge: config.DefaultMaxAge},
	}
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")
	if _, err := fetcher.Cache.Store(config.FeedSituations, cfg.Feeds[config.FeedSituations].URL,
		bytes.NewReader([]byte("esto no es DATEX II")), time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("seeding the cache: %v", err)
	}

	status, err := FeedStatusFor(context.Background(), cfg, fetcher, config.FeedSituations)
	if err != nil {
		t.Fatalf("FeedStatusFor must record the per-feed error, not fail: %v", err)
	}
	if status.Status != FeedStatusError {
		t.Errorf("status = %q, want %q", status.Status, FeedStatusError)
	}
	if !status.FromCache {
		t.Error("the served body came from the cache: from_cache must be true")
	}
	if !status.Stale {
		t.Error("a cache-fallback serve is always stale: stale must be true")
	}
	if status.Warning == nil || *status.Warning == "" {
		t.Error("the fallback warning must travel to the command layer for stderr")
	}
	if status.Error == nil || *status.Error == "" {
		t.Fatal("the parse error must be recorded as the per-feed error")
	}
	if strings.Contains(*status.Error, "respondió") {
		t.Errorf("a cache-fallback serve must not claim the feed responded: %q", *status.Error)
	}
	if !strings.Contains(*status.Error, "caché") {
		t.Errorf("a cache-fallback serve must say the data came from the cache: %q", *status.Error)
	}
}

func TestFeedStatusForReportsUTCTimes(t *testing.T) {
	// Dates travel in ISO 8601 UTC (ARRANQUE §6.1): fetched_at and
	// published_at must be normalised no matter where they were created.
	body := snapshotFeed(t)
	srv, _ := newFeedServer(t, map[string]http.HandlerFunc{
		"/situations": func(w http.ResponseWriter, r *http.Request) { w.Write(body) },
	})
	cfg := testConfig(srv.URL)
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")

	status, err := FeedStatusFor(context.Background(), cfg, fetcher, config.FeedSituations)
	if err != nil {
		t.Fatalf("FeedStatusFor failed: %v", err)
	}
	if status.FetchedAt == nil || status.FetchedAt.Location() != time.UTC {
		t.Errorf("fetched_at must be UTC, got %v (location %v)", status.FetchedAt, status.FetchedAt)
	}
	if status.PublishedAt != nil && status.PublishedAt.Location() != time.UTC {
		t.Errorf("published_at must be UTC, got %v", status.PublishedAt)
	}
}

func TestFeedStatusForUnknownFeedIsUsageError(t *testing.T) {
	cfg := testConfig("http://127.0.0.1:1")
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")

	_, err := FeedStatusFor(context.Background(), cfg, fetcher, "no-such-feed")
	if err == nil {
		t.Fatal("FeedStatusFor with an unknown feed must fail")
	}
	if code := errs.ExitCodeFor(err); code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want %d (usage)", code, errs.ExitUsage)
	}
}
