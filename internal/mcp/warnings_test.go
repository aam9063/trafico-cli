package mcp

import (
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
)

// TestCacheFallbackWarningNeverTouchesTheProtocol pins the P-01 rule of the
// stdio transport: a cache-fallback warning lands in the diagnostic writer,
// NOT in the tool answer's stdout contract beyond its own meta fields. The
// session still answers, and the answer says from_cache honestly (hay una
// vez que la DGT no responde y la caché existe).
func TestCacheFallbackWarningNeverTouchesTheProtocol(t *testing.T) {
	srv, hits := mcpFixtureServer(t, "situations")
	deps, diag := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	// Warm the cache: the first call is a real fetch against the fixture.
	first := contractJSON(t, callTool(t, session, "incidencias", map[string]any{}))
	if hits("/situations") != 1 {
		t.Fatalf("the first call made %d requests, want 1", hits("/situations"))
	}
	if first["meta"].(map[string]any)["from_cache"] != false {
		t.Fatal("the first call served the cache unexpectedly")
	}

	// Break the network (close the loopback server) and ask again: with the
	// clock past the 60 s TTL of the cache the fetch falls back to it, with
	// a warning on the diagnostic channel.
	srv.Close()
	deps.Fetcher.Now = func() time.Time { return mcpClock.Add(2 * time.Minute) }
	second := contractJSON(t, callTool(t, session, "incidencias", map[string]any{"road": []string{"A-372"}}))
	assertEnvelope(t, second)
	if second["meta"].(map[string]any)["from_cache"] != true {
		t.Fatal("after the network died the answer must come from the cache (meta.from_cache)")
	}
	if diag.Len() == 0 {
		t.Fatal("the cache fallback carried no warning on the diagnostic channel (P-01: stderr, never the protocol stdout)")
	}
}

// TestDiagWriterStaysQuietOnError pins that a tool error adds nothing to
// anything beyond the tool result: the rejected input leaves the contract
// and the diagnostics channel clean.
func TestDiagWriterStaysQuietOnError(t *testing.T) {
	srv, _ := mcpFixtureServer(t, "situations")
	deps, diag := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	result := callTool(t, session, "incidencias", map[string]any{"from": "ayer"})
	if !result.IsError {
		t.Fatal("a non-RFC3339 from must be rejected")
	}
	if diag.Len() != 0 {
		t.Errorf("a rejected input wrote %q to the diagnostics channel, want nothing", diag.String())
	}
}

// TestEstadoFeedsWarningTravelsToDiag pins the per-feed warning channel of
// the feeds tool: the stderr notes of the fetches (cache fallback) surface
// on the diagnostic writer, the tool answer keeps its own Warning key out
// (S4: the contract documents it as a stderr channel field).
func TestEstadoFeedsWarningTravelsToDiag(t *testing.T) {
	srv, _ := mcpFixtureServer(t, "situations")
	deps, diag := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	// Warm the situation cache, break the loopback, and check: the merged
	// status reports the situations feed with a Warning and the merged
	// status stays an ok-shape answer (one failing feed never fails the
	// whole status, the same contract as `trafico feeds status`).
	contractJSON(t, callTool(t, session, "estado_feeds", map[string]any{"check": true}))
	srv.Close()
	deps.Fetcher.Now = func() time.Time { return mcpClock.Add(2 * time.Minute) }
	doc := contractJSON(t, callTool(t, session, "estado_feeds", map[string]any{"check": true}))
	assertEnvelope(t, doc)
	if diag.Len() == 0 {
		t.Fatal("the cache fallback of one feed carried no warning on the diagnostic channel")
	}
	situations := feedStatusByName(t, doc, config.FeedSituations)
	if situations["stale"] != true && situations["from_cache"] != true {
		t.Errorf("the served cache fallback reports stale and from_cache honestly: %v", situations)
	}
}

// feedStatusByName indexes the feeds array of a status answer by feed name.
func feedStatusByName(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	for _, feed := range payloadOf(t, doc, "feeds") {
		entry := feed.(map[string]any)
		if entry["name"] == name {
			return entry
		}
	}
	t.Fatalf("the status answer carries no %q entry: %v", name, doc["feeds"])
	return nil
}
