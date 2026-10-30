package feeds

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
)

// gzipHandler serves body with Content-Encoding: gzip, as the real feeds do.
func gzipHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte(body))
		_ = gz.Close()
	})
}

const testFeedBody = "<d2:payload>situaciones</d2:payload>"

// newTestFetcher builds a Fetcher over an httptest server URL with a
// controllable clock (nil means the real time.Now). The cache lives in a
// per-test temporary directory, so tests never share state.
func newTestFetcher(t *testing.T, srvURL string, clock *time.Time) *Fetcher {
	t.Helper()
	cfg := config.Default()
	feed := cfg.Feeds[config.FeedSituations]
	feed.URL = srvURL
	cfg.Feeds[config.FeedSituations] = feed

	f := NewFetcher(cfg, t.TempDir(), "test")
	if clock != nil {
		f.Now = func() time.Time { return *clock }
	}
	return f
}

// P-06: two commands in a row reuse one download (60 s shared cache).
func TestFetchFirstMissThenCacheHit(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(testFeedBody))
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv.URL, nil)
	ctx := context.Background()

	first, err := f.Fetch(ctx, config.FeedSituations)
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	firstBody, err := io.ReadAll(first.Body())
	if err != nil {
		t.Fatalf("reading first body: %v", err)
	}
	if string(firstBody) != testFeedBody {
		t.Errorf("first body = %q, want %q", firstBody, testFeedBody)
	}
	if first.FromCache {
		t.Error("first fetch should be a cache miss")
	}
	if first.Status != http.StatusOK {
		t.Errorf("first Status = %d, want 200", first.Status)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	second, err := f.Fetch(ctx, config.FeedSituations)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	defer second.Close()
	secondBody, err := io.ReadAll(second.Body())
	if err != nil {
		t.Fatalf("reading second body: %v", err)
	}
	if string(secondBody) != testFeedBody {
		t.Errorf("second body = %q, want %q", secondBody, testFeedBody)
	}
	if !second.FromCache {
		t.Error("second fetch within 60s should come from cache")
	}
	if second.Status != 0 {
		t.Errorf("cache hit Status = %d, want 0", second.Status)
	}
	if requests != 1 {
		t.Errorf("server received %d requests, want exactly 1", requests)
	}
}

// An entry older than the 60 s TTL triggers a refetch.
func TestFetchExpiredEntryRefetches(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(testFeedBody))
	}))
	defer srv.Close()

	now := time.Now()
	f := newTestFetcher(t, srv.URL, &now)
	ctx := context.Background()

	first, err := f.Fetch(ctx, config.FeedSituations)
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	now = now.Add(61 * time.Second)

	second, err := f.Fetch(ctx, config.FeedSituations)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	defer second.Close()
	if second.FromCache {
		t.Error("expired entry must trigger a refetch, not a cache hit")
	}
	if requests != 2 {
		t.Errorf("server received %d requests, want 2", requests)
	}
}

// §5.6: if the network fails and there is a cache entry (even expired), the
// command gets the cached body with no error, FromCache and Stale, plus a
// warning for stderr.
func TestFetchNetworkFailureFallsBackToCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(testFeedBody))
	}))
	now := time.Now()
	f := newTestFetcher(t, srv.URL, &now)
	ctx := context.Background()

	first, err := f.Fetch(ctx, config.FeedSituations)
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// The URL is dead from now on: transport errors only. The cache has also
	// expired, so the fetch really tries the network.
	srv.Close()
	now = now.Add(61 * time.Second)

	result, err := f.Fetch(ctx, config.FeedSituations)
	if err != nil {
		t.Fatalf("Fetch after network failure should fall back to cache, got error: %v", err)
	}
	defer result.Close()
	if !result.FromCache {
		t.Error("fallback result should be FromCache")
	}
	if !result.Stale {
		t.Error("fallback result should be Stale")
	}
	if result.Warning == "" {
		t.Error("fallback result should carry a warning for stderr")
	}
	body, err := io.ReadAll(result.Body())
	if err != nil {
		t.Fatalf("reading fallback body: %v", err)
	}
	if string(body) != testFeedBody {
		t.Errorf("fallback body = %q, want %q", body, testFeedBody)
	}
	if !result.Freshness(nil, 10*time.Minute, time.Now()).Stale {
		t.Error("fallback freshness must be stale even without a publication time")
	}
}

// §6.3: no network and no cache is exit code 6, type network.
func TestFetchNetworkFailureWithoutCacheReturnsNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // never fetched, so the cache is empty

	f := newTestFetcher(t, srv.URL, nil)
	_, err := f.Fetch(context.Background(), config.FeedSituations)
	if err == nil {
		t.Fatal("Fetch with no network and no cache should fail")
	}
	var target *errs.Error
	if !errors.As(err, &target) {
		t.Fatalf("error type = %T, want *errs.Error", err)
	}
	if target.Type != "network" {
		t.Errorf("error type = %q, want network", target.Type)
	}
	if got := errs.ExitCodeFor(err); got != errs.ExitNetwork {
		t.Errorf("exit code = %d, want 6", got)
	}
	// errs.NewNetwork has no feed field; the feed name travels in the message.
	if !strings.Contains(err.Error(), config.FeedSituations) {
		t.Errorf("error message should name the feed %q, got %q", config.FeedSituations, err.Error())
	}
}

func TestFetchRateLimitedReturnsRateLimitedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv.URL, nil)
	_, err := f.Fetch(context.Background(), config.FeedSituations)
	if err == nil {
		t.Fatal("Fetch on 429 should fail")
	}
	var target *errs.Error
	if !errors.As(err, &target) || target.Type != "rate_limited" {
		t.Fatalf("error = %v, want type rate_limited", err)
	}
	if got := errs.ExitCodeFor(err); got != errs.ExitUpstream {
		t.Errorf("exit code = %d, want 5", got)
	}
}

func TestFetchUpstreamUnavailableReturnsUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv.URL, nil)
	_, err := f.Fetch(context.Background(), config.FeedSituations)
	if err == nil {
		t.Fatal("Fetch on 503 should fail")
	}
	if got := errs.ExitCodeFor(err); got != errs.ExitUpstream {
		t.Errorf("exit code = %d, want 5", got)
	}
	var target *errs.Error
	if !errors.As(err, &target) || target.Type != "upstream_unavailable" {
		t.Fatalf("error = %v, want type upstream_unavailable", err)
	}
}

func TestFetchNotFoundReturnsFeedChangedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv.URL, nil)
	_, err := f.Fetch(context.Background(), config.FeedSituations)
	if err == nil {
		t.Fatal("Fetch on 404 should fail")
	}
	var target *errs.Error
	if !errors.As(err, &target) || target.Type != "feed_changed" {
		t.Fatalf("error = %v, want type feed_changed", err)
	}
	if got := errs.ExitCodeFor(err); got != errs.ExitFeedChanged {
		t.Errorf("exit code = %d, want 7", got)
	}
}

// The same §5.6 rule applies when the upstream answers 5xx while the cache
// holds usable data.
func TestFetchServerErrorFallsBackToCache(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			_, _ = w.Write([]byte(testFeedBody))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	now := time.Now()
	f := newTestFetcher(t, srv.URL, &now)
	ctx := context.Background()

	first, err := f.Fetch(ctx, config.FeedSituations)
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	now = now.Add(61 * time.Second) // cache expired, so the fetch hits the 503

	result, err := f.Fetch(ctx, config.FeedSituations)
	if err != nil {
		t.Fatalf("Fetch on 503 with cache present should fall back, got error: %v", err)
	}
	defer result.Close()
	if !result.FromCache || !result.Stale {
		t.Errorf("fallback result FromCache/Stale = %v/%v, want true/true", result.FromCache, result.Stale)
	}
	if result.Warning == "" {
		t.Error("fallback result should carry a warning for stderr")
	}
}

// The gzip negotiation of the client also applies through the whole fetch, and
// the cache stores the plain bytes.
func TestFetchDecodesGzipBody(t *testing.T) {
	srv := httptest.NewServer(gzipHandler(testFeedBody))
	defer srv.Close()

	f := newTestFetcher(t, srv.URL, nil)
	result, err := f.Fetch(context.Background(), config.FeedSituations)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	defer result.Close()
	body, err := io.ReadAll(result.Body())
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(body) != testFeedBody {
		t.Errorf("body = %q, want %q", body, testFeedBody)
	}
}

// P-08: staleness comes only from the publication time of the feed.
func TestComputeFreshness(t *testing.T) {
	now := time.Now()
	maxAge := 10 * time.Minute

	old := now.Add(-20 * time.Minute)
	fresh := ComputeFreshness(&old, maxAge, now)
	if !fresh.Stale {
		t.Error("a publication 20 minutes old with max_age 10m must be stale")
	}
	if fresh.PublishedAt == nil || !fresh.PublishedAt.Equal(old) {
		t.Errorf("PublishedAt = %v, want %v", fresh.PublishedAt, old)
	}
	if fresh.AgeSeconds == nil || *fresh.AgeSeconds != 1200 {
		t.Errorf("AgeSeconds = %v, want 1200", fresh.AgeSeconds)
	}

	recent := now.Add(-2 * time.Minute)
	notStale := ComputeFreshness(&recent, maxAge, now)
	if notStale.Stale {
		t.Error("a publication 2 minutes old with max_age 10m must not be stale")
	}
	if notStale.AgeSeconds == nil || *notStale.AgeSeconds != 120 {
		t.Errorf("AgeSeconds = %v, want 120", notStale.AgeSeconds)
	}

	// Several feeds carry no publicationTime (docs/feeds.md §9): do not invent
	// freshness.
	missing := ComputeFreshness(nil, maxAge, now)
	if missing.PublishedAt != nil {
		t.Errorf("PublishedAt = %v, want nil", missing.PublishedAt)
	}
	if missing.AgeSeconds != nil {
		t.Errorf("AgeSeconds = %v, want nil", missing.AgeSeconds)
	}
	if missing.Stale {
		t.Error("a feed without publication time must not be reported stale")
	}
}

// A network-failure fallback must be marked stale even when the publication
// time itself looks recent: the agent must never present old data as "now".
func TestResultFreshnessFallbackForcesStale(t *testing.T) {
	now := time.Now()
	recent := now.Add(-30 * time.Second)

	fallback := &Result{FetchedAt: now, FromCache: true, Stale: true}
	if got := fallback.Freshness(&recent, 10*time.Minute, now); !got.Stale {
		t.Error("fallback result must be stale even with a recent publication time")
	}

	normal := &Result{FetchedAt: now}
	if got := normal.Freshness(&recent, 10*time.Minute, now); got.Stale {
		t.Error("a normal result with a recent publication time must not be stale")
	}
}

func TestFetchUnknownFeedName(t *testing.T) {
	f := newTestFetcher(t, "http://unused.test", nil)
	_, err := f.Fetch(context.Background(), "no-such-feed")
	if err == nil {
		t.Fatal("Fetch with an unknown feed name should fail")
	}
	if got := errs.ExitCodeFor(err); got != errs.ExitUsage {
		t.Errorf("exit code = %d, want 2", got)
	}
}

// truncatingHandler answers 200 with a Content-Length that promises more bytes
// than it sends, then drops the connection: the realistic shape of a failure
// on a 7 MB download over a bad link.
func truncatingHandler(requests *int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests++
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<d2:payload>a medias"))
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	})
}

// Regression for review blocker 1: a connection that dies mid-body is a
// transport failure, so it must reach the cache fallback instead of surfacing
// as an unexpected internal error (exit 1).
func TestFetchMidBodyFailureFallsBackToCache(t *testing.T) {
	f := newTestFetcher(t, "http://example.invalid/feed", nil)
	ctx := context.Background()

	// An expired entry from an earlier download: old enough that a refetch is due.
	expiredAt := time.Now().Add(-time.Hour)
	if _, err := f.Cache.Store(config.FeedSituations, "http://example.invalid/feed",
		strings.NewReader(testFeedBody), expiredAt); err != nil {
		t.Fatalf("seeding the cache: %v", err)
	}

	requests := 0
	srv := httptest.NewServer(truncatingHandler(&requests))
	defer srv.Close()
	f.URLs[config.FeedSituations] = srv.URL

	res, err := f.Fetch(ctx, config.FeedSituations)
	if err != nil {
		t.Fatalf("a mid-body failure with usable cache must not fail, got %v", err)
	}
	if !res.FromCache || !res.Stale {
		t.Fatalf("result FromCache=%v Stale=%v, want both true", res.FromCache, res.Stale)
	}
	if res.Warning == "" {
		t.Error("serving stale data after a failure must warn the caller")
	}
	defer res.Close()
	body, err := io.ReadAll(res.Body())
	if err != nil {
		t.Fatalf("reading the cached body: %v", err)
	}
	if string(body) != testFeedBody {
		t.Errorf("cached body = %q, want %q", body, testFeedBody)
	}
}

// The same failure with no cache must be a network error (exit 6), never the
// internal exit code.
func TestFetchMidBodyFailureWithNoCacheIsNetworkError(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(truncatingHandler(&requests))
	defer srv.Close()

	f := newTestFetcher(t, srv.URL, nil)

	_, err := f.Fetch(context.Background(), config.FeedSituations)
	if err == nil {
		t.Fatal("a mid-body failure with no cache must fail")
	}
	if got := errs.ExitCodeFor(err); got != errs.ExitNetwork {
		t.Fatalf("exit code = %d, want %d (network)", got, errs.ExitNetwork)
	}
}

// Regression for review blocker 2: an empty 200 must not be cached, because a
// poisoned entry would be served as a successful download on the next call.
func TestFetchEmptyBodyDoesNotPoisonCache(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv.URL, nil)
	ctx := context.Background()

	if _, err := f.Fetch(ctx, config.FeedSituations); err == nil {
		t.Fatal("an empty body must fail with feed_changed")
	} else if got := errs.ExitCodeFor(err); got != errs.ExitFeedChanged {
		t.Fatalf("exit code = %d, want %d (feed_changed)", got, errs.ExitFeedChanged)
	}

	// The next call inside the TTL must go back to the server and fail again,
	// not answer an empty body as a cache hit.
	_, err := f.Fetch(ctx, config.FeedSituations)
	if err == nil {
		t.Fatal("the second call served the empty body as a success: the cache was poisoned")
	}
	if got := errs.ExitCodeFor(err); got != errs.ExitFeedChanged {
		t.Fatalf("second exit code = %d, want %d (feed_changed)", got, errs.ExitFeedChanged)
	}
	if requests != 2 {
		t.Fatalf("server requests = %d, want 2: a rejected body must not satisfy the cache", requests)
	}
}

// A cache entry that looks future-dated after a clock jump must not be
// reported as fresh for its full TTL plus the jump.
func TestCacheAgeClampsNegativeClock(t *testing.T) {
	cache := NewCache(t.TempDir())
	entry := Entry{FetchedAt: time.Now().Add(time.Hour)}

	if age := cache.Age(entry, time.Now()); age != 0 {
		t.Errorf("Age() of a future-dated entry = %v, want 0", age)
	}
	if cache.Fresh(entry, time.Now()) {
		t.Error("a future-dated entry must not be treated as fresh: it is refetched instead")
	}
}
