package app

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/datex2"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/output"
)

// DeriveMeta builds the envelope meta shared by every multi-feed command
// (`feeds status`, `doctor`): the newest publication time across the feeds,
// its age, and — only when fetched is true — stale and from_cache set when any
// feed carries them.
//
// The fetched flag exists because a command that checked nothing must not
// claim the data are fresh: `feeds list` reads config only, so it passes
// false and keeps stale and from_cache at their zero value deliberately.
//
// It lives here, not in the CLI, so the aggregation cannot drift between the
// two commands that present the same facts.
func DeriveMeta(list []FeedStatus, fetchedAt time.Time, fetched bool) *output.Meta {
	meta := &output.Meta{FetchedAt: fetchedAt.UTC(), Total: len(list)}
	if fetched {
		for _, status := range list {
			if status.Stale {
				meta.Stale = true
			}
			if status.FromCache {
				meta.FromCache = true
			}
		}
	}
	for _, status := range list {
		if status.PublishedAt != nil && (meta.PublishedAt == nil || status.PublishedAt.After(*meta.PublishedAt)) {
			meta.PublishedAt = status.PublishedAt
		}
	}
	// P-08: age_seconds is derived from the publication time we already have,
	// so a reader never has to subtract dates to know how old the data are.
	if meta.PublishedAt != nil {
		age := int64(meta.FetchedAt.Sub(*meta.PublishedAt).Seconds())
		if age < 0 {
			age = 0
		}
		meta.AgeSeconds = &age
	}
	return meta
}

// Lifecycle states of FeedStatus.Status (CMD-02). Disabled feeds are never
// fetched: spending a request on a feed the user turned off would be waste.
const (
	FeedStatusOK       = "ok"
	FeedStatusEnabled  = "enabled"
	FeedStatusDisabled = "disabled"
	FeedStatusError    = "error"
)

// FeedStatus is the per-feed health line of `trafico feeds` and `doctor`
// (docs/ARRANQUE.md §5.1): URL, last download, publication time, size and
// state. Nullable fields stay null when the feed does not carry the value:
// freshness is never invented (P-04, P-08).
type FeedStatus struct {
	// Name is the config key of the feed (e.g. "dgt-situations").
	Name string `json:"name"`
	// Enabled reports whether the user turned the feed on.
	Enabled bool `json:"enabled"`
	// URL is the source URL of the feed.
	URL string `json:"url"`
	// FetchedAt is when the body was downloaded from the source; null when
	// the feed was never fetched or is disabled and has no cache entry.
	FetchedAt *time.Time `json:"fetched_at"`
	// PublishedAt is the publication time the feed itself declares; null
	// when the feed carries none (docs/feeds.md §9) or no parser exists
	// for its dialect yet.
	PublishedAt *time.Time `json:"published_at"`
	// AgeSeconds is now - PublishedAt; null when PublishedAt is null.
	AgeSeconds *int64 `json:"age_seconds"`
	// Stale reports that the data are older than [stale] max_age, or that
	// a cache fallback served them after a network failure.
	Stale bool `json:"stale"`
	// FromCache reports whether the served body came from the shared cache.
	FromCache bool `json:"from_cache"`
	// SizeBytes is the size of the raw body in bytes; null when unknown.
	SizeBytes *int64 `json:"size_bytes"`
	// Status is the state of the feed. `feeds list` never checks anything, so
	// it reports only enabled or disabled; `feeds status` and doctor actually
	// fetch the feed and report ok, disabled or error.
	Status string `json:"status"`
	// Error carries the user-facing message of a failed fetch; null when
	// the feed is healthy or disabled.
	Error *string `json:"error"`
	// Warning carries the stderr note of a cache fallback after a network
	// failure; null otherwise.
	Warning *string `json:"warning"`
}

// FeedsList returns every configured feed with its URL and enabled state,
// enriched with local cache metadata. It never touches the network: it is
// the static view of `trafico feeds list`.
func FeedsList(_ context.Context, cfg *config.Config, fetcher *feeds.Fetcher) ([]FeedStatus, error) {
	if cfg == nil {
		return nil, errs.NewInternal("no hay configuración que listar")
	}
	list := make([]FeedStatus, 0, len(cfg.Feeds))
	for _, name := range sortedFeedNames(cfg) {
		feed := cfg.Feeds[name]
		status := FeedStatus{Name: name, Enabled: feed.Enabled, URL: feed.URL}
		// feeds list makes no requests (CMD-02), so "ok" would read as verified
		// health: it reports the enabled state, not checked health.
		if feed.Enabled {
			status.Status = FeedStatusEnabled
		} else {
			status.Status = FeedStatusDisabled
		}
		fillCacheMetadata(fetcher, name, &status)
		list = append(list, status)
	}
	return list, nil
}

// FeedsStatus fetches every enabled feed and reports freshness and errors
// per feed. A feed whose fetch fails must not fail the whole command: `feeds
// status` is a diagnostic, and a command that reports the health of
// everything has to survive one sick entry, so per-feed failures are
// recorded with status "error" and the command returns no error.
func FeedsStatus(ctx context.Context, cfg *config.Config, fetcher *feeds.Fetcher) ([]FeedStatus, error) {
	if cfg == nil {
		return nil, errs.NewInternal("no hay configuración que comprobar")
	}
	if fetcher == nil {
		return nil, errs.NewInternal("no hay descargador de feeds configurado")
	}
	list := make([]FeedStatus, 0, len(cfg.Feeds))
	for _, name := range sortedFeedNames(cfg) {
		status, err := FeedStatusFor(ctx, cfg, fetcher, name)
		if err != nil {
			return nil, err
		}
		list = append(list, status)
	}
	return list, nil
}

// FeedStatusFor builds the live status of one feed: disabled feeds are not
// fetched, a failed fetch is recorded with status "error" (never fatal),
// and a healthy feed reports its fetch time, publication time, age, size and
// staleness.
func FeedStatusFor(ctx context.Context, cfg *config.Config, fetcher *feeds.Fetcher, name string) (FeedStatus, error) {
	if cfg == nil {
		return FeedStatus{}, errs.NewInternal("no hay configuración que comprobar")
	}
	feed, ok := cfg.Feeds[name]
	if !ok {
		return FeedStatus{}, errs.NewUsage(fmt.Sprintf("feed desconocido: %q (no está en la configuración)", name))
	}
	status := FeedStatus{Name: name, Enabled: feed.Enabled, URL: feed.URL}
	if !feed.Enabled {
		status.Status = FeedStatusDisabled
		fillCacheMetadata(fetcher, name, &status)
		return status, nil
	}
	if fetcher == nil {
		return FeedStatus{}, errs.NewInternal("no hay descargador de feeds configurado")
	}

	result, err := fetcher.Fetch(ctx, name)
	if err != nil {
		status.Status = FeedStatusError
		message := err.Error()
		status.Error = &message
		// A cache fallback may have served the body anyway: show it.
		fillCacheMetadata(fetcher, name, &status)
		return status, nil
	}

	publishedAt, parseErr := publicationTime(name, result.Body())
	result.Close()
	if parseErr != nil {
		// The body could not be interpreted: record the per-feed error instead
		// of failing the command. Whether the body was served live or from the
		// cache must stay visible (P-08): a fallback serve is never presented
		// as a fresh answer from the feed.
		status.Status = FeedStatusError
		var message string
		if result.FromCache {
			message = fmt.Sprintf("se sirven los datos guardados en caché del feed %q pero su contenido no se pudo interpretar: %v", name, parseErr)
		} else {
			message = fmt.Sprintf("el feed %q respondió pero su contenido no se pudo interpretar: %v", name, parseErr)
		}
		status.Error = &message
		status.FromCache = result.FromCache
		status.Stale = result.Stale
		if result.Warning != "" {
			warning := result.Warning
			status.Warning = &warning
		}
		fillCacheMetadata(fetcher, name, &status)
		return status, nil
	}

	freshness := result.Freshness(publishedAt, fetcher.MaxAge, fetcher.Now())
	status.Status = FeedStatusOK
	status.PublishedAt = utcTimePtr(freshness.PublishedAt)
	status.AgeSeconds = freshness.AgeSeconds
	status.Stale = freshness.Stale
	status.FromCache = result.FromCache
	status.FetchedAt = utcTimePtr(&result.FetchedAt)
	if result.Warning != "" {
		warning := result.Warning
		status.Warning = &warning
	}
	fillCacheMetadata(fetcher, name, &status)
	return status, nil
}

// publicationTime extracts the publication time of a feed body. Only
// internal/datex2 knows the XML (AGENTS.md), and today it only parses the
// situations dialect: every other feed reports a null published_at until its
// parser lands (FEED-03/04/05), as docs/feeds.md §9 anticipated.
func publicationTime(feedName string, body io.Reader) (*time.Time, error) {
	if feedName != config.FeedSituations {
		return nil, nil
	}
	publication, err := datex2.ParseSituations(body)
	if err != nil {
		return nil, err
	}
	if publication.PublishedAt.IsZero() {
		return nil, nil
	}
	return &publication.PublishedAt, nil
}

// fillCacheMetadata enriches a status with the local cache entry of the
// feed, if any: last download time and body size. It never touches the
// network, so `feeds list` stays a zero-request command.
func fillCacheMetadata(fetcher *feeds.Fetcher, name string, status *FeedStatus) {
	if fetcher == nil || fetcher.Cache == nil {
		return
	}
	body, entry, err := fetcher.Cache.Load(name)
	if err != nil {
		return
	}
	body.Close()
	status.FetchedAt = utcTimePtr(&entry.FetchedAt)
	size := entry.Size
	status.SizeBytes = &size
}

// utcTimePtr returns a copy of t in UTC, or nil when t is nil. Every
// *time.Time stored on FeedStatus goes through here so the dates serialise
// as ISO 8601 UTC (ARRANQUE §6.1) no matter where they were created.
func utcTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}

// sortedFeedNames returns the configured feed names in a stable order so the
// JSON output is deterministic.
func sortedFeedNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Feeds))
	for name := range cfg.Feeds {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
