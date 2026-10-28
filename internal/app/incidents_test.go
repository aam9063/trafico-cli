package app

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/filter"
	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/output"
)

// update regenerates the golden files of internal/app/testdata when
// explicitly requested: go test ./internal/app/ -update. It is never
// automatic (CMD-01).
var update = flag.Bool("update", false, "rewrite internal/app/testdata/*.golden.json")

// goldenClock sits after the publication time of the snap-1 fixture
// (2026-10-28T12:12:17Z) and before no validity start, so every fixture
// incident is active and the golden output is byte-stable.
var goldenClock = time.Date(2026, 10, 28, 14, 0, 0, 0, time.UTC)

// plannedClock sits before several validity starts of the fixture, so those
// incidents compute as planned and the --status planned test has matches.
var plannedClock = time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

// floatPtr returns a pointer to v (test helper for PK bounds and radii).
func floatPtr(v float64) *float64 { return &v }

// newIncidentsFetcher builds a fetcher pointed at the test server with a
// fixed clock, so statuses, ages and timestamps are deterministic.
func newIncidentsFetcher(t *testing.T, srvURL string, now time.Time) (*config.Config, *feeds.Fetcher) {
	t.Helper()
	cfg := testConfig(srvURL)
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")
	fetcher.Now = func() time.Time { return now }
	return cfg, fetcher
}

// incidentsFixtureServer serves the snap-1 snapshot on /situations.
func incidentsFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _ := newFeedServer(t, map[string]http.HandlerFunc{
		"/situations": func(w http.ResponseWriter, r *http.Request) { w.Write(snapshotFeed(t)) },
	})
	return srv
}

// incidentsBodyServer serves an arbitrary body on /situations.
func incidentsBodyServer(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv, _ := newFeedServer(t, map[string]http.HandlerFunc{
		"/situations": func(w http.ResponseWriter, r *http.Request) { w.Write(body) },
	})
	return srv
}

func TestIncidentsGolden(t *testing.T) {
	srv := incidentsFixtureServer(t)
	tests := []struct {
		name     string
		golden   string
		clock    time.Time
		criteria IncidentCriteria
	}{
		{
			name:     "unfiltered (default status active)",
			golden:   "incidents-unfiltered.golden.json",
			clock:    goldenClock,
			criteria: IncidentCriteria{},
		},
		{
			name:   "by road",
			golden: "incidents-road.golden.json",
			clock:  goldenClock,
			criteria: IncidentCriteria{Criteria: filter.Criteria{
				Roads: []string{"A-62"},
			}},
		},
		{
			name:   "by road and pk",
			golden: "incidents-pk.golden.json",
			clock:  goldenClock,
			criteria: IncidentCriteria{Criteria: filter.Criteria{
				Roads: []string{"A-372"},
				PK:    &filter.Range{From: floatPtr(50), To: floatPtr(51)},
			}},
		},
		{
			name:   "by direction",
			golden: "incidents-direction.golden.json",
			clock:  goldenClock,
			criteria: IncidentCriteria{Criteria: filter.Criteria{
				Status:    "all",
				Direction: "positive",
			}},
		},
		{
			name:   "by type",
			golden: "incidents-type.golden.json",
			clock:  goldenClock,
			criteria: IncidentCriteria{Criteria: filter.Criteria{
				Status: "all",
				Types:  []string{"accident"},
			}},
		},
		{
			name:   "status planned",
			golden: "incidents-planned.golden.json",
			clock:  plannedClock,
			criteria: IncidentCriteria{Criteria: filter.Criteria{
				Status: "planned",
			}},
		},
		{
			name:   "limit and sort",
			golden: "incidents-limit-sort.golden.json",
			clock:  goldenClock,
			criteria: IncidentCriteria{Criteria: filter.Criteria{
				Status: "all",
				Sort:   "start",
				Limit:  3,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, fetcher := newIncidentsFetcher(t, srv.URL, tt.clock)
			response, err := Incidents(context.Background(), cfg, fetcher, tt.criteria)
			if err != nil {
				t.Fatalf("Incidents failed: %v", err)
			}
			var buf bytes.Buffer
			if err := output.Write(&buf, response, output.Options{}); err != nil {
				t.Fatalf("serialising the response: %v", err)
			}
			got := buf.Bytes()
			goldenPath := filepath.Join("testdata", tt.golden)
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
				t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", tt.golden, got, want)
			}
		})
	}
}

// TestIncidentsGoldenValidatesAgainstSchema validates every golden file
// against schemas/incidents-response.schema.json with the focused validator
// (no new dependency, MODEL-01 precedent).
func TestIncidentsGoldenValidatesAgainstSchema(t *testing.T) {
	schema, loader := loadIncidentsResponseSchema(t)
	goldens, err := filepath.Glob(filepath.Join("testdata", "incident*.golden.json"))
	if err != nil {
		t.Fatalf("glob goldens: %v", err)
	}
	if len(goldens) == 0 {
		t.Fatal("no golden files found (regenerate with go test ./internal/app/ -update)")
	}
	for _, goldenPath := range goldens {
		t.Run(filepath.Base(goldenPath), func(t *testing.T) {
			golden, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			var value any
			if err := json.Unmarshal(golden, &value); err != nil {
				t.Fatalf("parse golden: %v", err)
			}
			if errs := schema.validate(value, "", loader); len(errs) > 0 {
				t.Errorf("golden does not validate against incidents-response.schema.json: %s", strings.Join(errs, "; "))
			}
		})
	}
}

// radiusOrderFeed builds a synthetic snapshot with exactly three records:
// "900001" inside the radius and with a province, "900002" far away and
// without a province, and "900003" with no location at all (hence no
// province either). It is the probe of the CMD-01 review: only this
// combination distinguishes "radius first, then Apply" from the flipped
// order, because only then do the two unlocated buckets differ.
func radiusOrderFeed(t *testing.T) []byte {
	t.Helper()
	const header = `<?xml version="1.0" encoding="UTF-8"?>
<d2:payload xsi:type="sit:SituationPublication" xmlns:com="http://levelC/schema/3/common" xmlns:sit="http://levelC/schema/3/situation" xmlns:loc="http://levelC/schema/3/locationReferencing" xmlns:lse="http://levelC/schema/3/locationReferencingSpanishExtension" xmlns:d2="http://levelC/schema/3/d2Payload" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <com:publicationTime>2026-10-28T13:12:17.098+01:00</com:publicationTime>`
	const pointRecord = `  <sit:situation id="900%[1]s">
    <sit:situationRecord xsi:type="sit:Roadworks" id="%[1]s" version="1">
      <sit:situationRecordCreationTime>2026-10-20T10:00:00.000+01:00</sit:situationRecordCreationTime>
      <sit:situationRecordVersionTime>2026-10-20T10:00:00.000+01:00</sit:situationRecordVersionTime>
      <sit:validity>
        <com:validityStatus>active</com:validityStatus>
        <com:validityTimeSpecification>
          <com:overallStartTime>2026-10-20T10:00:00.000+01:00</com:overallStartTime>
        </com:validityTimeSpecification>
      </sit:validity>
      <sit:cause><sit:causeType>roadMaintenance</sit:causeType></sit:cause>%[2]s
    </sit:situationRecord>
  </sit:situation>`
	// Inside the radius and with a province.
	inside := fmt.Sprintf(pointRecord, "900001", `
      <sit:locationReference xsi:type="loc:PointLocation">
        <loc:tpegPointLocation xsi:type="loc:TpegSimplePoint">
          <loc:point xsi:type="loc:TpegNonJunctionPoint">
            <loc:pointCoordinates><loc:latitude>38.0</loc:latitude><loc:longitude>-0.5</loc:longitude></loc:pointCoordinates>
            <loc:_tpegNonJunctionPointExtension>
              <loc:extendedTpegNonJunctionPoint>
                <lse:province>Toledo</lse:province>
                <lse:municipality>Ontígola</lse:municipality>
              </loc:extendedTpegNonJunctionPoint>
            </loc:_tpegNonJunctionPointExtension>
          </loc:point>
        </loc:tpegPointLocation>
      </sit:locationReference>`)
	// Far away and without a province: the geo filter drops it, the province
	// filter would count it as unlocated.
	far := fmt.Sprintf(pointRecord, "900002", `
      <sit:locationReference xsi:type="loc:PointLocation">
        <loc:tpegPointLocation xsi:type="loc:TpegSimplePoint">
          <loc:point xsi:type="loc:TpegNonJunctionPoint">
            <loc:pointCoordinates><loc:latitude>43.0</loc:latitude><loc:longitude>-5.0</loc:longitude></loc:pointCoordinates>
          </loc:point>
        </loc:tpegPointLocation>
      </sit:locationReference>`)
	// No location reference at all: no coordinates and no province, so both
	// stages count it as unlocated.
	unlocated := fmt.Sprintf(pointRecord, "900003", "")
	return []byte(header + "\n" + inside + "\n" + far + "\n" + unlocated + "\n</d2:payload>")
}

// TestIncidentsRadiusRunsBeforeApply pins the documented composition order
// (internal/geo, internal/filter docs) with the probe of the CMD-01 review:
// a feed whose three records make the two unlocated buckets diverge. It
// fails if the radius were applied after filter.Apply.
func TestIncidentsRadiusRunsBeforeApply(t *testing.T) {
	srv := incidentsBodyServer(t, radiusOrderFeed(t))
	cfg, fetcher := newIncidentsFetcher(t, srv.URL, goldenClock)

	// Control run, province only: the geo filter does not run, so the two
	// records without a province both land in meta.unlocated.
	byProvince, err := Incidents(context.Background(), cfg, fetcher, IncidentCriteria{
		Criteria: filter.Criteria{Provinces: []string{"Toledo"}},
	})
	if err != nil {
		t.Fatalf("Incidents (province only) failed: %v", err)
	}
	if len(byProvince.Incidents) != 1 || byProvince.Incidents[0].ID != "dgt:900001" {
		t.Errorf("province-only kept %v, want only dgt:900001", idsOf(byProvince.Incidents))
	}
	if byProvince.Meta == nil || byProvince.Meta.Unlocated != 2 {
		t.Errorf("province-only meta.unlocated = %v, want 2 (no province and no location at all)", byProvince.Meta)
	}

	// Radius and province together: the geo filter must run first, so the
	// far record is dropped by geo and only the unlocated one (no location
	// at all) lands in meta.unlocated. If the order flipped, the province
	// filter would run first and report unlocated == 2 instead.
	radius, err := Incidents(context.Background(), cfg, fetcher, IncidentCriteria{
		Criteria: filter.Criteria{Provinces: []string{"Toledo"}},
		Lat:      floatPtr(38.0),
		Lon:      floatPtr(-0.5),
		RadiusKm: 50,
	})
	if err != nil {
		t.Fatalf("Incidents (radius + province) failed: %v", err)
	}
	if len(radius.Incidents) != 1 || radius.Incidents[0].ID != "dgt:900001" {
		t.Errorf("radius kept %v, want only dgt:900001", idsOf(radius.Incidents))
	}
	if radius.Meta == nil || radius.Meta.Unlocated != 1 {
		t.Errorf("radius meta.unlocated = %v, want 1 (only the record without location)", radius.Meta)
	}
}

func idsOf(incidents []model.Incident) []string {
	out := make([]string, 0, len(incidents))
	for _, inc := range incidents {
		out = append(out, inc.ID)
	}
	return out
}

// loadIncidentsResponseSchema loads schemas/incidents-response.schema.json
// together with a $ref loader rooted at schemas/.
func loadIncidentsResponseSchema(t *testing.T) (*schemaNode, *refLoader) {
	t.Helper()
	schemasDir := filepath.Join("..", "..", "schemas")
	schemaData, err := os.ReadFile(filepath.Join(schemasDir, "incidents-response.schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var schema schemaNode
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	return &schema, &refLoader{dir: schemasDir, cache: map[string]*schemaNode{}}
}

// TestGoldenValidatorResolvesIncidentRef proves the focused validator
// resolves the local $ref of schemas/incidents-response.schema.json to
// incident.schema.json: mutating a golden incident's severity to a value
// outside the incident enum must fail validation. Before the $ref support
// the incident nodes validated as "anything" and this mutation went
// unnoticed (CMD-01 review, minor 4).
func TestGoldenValidatorResolvesIncidentRef(t *testing.T) {
	schema, loader := loadIncidentsResponseSchema(t)
	raw, err := os.ReadFile(filepath.Join("testdata", "incidents-unfiltered.golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	// Control: the golden as published validates.
	if errs := schema.validate(doc, "", loader); len(errs) > 0 {
		t.Fatalf("the published golden must validate: %s", strings.Join(errs, "; "))
	}
	// The envelope schema says nothing about incident fields; only the
	// referenced incident.schema.json knows the severity enum. A violation
	// here is therefore only detectable when the $ref is resolved.
	incidents, ok := doc["incidents"].([]any)
	if !ok || len(incidents) == 0 {
		t.Fatal("the golden carries no incidents array to mutate")
	}
	first, ok := incidents[0].(map[string]any)
	if !ok {
		t.Fatal("the first golden incident is not an object")
	}
	first["severity"] = "catastrófica"
	if errs := schema.validate(doc, "", loader); len(errs) == 0 {
		t.Fatal("a golden incident with an invalid severity passed validation: the $ref to incident.schema.json was not resolved")
	}
}

// TestIncidentsCoverageNote pins the rule decided by the user and written
// into docs/ARRANQUE.md §5.3: the note names the managers (SCT, Gobierno
// Vasco), and disappears only when a --province filter restricts to
// provinces outside Catalonia and the Basque Country (docs/feeds.md §4.6).
func TestIncidentsCoverageNote(t *testing.T) {
	srv := incidentsFixtureServer(t)
	tests := []struct {
		name      string
		provinces []string
		want      bool
	}{
		{name: "unfiltered carries the note", provinces: nil, want: true},
		{name: "barcelona carries the note (the DGT does publish there)", provinces: []string{"barcelona"}, want: true},
		{name: "girona carries the note", provinces: []string{"Girona"}, want: true},
		{name: "bizkaia carries the note", provinces: []string{"bizkaia"}, want: true},
		{name: "alicante omits the note", provinces: []string{"alicante"}, want: false},
		{name: "mixed filter keeps the note", provinces: []string{"alicante", "girona"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, fetcher := newIncidentsFetcher(t, srv.URL, goldenClock)
			response, err := Incidents(context.Background(), cfg, fetcher, IncidentCriteria{
				Criteria: filter.Criteria{Provinces: tt.provinces},
			})
			if err != nil {
				t.Fatalf("Incidents failed: %v", err)
			}
			if tt.want && response.Meta.CoverageNote != coverageNoteRegional {
				t.Errorf("coverage_note = %q, want the regional-manager literal", response.Meta.CoverageNote)
			}
			if !tt.want && response.Meta.CoverageNote != "" {
				t.Errorf("coverage_note = %q, want it omitted", response.Meta.CoverageNote)
			}
		})
	}
}

// TestIncidentsQueryEchoesLatLon pins the echo of the geo centre: when the
// radius was centred with --lat/--lon, the query block carries those exact
// values (so an agent can verify what produced the centre); when no geo
// filter ran, the keys stay absent (P-04).
func TestIncidentsQueryEchoesLatLon(t *testing.T) {
	srv := incidentsFixtureServer(t)
	cfg, fetcher := newIncidentsFetcher(t, srv.URL, goldenClock)

	geo, err := Incidents(context.Background(), cfg, fetcher, IncidentCriteria{
		Lat:      floatPtr(38.15),
		Lon:      floatPtr(-0.575),
		RadiusKm: 10,
	})
	if err != nil {
		t.Fatalf("Incidents with lat/lon failed: %v", err)
	}
	if geo.Query == nil {
		t.Fatal("the response carries no query block")
	}
	if geo.Query.Lat == nil || *geo.Query.Lat != 38.15 {
		t.Errorf("query.lat = %v, want 38.15", geo.Query.Lat)
	}
	if geo.Query.Lon == nil || *geo.Query.Lon != -0.575 {
		t.Errorf("query.lon = %v, want -0.575", geo.Query.Lon)
	}
	if geo.Query.Radius == nil || *geo.Query.Radius != 10 {
		t.Errorf("query.radius = %v, want 10", geo.Query.Radius)
	}

	// Without a geo filter the keys stay absent, not null (P-04).
	plain, err := Incidents(context.Background(), cfg, fetcher, IncidentCriteria{})
	if err != nil {
		t.Fatalf("Incidents without a geo filter failed: %v", err)
	}
	if plain.Query == nil {
		t.Fatal("the response carries no query block")
	}
	if plain.Query.Lat != nil || plain.Query.Lon != nil {
		t.Errorf("query.lat/lon = %v/%v, want both absent", plain.Query.Lat, plain.Query.Lon)
	}
	if plain.Query.Radius != nil {
		t.Errorf("query.radius = %v, want absent", plain.Query.Radius)
	}
}

// TestIncidentShow pins `incidents show`: the incident with the requested id
// comes back unfiltered, and an absent id is a not_found error (exit 4).
func TestIncidentShow(t *testing.T) {
	srv := incidentsFixtureServer(t)
	cfg, fetcher := newIncidentsFetcher(t, srv.URL, goldenClock)

	detail, err := IncidentDetail(context.Background(), cfg, fetcher, "dgt:18811074")
	if err != nil {
		t.Fatalf("IncidentDetail failed: %v", err)
	}
	if len(detail.Incidents) != 1 {
		t.Fatalf("show returned %d incidents, want exactly 1", len(detail.Incidents))
	}
	if detail.Incidents[0].ID != "dgt:18811074" {
		t.Errorf("show returned id %q, want dgt:18811074", detail.Incidents[0].ID)
	}
	if detail.Incidents[0].Road != "N-400" {
		t.Errorf("show returned road %q, want N-400", detail.Incidents[0].Road)
	}
	if detail.Meta == nil || detail.Meta.Total != 1 {
		t.Errorf("show meta.total = %v, want 1", detail.Meta)
	}

	_, err = IncidentDetail(context.Background(), cfg, fetcher, "dgt:does-not-exist")
	if err == nil {
		t.Fatal("show for an absent id must fail")
	}
	var target *errs.Error
	if !asErrsError(err, &target) || target.ExitCode != errs.ExitNotFound {
		t.Fatalf("absent id error = %v, want not_found (exit 4)", err)
	}

	byID, err := IncidentByID(context.Background(), cfg, fetcher, "dgt:18811074")
	if err != nil {
		t.Fatalf("IncidentByID failed: %v", err)
	}
	if byID.ID != "dgt:18811074" {
		t.Errorf("IncidentByID returned id %q, want dgt:18811074", byID.ID)
	}
}

// TestIncidentShowGolden byte-compares the `incidents show` output for a
// present id against its golden file, pinning total: 1, the one-element
// array, the absence of a query block (show takes no filters) and the
// presence of the coverage note (CMD-01 review, minor 5).
func TestIncidentShowGolden(t *testing.T) {
	srv := incidentsFixtureServer(t)
	cfg, fetcher := newIncidentsFetcher(t, srv.URL, goldenClock)

	response, err := IncidentDetail(context.Background(), cfg, fetcher, "dgt:18811074")
	if err != nil {
		t.Fatalf("IncidentDetail failed: %v", err)
	}
	var buf bytes.Buffer
	if err := output.Write(&buf, response, output.Options{}); err != nil {
		t.Fatalf("serialising the response: %v", err)
	}
	got := buf.Bytes()
	goldenPath := filepath.Join("testdata", "incident-show.golden.json")
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
		t.Errorf("golden mismatch for incident-show.golden.json\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	// The contract pins the shape, not only the bytes: total 1, exactly one
	// incident, no query block, and the coverage note always travels (§5.3).
	var doc map[string]any
	if err := json.Unmarshal(want, &doc); err != nil {
		t.Fatalf("golden is not one JSON value: %v", err)
	}
	meta, _ := doc["meta"].(map[string]any)
	if meta == nil || meta["total"] != float64(1) {
		t.Errorf("show meta.total = %v, want 1", meta["total"])
	}
	if incidents, _ := doc["incidents"].([]any); len(incidents) != 1 {
		t.Errorf("show incidents = %v, want exactly one element", doc["incidents"])
	}
	if _, present := doc["query"]; present {
		t.Error("show output must carry no query block: no filter applies to show")
	}
	if note, _ := meta["coverage_note"].(string); note == "" {
		t.Error("show must always carry the coverage note (§5.3)")
	}
}

// asErrsError is a local alias of errors.As for *errs.Error, so the test
// file does not need the errors import for one call.
func asErrsError(err error, target **errs.Error) bool {
	for err != nil {
		if e, ok := err.(*errs.Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// TestIncidentsValidatesBeforeFetch pins that invalid filter combinations
// fail as usage errors (exit 2) without spending a request on the feed.
func TestIncidentsValidatesBeforeFetch(t *testing.T) {
	srv, hits := newFeedServer(t, map[string]http.HandlerFunc{
		"/situations": func(w http.ResponseWriter, r *http.Request) { w.Write(snapshotFeed(t)) },
	})
	cfg, fetcher := newIncidentsFetcher(t, srv.URL, goldenClock)

	_, err := Incidents(context.Background(), cfg, fetcher, IncidentCriteria{
		Criteria: filter.Criteria{PK: &filter.Range{From: floatPtr(1), To: floatPtr(2)}},
	})
	var target *errs.Error
	if !asErrsError(err, &target) || target.ExitCode != errs.ExitUsage {
		t.Fatalf("--pk without --road: err = %v, want a usage error (exit 2)", err)
	}
	if got := hits("/situations"); got != 0 {
		t.Errorf("invalid criteria made %d requests, want 0", got)
	}
}

// TestIncidentsAppliesRadiusBeforeFilters pins the documented composition
// order (internal/geo doc): the radius runs first, its unlocated count lands
// in the same meta.unlocated bucket as the filter's, and the remaining
// filters apply to the survivors.
func TestIncidentsAppliesRadiusBeforeFilters(t *testing.T) {
	srv := incidentsFixtureServer(t)
	cfg, fetcher := newIncidentsFetcher(t, srv.URL, goldenClock)

	// Locate the V-21 incident and centre the radius on its start point.
	all, err := Incidents(context.Background(), cfg, fetcher, IncidentCriteria{
		Criteria: filter.Criteria{Status: "all", Roads: []string{"V-21"}},
	})
	if err != nil {
		t.Fatalf("Incidents failed: %v", err)
	}
	if len(all.Incidents) != 1 || all.Incidents[0].Location.Start == nil {
		t.Fatalf("expected exactly one V-21 incident with coordinates, got %+v", all.Incidents)
	}
	centre := *all.Incidents[0].Location.Start

	filtered, err := Incidents(context.Background(), cfg, fetcher, IncidentCriteria{
		Criteria: filter.Criteria{Status: "all"},
		Lat:      floatPtr(centre.Latitude),
		Lon:      floatPtr(centre.Longitude),
		RadiusKm: 1,
	})
	if err != nil {
		t.Fatalf("Incidents with radius failed: %v", err)
	}
	if len(filtered.Incidents) != 1 || filtered.Incidents[0].ID != all.Incidents[0].ID {
		t.Errorf("radius filter kept %d incidents, want exactly the V-21 one", len(filtered.Incidents))
	}
	if filtered.Meta.Unlocated != 0 {
		t.Errorf("meta.unlocated = %d, want 0: every fixture record has coordinates", filtered.Meta.Unlocated)
	}
}

// TestIncidentsMetaPins pins the freshness contract of the meta block:
// published_at comes from the feed, age_seconds never goes negative, and
// from_cache reflects the fetch (P-08).
func TestIncidentsMetaPins(t *testing.T) {
	srv := incidentsFixtureServer(t)
	cfg, fetcher := newIncidentsFetcher(t, srv.URL, goldenClock)

	response, err := Incidents(context.Background(), cfg, fetcher, IncidentCriteria{})
	if err != nil {
		t.Fatalf("Incidents failed: %v", err)
	}
	meta := response.Meta
	if meta == nil {
		t.Fatal("the response carries no meta")
	}
	if meta.Feed != config.FeedSituations {
		t.Errorf("meta.feed = %q, want %q", meta.Feed, config.FeedSituations)
	}
	if meta.PublishedAt == nil || !meta.PublishedAt.Equal(time.Date(2026, 10, 28, 12, 12, 17, 98e6, time.UTC)) {
		t.Errorf("meta.published_at = %v, want the fixture publication time", meta.PublishedAt)
	}
	if meta.AgeSeconds == nil || *meta.AgeSeconds != 6462 {
		t.Errorf("meta.age_seconds = %v, want 6462", meta.AgeSeconds)
	}
	// goldenClock is 6462 s after the fixture publication time, past the
	// 10-minute staleness threshold: the data are genuinely stale (P-08),
	// even though the download itself was live.
	if !meta.Stale {
		t.Error("meta.stale = false, want true: the fixture data are older than the staleness threshold")
	}
	if meta.FromCache {
		t.Error("meta.from_cache = true on a fresh download, want false")
	}
	if meta.Total != 24 {
		t.Errorf("meta.total = %d, want 24 (every fixture record is active at goldenClock)", meta.Total)
	}
}

// --- focused schema validator --------------------------------------------

// schemaNode is a validator for the subset of JSON Schema draft 2020-12 the
// contract schemas use (copied from the MODEL-01 test helper; no JSON Schema
// library is added, CMD-01 constraint). Local $ref values that name another
// file of schemas/ are resolved through refLoader (CMD-01 review, minor 4).
type schemaNode struct {
	Ref                  string                 `json:"$ref"`
	Type                 any                    `json:"type"`
	Const                any                    `json:"const"`
	Enum                 []any                  `json:"enum"`
	Properties           map[string]*schemaNode `json:"properties"`
	Required             []string               `json:"required"`
	AdditionalProperties *bool                  `json:"additionalProperties"`
	Items                *schemaNode            `json:"items"`
}

// refLoader resolves the local $ref values of a schema: only a plain file
// name that names a sibling of the loading schema in schemas/ is supported;
// anything else must fail loudly instead of silently skipping the node.
type refLoader struct {
	dir   string
	cache map[string]*schemaNode
}

func (l *refLoader) resolve(ref string) (*schemaNode, error) {
	if node, ok := l.cache[ref]; ok {
		return node, nil
	}
	if filepath.IsAbs(ref) || filepath.Base(ref) != ref || strings.Contains(ref, "://") || strings.Contains(ref, "#") {
		return nil, fmt.Errorf("unsupported $ref %q: only a schema file name inside schemas/ is supported", ref)
	}
	data, err := os.ReadFile(filepath.Join(l.dir, ref))
	if err != nil {
		return nil, fmt.Errorf("reading $ref %q: %w", ref, err)
	}
	var node schemaNode
	if err := json.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("parsing $ref %q: %w", ref, err)
	}
	l.cache[ref] = &node
	return &node, nil
}

// validate returns the list of violations of v against this schema node.
func (s *schemaNode) validate(v any, path string, loader *refLoader) []string {
	var errs []string
	at := path
	if at == "" {
		at = "(root)"
	}
	if s.Ref != "" {
		if loader == nil {
			return []string{fmt.Sprintf("%s: $ref %q found but no loader is configured", at, s.Ref)}
		}
		resolved, err := loader.resolve(s.Ref)
		if err != nil {
			return []string{fmt.Sprintf("%s: cannot resolve $ref: %v", at, err)}
		}
		return resolved.validate(v, path, loader)
	}
	if s.Const != nil && !jsonEqual(v, s.Const) {
		errs = append(errs, fmt.Sprintf("%s: %v does not equal const %v", at, v, s.Const))
	}
	if s.Type != nil && !s.typeMatches(v) {
		errs = append(errs, fmt.Sprintf("%s: %v does not match type %v", at, v, s.Type))
	}
	if len(s.Enum) > 0 {
		ok := false
		for _, e := range s.Enum {
			if jsonEqual(v, e) {
				ok = true
				break
			}
		}
		if !ok {
			errs = append(errs, fmt.Sprintf("%s: %v is not in enum", at, v))
		}
	}
	obj, isObj := v.(map[string]any)
	if isObj {
		for _, req := range s.Required {
			if _, ok := obj[req]; !ok {
				errs = append(errs, fmt.Sprintf("%s: missing required property %q", at, req))
			}
		}
		if s.AdditionalProperties != nil && !*s.AdditionalProperties {
			for key := range obj {
				if _, ok := s.Properties[key]; !ok {
					errs = append(errs, fmt.Sprintf("%s: unexpected property %q", at, key))
				}
			}
		}
		for key, sub := range s.Properties {
			if val, ok := obj[key]; ok {
				errs = append(errs, sub.validate(val, path+"/"+key, loader)...)
			}
		}
	}
	if arr, ok := v.([]any); ok && s.Items != nil {
		for i, item := range arr {
			errs = append(errs, s.Items.validate(item, fmt.Sprintf("%s[%d]", path, i), loader)...)
		}
	}
	return errs
}

// typeMatches checks the JSON type of v against the declared type, which may
// be a single string or an array such as ["string","null"].
func (s *schemaNode) typeMatches(v any) bool {
	check := func(t string) bool {
		switch t {
		case "string":
			_, ok := v.(string)
			return ok
		case "number":
			_, ok := v.(float64)
			return ok
		case "integer":
			f, ok := v.(float64)
			return ok && f == math.Trunc(f)
		case "boolean":
			_, ok := v.(bool)
			return ok
		case "null":
			return v == nil
		case "object":
			_, ok := v.(map[string]any)
			return ok
		case "array":
			_, ok := v.([]any)
			return ok
		}
		return false
	}
	switch t := s.Type.(type) {
	case string:
		return check(t)
	case []any:
		for _, e := range t {
			if str, ok := e.(string); ok && check(str) {
				return true
			}
		}
	}
	return false
}

// jsonEqual compares two decoded JSON values for deep equality; decoded
// numbers are always float64, so a plain == on them is safe.
func jsonEqual(a, b any) bool {
	aa, aok := a.(map[string]any)
	bb, bok := b.(map[string]any)
	if aok && bok {
		if len(aa) != len(bb) {
			return false
		}
		for k, av := range aa {
			bv, ok := bb[k]
			if !ok || !jsonEqual(av, bv) {
				return false
			}
		}
		return true
	}
	return a == b
}
