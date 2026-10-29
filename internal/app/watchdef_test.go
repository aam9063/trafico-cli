package app

import (
	"errors"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/filter"
	"github.com/aam9063/trafico-cli/internal/store"
)

// watchConfig builds the in-memory config the watch tests use: one route
// with two segments, one saved place and one sink.
func watchConfig() *config.Config {
	cfg := config.Default()
	cfg.Routes["trabajo"] = config.Route{
		Segments: []string{"A-7:102-118", "a7:200-210"},
	}
	cfg.Places["casa"] = "38.3452,-0.4810"
	cfg.Sinks["phone"] = config.Sink{Type: "ntfy", Topic: "un-topic-largo"}
	return cfg
}

// parseRouteWatch parses the route-shaped spec every target-shape test
// starts from, failing the test on any error.
func parseRouteWatch(t *testing.T, cfg *config.Config, target WatchTarget, windowSpecs []string, minSeverity string, sinks []string) WatchSpec {
	t.Helper()
	spec, err := ParseWatchSpec("trabajo", target, windowSpecs, minSeverity, sinks, 5*time.Minute, cfg)
	if err != nil {
		t.Fatalf("ParseWatchSpec: %v", err)
	}
	return spec
}

// TestParseWatchSpecRouteBuildsPerSegmentCriteria checks that a route watch
// translates into one filter.Criteria per declared segment, each with the
// segment's normalised road and its kilometre bounds, so the engine caller
// can reuse filter.Apply with route-check precision.
func TestParseWatchSpecRouteBuildsPerSegmentCriteria(t *testing.T) {
	cfg := watchConfig()
	spec := parseRouteWatch(t, cfg, WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		[]string{"lun-vie 07:00-09:00"}, "medium", []string{"phone"})

	if spec.Name != "trabajo" || spec.MinSeverity != "medium" || spec.Interval != 5*time.Minute {
		t.Fatalf("spec = %+v, want the parsed fields echoed", spec)
	}
	if len(spec.Windows) != 1 || len(spec.WindowSpecs) != 1 {
		t.Fatalf("windows = %+v, want the one parsed window", spec.Windows)
	}

	criteria, err := Criteria(spec, cfg)
	if err != nil {
		t.Fatalf("Criteria: %v", err)
	}
	if len(criteria) != 2 {
		t.Fatalf("Criteria returned %d entries, want one per segment", len(criteria))
	}
	// Segment 1: A-7:102-118, with the medium severity floor.
	if len(criteria[0].Roads) != 1 || criteria[0].Roads[0] != "A-7" {
		t.Errorf("criteria[0].Roads = %v, want [A-7]", criteria[0].Roads)
	}
	if criteria[0].PK == nil || criteria[0].PK.From == nil || *criteria[0].PK.From != 102 ||
		criteria[0].PK.To == nil || *criteria[0].PK.To != 118 {
		t.Errorf("criteria[0].PK = %+v, want 102-118", criteria[0].PK)
	}
	if len(criteria[0].Severities) != 2 || criteria[0].Severities[0] != "medium" || criteria[0].Severities[1] != "high" {
		t.Errorf("criteria[0].Severities = %v, want [medium high]", criteria[0].Severities)
	}
	// Segment 2 declares "a7:200-210": the road must be normalised.
	if len(criteria[1].Roads) != 1 || criteria[1].Roads[0] != "A-7" {
		t.Errorf("criteria[1].Roads = %v, want the normalised [A-7]", criteria[1].Roads)
	}
	if criteria[1].PK == nil || criteria[1].PK.From == nil || *criteria[1].PK.From != 200 ||
		criteria[1].PK.To == nil || *criteria[1].PK.To != 210 {
		t.Errorf("criteria[1].PK = %+v, want 200-210", criteria[1].PK)
	}
}

// TestParseWatchSpecRoadBuildsCriteria checks the road shape: one criteria
// with the normalised road and the requested kilometre range.
func TestParseWatchSpecRoadBuildsCriteria(t *testing.T) {
	cfg := watchConfig()
	from, to := 600.0, 700.0
	spec := parseRouteWatch(t, cfg, WatchTarget{Kind: store.WatchKindRoad, Road: "ap-7", PK: &filter.Range{From: &from, To: &to}},
		[]string{"lun-vie 07:00-09:00"}, "", []string{"phone"})

	criteria, err := Criteria(spec, cfg)
	if err != nil {
		t.Fatalf("Criteria: %v", err)
	}
	if len(criteria) != 1 {
		t.Fatalf("Criteria returned %d entries, want 1", len(criteria))
	}
	if len(criteria[0].Roads) != 1 || criteria[0].Roads[0] != "AP-7" {
		t.Errorf("Roads = %v, want the normalised [AP-7]", criteria[0].Roads)
	}
	if criteria[0].PK == nil || criteria[0].PK.From == nil || *criteria[0].PK.From != 600 ||
		criteria[0].PK.To == nil || *criteria[0].PK.To != 700 {
		t.Errorf("PK = %+v, want 600-700", criteria[0].PK)
	}
	if len(criteria[0].Severities) != 0 {
		t.Errorf("Severities = %v, want none with an empty min severity", criteria[0].Severities)
	}
}

// TestParseWatchSpecNearBuildsCriteria checks the near shape: no road or PK
// filter in the criteria — the radius composition (geo.ResolveTarget, then
// geo.FilterRadius, then filter.Apply) is the caller's documented job — and
// the severity floor applied like in the other shapes.
func TestParseWatchSpecNearBuildsCriteria(t *testing.T) {
	cfg := watchConfig()
	spec := parseRouteWatch(t, cfg, WatchTarget{Kind: store.WatchKindNear, Place: "casa", RadiusKm: 15},
		[]string{"lun-vie 07:00-09:00"}, "high", []string{"phone"})

	criteria, err := Criteria(spec, cfg)
	if err != nil {
		t.Fatalf("Criteria: %v", err)
	}
	if len(criteria) != 1 {
		t.Fatalf("Criteria returned %d entries, want 1", len(criteria))
	}
	if len(criteria[0].Roads) != 0 || criteria[0].PK != nil {
		t.Errorf("criteria = %+v, want no road/PK filter for a near watch", criteria[0])
	}
	if len(criteria[0].Severities) != 1 || criteria[0].Severities[0] != "high" {
		t.Errorf("Severities = %v, want [high]", criteria[0].Severities)
	}
	if spec.Target.Place != "casa" || spec.Target.RadiusKm != 15 {
		t.Errorf("target = %+v, want the stored inputs echoed", spec.Target)
	}
}

// TestParseWatchSpecNearCoordinates checks the lat/lon centre variant.
func TestParseWatchSpecNearCoordinates(t *testing.T) {
	cfg := watchConfig()
	lat, lon := 38.3452, -0.4810
	spec := parseRouteWatch(t, cfg, WatchTarget{Kind: store.WatchKindNear, Lat: &lat, Lon: &lon, RadiusKm: 10},
		[]string{"lun-vie 07:00-09:00"}, "", []string{"phone"})

	if spec.Target.Lat == nil || *spec.Target.Lat != lat || spec.Target.Lon == nil || *spec.Target.Lon != lon {
		t.Errorf("target = %+v, want the coordinates echoed", spec.Target)
	}
	if spec.Target.Place != "" {
		t.Errorf("target.Place = %q, want empty", spec.Target.Place)
	}
}

// TestParseWatchSpecRouteMissingIsNotFound pins that a watch on a route the
// config does not carry is a not_found error (exit 4).
func TestParseWatchSpecRouteMissingIsNotFound(t *testing.T) {
	cfg := watchConfig()
	_, err := ParseWatchSpec("trabajo", WatchTarget{Kind: store.WatchKindRoute, Route: "inexistente"},
		[]string{"lun-vie 07:00-09:00"}, "", []string{"phone"}, 5*time.Minute, cfg)
	if err == nil {
		t.Fatal("ParseWatchSpec on a missing route succeeded, want not_found")
	}
	var errsErr *errs.Error
	if !errors.As(err, &errsErr) || errsErr.Type != "not_found" {
		t.Fatalf("error = %v, want a not_found errs.Error", err)
	}
}

// TestParseWatchSpecRejectsBadInput covers every usage-error rule of the
// definition layer: unsafe name, no windows, bad window spec, bad minimum
// severity, unknown sink, interval below the minimum, and the near-shape
// combinations geo rejects (no radius, both centre forms, unknown place).
func TestParseWatchSpecRejectsBadInput(t *testing.T) {
	cfg := watchConfig()
	good := WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"}
	lat, lon := 38.3452, -0.4810
	from, to := 700.0, 600.0

	cases := []struct {
		name    string
		watch   string
		target  WatchTarget
		windows []string
		min     string
		sinks   []string
		margin  time.Duration
	}{
		{"unsafe name", "mi ruta;", good, []string{"lun-vie 07:00-09:00"}, "", []string{"phone"}, 5 * time.Minute},
		{"no windows", "trabajo", good, nil, "", []string{"phone"}, 5 * time.Minute},
		{"bad window", "trabajo", good, []string{"siempre"}, "", []string{"phone"}, 5 * time.Minute},
		{"bad min severity", "trabajo", good, []string{"lun-vie 07:00-09:00"}, "critical", []string{"phone"}, 5 * time.Minute},
		{"unknown sink", "trabajo", good, []string{"lun-vie 07:00-09:00"}, "", []string{"reloj"}, 5 * time.Minute},
		{"interval below minimum", "trabajo", good, []string{"lun-vie 07:00-09:00"}, "", []string{"phone"}, time.Minute},
		{"near without radius", "trabajo", WatchTarget{Kind: store.WatchKindNear, Place: "casa"}, []string{"lun-vie 07:00-09:00"}, "", []string{"phone"}, 5 * time.Minute},
		{"near with both centres", "trabajo", WatchTarget{Kind: store.WatchKindNear, Place: "casa", Lat: &lat, Lon: &lon, RadiusKm: 10}, []string{"lun-vie 07:00-09:00"}, "", []string{"phone"}, 5 * time.Minute},
		{"near with unknown place", "trabajo", WatchTarget{Kind: store.WatchKindNear, Place: "oficina", RadiusKm: 10}, []string{"lun-vie 07:00-09:00"}, "", []string{"phone"}, 5 * time.Minute},
		{"road without name", "trabajo", WatchTarget{Kind: store.WatchKindRoad}, []string{"lun-vie 07:00-09:00"}, "", []string{"phone"}, 5 * time.Minute},
		{"road with inverted PK", "trabajo", WatchTarget{Kind: store.WatchKindRoad, Road: "A-7", PK: &filter.Range{From: &from, To: &to}}, []string{"lun-vie 07:00-09:00"}, "", []string{"phone"}, 5 * time.Minute},
		{"route without name", "trabajo", WatchTarget{Kind: store.WatchKindRoute}, []string{"lun-vie 07:00-09:00"}, "", []string{"phone"}, 5 * time.Minute},
	}
	for _, tc := range cases {
		_, err := ParseWatchSpec(tc.watch, tc.target, tc.windows, tc.min, tc.sinks, tc.margin, cfg)
		if err == nil {
			t.Errorf("%s: ParseWatchSpec succeeded, want a usage error", tc.name)
			continue
		}
		var errsErr *errs.Error
		if !errors.As(err, &errsErr) || errsErr.Type != "usage" {
			t.Errorf("%s: error = %v, want a usage errs.Error", tc.name, err)
		}
	}
}

// TestSpecRoundTripsThroughStore checks the full storage cycle: a parsed
// spec saved through the store reads back into an equivalent spec, and the
// criteria rebuilt from the stored spec match the original ones.
func TestSpecRoundTripsThroughStore(t *testing.T) {
	cfg := watchConfig()
	st, err := store.Open(stPath(t))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer st.Close()

	created := time.Date(2026, 10, 29, 8, 30, 0, 0, time.UTC)
	spec, err := ParseWatchSpec("trabajo", WatchTarget{Kind: store.WatchKindRoute, Route: "trabajo"},
		[]string{"lun-vie 07:00-09:00", "lun-vie 17:30-19:30"}, "medium", []string{"phone"}, 5*time.Minute, cfg)
	if err != nil {
		t.Fatalf("ParseWatchSpec: %v", err)
	}
	spec.CreatedAt = created
	if err := st.SaveWatch(spec.storeWatch()); err != nil {
		t.Fatalf("SaveWatch: %v", err)
	}

	stored, err := st.Watch("trabajo")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	back, err := SpecFromStore(stored, cfg)
	if err != nil {
		t.Fatalf("SpecFromStore: %v", err)
	}
	if back.Name != spec.Name || back.Target != spec.Target ||
		back.MinSeverity != spec.MinSeverity || back.Interval != spec.Interval ||
		!back.CreatedAt.Equal(created) {
		t.Errorf("round trip = %+v, want %+v", back, spec)
	}
	if len(back.Windows) != 2 || len(back.WindowSpecs) != 2 {
		t.Errorf("windows after round trip = %+v (specs %+v), want the two parsed windows", back.Windows, back.WindowSpecs)
	}
	for i, w := range spec.Windows {
		got, want := back.Windows[i], w
		if len(got.Days) != len(want.Days) || got.StartMinute != want.StartMinute || got.EndMinute != want.EndMinute {
			t.Errorf("window %d after round trip = %+v, want %+v", i, got, want)
			continue
		}
		for j, day := range want.Days {
			if got.Days[j] != day {
				t.Errorf("window %d day %d after round trip = %v, want %v", i, j, got.Days[j], day)
			}
		}
	}

	want, err := Criteria(spec, cfg)
	if err != nil {
		t.Fatalf("Criteria of the original spec: %v", err)
	}
	got, err := Criteria(back, cfg)
	if err != nil {
		t.Fatalf("Criteria of the stored spec: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("criteria after round trip: %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if len(got[i].Roads) != len(want[i].Roads) || got[i].Roads[0] != want[i].Roads[0] {
			t.Errorf("criteria[%d].Roads after round trip = %v, want %v", i, got[i].Roads, want[i].Roads)
		}
		if (got[i].PK == nil) != (want[i].PK == nil) {
			t.Errorf("criteria[%d].PK after round trip = %+v, want %+v", i, got[i].PK, want[i].PK)
		}
	}
}

// TestCriteriaSeverityFloors pins the exact severity set each floor
// produces, including the unknown rule of docs/feeds.md §4.5 (the DGT
// publishes no severity for the large majority of records): the floor "low"
// reads as "any published severity", so its set includes unknown; the floors
// "medium" and "high" exclude it, because "not published" is not "at least
// medium"; an empty floor sets no restriction at all.
func TestCriteriaSeverityFloors(t *testing.T) {
	cfg := watchConfig()
	cases := []struct {
		name string
		min  string
		want []string
	}{
		{"no floor", "", nil},
		{"low", "low", []string{"unknown", "low", "medium", "high"}},
		{"medium", "medium", []string{"medium", "high"}},
		{"high", "high", []string{"high"}},
	}
	for _, tc := range cases {
		spec := parseRouteWatch(t, cfg, WatchTarget{Kind: store.WatchKindRoad, Road: "A-7"},
			[]string{"lun-vie 07:00-09:00"}, tc.min, []string{"phone"})
		criteria, err := Criteria(spec, cfg)
		if err != nil {
			t.Fatalf("%s: Criteria: %v", tc.name, err)
		}
		if len(criteria) != 1 {
			t.Fatalf("%s: %d criteria, want 1", tc.name, len(criteria))
		}
		got := criteria[0].Severities
		if len(got) != len(tc.want) {
			t.Fatalf("%s: Severities = %v, want %v", tc.name, got, tc.want)
		}
		for i, want := range tc.want {
			if got[i] != want {
				t.Fatalf("%s: Severities = %v, want %v", tc.name, got, tc.want)
			}
		}
	}
}

// TestSpecFromStoreRejectsWatchWithoutWindows pins the read-path window
// rule: a stored watch with no window (the JSON "null" and [] shapes a hand
// edit or a corrupt write can produce) must fail where it is read, with the
// same usage error the parse path gives, instead of silently running 24/7 —
// the engine treats an empty window list as "always".
func TestSpecFromStoreRejectsWatchWithoutWindows(t *testing.T) {
	cfg := watchConfig()
	base := store.Watch{
		Name:         "trabajo",
		Kind:         store.WatchKindRoute,
		Target:       store.Target{Route: "trabajo"},
		MinSeverity:  "",
		Sinks:        []string{"phone"},
		IntervalSecs: 300,
		CreatedAt:    time.Date(2026, 10, 29, 8, 30, 0, 0, time.UTC),
	}
	for name, windows := range map[string][]string{"null": nil, "empty": {}} {
		stored := base
		stored.Windows = windows
		_, err := SpecFromStore(stored, cfg)
		if err == nil {
			t.Errorf("%s windows: SpecFromStore succeeded, want a usage error", name)
			continue
		}
		var errsErr *errs.Error
		if !errors.As(err, &errsErr) || errsErr.Type != "usage" {
			t.Errorf("%s windows: error = %v, want a usage errs.Error", name, err)
		}
	}
}

// TestSpecFromStoreRevalidatesTarget pins that the read path re-runs the
// same target rules the write path enforces, so a corrupt or stale row fails
// where it is read: a near target with both a place and coordinates is a
// usage error, and a route the config no longer carries stays not_found
// (exit 4), exactly like the write path.
func TestSpecFromStoreRevalidatesTarget(t *testing.T) {
	cfg := watchConfig()
	lat, lon := 38.3452, -0.4810
	created := time.Date(2026, 10, 29, 8, 30, 0, 0, time.UTC)

	stale := store.Watch{
		Name:         "cerca",
		Kind:         store.WatchKindNear,
		Target:       store.Target{Place: "casa", Lat: &lat, Lon: &lon, RadiusKm: 10},
		Windows:      []string{"lun-vie 07:00-09:00"},
		Sinks:        []string{"phone"},
		IntervalSecs: 300,
		CreatedAt:    created,
	}
	_, err := SpecFromStore(stale, cfg)
	if err == nil {
		t.Fatal("SpecFromStore on a near target with both centres succeeded, want a usage error")
	}
	var errsErr *errs.Error
	if !errors.As(err, &errsErr) || errsErr.Type != "usage" {
		t.Fatalf("error = %v, want a usage errs.Error", err)
	}

	missing := store.Watch{
		Name:         "vieja",
		Kind:         store.WatchKindRoute,
		Target:       store.Target{Route: "borrada"},
		Windows:      []string{"lun-vie 07:00-09:00"},
		Sinks:        []string{"phone"},
		IntervalSecs: 300,
		CreatedAt:    created,
	}
	_, err = SpecFromStore(missing, cfg)
	if err == nil {
		t.Fatal("SpecFromStore on a route the config no longer carries succeeded, want not_found")
	}
	if !errors.As(err, &errsErr) || errsErr.Type != "not_found" {
		t.Fatalf("error = %v, want a not_found errs.Error", err)
	}
}

// TestParseWatchSpecAllowsZeroSinks pins the documented decision: the spec
// does not mandate --notify, so a watch with zero sinks is accepted. It only
// records history (the events are appended and never delivered), and watch
// list shows it like any other watch.
func TestParseWatchSpecAllowsZeroSinks(t *testing.T) {
	cfg := watchConfig()
	spec, err := ParseWatchSpec("historial", WatchTarget{Kind: store.WatchKindRoad, Road: "A-7"},
		[]string{"lun-vie 07:00-09:00"}, "", nil, 5*time.Minute, cfg)
	if err != nil {
		t.Fatalf("ParseWatchSpec with zero sinks: %v", err)
	}
	if len(spec.Sinks) != 0 {
		t.Fatalf("Sinks = %v, want none", spec.Sinks)
	}
}

// TestSpecFromStoreRejectsCorruptTarget checks that a stored row whose
// target does not match its kind surfaces as an error instead of producing
// a spec that would silently watch nothing.
func TestSpecFromStoreRejectsCorruptTarget(t *testing.T) {
	cfg := watchConfig()
	stored := store.Watch{
		Name:         "trabajo",
		Kind:         store.WatchKindRoad,
		Target:       store.Target{Route: "trabajo"}, // road kind with route fields
		Windows:      []string{"lun-vie 07:00-09:00"},
		MinSeverity:  "",
		Sinks:        []string{"phone"},
		IntervalSecs: 300,
		CreatedAt:    time.Date(2026, 10, 29, 8, 30, 0, 0, time.UTC),
	}
	if _, err := SpecFromStore(stored, cfg); err == nil {
		t.Fatal("SpecFromStore on a corrupt target succeeded, want an error")
	}
}

// stPath returns the database path of this test's temporary directory.
func stPath(t *testing.T) string {
	t.Helper()
	return t.TempDir() + string('/') + "trafico.db"
}
