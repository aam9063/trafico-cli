package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/output"
)

// radarsBody loads the real trimmed radars snapshot (public CC BY data) for
// the test server.
func radarsBody(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/feeds/radars/snap-1.xml")
	if err != nil {
		t.Fatalf("reading the radars snapshot: %v", err)
	}
	return data
}

// testRadarsConfig builds a configuration with the radars feed enabled and
// pointed at the test server.
func testRadarsConfig(srvURL string) *config.Config {
	return &config.Config{
		Feeds: map[string]config.Feed{
			config.FeedRadars: {URL: srvURL + "/radars", Enabled: true},
		},
		Stale: config.Stale{MaxAge: config.DefaultMaxAge},
	}
}

func newRadarsFetcher(t *testing.T, srvURL string, now time.Time) (*config.Config, *feeds.Fetcher) {
	t.Helper()
	cfg := testRadarsConfig(srvURL)
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")
	fetcher.Now = func() time.Time { return now }
	return cfg, fetcher
}

// newRadarsFixtureServer serves the real radars snapshot and reports how
// many requests it received, so tests can prove which feeds were (never)
// fetched. No test ever touches the real network (AGENTS.md).
func newRadarsFixtureServer(t *testing.T) (*httptest.Server, func(string) int) {
	t.Helper()
	body := radarsBody(t)
	srv, counts := newFeedServer(t, map[string]http.HandlerFunc{
		"/radars": func(w http.ResponseWriter, r *http.Request) { w.Write(body) },
	})
	return srv, counts
}

// TestRadarsFixtureCounts pins the observed counts of the real fixture
// (docs/feeds.md §6): 12 radars (6 segment CVM, 6 cabins), every one with a
// published province_code, and — docs/feeds.md §9 — meta.published_at null,
// never invented, with no age and not stale.
func TestRadarsFixtureCounts(t *testing.T) {
	srv, counts := newRadarsFixtureServer(t)
	cfg, fetcher := newRadarsFetcher(t, srv.URL, goldenClock)

	response, err := Radars(context.Background(), cfg, fetcher, RadarCriteria{})
	if err != nil {
		t.Fatalf("Radars failed: %v", err)
	}
	if got := counts("/radars"); got != 1 {
		t.Errorf("radars feed fetched %d times, want 1", got)
	}
	if len(response.Radars) != 12 {
		t.Fatalf("Radars returned %d radars, want 12", len(response.Radars))
	}
	if response.Meta.Total != 12 {
		t.Errorf("meta.total = %d, want 12", response.Meta.Total)
	}
	if response.Meta.PublishedAt != nil {
		t.Errorf("meta.published_at = %v, want null (docs/feeds.md §9: this feed publishes no usable publicationTime)", response.Meta.PublishedAt)
	}
	if response.Meta.AgeSeconds != nil {
		t.Errorf("meta.age_seconds = %v, want null", response.Meta.AgeSeconds)
	}
	if response.Meta.Stale {
		t.Errorf("meta.stale = true, want false (an undated feed is not stale)")
	}
	if response.Meta.Feed != config.FeedRadars {
		t.Errorf("meta.feed = %q, want %q", response.Meta.Feed, config.FeedRadars)
	}
	withCode := 0
	for _, radar := range response.Radars {
		if radar.ProvinceCode != nil && *radar.ProvinceCode != "" {
			withCode++
		}
	}
	if withCode != 12 {
		t.Errorf("%d radars carry a province_code, want 12", withCode)
	}
}

// TestRadarsGolden pins the full envelope of `trafico radars` over the real
// fixture, byte-compared against internal/app/testdata/radars.golden.json.
func TestRadarsGolden(t *testing.T) {
	srv, _ := newRadarsFixtureServer(t)
	cfg, fetcher := newRadarsFetcher(t, srv.URL, goldenClock)

	response, err := Radars(context.Background(), cfg, fetcher, RadarCriteria{})
	if err != nil {
		t.Fatalf("Radars failed: %v", err)
	}
	var buf bytes.Buffer
	if err := output.Write(&buf, response, output.Options{}); err != nil {
		t.Fatalf("serialising the response: %v", err)
	}
	got := buf.Bytes()
	goldenPath := "testdata/radars.golden.json"
	if *update {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (regenerate with go test ./internal/app/ -update): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestRadarsProvinceFilterUsesINETable pins the whole point of the INE
// table: --province zaragoza matches the radars whose provinceINEIdentifier
// is 50, through the resolved province name.
func TestRadarsProvinceFilterUsesINETable(t *testing.T) {
	srv, _ := newRadarsFixtureServer(t)
	cfg, fetcher := newRadarsFetcher(t, srv.URL, goldenClock)

	matches, err := Radars(context.Background(), cfg, fetcher, RadarCriteria{Provinces: []string{"zaragoza"}})
	if err != nil {
		t.Fatalf("Radars failed: %v", err)
	}
	if len(matches.Radars) != 1 {
		t.Fatalf("Radars = %d, want the 1 Zaragoza radar (INE 50)", len(matches.Radars))
	}
	radar := matches.Radars[0]
	if radar.ID != "dgt:GUID_CVM_161274" || radar.Road != "Z-40" {
		t.Errorf("radar = %s on %s, want dgt:GUID_CVM_161274 on Z-40", radar.ID, radar.Road)
	}
	if radar.Province == nil || *radar.Province != "Zaragoza" || radar.ProvinceCode == nil || *radar.ProvinceCode != "50" {
		t.Errorf("province/code = %v/%v, want Zaragoza/50", radar.Province, radar.ProvinceCode)
	}
	if matches.Meta.Unlocated != 0 {
		t.Errorf("meta.unlocated = %d, want 0", matches.Meta.Unlocated)
	}
}

// TestRadarsUnknownProvinceCodeIsNullAndUnlocated pins both halves of the
// unknown-code rule: the radar serialises province null with province_code
// set, and a province filter counts it as unlocated instead of dropping it
// silently (the shared meta.unlocated contract).
func TestRadarsUnknownProvinceCodeIsNullAndUnlocated(t *testing.T) {
	// One synthetic radar with an INE code outside the table.
	body := strings.Replace(string(radarsBody(t)), "<_0:provinceINEIdentifier>50</_0:provinceINEIdentifier>",
		"<_0:provinceINEIdentifier>99</_0:provinceINEIdentifier>", 1)
	if body == string(radarsBody(t)) {
		t.Fatal("the test fixture edit did not change the INE code")
	}
	srv, _ := newFeedServer(t, map[string]http.HandlerFunc{
		"/radars": func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) },
	})
	cfg, fetcher := newRadarsFetcher(t, srv.URL, goldenClock)

	response, err := Radars(context.Background(), cfg, fetcher, RadarCriteria{})
	if err != nil {
		t.Fatalf("Radars failed: %v", err)
	}
	edited := false
	for _, radar := range response.Radars {
		if radar.ID == "dgt:GUID_CVM_161274" {
			edited = true
			if radar.Province != nil {
				t.Errorf("province = %v, want null for the unknown code 99", radar.Province)
			}
			if radar.ProvinceCode == nil || *radar.ProvinceCode != "99" {
				t.Errorf("province_code = %v, want 99", radar.ProvinceCode)
			}
		}
	}
	if !edited {
		t.Fatal("the edited radar is missing from the response")
	}

	matches, err := Radars(context.Background(), cfg, fetcher, RadarCriteria{Provinces: []string{"zaragoza"}})
	if err != nil {
		t.Fatalf("Radars with a province filter failed: %v", err)
	}
	if len(matches.Radars) != 0 {
		t.Errorf("Radars = %d, want 0 (the unknown-code radar is not Zaragoza)", len(matches.Radars))
	}
	if matches.Meta.Unlocated != 1 {
		t.Errorf("meta.unlocated = %d, want 1 (the unknown-code radar cannot be placed)", matches.Meta.Unlocated)
	}
}

// TestRadarsRoadFilterMatchesNormalised pins --road on radars: the filter
// compares normalised road names, so "z-40" matches the feed's Z-40, and an
// unknown road is an empty array, never an error.
func TestRadarsRoadFilterMatchesNormalised(t *testing.T) {
	srv, _ := newRadarsFixtureServer(t)
	cfg, fetcher := newRadarsFetcher(t, srv.URL, goldenClock)

	matches, err := Radars(context.Background(), cfg, fetcher, RadarCriteria{Roads: []string{"z-40"}})
	if err != nil {
		t.Fatalf("Radars failed: %v", err)
	}
	if len(matches.Radars) != 1 || matches.Radars[0].Road != "Z-40" {
		t.Fatalf("Radars = %+v, want only the Z-40 radar", matches.Radars)
	}

	unknown, err := Radars(context.Background(), cfg, fetcher, RadarCriteria{Roads: []string{"ZZ-99"}})
	if err != nil {
		t.Fatalf("Radars with an unknown road failed: %v", err)
	}
	if unknown.Radars == nil || len(unknown.Radars) != 0 {
		t.Errorf("Radars with an unknown road = %+v, want an empty non-nil array", unknown.Radars)
	}
}

// TestRadarsDirectionFilter pins the shared direction semantics: the
// primary directionRelative decides, positive matches only that direction,
// and an unknown direction is a usage error rejected before any request
// reaches the feed.
func TestRadarsDirectionFilter(t *testing.T) {
	srv, counts := newRadarsFixtureServer(t)
	cfg, fetcher := newRadarsFetcher(t, srv.URL, goldenClock)

	matches, err := Radars(context.Background(), cfg, fetcher, RadarCriteria{Direction: "negative"})
	if err != nil {
		t.Fatalf("Radars failed: %v", err)
	}
	if len(matches.Radars) != 3 {
		t.Fatalf("Radars = %d, want the 3 negative-direction radars of the fixture", len(matches.Radars))
	}

	if _, err := Radars(context.Background(), cfg, fetcher, RadarCriteria{Direction: "north"}); err == nil {
		t.Fatal("Radars accepted an unknown direction, want a usage error")
	} else if errs.ExitCodeFor(err) != errs.ExitUsage {
		t.Errorf("error = %v, want a usage error", err)
	}
	if got := counts("/radars"); got != 1 {
		// The valid call fetched once; the invalid one spent 0.
		t.Errorf("invalid direction spent %d requests, want 1 (only the valid call's)", got)
	}
}

// TestRadarsLimitTruncates pins --limit on radars.
func TestRadarsLimitTruncates(t *testing.T) {
	srv, _ := newRadarsFixtureServer(t)
	cfg, fetcher := newRadarsFetcher(t, srv.URL, goldenClock)

	matches, err := Radars(context.Background(), cfg, fetcher, RadarCriteria{Limit: 2})
	if err != nil {
		t.Fatalf("Radars failed: %v", err)
	}
	if len(matches.Radars) != 2 || matches.Meta.Total != 2 {
		t.Fatalf("Radars = %d radars (total %d), want 2 and 2", len(matches.Radars), matches.Meta.Total)
	}
}

// TestRadarsNegativeLimitIsUsage pins that a negative --limit is rejected
// before any request reaches the feed.
func TestRadarsNegativeLimitIsUsage(t *testing.T) {
	srv, counts := newRadarsFixtureServer(t)
	cfg, fetcher := newRadarsFetcher(t, srv.URL, goldenClock)

	if _, err := Radars(context.Background(), cfg, fetcher, RadarCriteria{Limit: -1}); err == nil {
		t.Fatal("Radars accepted a negative limit, want a usage error")
	} else if errs.ExitCodeFor(err) != errs.ExitUsage {
		t.Errorf("error = %v, want a usage error", err)
	}
	if got := counts("/radars"); got != 0 {
		t.Errorf("invalid criteria made %d requests, want 0", got)
	}
}

// TestRadarsForeignDialectIsFeedChanged pins the parser identity guard end
// to end: serving a v3.7 situations body on the radars URL is a
// feed_changed error (exit 7), never a silent empty list.
func TestRadarsForeignDialectIsFeedChanged(t *testing.T) {
	situations, err := os.ReadFile("../../testdata/feeds/situations/snap-1.xml")
	if err != nil {
		t.Fatalf("reading the situations snapshot: %v", err)
	}
	srv, _ := newFeedServer(t, map[string]http.HandlerFunc{
		"/radars": func(w http.ResponseWriter, r *http.Request) { w.Write(situations) },
	})
	cfg, fetcher := newRadarsFetcher(t, srv.URL, goldenClock)

	if _, err := Radars(context.Background(), cfg, fetcher, RadarCriteria{}); err == nil {
		t.Fatal("Radars accepted a foreign dialect, want a feed_changed error")
	} else if errs.ExitCodeFor(err) != errs.ExitFeedChanged {
		t.Errorf("error = %v, want feed_changed (exit 7)", err)
	}
}
