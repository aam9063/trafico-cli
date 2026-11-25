package mcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestToolsListOverSession pins the v1 wire surface (S1, S2): exactly the six
// read-only tools — no extra, no missing — and every one of them annotated
// read-only, cross-checked against the registry it came from.
func TestToolsListOverSession(t *testing.T) {
	srv, _ := mcpFixtureServer(t) // not fetched: tools/list is metadata only
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	result, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}
	want := []string{"incidencias", "paneles", "radares", "tramos", "rutas", "estado_feeds"}
	wanted := make(map[string]bool, len(want))
	for _, name := range want {
		wanted[name] = true
	}
	if len(result.Tools) != len(want) {
		t.Fatalf("tools/list returned %d tools, want %d: %v", len(result.Tools), len(want), result.Tools)
	}
	byName := toolByName(t, result.Tools)
	for name, tool := range byName {
		if !wanted[name] {
			t.Errorf("tools/list returned the unexpected tool %q (v1 surface is %v)", name, want)
			continue
		}
		if !tool.Annotations.ReadOnlyHint {
			t.Errorf("the wire tool %q is not annotated read-only (S2)", name)
		}
		entry, ok := registryEntry(name)
		if !ok {
			t.Fatalf("the wire tool %q has no registry entry", name)
		}
		if entry.Mutating {
			t.Errorf("the registry marks %q as mutating, want a read-only v1 entry", name)
		}
	}
}

// TestMutatingToolNeverReachesTheWire pins the S2 gate at code level: a
// mutating registry entry exists in the data structure (the door is open to
// future routes/watches writes) but the server refuses to expose it. If the
// gate is removed, tools/list gains a mutating tool and this test fails.
func TestMutatingToolNeverReachesTheWire(t *testing.T) {
	srv, _ := mcpFixtureServer(t)
	deps, _ := newMCPDeps(t, srv.URL)

	entries := append(toolRegistry(), ToolDefinition{
		Name:        "escribir-demo",
		Description: "demo of a mutating tool: the v1 gate must keep it off the wire",
		Mutating:    true,
		Register: func(server *mcp.Server, _ Deps) {
			mcp.AddTool(server, &mcp.Tool{Name: "escribir-demo", Description: "demo"}, func(
				_ context.Context, _ *mcp.CallToolRequest, args struct{},
			) (*mcp.CallToolResult, any, error) {
				panic("a mutating v1 tool must never be registered, let alone called")
			})
		},
	})

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := newServer(deps, entries)
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatalf("connecting the server: %v", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil).
		Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connecting the client: %v", err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}
	byName := toolByName(t, result.Tools)
	if _, exposed := byName["escribir-demo"]; exposed {
		t.Fatal("the v1 server exposes a mutating tool: the S2 gate leaked (docs/mcp.md: route and watch writes are the extension point)")
	}
	if len(result.Tools) != 6 {
		t.Fatalf("the read-only surface changed to %d tools, want 6", len(result.Tools))
	}
}

// TestIncidenciasToolContract pins the incidents tool against the same
// snap-1 fixture and the same app.Incidents the command uses (S3, S4): the
// contract JSON carries schema_version, attribution, meta.published_at
// (this feed publishes one) and the two A-372 incidents in the pk window.
func TestIncidenciasToolContract(t *testing.T) {
	srv, hits := mcpFixtureServer(t, "situations")
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	pkFrom, pkTo := 50.0, 51.0
	result := contractJSON(t, callTool(t, session, "incidencias", map[string]any{
		"road":    []string{"A-372"},
		"pk_from": pkFrom,
		"pk_to":   pkTo,
	}))
	assertEnvelope(t, result)
	if hits("/situations") != 1 {
		t.Fatalf("the tool made %d requests to the situations feed, want 1", hits("/situations"))
	}
	incidents := payloadOf(t, result, "incidents")
	if len(incidents) != 2 {
		t.Fatalf("incidents = %d entries, want the 2 of the fixture snapshot in the pk window", len(incidents))
	}
	road := incidents[0].(map[string]any)["road"]
	if road != "A-372" {
		t.Errorf("incidents[0].road = %v, want A-372", road)
	}
}

// TestIncidenciasToolRejectsBadInput pins that an invalid combination —
// direction outside the documented set — comes back as a tool ERROR with a
// user-facing Spanish message, and that nothing else changed.
func TestIncidenciasToolRejectsBadInput(t *testing.T) {
	srv, _ := mcpFixtureServer(t, "situations")
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	result := callTool(t, session, "incidencias", map[string]any{"direction": "norte"})
	if !result.IsError {
		t.Fatalf("the tool accepted an unknown direction: %v", result.Content)
	}
	if text := textOf(t, result); text == "" {
		t.Fatal("the error result carries no message for the agent")
	}
}

// TestPanelesToolContract pins the panels tool: the joined VMS contract with
// the five fixture panels and a published_at of its own (S4).
func TestPanelesToolContract(t *testing.T) {
	srv, hits := mcpFixtureServer(t, "vms-locations", "vms-messages")
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	doc := contractJSON(t, callTool(t, session, "paneles", map[string]any{}))
	assertEnvelope(t, doc)
	if hits("/vms-locations") != 1 || hits("/vms-messages") != 1 {
		t.Fatalf("the tool fetched each VMS feed once, got locations=%d messages=%d", hits("/vms-locations"), hits("/vms-messages"))
	}
	signs := payloadOf(t, doc, "signs")
	if len(signs) != 5 {
		t.Fatalf("signs = %d entries, want the 5 of the fixture snapshot", len(signs))
	}
}

// TestRadaresToolContract pins the radars tool, including the feed fact that
// published_at stays null for this dataset (docs/feeds.md §9): null in the
// document, never an invented instant.
func TestRadaresToolContract(t *testing.T) {
	srv, _ := mcpFixtureServer(t, "radars")
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	doc := contractJSON(t, callTool(t, session, "radares", map[string]any{}))
	assertEnvelope(t, doc)
	radars := payloadOf(t, doc, "radars")
	if len(radars) != 12 {
		t.Fatalf("radars = %d entries, want the 12 of the fixture snapshot", len(radars))
	}
	if doc["meta"].(map[string]any)["published_at"] != nil {
		t.Errorf("published_at = %v, want null: the radar feed publishes no date", doc["meta"].(map[string]any)["published_at"])
	}
}

// TestTramosToolContract pins the sections tool: kind animals answers the
// three TEFIVA black spots of the fixture, in the route contract.
func TestTramosToolContract(t *testing.T) {
	srv, _ := mcpFixtureServer(t, "tefiva")
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	doc := contractJSON(t, callTool(t, session, "tramos", map[string]any{"kind": "animals"}))
	assertEnvelope(t, doc)
	if sections := payloadOf(t, doc, "sections"); len(sections) != 3 {
		t.Fatalf("sections = %d entries, want the 3 of the fixture snapshot", len(sections))
	}

	// An unknown kind is a usage error, the same usage error the command
	// raises: app.Sections owns the rule (S3).
	result := callTool(t, session, "tramos", map[string]any{"kind": "urbanas"})
	if !result.IsError {
		t.Fatal("the tool accepted an unknown kind")
	}
}

// TestTramosToolRequiresKind pins that a missing kind is a usage error, not
// a silent default between two different datasets.
func TestTramosToolRequiresKind(t *testing.T) {
	srv, _ := mcpFixtureServer(t, "tefiva")
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	result := callTool(t, session, "tramos", map[string]any{})
	if !result.IsError {
		t.Fatal("the tool defaulted the kind: there is no sensible default between invive and animals")
	}
}
