package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/output"
)

// newRouteStore returns an empty default config and the temp file path its
// mutations must persist to (never the real user config, AGENTS.md).
func newRouteStore(t *testing.T) (*config.Config, string) {
	t.Helper()
	return config.Default(), filepath.Join(t.TempDir(), "config.toml")
}

// routeSummaryFeed builds a synthetic situations snapshot with exactly two
// records: a medium roadworks on A-7 spanning pk 99-101, and a highest
// severity closure (roadClosed) on N-332 spanning pk 49-51. It gives the
// summary-counts test a fully deterministic input.
func routeSummaryFeed(t *testing.T) []byte {
	t.Helper()
	const header = `<?xml version="1.0" encoding="UTF-8"?>
<d2:payload xsi:type="sit:SituationPublication" xmlns:com="http://levelC/schema/3/common" xmlns:sit="http://levelC/schema/3/situation" xmlns:loc="http://levelC/schema/3/locationReferencing" xmlns:lse="http://levelC/schema/3/locationReferencingSpanishExtension" xmlns:d2="http://levelC/schema/3/d2Payload" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <com:publicationTime>2026-10-28T13:12:17.098+01:00</com:publicationTime>`
	const linearRecord = `  <sit:situation id="900%[1]s">
    <sit:situationRecord xsi:type="sit:Roadworks" id="%[1]s" version="1">
      <sit:situationRecordCreationTime>2026-10-20T10:00:00.000+01:00</sit:situationRecordCreationTime>
      <sit:situationRecordVersionTime>2026-10-20T10:00:00.000+01:00</sit:situationRecordVersionTime>
      <sit:severity>%[2]s</sit:severity>
      <sit:validity>
        <com:validityStatus>active</com:validityStatus>
        <com:validityTimeSpecification>
          <com:overallStartTime>2026-10-20T10:00:00.000+01:00</com:overallStartTime>
        </com:validityTimeSpecification>
      </sit:validity>
      <sit:cause>%[3]s</sit:cause>
      <sit:locationReference xsi:type="loc:SingleRoadLinearLocation">
        <loc:supplementaryPositionalDescription>
          <loc:roadInformation><loc:roadName>%[4]s</loc:roadName></loc:roadInformation>
        </loc:supplementaryPositionalDescription>
        <loc:tpegLinearLocation>
          <loc:tpegDirection>positive</loc:tpegDirection>
          <loc:tpegLinearLocationType>segment</loc:tpegLinearLocationType>
          <loc:to xsi:type="loc:TpegNonJunctionPoint">
            <loc:pointCoordinates><loc:latitude>38.10</loc:latitude><loc:longitude>-0.55</loc:longitude></loc:pointCoordinates>
            <loc:_tpegNonJunctionPointExtension>
              <loc:extendedTpegNonJunctionPoint>
                <lse:kilometerPoint>%[5]s</lse:kilometerPoint>
              </loc:extendedTpegNonJunctionPoint>
            </loc:_tpegNonJunctionPointExtension>
          </loc:to>
          <loc:from xsi:type="loc:TpegNonJunctionPoint">
            <loc:pointCoordinates><loc:latitude>38.00</loc:latitude><loc:longitude>-0.50</loc:longitude></loc:pointCoordinates>
            <loc:_tpegNonJunctionPointExtension>
              <loc:extendedTpegNonJunctionPoint>
                <lse:kilometerPoint>%[6]s</lse:kilometerPoint>
              </loc:extendedTpegNonJunctionPoint>
            </loc:_tpegNonJunctionPointExtension>
          </loc:from>
        </loc:tpegLinearLocation>
      </sit:locationReference>
    </sit:situationRecord>
  </sit:situation>`
	roadworks := `<sit:causeType>roadMaintenance</sit:causeType><sit:detailedCauseType><sit:roadMaintenanceType>roadworks</sit:roadMaintenanceType></sit:detailedCauseType>`
	closure := `<sit:causeType>roadOrCarriagewayOrLaneManagement</sit:causeType><sit:detailedCauseType><sit:roadOrCarriagewayOrLaneManagementType>roadClosed</sit:roadOrCarriagewayOrLaneManagementType></sit:detailedCauseType>`
	body := header + "\n" +
		// A-7 first in the feed, deliberately: the declaration-order test
		// declares N-332 first and must still get it back first.
		fmt.Sprintf(linearRecord, "001", "medium", roadworks, "A-7", "101", "99") + "\n" +
		fmt.Sprintf(linearRecord, "002", "highest", closure, "N-332", "51", "49") + "\n" +
		"</d2:payload>"
	return []byte(body)
}

func TestAddRoutePersistsAndLists(t *testing.T) {
	cfg, path := newRouteStore(t)

	if err := AddRoute(cfg, path, "trabajo", []string{"a7:100-140", "N-332:50-70"}, "both"); err != nil {
		t.Fatalf("AddRoute(trabajo) error = %v", err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load after AddRoute error = %v", err)
	}
	routes, err := Routes(reloaded)
	if err != nil {
		t.Fatalf("Routes() error = %v", err)
	}
	if len(routes) != 1 {
		t.Fatalf("Routes() returned %d routes, want 1", len(routes))
	}
	route := routes[0]
	if route.Name != "trabajo" || route.Direction != "both" {
		t.Errorf("route = %+v, want trabajo/both", route)
	}
	if len(route.Segments) != 2 {
		t.Fatalf("route has %d segments, want 2", len(route.Segments))
	}
	// Declaration order, not feed or map order.
	if route.Segments[0].Road != "A-7" || route.Segments[0].From != 100 || route.Segments[0].To != 140 {
		t.Errorf("segment 0 = %+v, want A-7 100-140", route.Segments[0])
	}
	if route.Segments[1].Road != "N-332" || route.Segments[1].From != 50 || route.Segments[1].To != 70 {
		t.Errorf("segment 1 = %+v, want N-332 50-70", route.Segments[1])
	}
}

// TestAddRouteRejectsInvalid pins the usage errors (exit 2) of route add:
// unsafe name, empty segment list, malformed segment, inverted range and
// unknown direction. None of them may touch the config file.
func TestAddRouteRejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		add  func(cfg *config.Config, path string) error
	}{
		{
			name: "name with a space",
			add: func(cfg *config.Config, path string) error {
				return AddRoute(cfg, path, "mi ruta", []string{"A-7:100-140"}, "both")
			},
		},
		{
			name: "no segments",
			add:  func(cfg *config.Config, path string) error { return AddRoute(cfg, path, "trabajo", nil, "both") },
		},
		{
			name: "segment without the range",
			add: func(cfg *config.Config, path string) error {
				return AddRoute(cfg, path, "trabajo", []string{"A-7"}, "both")
			},
		},
		{
			name: "inverted kilometre range",
			add: func(cfg *config.Config, path string) error {
				return AddRoute(cfg, path, "trabajo", []string{"A-7:140-100"}, "both")
			},
		},
		{
			name: "unknown direction",
			add: func(cfg *config.Config, path string) error {
				return AddRoute(cfg, path, "trabajo", []string{"A-7:100-140"}, "north")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, path := newRouteStore(t)
			err := tt.add(cfg, path)
			var target *errs.Error
			if !errors.As(err, &target) || target.ExitCode != errs.ExitUsage {
				t.Fatalf("AddRoute error = %v, want a usage error (exit 2)", err)
			}
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Errorf("a rejected route add touched the config file (stat error = %v), want no file at all", statErr)
			}
		})
	}
}

// TestRemoveRouteMissingIsNotFound pins exit 4 for a route that does not
// exist, and that nothing is written when the removal refuses.
func TestRemoveRouteMissingIsNotFound(t *testing.T) {
	cfg, path := newRouteStore(t)

	err := RemoveRoute(cfg, path, "trabajo")
	var target *errs.Error
	if !errors.As(err, &target) || target.ExitCode != errs.ExitNotFound {
		t.Fatalf("RemoveRoute(missing) error = %v, want not_found (exit 4)", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("a refused removal wrote the config file (stat error = %v)", statErr)
	}
}

// TestRemoveRoutePersists pins that the removal deletes exactly the
// requested route and survives a reload.
func TestRemoveRoutePersists(t *testing.T) {
	cfg, path := newRouteStore(t)
	if err := AddRoute(cfg, path, "trabajo", []string{"A-7:100-140"}, "both"); err != nil {
		t.Fatalf("AddRoute(trabajo) error = %v", err)
	}
	if err := AddRoute(cfg, path, "casa", []string{"CV-86:0-6"}, "positive"); err != nil {
		t.Fatalf("AddRoute(casa) error = %v", err)
	}

	if err := RemoveRoute(cfg, path, "casa"); err != nil {
		t.Fatalf("RemoveRoute(casa) error = %v", err)
	}

	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load after RemoveRoute error = %v", err)
	}
	routes, err := Routes(reloaded)
	if err != nil {
		t.Fatalf("Routes() error = %v", err)
	}
	if len(routes) != 1 || routes[0].Name != "trabajo" {
		t.Fatalf("Routes() after removal = %+v, want only trabajo", routes)
	}
}

// newRouteCheckFetcher builds a config and fetcher over a test server that
// serves body on /situations, with the fixed golden clock so status and
// freshness stay deterministic.
func newRouteCheckFetcher(t *testing.T, body []byte) (*config.Config, *feeds.Fetcher, func(string) int) {
	t.Helper()
	srv, hits := newFeedServer(t, map[string]http.HandlerFunc{
		"/situations": func(w http.ResponseWriter, r *http.Request) { w.Write(body) },
	})
	cfg := testConfig(srv.URL)
	// testConfig only builds the feeds table; the route tests need the
	// routes map of the full config.
	cfg.Routes = map[string]config.Route{}
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")
	fetcher.Now = func() time.Time { return goldenClock }
	return cfg, fetcher, hits
}

// TestRouteCheckGroupsInDeclarationOrder pins docs/ARRANQUE.md §5.4: the
// segments come back in the order the route declares them, even when the
// feed publishes their incidents in the opposite order (the synthetic feed
// publishes A-7 first, the route declares N-332 first), and each segment
// carries only its own road's incidents.
func TestRouteCheckGroupsInDeclarationOrder(t *testing.T) {
	cfg, fetcher, _ := newRouteCheckFetcher(t, routeSummaryFeed(t))
	cfg.Routes["orden"] = config.Route{Direction: "both", Segments: []string{"N-332:0-100", "A-7:0-200"}}

	response, err := RouteCheck(context.Background(), cfg, fetcher, "orden")
	if err != nil {
		t.Fatalf("RouteCheck(orden) error = %v", err)
	}
	if len(response.Segments) != 2 {
		t.Fatalf("RouteCheck returned %d segments, want 2", len(response.Segments))
	}
	if response.Segments[0].Road != "N-332" {
		t.Errorf("segment 0 = %q, want N-332 (declaration order, not feed order)", response.Segments[0].Road)
	}
	if response.Segments[1].Road != "A-7" {
		t.Errorf("segment 1 = %q, want A-7", response.Segments[1].Road)
	}
	if got := response.Segments[0].PK; got.From != 0 || got.To != 100 {
		t.Errorf("segment 0 pk = %+v, want 0-100", got)
	}
	for _, segment := range response.Segments {
		for _, incident := range segment.Incidents {
			want := "dgt:002"
			if segment.Road == "A-7" {
				want = "dgt:001"
			}
			if incident.ID != want {
				t.Errorf("segment %s carries incident %q, want %q", segment.Road, incident.ID, want)
			}
		}
	}
}

// TestRouteCheckSummaryCounts pins the §5.4 summary: total incidents, how
// many are high severity, and how many are closures.
func TestRouteCheckSummaryCounts(t *testing.T) {
	cfg, fetcher, _ := newRouteCheckFetcher(t, routeSummaryFeed(t))
	cfg.Routes["resumen"] = config.Route{Direction: "both", Segments: []string{"A-7:0-200", "N-332:0-100"}}

	response, err := RouteCheck(context.Background(), cfg, fetcher, "resumen")
	if err != nil {
		t.Fatalf("RouteCheck(resumen) error = %v", err)
	}
	want := RouteCheckSummary{Incidents: 2, HighSeverity: 1, Closures: 1}
	if response.Summary != want {
		t.Errorf("summary = %+v, want %+v", response.Summary, want)
	}
	// The meta block keeps the incidents contract: one fetch, published_at
	// from the feed, attribution present.
	if response.Meta == nil || response.Meta.Total != 2 {
		t.Errorf("meta.total = %+v, want 2", response.Meta)
	}
	if response.Meta != nil && response.Meta.CoverageNote == "" {
		t.Error("route check must carry the coverage note: a route filter is not a province filter (§5.3)")
	}
}

// TestRouteCheckFetchesTheFeedOnce pins the ROUTE-01 constraint: a
// multi-segment route check makes exactly one HTTP request, never one per
// segment.
func TestRouteCheckFetchesTheFeedOnce(t *testing.T) {
	cfg, fetcher, hits := newRouteCheckFetcher(t, routeSummaryFeed(t))
	cfg.Routes["larga"] = config.Route{Direction: "both", Segments: []string{
		"A-7:0-50", "A-7:50-100", "N-332:0-100",
	}}

	if _, err := RouteCheck(context.Background(), cfg, fetcher, "larga"); err != nil {
		t.Fatalf("RouteCheck(larga) error = %v", err)
	}
	if got := hits("/situations"); got != 1 {
		t.Errorf("route check made %d requests for a 3-segment route, want 1", got)
	}
}

// TestRouteCheckMissingRouteIsNotFound pins exit 4 without spending any
// request on the feed.
func TestRouteCheckMissingRouteIsNotFound(t *testing.T) {
	cfg, fetcher, hits := newRouteCheckFetcher(t, routeSummaryFeed(t))

	_, err := RouteCheck(context.Background(), cfg, fetcher, "desconocida")
	var target *errs.Error
	if !errors.As(err, &target) || target.ExitCode != errs.ExitNotFound {
		t.Fatalf("RouteCheck(missing) error = %v, want not_found (exit 4)", err)
	}
	if got := hits("/situations"); got != 0 {
		t.Errorf("a missing route made %d requests, want 0", got)
	}
}

// TestRouteCheckGolden byte-compares the `route check` output of a
// two-segment route against its golden file, pinning the §5.4 shape: the
// declaration order, the pk echo, the incidents arrays (never null) and the
// summary block.
func TestRouteCheckGolden(t *testing.T) {
	body := snapshotFeed(t)
	cfg, fetcher, _ := newRouteCheckFetcher(t, body)
	// A-372 carries three incidents between pk 40 and 55 in the snapshot
	// and CT-34 two between 0 and 10 (one of them highest severity); both
	// roads publish after each other in the feed, so the golden also pins
	// that the declaration order wins.
	cfg.Routes["trabajo"] = config.Route{Direction: "both", Segments: []string{"A-372:40-55", "CT-34:0-10"}}

	response, err := RouteCheck(context.Background(), cfg, fetcher, "trabajo")
	if err != nil {
		t.Fatalf("RouteCheck(trabajo) error = %v", err)
	}
	var buf bytes.Buffer
	if err := output.Write(&buf, response, output.Options{}); err != nil {
		t.Fatalf("serialising the response: %v", err)
	}
	got := buf.Bytes()
	goldenPath := filepath.Join("testdata", "route-check.golden.json")
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
		t.Errorf("golden mismatch for route-check.golden.json\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
