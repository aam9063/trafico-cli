package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/output"
)

// TestContractParityIncidenciasAndTramos pins ola-7-mcp S4 at its full depth:
// a `tramos` or `incidencias` result over the real server (the one
// `trafico mcp` runs, over the SDK in-memory transport) carries exactly the
// same document as the same command's `--json` output over the same fixture,
// attribution and meta.published_at included. The assessment below compares
// the FULL recursive key sets of both documents — every object level, not a
// couple of spot-checked fields — so any key the MCP side takes away, adds or
// renames against the CLI document makes this test fail.
//
// If a key legitimately changes, that is a contract change (schemas/ plus a
// schema_version bump), never a silent divergence.
//
// The assessment is shape-level by design: key sets and value kinds at every
// object and array level. Only meta.age_seconds is ignored: it is now minus
// published_at, a clock-relative delta that two calls are free to make
// differently without breaking the contract (docs/ARRANQUE.md §5.3).
// meta.published_at itself is pinned to the same instant on both sides,
// because the feed declares it and the local clock does not (P-08); a null is
// legitimate (docs/feeds.md §9) and then both sides must carry null.
//
// Why the CLI document is built here from app+output instead of running the
// cobra command: internal/cli imports internal/mcp (the `trafico mcp`
// command), so this package cannot import it back. For these two commands the
// CLI path is a verbatim pass-through of the app response —
// writeIncidentsResponse (internal/cli/incidents.go) and writeSectionsResponse
// (internal/cli/sections.go) hand the app response straight to
// output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
// and with --json that is the compact encoding of the app response — so
// app.Incidents/app.Sections composed with output.Write are the bytes of the
// CLI document over this fixture. If the CLI layer ever starts reshaping
// those responses before writing them, this proxy must be extended in the
// same change; from then on the assessment covers the real CLI wrapper too.
func TestContractParityIncidenciasAndTramos(t *testing.T) {
	srv, _ := mcpFixtureServer(t)
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	cliFetcher := func(t *testing.T) *feeds.Fetcher {
		t.Helper()
		// The same fixed clock the MCP suite runs on (P-08): the CLI path
		// passes through it too, so the only clock-relative field left in a
		// compared document is meta.age_seconds, which the parity assessment
		// ignores by contract.
		fetcher := feeds.NewFetcher(deps.Config, t.TempDir(), "test")
		fetcher.Now = func() time.Time { return mcpClock }
		return fetcher
	}

	t.Run("incidencias", func(t *testing.T) {
		mcpDoc := contractJSON(t, callTool(t, session, "incidencias", map[string]any{}))
		response, err := app.Incidents(t.Context(), deps.Config, cliFetcher(t), app.IncidentCriteria{})
		if err != nil {
			t.Fatalf("building the CLI-path incidents response: %v", err)
		}
		assertContractParity(t, "incidencias", mcpDoc, cliContractDocument(t, response))
	})

	t.Run("tramos", func(t *testing.T) {
		mcpDoc := contractJSON(t, callTool(t, session, "tramos", map[string]any{"kind": "animals"}))
		response, err := app.Sections(t.Context(), deps.Config, cliFetcher(t), app.SectionCriteria{Kind: "animals"})
		if err != nil {
			t.Fatalf("building the CLI-path sections response: %v", err)
		}
		assertContractParity(t, "tramos", mcpDoc, cliContractDocument(t, response))
	})
}

// cliContractDocument renders an app response exactly as the CLI commands do
// across output.Write with --json: one compact JSON value, single-valued on
// stdout (P-01), then decodes it for the shape comparison.
func cliContractDocument(t *testing.T, response any) map[string]any {
	t.Helper()
	var stdout bytes.Buffer
	if err := output.Write(&stdout, response, output.Options{}); err != nil {
		t.Fatalf("writing the CLI-path document: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("the CLI-path document is not one JSON value: %v\n%s", err, stdout.String())
	}
	return doc
}

// metaOf extracts one document's meta block for the named side of the parity
// assessment, failing with the side named so the error says where it broke.
func metaOf(t *testing.T, tool, side string, doc map[string]any) map[string]any {
	t.Helper()
	block, ok := doc["meta"].(map[string]any)
	if !ok {
		t.Fatalf("%s: the %s document carries no meta block: %v", tool, side, doc)
	}
	return block
}

// assertContractParity compares the recursive key sets of the two documents
// (assertParityShape) and the two pins the contract names explicitly:
// attribution present on both sides and meta.published_at carrying the same
// feed instant (or both null) on both sides.
func assertContractParity(t *testing.T, tool string, mcpDoc, cliDoc map[string]any) {
	t.Helper()
	if _, ok := mcpDoc["attribution"]; !ok {
		t.Errorf("%s: the MCP document carries no attribution key: %v", tool, mcpDoc)
	}
	if _, ok := cliDoc["attribution"]; !ok {
		t.Errorf("%s: the CLI document carries no attribution key: %v", tool, cliDoc)
	}
	assertParityShape(t, tool, "", mcpDoc, cliDoc)

	mcpMeta, cliMeta := metaOf(t, tool, "MCP", mcpDoc), metaOf(t, tool, "CLI", cliDoc)
	gotPublished, wantPublished := mcpMeta["published_at"], cliMeta["published_at"]
	if got, ok := gotPublished.(string); ok {
		want, isString := wantPublished.(string)
		if !isString {
			t.Errorf("%s: meta.published_at = %v on the MCP side and %v on the CLI side", tool, gotPublished, wantPublished)
		} else if got != want {
			t.Errorf("%s: meta.published_at = %q on the MCP side and %q on the CLI side (same fixture, so the same feed instant, P-08)", tool, got, want)
		}
	} else if mcpMeta["published_at"] == nil && cliMeta["published_at"] != nil || mcpMeta["published_at"] != nil && cliMeta["published_at"] == nil {
		t.Errorf("%s: meta.published_at = %v on the MCP side and %v on the CLI side (one side invented or dropped the value, P-04)", tool, gotPublished, wantPublished)
	}
}

// assertParityShape walks two decoded documents together and asserts their
// SHAPE: the same key set at every object level, the same length and paired
// kinds at every array level. Values are deliberately not compared, except
// where the caller pins them: the contract keys travel, the data stay the
// fixture's own business. The empty path is the top level.
func assertParityShape(t *testing.T, tool, path string, got, want any) {
	t.Helper()
	if path == "meta.age_seconds" {
		// Clock-relative delta (now minus published_at): legitimately
		// different between two calls, so it is the ONE ignored key.
		return
	}
	gotMap, gotIsObject := got.(map[string]any)
	wantMap, wantIsObject := want.(map[string]any)
	if gotIsObject && wantIsObject {
		gotKeys := slices.Sorted(maps.Keys(gotMap))
		wantKeys := slices.Sorted(maps.Keys(wantMap))
		if !slices.Equal(gotKeys, wantKeys) {
			onlyMCP, onlyCLI := difference(gotKeys, wantKeys), difference(wantKeys, gotKeys)
			t.Errorf("%s: the key set at %s diverges (this pin is the FULL key set, every divergence fails): MCP-only %v, CLI-only %v", tool, at(path), onlyMCP, onlyCLI)
			return
		}
		for _, key := range gotKeys {
			assertParityShape(t, tool, joinPath(path, key), gotMap[key], wantMap[key])
		}
		return
	}
	gotArray, gotIsArray := got.([]any)
	wantArray, wantIsArray := want.([]any)
	if gotIsArray && wantIsArray {
		if len(gotArray) != len(wantArray) {
			t.Errorf("%s: the array at %s has %d entries on the MCP side and %d on the CLI side (same fixture, so the same pipeline)", tool, at(path), len(gotArray), len(wantArray))
			return
		}
		for i := range gotArray {
			assertParityShape(t, tool, fmt.Sprintf("%s[%d]", path, i), gotArray[i], wantArray[i])
		}
		return
	}
	if gotIsObject != wantIsObject || gotIsArray != wantIsArray || kindOf(got) != kindOf(want) {
		t.Errorf("%s: the value at %s is %T on the MCP side and %T on the CLI side (a null in one place and a value in the other is a divergence too)", tool, at(path), got, want)
	}
}

// kindOf narrows a decoded JSON value to its kind, so scalars compare by
// shape: numbers, strings, booleans and null are different kinds.
func kindOf(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "bool"
	case float64:
		return "number"
	default:
		return fmt.Sprintf("%T", value)
	}
}

// difference returns the keys of a that b does not carry, in sorted order.
func difference(a, b []string) []string {
	set := make(map[string]bool, len(b))
	for _, key := range b {
		set[key] = true
	}
	missing := []string{}
	for _, key := range a {
		if !set[key] {
			missing = append(missing, key)
		}
	}
	return missing
}

// joinPath extends a parity path with one object key.
func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// at renders a parity path for error messages: `meta.published_at`,
// `incidents[2].geo.lat`, or `<top level>` for the document root.
func at(path string) string {
	if path == "" {
		return "<top level>"
	}
	return path
}
