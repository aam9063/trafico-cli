package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/output"
)

// doctorPaths builds a Paths value inside a temp directory. When withConfig
// is true it writes a valid 0600 config file so the config section reports
// exists, loaded and permissions ok.
func doctorPaths(t *testing.T, withConfig bool, srvURL string) config.Paths {
	t.Helper()
	base := t.TempDir()
	paths := config.Paths{
		ConfigPath: filepath.Join(base, "trafico-cli", "config.toml"),
		DataPath:   filepath.Join(base, "trafico-cli", "trafico.db"),
		CacheDir:   filepath.Join(base, "trafico-cli", "cache"),
	}
	if withConfig {
		if err := os.MkdirAll(filepath.Dir(paths.ConfigPath), 0o700); err != nil {
			t.Fatalf("creating the config directory: %v", err)
		}
		content := "[feeds.dgt-situations]\nurl = \"" + srvURL + "/situations\"\nenabled = true\n"
		if err := os.WriteFile(paths.ConfigPath, []byte(content), 0o600); err != nil {
			t.Fatalf("writing the config file: %v", err)
		}
	}
	return paths
}

func TestDoctorReportsHealthySetup(t *testing.T) {
	body := snapshotFeed(t)
	srv, _ := newFeedServer(t, map[string]http.HandlerFunc{
		"/situations": func(w http.ResponseWriter, r *http.Request) { w.Write(body) },
	})
	paths := doctorPaths(t, true, srv.URL)
	cfg, err := config.Load(paths.ConfigPath)
	if err != nil {
		t.Fatalf("loading the test config: %v", err)
	}
	fetcher := feeds.NewFetcher(cfg, paths.CacheDir, "test")

	report, err := Doctor(context.Background(), cfg, paths, fetcher)
	if err != nil {
		t.Fatalf("Doctor must not fail on a healthy setup: %v", err)
	}
	if !report.OK {
		t.Errorf("doctor reported ok = false on a healthy setup: config %+v, feeds %+v", report.Config, report.Feeds)
	}
	if !report.Config.Exists || !report.Config.Loaded || !report.Config.PermissionsOK {
		t.Errorf("config section = %+v, want exists, loaded and permissions ok", report.Config)
	}
	if report.Config.Path != paths.ConfigPath {
		t.Errorf("config path = %q, want %q", report.Config.Path, paths.ConfigPath)
	}
	if report.NotOfficial == "" {
		t.Error("doctor must carry the not-an-official-source warning (ARRANQUE §13)")
	}
	if report.Cache.Entries != 1 {
		t.Errorf("cache entries = %d, want 1 (the freshly fetched feed)", report.Cache.Entries)
	}
	if report.Cache.TotalBytes <= 0 {
		t.Errorf("cache total bytes = %d, want > 0", report.Cache.TotalBytes)
	}
	if report.Cache.Dir != paths.CacheDir {
		t.Errorf("cache dir = %q, want %q", report.Cache.Dir, paths.CacheDir)
	}
	if report.SchemaVersion != output.SchemaVersion {
		t.Errorf("schema_version = %d, want %d", report.SchemaVersion, output.SchemaVersion)
	}
	if report.Attribution.Source == "" {
		t.Error("doctor output must carry attribution (P-05)")
	}
	// config.Load merges the file over the compiled defaults, so the report
	// lists all six feeds: one enabled and healthy, five disabled.
	if len(report.Feeds) != 6 {
		t.Fatalf("feeds section = %d entries, want the 6 configured feeds", len(report.Feeds))
	}
	for _, status := range report.Feeds {
		if status.Name == config.FeedSituations && status.Status != FeedStatusOK {
			t.Errorf("dgt-situations status = %q, want %q", status.Status, FeedStatusOK)
		}
		if status.Name != config.FeedSituations && status.Status != FeedStatusDisabled {
			t.Errorf("%s status = %q, want %q", status.Name, status.Status, FeedStatusDisabled)
		}
	}
}

func TestDoctorFailingFeedTurnsOKFalseWithoutFailingCommand(t *testing.T) {
	body := snapshotFeed(t)
	srv, _ := newFeedServer(t, map[string]http.HandlerFunc{
		"/situations": func(w http.ResponseWriter, r *http.Request) { w.Write(body) },
		"/broken":     func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) },
	})
	paths := doctorPaths(t, true, srv.URL)
	cfg, err := config.Load(paths.ConfigPath)
	if err != nil {
		t.Fatalf("loading the test config: %v", err)
	}
	// Point a second enabled feed at the failing endpoint.
	cfg.Feeds[config.FeedRadars] = config.Feed{URL: srv.URL + "/broken", Enabled: true}
	fetcher := feeds.NewFetcher(cfg, paths.CacheDir, "test")

	report, err := Doctor(context.Background(), cfg, paths, fetcher)
	if err != nil {
		t.Fatalf("doctor must not fail because one feed is down: %v", err)
	}
	if report.OK {
		t.Error("doctor must report ok = false when an enabled feed fails")
	}
	found := false
	for _, status := range report.Feeds {
		if status.Name == config.FeedRadars {
			found = true
			if status.Status != FeedStatusError {
				t.Errorf("dgt-radars status = %q, want %q", status.Status, FeedStatusError)
			}
		}
	}
	if !found {
		t.Error("doctor must report every enabled feed, including the failing one")
	}
}

func TestDoctorWithoutUsableConfigReportsProblemAndStaysAlive(t *testing.T) {
	// A corrupt config file is what makes the caller pass a nil config:
	// doctor must report the problem (ok = false) without failing.
	paths := doctorPaths(t, false, "http://127.0.0.1:1")
	if err := os.MkdirAll(filepath.Dir(paths.ConfigPath), 0o700); err != nil {
		t.Fatalf("creating the config directory: %v", err)
	}
	if err := os.WriteFile(paths.ConfigPath, []byte("esto no es toml {{{"), 0o600); err != nil {
		t.Fatalf("writing the broken config file: %v", err)
	}

	report, err := Doctor(context.Background(), nil, paths, nil)
	if err != nil {
		t.Fatalf("doctor must not fail when the config is corrupt: %v", err)
	}
	if report.OK {
		t.Error("doctor must report ok = false without a usable config")
	}
	if !report.Config.Exists {
		t.Error("config.exists must be true for a corrupt file")
	}
	if report.Config.Loaded {
		t.Error("config.loaded must be false for a corrupt file")
	}
	if report.Config.Error == nil || *report.Config.Error == "" {
		t.Error("config.error must carry the load error")
	}
	if len(report.Feeds) != 0 {
		t.Errorf("feeds section must stay empty without a config, got %d entries", len(report.Feeds))
	}
}

func TestDoctorWithMissingConfigUsesDefaultsAndStaysHealthy(t *testing.T) {
	// A missing config file is not a problem (P-03): the compiled defaults
	// apply and trafico works out of the box, so ok stays true.
	paths := doctorPaths(t, false, "http://127.0.0.1:1")

	report, err := Doctor(context.Background(), nil, paths, nil)
	if err != nil {
		t.Fatalf("doctor must not fail without a config file: %v", err)
	}
	if !report.OK {
		t.Error("ok must stay true: nothing critical is broken with the compiled defaults")
	}
	if report.Config.Exists {
		t.Error("config.exists must be false for a missing file")
	}
	if !report.Config.Loaded {
		t.Error("config.loaded must be true: the defaults loaded fine")
	}
	if report.Config.Error != nil {
		t.Errorf("config.error must be null without a load error, got %q", *report.Config.Error)
	}
}

// TestDoctorMetaReflectsCacheFallback is the regression for the aggregation gap
// found after the CMD-02 correction round: doctor and `feeds status` present
// the same facts, so they must not disagree about whether the served data came
// from the cache. A stale fallback has to surface in meta, not only in the
// per-feed entry.
func TestDoctorMetaReflectsCacheFallback(t *testing.T) {
	body := snapshotFeed(t)
	srv, _ := newFeedServer(t, map[string]http.HandlerFunc{
		"/situations": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) },
	})
	paths := doctorPaths(t, true, srv.URL)
	cfg, err := config.Load(paths.ConfigPath)
	if err != nil {
		t.Fatalf("loading the test config: %v", err)
	}
	fetcher := feeds.NewFetcher(cfg, paths.CacheDir, "test")

	// One successful download fills the cache; then the network goes away.
	seed, err := fetcher.Fetch(context.Background(), config.FeedSituations)
	if err != nil {
		t.Fatalf("seeding the cache: %v", err)
	}
	seed.Close()
	srv.Close()

	// Past the 60 s TTL so doctor must refetch, fail, and fall back to the cache.
	fetcher.Now = func() time.Time { return time.Now().Add(5 * time.Minute) }

	report, err := Doctor(context.Background(), cfg, paths, fetcher)
	if err != nil {
		t.Fatalf("Doctor must not fail when a feed falls back to the cache: %v", err)
	}
	if !report.Meta.FromCache {
		t.Error("meta.from_cache = false while the only enabled feed was served from the cache")
	}
	if !report.Meta.Stale {
		t.Error("meta.stale = false while the served data are stale")
	}
	if report.Meta.FetchedAt.Location() != time.UTC {
		t.Errorf("meta.fetched_at location = %v, want UTC", report.Meta.FetchedAt.Location())
	}
	if report.Meta.AgeSeconds == nil {
		t.Error("meta.age_seconds must be derived when a publication time is known")
	}
}
