package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/output"
)

// sectionsFixture loads one of the real trimmed DGT snapshots (public CC BY
// data) for the test server.
func sectionsFixture(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return data
}

// newSectionsFixtureServer serves the real INVIVE and TEFIVA snapshots and
// reports how many requests each path received, so tests can prove which
// feeds were (never) fetched. No test ever touches the real network
// (AGENTS.md).
func newSectionsFixtureServer(t *testing.T) (*httptest.Server, func(string) int) {
	t.Helper()
	invive := sectionsFixture(t, "../../testdata/feeds/invive/snap-1.xml")
	tefiva := sectionsFixture(t, "../../testdata/feeds/tefiva/snap-1.xml")
	srv, counts := newFeedServer(t, map[string]http.HandlerFunc{
		"/invive": func(w http.ResponseWriter, r *http.Request) { w.Write(invive) },
		"/tefiva": func(w http.ResponseWriter, r *http.Request) { w.Write(tefiva) },
	})
	return srv, counts
}

// newSectionsFetcher builds a configuration with both section feeds enabled
// and pointed at the test server.
func newSectionsFetcher(t *testing.T, srvURL string, now time.Time) (*config.Config, *feeds.Fetcher) {
	t.Helper()
	cfg := &config.Config{
		Feeds: map[string]config.Feed{
			config.FeedSectionsInvive:  {URL: srvURL + "/invive", Enabled: true},
			config.FeedSectionsAnimals: {URL: srvURL + "/tefiva", Enabled: true},
		},
		Stale: config.Stale{MaxAge: config.DefaultMaxAge},
	}
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")
	fetcher.Now = func() time.Time { return now }
	return cfg, fetcher
}

// TestSectionsFixtureCounts pins the observed counts of the real fixtures
// (docs/feeds.md §7 and §8): 3 trimmed sections per kind, every one with a
// published pk_source, and — docs/feeds.md §9 — meta.published_at null,
// never invented, with no age and not stale.
func TestSectionsFixtureCounts(t *testing.T) {
	srv, counts := newSectionsFixtureServer(t)
	cfg, fetcher := newSectionsFetcher(t, srv.URL, goldenClock)

	invive, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: model.SectionKindInvive})
	if err != nil {
		t.Fatalf("Sections invive failed: %v", err)
	}
	if len(invive.Sections) != 3 || invive.Meta.Total != 3 {
		t.Fatalf("invive = %d sections (total %d), want 3 and 3", len(invive.Sections), invive.Meta.Total)
	}
	if invive.Meta.Feed != config.FeedSectionsInvive {
		t.Errorf("meta.feed = %q, want %q", invive.Meta.Feed, config.FeedSectionsInvive)
	}
	if invive.Meta.PublishedAt != nil || invive.Meta.AgeSeconds != nil {
		t.Errorf("published_at/age = %v/%v, want null/null (docs/feeds.md §9)", invive.Meta.PublishedAt, invive.Meta.AgeSeconds)
	}
	if invive.Meta.Stale {
		t.Errorf("meta.stale = true, want false (an undated feed is not stale)")
	}
	for _, section := range invive.Sections {
		if section.Kind != model.SectionKindInvive {
			t.Errorf("section %s kind = %q, want invive", section.ID, section.Kind)
		}
		if section.PK == nil || section.PKSource == nil || *section.PKSource != model.PKSourceReference {
			t.Errorf("section %s pk/pk_source = %v/%v, want a reference-derived PK", section.ID, section.PK, section.PKSource)
		}
	}

	animals, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: model.SectionKindAnimals})
	if err != nil {
		t.Fatalf("Sections animals failed: %v", err)
	}
	if len(animals.Sections) != 3 || animals.Meta.Total != 3 {
		t.Fatalf("animals = %d sections (total %d), want 3 and 3", len(animals.Sections), animals.Meta.Total)
	}
	if animals.Meta.Feed != config.FeedSectionsAnimals {
		t.Errorf("meta.feed = %q, want %q", animals.Meta.Feed, config.FeedSectionsAnimals)
	}
	for _, section := range animals.Sections {
		if section.Kind != model.SectionKindAnimals {
			t.Errorf("section %s kind = %q, want animals", section.ID, section.Kind)
		}
		// The feed publishes the kilometre point in distanceAlong, so the
		// element is the source here; the identifier is only the fallback.
		if section.PK == nil || section.PKSource == nil || *section.PKSource != model.PKSourceReference {
			t.Errorf("section %s pk/pk_source = %v/%v, want a PK read from the published elements", section.ID, section.PK, section.PKSource)
		}
	}
	if got := counts("/invive") + counts("/tefiva"); got != 2 {
		t.Errorf("section feeds fetched %d times, want 2 (one per kind)", got)
	}
}

// TestSectionsGolden pins the full envelope of `trafico sections` per kind
// over the real fixtures, byte-compared against the golden files.
func TestSectionsGolden(t *testing.T) {
	srv, _ := newSectionsFixtureServer(t)
	cfg, fetcher := newSectionsFetcher(t, srv.URL, goldenClock)

	for _, tt := range []struct {
		kind   string
		golden string
	}{
		{model.SectionKindInvive, "testdata/sections-invive.golden.json"},
		{model.SectionKindAnimals, "testdata/sections-animals.golden.json"},
	} {
		response, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: tt.kind})
		if err != nil {
			t.Fatalf("Sections %s failed: %v", tt.kind, err)
		}
		var buf bytes.Buffer
		if err := output.Write(&buf, response, output.Options{}); err != nil {
			t.Fatalf("serialising the %s response: %v", tt.kind, err)
		}
		got := buf.Bytes()
		if *update {
			if err := os.WriteFile(tt.golden, got, 0o644); err != nil {
				t.Fatalf("write golden: %v", err)
			}
			continue
		}
		want, err := os.ReadFile(tt.golden)
		if err != nil {
			t.Fatalf("read golden (regenerate with go test ./internal/app/ -update): %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", tt.kind, got, want)
		}
	}
}

// TestSectionsKindIsRequiredAndClosed pins that --kind is mandatory and
// closed: no default exists between two different datasets, and an unknown
// kind is a usage error rejected before any request reaches the feed.
func TestSectionsKindIsRequiredAndClosed(t *testing.T) {
	srv, counts := newSectionsFixtureServer(t)
	cfg, fetcher := newSectionsFetcher(t, srv.URL, goldenClock)

	if _, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{}); err == nil {
		t.Fatal("Sections accepted an empty kind, want a usage error")
	} else if errs.ExitCodeFor(err) != errs.ExitUsage {
		t.Errorf("error = %v, want a usage error", err)
	}

	if _, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: "motos"}); err == nil {
		t.Fatal("Sections accepted an unknown kind, want a usage error")
	} else if errs.ExitCodeFor(err) != errs.ExitUsage {
		t.Errorf("error = %v, want a usage error", err)
	}

	if got := counts("/invive") + counts("/tefiva"); got != 0 {
		t.Errorf("invalid kinds made %d requests, want 0", got)
	}
}

// TestSectionsRoadAndDirectionFilters pin the shared filters on sections:
// --road compares normalised names and --direction follows the shared
// semantics of internal/filter.
func TestSectionsRoadAndDirectionFilters(t *testing.T) {
	srv, _ := newSectionsFixtureServer(t)
	cfg, fetcher := newSectionsFetcher(t, srv.URL, goldenClock)

	matches, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: model.SectionKindInvive, Roads: []string{"cm-220"}})
	if err != nil {
		t.Fatalf("Sections with a road filter failed: %v", err)
	}
	if len(matches.Sections) != 1 || matches.Sections[0].Road != "CM-220" {
		t.Fatalf("Sections = %+v, want only the CM-220 section", matches.Sections)
	}

	unknown, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: model.SectionKindInvive, Roads: []string{"ZZ-99"}})
	if err != nil {
		t.Fatalf("Sections with an unknown road failed: %v", err)
	}
	if unknown.Sections == nil || len(unknown.Sections) != 0 {
		t.Errorf("Sections with an unknown road = %+v, want an empty non-nil array", unknown.Sections)
	}

	both, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: model.SectionKindAnimals, Direction: "both"})
	if err != nil {
		t.Fatalf("Sections with a direction filter failed: %v", err)
	}
	if len(both.Sections) != 3 {
		t.Errorf("Sections with --direction both = %d, want the 3 both-ways sections", len(both.Sections))
	}

	if _, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: model.SectionKindAnimals, Direction: "north"}); err == nil {
		t.Error("Sections accepted an unknown direction, want a usage error")
	} else if errs.ExitCodeFor(err) != errs.ExitUsage {
		t.Errorf("error = %v, want a usage error", err)
	}
}

// TestSectionsProvinceFilter pins the province filter per feed: INVIVE
// carries the published administrative area name (matched case-insensitively
// through ProvinceEqual), while TEFIVA carries no province, so a province
// filter counts every animal section as unlocated instead of dropping it
// silently (the shared meta.unlocated contract).
func TestSectionsProvinceFilter(t *testing.T) {
	srv, _ := newSectionsFixtureServer(t)
	cfg, fetcher := newSectionsFetcher(t, srv.URL, goldenClock)

	invive, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: model.SectionKindInvive, Provinces: []string{"albacete"}})
	if err != nil {
		t.Fatalf("Sections invive with a province filter failed: %v", err)
	}
	if len(invive.Sections) != 3 || invive.Meta.Unlocated != 0 {
		t.Fatalf("invive = %d sections (unlocated %d), want 3 and 0", len(invive.Sections), invive.Meta.Unlocated)
	}
	if invive.Sections[0].Province == nil || *invive.Sections[0].Province != "ALBACETE" {
		t.Errorf("province = %v, want the published ALBACETE", invive.Sections[0].Province)
	}

	animals, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: model.SectionKindAnimals, Provinces: []string{"huelva"}})
	if err != nil {
		t.Fatalf("Sections animals with a province filter failed: %v", err)
	}
	if len(animals.Sections) != 0 {
		t.Errorf("animals = %d sections, want 0 (TEFIVA carries no province)", len(animals.Sections))
	}
	if animals.Meta.Unlocated != 3 {
		t.Errorf("meta.unlocated = %d, want 3 (the TEFIVA sections cannot be placed)", animals.Meta.Unlocated)
	}
}

// TestSectionsLimitTruncates pins --limit on sections.
func TestSectionsLimitTruncates(t *testing.T) {
	srv, _ := newSectionsFixtureServer(t)
	cfg, fetcher := newSectionsFetcher(t, srv.URL, goldenClock)

	matches, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: model.SectionKindAnimals, Limit: 2})
	if err != nil {
		t.Fatalf("Sections with a limit failed: %v", err)
	}
	if len(matches.Sections) != 2 || matches.Meta.Total != 2 {
		t.Fatalf("Sections = %d sections (total %d), want 2 and 2", len(matches.Sections), matches.Meta.Total)
	}
}

// TestSectionsNegativeLimitIsUsage pins that a negative --limit is rejected
// before any request reaches the feed.
func TestSectionsNegativeLimitIsUsage(t *testing.T) {
	srv, counts := newSectionsFixtureServer(t)
	cfg, fetcher := newSectionsFetcher(t, srv.URL, goldenClock)

	if _, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: model.SectionKindInvive, Limit: -1}); err == nil {
		t.Fatal("Sections accepted a negative limit, want a usage error")
	} else if errs.ExitCodeFor(err) != errs.ExitUsage {
		t.Errorf("error = %v, want a usage error", err)
	}
	if got := counts("/invive") + counts("/tefiva"); got != 0 {
		t.Errorf("invalid criteria made %d requests, want 0", got)
	}
}

// TestSectionsSwappedFeedsAreFeedChanged pins the parser identity guard end
// to end: serving the wrong same-dialect body on a section URL — the TEFIVA
// v3 payload on the INVIVE URL and the other way around — is a feed_changed
// error (exit 7), never a silent empty list.
func TestSectionsSwappedFeedsAreFeedChanged(t *testing.T) {
	invive := sectionsFixture(t, "../../testdata/feeds/invive/snap-1.xml")
	tefiva := sectionsFixture(t, "../../testdata/feeds/tefiva/snap-1.xml")
	srv, _ := newFeedServer(t, map[string]http.HandlerFunc{
		"/invive": func(w http.ResponseWriter, r *http.Request) { w.Write(tefiva) },
		"/tefiva": func(w http.ResponseWriter, r *http.Request) { w.Write(invive) },
	})
	cfg, fetcher := newSectionsFetcher(t, srv.URL, goldenClock)

	if _, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: model.SectionKindInvive}); err == nil {
		t.Error("Sections accepted a TEFIVA body on the invive URL, want a feed_changed error")
	} else if errs.ExitCodeFor(err) != errs.ExitFeedChanged {
		t.Errorf("error = %v, want feed_changed (exit 7)", err)
	}

	if _, err := Sections(context.Background(), cfg, fetcher, SectionCriteria{Kind: model.SectionKindAnimals}); err == nil {
		t.Error("Sections accepted an INVIVE body on the tefiva URL, want a feed_changed error")
	} else if errs.ExitCodeFor(err) != errs.ExitFeedChanged {
		t.Errorf("error = %v, want feed_changed (exit 7)", err)
	}
}
