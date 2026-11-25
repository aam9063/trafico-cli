package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/feeds"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpClock sits after the publication time of every snap-1 fixture
// (2026-10-28T12:12:17Z and similar) and before no validity start, so every
// fixture incident is active and the output is deterministic (the same fixed
// clock the app suite runs on, P-08).
var mcpClock = time.Date(2026, 10, 28, 14, 0, 0, 0, time.UTC)

// mcpFixtureServer serves every snap-1 snapshot this suite answers from: the
// DGT situation feed, both VMS feeds, radars and both section datasets. These
// are loopback fixtures, never the real network (AGENTS.md, S7): MCP
// sessions run over the SDK in-memory transport and the DGT "network" is an
// httptest server in the test process.
func mcpFixtureServer(t *testing.T, datasets ...string) (*httptest.Server, func(string) int) {
	t.Helper()
	paths := map[string]string{
		"/situations":    "situations",
		"/vms-locations": "vms-locations",
		"/vms-messages":  "vms-messages",
		"/radars":        "radars",
		"/invive":        "invive",
		"/tefiva":        "tefiva",
	}
	var mu sync.Mutex
	counts := make(map[string]int)
	mux := http.NewServeMux()
	for path, dataset := range paths {
		if len(datasets) > 0 && !slices.Contains(datasets, dataset) && !slices.Contains(datasets, "*") {
			continue
		}
		body, err := os.ReadFile(fmt.Sprintf("../../testdata/feeds/%s/snap-1.xml", dataset))
		if err != nil {
			t.Fatalf("reading the %s snapshot: %v", dataset, err)
		}
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			counts[path]++
			mu.Unlock()
			if _, err := w.Write(body); err != nil {
				t.Errorf("writing the %s body: %v", path, err)
			}
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return counts[path]
	}
}

// newMCPDeps builds the Deps of a test server: every feed pointed at the
// fixture server, a fetcher with the fixed test clock, a temp cache and a
// diagnostics buffer the warning test can inspect.
func newMCPDeps(t *testing.T, srvURL string) (Deps, *bytes.Buffer) {
	t.Helper()
	cfg := &config.Config{
		Feeds: map[string]config.Feed{
			config.FeedSituations:      {URL: srvURL + "/situations", Enabled: true},
			config.FeedSigns:           {URL: srvURL + "/vms-locations", Enabled: true},
			config.FeedSignsMessages:   {URL: srvURL + "/vms-messages", Enabled: true},
			config.FeedRadars:          {URL: srvURL + "/radars", Enabled: true},
			config.FeedSectionsInvive:  {URL: srvURL + "/invive", Enabled: true},
			config.FeedSectionsAnimals: {URL: srvURL + "/tefiva", Enabled: true},
		},
		Places: map[string]string{"casa": "38.3452,-0.4810"},
		Stale:  config.Stale{MaxAge: config.DefaultMaxAge},
	}
	fetcher := feeds.NewFetcher(cfg, t.TempDir(), "test")
	fetcher.Now = func() time.Time { return mcpClock }
	diag := &bytes.Buffer{}
	return Deps{Config: cfg, Fetcher: fetcher, Version: "test", Diag: diag}, diag
}

// connectMCP connects a client to a fresh server over the SDK in-memory
// transport (S7): a full initialize handshake, exactly what an agent does.
func connectMCP(t *testing.T, deps Deps) *mcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := NewServer(deps)
	if _, err := server.Connect(context.Background(), serverTransport, nil); err != nil {
		t.Fatalf("connecting the server: %v", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil).
		Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connecting the client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// callTool invokes one tool and returns the client-visible result.
func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("calling %s: %v", name, err)
	}
	return result
}

// contractJSON extracts the one TextContent of a successful result and parses
// it as JSON, so every contract test reads the same keys a --json consumer
// would (S4).
func contractJSON(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	if result.IsError {
		t.Fatalf("the tool call ended in error: %s", textOf(t, result))
	}
	raw := textOf(t, result)
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("the result is not the JSON contract (not one JSON value): %v\n%s", err, raw)
	}
	return doc
}

// textOf asserts the result carries exactly one text content: the contract.
func textOf(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) != 1 {
		t.Fatalf("the result carries %d content items, want exactly one", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("the content is %T, want *mcp.TextContent", result.Content[0])
	}
	return text.Text
}

// assertEnvelope pins the shared envelope of a contract document:
// schema_version, attribution and meta.published_at present, whatever the
// payload (S4).
func assertEnvelope(t *testing.T, doc map[string]any) {
	t.Helper()
	if doc["schema_version"] != float64(1) {
		t.Errorf("schema_version = %v, want 1", doc["schema_version"])
	}
	if _, ok := doc["attribution"].(map[string]any); !ok {
		t.Fatalf("the document carries no attribution: %v", doc)
	}
	if _, ok := doc["meta"].(map[string]any); !ok {
		t.Fatalf("the document carries no meta: %v", doc)
	}
	if _, ok := doc["meta"].(map[string]any)["published_at"]; !ok {
		t.Fatal("the meta block carries no published_at key: null or instant, but the key travels always")
	}
}

// payloadOf extracts a document array by key.
func payloadOf(t *testing.T, doc map[string]any, key string) []any {
	t.Helper()
	raw, ok := doc[key].([]any)
	if !ok {
		t.Fatalf("the document carries no %s array: %v", key, doc)
	}
	return raw
}
