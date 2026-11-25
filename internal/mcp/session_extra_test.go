package mcp

import (
	"testing"

	"github.com/aam9063/trafico-cli/internal/config"
)

// TestRutasToolListContract pins the list half of the `rutas` tool: with an
// empty configuration it answers the `trafico route list --json` contract —
// schema_version, attribution, and an empty routes array, never null.
func TestRutasToolListContract(t *testing.T) {
	srv, hits := mcpFixtureServer(t, "*")
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	doc := contractJSON(t, callTool(t, session, "rutas", map[string]any{}))
	assertEnvelope(t, doc)
	if hits("/situations") != 0 {
		t.Fatal("listing saved routes never fetches a feed (S3: app.Routes reads config only)")
	}
	if routes := payloadOf(t, doc, "routes"); len(routes) != 0 {
		t.Fatalf("routes = %v, want an empty array with no saved route", routes)
	}
}

// TestRutasToolListsWithRoutes pins the list contract with real saved routes:
// every route with its segments parsed, in name order.
func TestRutasToolListsWithRoutes(t *testing.T) {
	srv, _ := mcpFixtureServer(t, "*")
	deps, _ := newMCPDeps(t, srv.URL)
	deps.Config.Routes = map[string]config.Route{
		"trabajo": {
			Direction: "both",
			Segments:  []string{"A-372:40-55", "CT-34:0-10"},
		},
	}
	session := connectMCP(t, deps)

	doc := contractJSON(t, callTool(t, session, "rutas", map[string]any{}))
	assertEnvelope(t, doc)
	routes := payloadOf(t, doc, "routes")
	if len(routes) != 1 {
		t.Fatalf("routes = %d entries, want the one saved route", len(routes))
	}
	route := routes[0].(map[string]any)
	if route["name"] != "trabajo" {
		t.Errorf("routes[0].name = %v, want trabajo", route["name"])
	}
	segments := payloadOf(t, route, "segments")
	if len(segments) != 2 {
		t.Fatalf("segments = %d entries, want the 2 declared segments", len(segments))
	}
	first := segments[0].(map[string]any)
	if first["road"] != "A-372" || first["from"] != float64(40) || first["to"] != float64(55) {
		t.Errorf("routes[0].segments[0] = %v, want the parsed A-372:40-55", first)
	}
}

// TestRutasToolCheckContract pins the check half: with a name it answers the
// `trafico route check --json` contract — one segment entry per declared
// segment in declaration order, each with its incidents, plus the §5.4
// summary. One fetch of the situations feed for the whole route (S3).
func TestRutasToolCheckContract(t *testing.T) {
	srv, hits := mcpFixtureServer(t, "situations")
	deps, _ := newMCPDeps(t, srv.URL)
	deps.Config.Routes = map[string]config.Route{
		"trabajo": {
			Direction: "both",
			Segments:  []string{"A-372:40-55", "CT-34:0-10"},
		},
	}
	session := connectMCP(t, deps)

	doc := contractJSON(t, callTool(t, session, "rutas", map[string]any{"name": "trabajo"}))
	assertEnvelope(t, doc)
	if hits("/situations") != 1 {
		t.Fatalf("the route check made %d requests, want exactly one fetch of the situations feed", hits("/situations"))
	}
	if doc["route"] != "trabajo" {
		t.Errorf("route = %v, want the requested name echoed", doc["route"])
	}
	segments := payloadOf(t, doc, "segments")
	if len(segments) != 2 {
		t.Fatalf("segments = %d entries, want the 2 declared segments in order", len(segments))
	}
	if len(segments[0].(map[string]any)["incidents"].([]any)) != 3 {
		t.Errorf("the first segment carries the wrong incident count: %v", segments[0])
	}
	summary := doc["summary"].(map[string]any)
	if summary["incidents"] != float64(5) {
		t.Errorf("summary.incidents = %v, want 5 (the fixture route carries five)", summary["incidents"])
	}
}

// TestRutasToolUnknownRouteIsToolError pins that an unknown route name comes
// back as a tool error with the not-found message, and that no feed request
// repairs it.
func TestRutasToolUnknownRouteIsToolError(t *testing.T) {
	srv, hits := mcpFixtureServer(t, "situations")
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	result := callTool(t, session, "rutas", map[string]any{"name": "viaje"})
	if !result.IsError {
		t.Fatal("an unknown route must be a tool error, not an empty answer")
	}
	if hits("/situations") != 0 {
		t.Fatal("an unknown route was resolved by consulting the feed")
	}
}

// TestEstadoFeedsToolListContract pins the no-request half of the
// `estado_feeds` tool: reads the config only, reports enabled and never
// invents freshness about feeds it did not check (S3, S7).
func TestEstadoFeedsToolListContract(t *testing.T) {
	srv, hits := mcpFixtureServer(t, "*")
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	doc := contractJSON(t, callTool(t, session, "estado_feeds", map[string]any{}))
	assertEnvelope(t, doc)
	totalRequests := 0
	for _, path := range []string{"/situations", "/vms-locations", "/vms-messages", "/radars", "/invive", "/tefiva"} {
		if n := hits(path); n > 0 {
			totalRequests += n
		}
	}
	if totalRequests != 0 {
		t.Fatalf("estado_feeds without check made %d requests, want 0", totalRequests)
	}
	feeds := payloadOf(t, doc, "feeds")
	if len(feeds) != 6 {
		t.Fatalf("feeds = %d entries, want the 6 configured feeds", len(feeds))
	}
	first := feeds[0].(map[string]any)
	if first["status"] != "enabled" {
		t.Errorf("feeds[0].status = %v, want enabled: the list never claims checked health", first["status"])
	}
}

// TestEstadoFeedsToolCheckContract pins the checking half: with check true
// the tool fetches every enabled feed once and reports the healthy ones as
// ok.
func TestEstadoFeedsToolCheckContract(t *testing.T) {
	srv, hits := mcpFixtureServer(t, "*")
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	doc := contractJSON(t, callTool(t, session, "estado_feeds", map[string]any{"check": true}))
	assertEnvelope(t, doc)
	for path := range map[string]bool{"/situations": true, "/vms-locations": true, "/vms-messages": true, "/radars": true, "/invive": true, "/tefiva": true} {
		if hits(path) != 1 {
			t.Errorf("estado_feeds hit %s %d times, want exactly one check", path, hits(path))
		}
	}
	for _, feed := range payloadOf(t, doc, "feeds") {
		status := feed.(map[string]any)["status"]
		if status != "ok" {
			t.Errorf("feed %v reports %v, want ok against healthy fixtures", feed.(map[string]any)["name"], status)
		}
	}
}

// TestEstadoFeedsToolSurfacesDisabledFeeds pins the mixed case: a disabled
// feed is never fetched and never reported ok (CMD-02).
func TestEstadoFeedsToolSurfacesDisabledFeeds(t *testing.T) {
	srv, hits := mcpFixtureServer(t, "radars", "situations")
	deps, _ := newMCPDeps(t, srv.URL)
	disabled := deps.Config.Feeds[config.FeedRadars]
	disabled.Enabled = false
	deps.Config.Feeds[config.FeedRadars] = disabled
	session := connectMCP(t, deps)

	doc := contractJSON(t, callTool(t, session, "estado_feeds", map[string]any{"check": true}))
	if hits("/radars") != 0 {
		t.Fatal("estado_feeds fetched a disabled feed: spending a request on it is waste (CMD-02)")
	}
	byName := make(map[string]map[string]any)
	for _, feed := range payloadOf(t, doc, "feeds") {
		entry := feed.(map[string]any)
		byName[entry["name"].(string)] = entry
	}
	radar := byName[config.FeedRadars]
	if radar["status"] != "disabled" {
		t.Errorf("a disabled feed reports %v, want disabled", radar["status"])
	}
}
