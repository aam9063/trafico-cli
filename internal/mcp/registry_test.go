package mcp

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registryEntry finds one entry of the v1 tool registry by name.
func registryEntry(name string) (ToolDefinition, bool) {
	for _, entry := range toolRegistry() {
		if entry.Name == name {
			return entry, true
		}
	}
	return ToolDefinition{}, false
}

// toolByName indexes a tools/list result by name.
func toolByName(t *testing.T, tools []*mcp.Tool) map[string]*mcp.Tool {
	t.Helper()
	byName := make(map[string]*mcp.Tool, len(tools))
	for _, tool := range tools {
		if _, seen := byName[tool.Name]; seen {
			t.Fatalf("tools/list returned %q twice", tool.Name)
		}
		byName[tool.Name] = tool
	}
	return byName
}

// v1ToolNames is the complete, closed v1 surface in one place: six read-only
// tools, no more.
var v1ToolNames = []string{"incidencias", "paneles", "radares", "tramos", "rutas", "estado_feeds"}

// TestV1RegistryCoversTheAgentSurface pins that every query of the ODD
// (incidents, signs, radars, sections, saved routes, feeds) has ONE tool
// wired to the app function the commands use (S3) — no extra, no missing —
// and that each entry is well-formed enough to reach a client.
func TestV1RegistryCoversTheAgentSurface(t *testing.T) {
	entries := toolRegistry()
	if len(entries) != len(v1ToolNames) {
		t.Fatalf("registry = %d entries, want %d", len(entries), len(v1ToolNames))
	}
	for _, name := range v1ToolNames {
		entry, ok := registryEntry(name)
		if !ok {
			t.Fatalf("registry is missing the %q tool", name)
		}
		if entry.Mutating {
			t.Errorf("the v1 entry %q is mutating: v1 is read-only (S2)", name)
		}
		if entry.Description == "" {
			t.Errorf("tool %q has no description: the agent learns the tool from it", name)
		}
		if entry.Register == nil {
			t.Errorf("tool %q has no Register: it would never reach the server", name)
		}
	}
}

// TestExposeToolsDropsMutating pins the filter (S2) at unit level, without a
// transport around it: a mutating demo entry disappears from the exposed
// list, the read-only ones survive.
func TestExposeToolsDropsMutating(t *testing.T) {
	entries := append(toolRegistry(), ToolDefinition{Name: "demo-mutante", Mutating: true})
	exposed := exposeTools(entries)
	if len(exposed) != 6 {
		t.Fatalf("exposeTools returned %d entries, want 6 without the mutating one", len(exposed))
	}
	for _, entry := range exposed {
		if entry.Mutating {
			t.Errorf("exposeTools exposed the mutating %q", entry.Name)
		}
	}
}

// TestToolAnnotationCarriesReadOnlyHint pins the annotation of a read-only
// tool before it goes on the wire: clients that filter by readOnlyHint see
// only queries, and the hint is explicit, not a default.
func TestToolAnnotationCarriesReadOnlyHint(t *testing.T) {
	tool := readOnlyTool("demo", "demo")
	toolJSON, err := json.Marshal(tool.Annotations)
	if err != nil {
		t.Fatalf("serialising the annotations: %v", err)
	}
	var annotations struct {
		ReadOnlyHint *bool `json:"readOnlyHint"`
	}
	if err := json.Unmarshal(toolJSON, &annotations); err != nil {
		t.Fatalf("parsing the annotations: %v\n%s", err, toolJSON)
	}
	if annotations.ReadOnlyHint == nil || !*annotations.ReadOnlyHint {
		t.Fatalf("readOnlyHint = %v, want explicit true (the wire hint must not rest on a default)", string(toolJSON))
	}
}
