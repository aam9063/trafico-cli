package feeds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
)

// Fetcher is the public API of the package: it resolves feed names to URLs,
// downloads them politely through HTTPClient, shares downloads through the
// 60 s Cache (P-06) and reports freshness (P-08). It neither parses the XML
// (internal/datex2 does) nor prints anything.
type Fetcher struct {
	// Client performs the HTTP downloads.
	Client *HTTPClient
	// Cache is the shared on-disk cache under config.Paths.CacheDir.
	Cache *Cache
	// URLs maps every configured feed name to its URL.
	URLs map[string]string
	// MaxAge is the staleness threshold of P-08 (config [stale] max_age,
	// default 10 minutes).
	MaxAge time.Duration
	// Now returns the current time; tests replace it with a fixed clock.
	Now func() time.Time
}

// NewFetcher builds a Fetcher from the configuration: one entry per configured
// feed, the cache under cacheDir and the staleness threshold of
// cfg.Stale.MaxAge (config.DefaultMaxAge when unset).
func NewFetcher(cfg *config.Config, cacheDir, version string) *Fetcher {
	urls := make(map[string]string, len(cfg.Feeds))
	for name, feed := range cfg.Feeds {
		if strings.TrimSpace(feed.URL) != "" {
			urls[name] = feed.URL
		}
	}
	maxAge := time.Duration(cfg.Stale.MaxAge)
	if maxAge <= 0 {
		maxAge = time.Duration(config.DefaultMaxAge)
	}
	return &Fetcher{
		Client: NewHTTPClient(version),
		Cache:  NewCache(cacheDir),
		URLs:   urls,
		MaxAge: maxAge,
		Now:    time.Now,
	}
}

// Result is one served feed: the raw bytes as a stream, when they were
// downloaded, where they came from, and how fresh they are.
type Result struct {
	// Feed is the feed name the result was requested under.
	Feed string
	// URL is the source URL of the body.
	URL string
	// Status is the HTTP status of the download; 0 when the body came from
	// the cache.
	Status int
	// FetchedAt is when the bytes were downloaded from the source (not when
	// this command served them).
	FetchedAt time.Time
	// FromCache reports whether the body came from the shared cache.
	FromCache bool
	// Stale reports that the body may be much older than usual: true only
	// when the network failed and an expired cache entry was served instead
	// (ARRANQUE §5.6). A normal cache hit inside the 60 s TTL is not stale
	// for this reason; staleness of the data itself is what Freshness
	// computes from the publication time.
	Stale bool
	// Warning is a note the command must write to stderr (P-01: stdout stays
	// pure JSON). It is empty unless the result comes from the cache
	// fallback.
	Warning string

	body io.ReadCloser
}

// Body returns a reader over the raw feed bytes. The caller reads it fully and
// calls Close.
func (r *Result) Body() io.ReadCloser { return r.body }

// Close releases the body reader. It is safe to call more than once.
func (r *Result) Close() error {
	if r.body == nil {
		return nil
	}
	err := r.body.Close()
	r.body = nil
	return err
}

// Freshness describes P-08: when the data were published and whether they are
// older than config.Stale.MaxAge. PublishedAt and AgeSeconds are nil when the
// feed carries no publication time (several DGT feeds do not, docs/feeds.md
// §9): freshness is never invented.
type Freshness struct {
	// PublishedAt is the publication time of the feed, or nil when the feed
	// carries none.
	PublishedAt *time.Time
	// AgeSeconds is the age of the data in seconds (now - PublishedAt), or
	// nil when the feed carries no publication time.
	AgeSeconds *int64
	// Stale reports whether the data are older than maxAge.
	Stale bool
}

// ComputeFreshness derives P-08 freshness from the publication time carried by
// the feed itself, the only valid source (docs/feeds.md §2 and §9). A nil or
// zero publication time yields a freshness with everything unset and Stale
// false: a feed without publicationTime is not stale, it is undated.
func ComputeFreshness(publishedAt *time.Time, maxAge time.Duration, now time.Time) Freshness {
	if publishedAt == nil || publishedAt.IsZero() {
		return Freshness{}
	}
	age := int64(now.Sub(*publishedAt).Seconds())
	freshness := Freshness{PublishedAt: publishedAt, AgeSeconds: &age}
	freshness.Stale = now.Sub(*publishedAt) > maxAge
	return freshness
}

// Freshness computes the P-08 freshness of this result. publishedAt is what
// internal/datex2 parsed from <com:publicationTime> of the body (nil when the
// feed carries none). A cache fallback after a network failure is always
// stale, regardless of the publication time: the agent must never present old
// data as "now" (P-08).
func (r *Result) Freshness(publishedAt *time.Time, maxAge time.Duration, now time.Time) Freshness {
	freshness := ComputeFreshness(publishedAt, maxAge, now)
	if r.Stale {
		freshness.Stale = true
	}
	return freshness
}

// Fetch returns the current body of the named feed (ARRANQUE §5.6, §6.3):
//
//   - a cache entry inside the 60 s TTL answers without the network (P-06);
//   - otherwise the feed is downloaded and stored in the cache;
//   - if the download fails (transport error, 429 or 5xx) and a cache entry
//     exists — even expired — the cached body is returned with no error,
//     FromCache and Stale set and a Warning for stderr: a command must never
//     fail while it holds usable data;
//   - 429 maps to rate_limited (exit 5), 5xx to upstream_unavailable (exit 5),
//     404 or an unexpected shape to feed_changed (exit 7) and no network with
//     no cache to network (exit 6). Every error names the feed.
func (f *Fetcher) Fetch(ctx context.Context, name string) (*Result, error) {
	url, ok := f.URLs[name]
	if !ok {
		return nil, errs.NewUsage(fmt.Sprintf("feed desconocido: %q (no está en la configuración)", name))
	}
	now := f.Now()

	// P-06: the 60 s shared cache answers repeated commands with one download.
	if body, entry, err := f.Cache.Load(name); err == nil {
		if f.Cache.Fresh(entry, now) {
			return &Result{
				Feed:      name,
				URL:       url,
				FetchedAt: entry.FetchedAt,
				FromCache: true,
				body:      body,
			}, nil
		}
		// Expired: drop the reader; a refetch will overwrite the entry.
		body.Close()
	}

	resp, err := f.Client.Get(ctx, url)
	if err != nil {
		return f.fallbackAfterFailure(name, url, now, errs.NewNetwork(
			fmt.Sprintf("no hay conexión con %s para el feed %q: %v", url, name, err)))
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return f.fallbackAfterFailure(name, url, now, errs.NewRateLimited(
			fmt.Sprintf("la DGT está limitando las peticiones (HTTP 429) del feed %q; espera un minuto", name), name))
	case resp.StatusCode >= 500:
		return f.fallbackAfterFailure(name, url, now, errs.NewUpstreamUnavailable(
			fmt.Sprintf("el servidor de la DGT no responde (HTTP %d) para el feed %q", resp.StatusCode, name), name))
	case resp.StatusCode != http.StatusOK:
		detail := fmt.Sprintf("el feed %q (%s) respondió HTTP %d", name, url, resp.StatusCode)
		if resp.StatusCode == http.StatusNotFound {
			detail = fmt.Sprintf("el feed %q ya no está en %s (HTTP 404): la DGT cambió o retiró la URL", name, url)
		}
		return nil, errs.NewFeedChanged(detail)
	}

	// Stream the body straight into the cache: the incidents feed is ~7 MB per
	// minute, so it is never held whole in memory (P-06). The timestamp is taken
	// once the response headers are in, not before the download, so a slow
	// transfer cannot make the entry look fresher than it is.
	entry, err := f.Cache.Store(name, url, resp.Body, f.Now())
	switch {
	case errors.Is(err, ErrBodyRead):
		// The connection dropped mid-body. That is a transport failure, not an
		// unexpected internal error: it gets the cache fallback, or exit 6.
		return f.fallbackAfterFailure(name, url, now, errs.NewNetwork(
			fmt.Sprintf("la conexión con %s se cortó mientras se descargaba el feed %q: %v", url, name, err)))
	case errors.Is(err, ErrEmptyBody):
		// Rejected before the rename, so it never poisons the cache.
		return nil, errs.NewFeedChanged(fmt.Sprintf("el feed %q (%s) respondió sin contenido (0 bytes)", name, url))
	case err != nil:
		return nil, errs.NewInternal(fmt.Sprintf("no se pudo guardar la caché del feed %q: %v", name, err))
	}

	body, _, err := f.Cache.Load(name)
	if err != nil {
		return nil, errs.NewInternal(fmt.Sprintf("no se pudo leer la caché recién escrita del feed %q: %v", name, err))
	}
	return &Result{
		Feed:      name,
		URL:       url,
		Status:    resp.StatusCode,
		FetchedAt: entry.FetchedAt,
		body:      body,
	}, nil
}

// fallbackAfterFailure serves an existing cache entry — even an expired one —
// when the download fails (ARRANQUE §5.6). When there is no usable cache, it
// returns noCacheErr, which carries the stable type and exit code of the
// failure.
func (f *Fetcher) fallbackAfterFailure(name, url string, now time.Time, noCacheErr *errs.Error) (*Result, error) {
	body, entry, err := f.Cache.Load(name)
	if err != nil {
		return nil, noCacheErr
	}
	return &Result{
		Feed:      name,
		URL:       url,
		FetchedAt: entry.FetchedAt,
		FromCache: true,
		Stale:     true,
		Warning: fmt.Sprintf("aviso: no se pudo descargar el feed %q; se sirven los datos guardados en caché (descargados %s, hace %s).",
			name, entry.FetchedAt.Format(time.RFC3339), f.Cache.Age(entry, now).Round(time.Second)),
		body: body,
	}, nil
}
