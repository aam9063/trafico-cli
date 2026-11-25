package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aam9063/trafico-cli/internal/agentdocs"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestPromptsListIsTheSection6Dot4Recipes pins S5 on the wire:
// prompts/list exposes exactly the recipes of the embedded agent guide
// (docs/ARRANQUE.md §6.4), under the ids `trafico skills` serves.
func TestPromptsListIsTheSection6Dot4Recipes(t *testing.T) {
	srv, _ := mcpFixtureServer(t)
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	result, err := session.ListPrompts(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing prompts: %v", err)
	}
	recipes := agentdocs.Recipes()
	if len(result.Prompts) != len(recipes) {
		t.Fatalf("prompts/list = %d prompts, want the %d recipes of §6.4",
			len(result.Prompts), len(recipes))
	}
	listed := make(map[string]bool, len(result.Prompts))
	for _, prompt := range result.Prompts {
		listed[prompt.Name] = true
	}
	for _, recipe := range recipes {
		if !listed[recipe.ID] {
			t.Errorf("prompts/list is missing the recipe %q (the prompt list matches `trafico skills`)", recipe.ID)
		}
	}
}

// TestGetPromptEstadoCarretera pins one recipe prompt end to end: a user
// message that names the tool of this server (incidencias) and the reading
// rules, so the model answers without running shell commands.
func TestGetPromptEstadoCarretera(t *testing.T) {
	srv, _ := mcpFixtureServer(t)
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	result, err := session.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "estado-carretera"})
	if err != nil {
		t.Fatalf("getting the prompt: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("the prompt carries %d messages, want one", len(result.Messages))
	}
	message := result.Messages[0]
	if string(message.Role) != "user" {
		t.Fatalf("the message role = %v, want user", message.Role)
	}
	text, ok := message.Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("the message content is %T, want text", message.Content)
	}
	for _, want := range []string{
		"Estado de una carretera", // the recipe summary travels into the message
		"`incidencias`",           // the equivalent tool of this server
		"pk_from",                 // the exact tool-argument form of the pk range
		"meta.published_at",       // the freshness reading rule
	} {
		if !strings.Contains(text.Text, want) {
			t.Errorf("the prompt text does not carry %q:\n%s", want, text.Text)
		}
	}
}

// TestResourcesListPinsTheEmbeddedSchemas pins S5 on the wire:
// resources/list exposes the published JSON Schemas under the same names
// `trafico schema` serves, read from the embed, and reading one returns
// valid JSON without touching any feed (S7).
func TestResourcesListPinsTheEmbeddedSchemas(t *testing.T) {
	srv, hits := mcpFixtureServer(t)
	deps, _ := newMCPDeps(t, srv.URL)
	session := connectMCP(t, deps)

	result, err := session.ListResources(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing resources: %v", err)
	}
	if len(result.Resources) != 2 {
		t.Fatalf("resources/list = %d resources, want the 2 published schemas", len(result.Resources))
	}
	if hits("/situations") != 0 {
		t.Fatal("listing resources never touches a feed (S7, no network in tests)")
	}
	uris := make(map[string]string, len(result.Resources))
	for _, resource := range result.Resources {
		uris[resource.Name] = resource.URI
	}
	for _, name := range []string{"incident", "incidents-response"} {
		uri, ok := uris[name]
		if !ok {
			t.Fatalf("resources/list is missing the schema %q: %v", name, result.Resources)
		}
		read, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("reading %s: %v", uri, err)
		}
		if len(read.Contents) != 1 {
			t.Fatalf("the resource %s carries %d contents, want one document", uri, len(read.Contents))
		}
		content := read.Contents[0]
		if content.MIMEType != "application/json" {
			t.Errorf("the schema resource carries mimeType %q, want application/json", content.MIMEType)
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(content.Text), &doc); err != nil {
			t.Fatalf("the resource %s is not valid JSON: %v\n%.200s", uri, err, content.Text)
		}
		if doc["$schema"] == nil && doc["title"] == nil {
			t.Errorf("the resource %s carries no schema document (%.100s)", uri, content.Text)
		}
	}
}
