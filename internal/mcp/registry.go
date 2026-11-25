package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolDefinition is one entry of the tool registry (ODD ola-7-mcp S2). The
// registry knows whether a tool writes BEFORE it exists on the wire: the
// server exposes only the read-only entries (exposeTools), so a write surface
// the tree later gains — routes and watches are the planned ones — is code
// and a test failing, never a silent side effect inside a read-only server.
type ToolDefinition struct {
	// Name is the MCP tool name, lower-case Spanish like the recipes: the
	// agent and the user speak the same words.
	Name string
	// Description is what the client model sees in tools/list, Spanish.
	Description string
	// Mutating reports whether the tool writes. Every v1 entry is false;
	// a future wave registers the route/watch writes here and the v1
	// gate — the test and exposeTools — keeps them off the wire until
	// mutation support is designed.
	Mutating bool
	// Register installs the tool on the server bound to deps. Read-only
	// tools must return the --json contract in a single mcp.TextContent
	// (S4): one JSON value, never a second format.
	Register func(server *mcp.Server, deps Deps)
}

// toolRegistry is the v1 tool surface, read-only. The queries behind the
// handlers are exactly the app functions the commands use (S3): incidencias
// → app.Incidents, paneles → app.Signs, radares → app.Radars, tramos →
// app.Sections, rutas → app.Routes / app.RouteCheck, estado_feeds →
// app.FeedsList / app.FeedsStatus.
func toolRegistry() []ToolDefinition {
	return []ToolDefinition{
		incidenciasToolDefinition(),
		panelesToolDefinition(),
		radaresToolDefinition(),
		tramosToolDefinition(),
		rutasToolDefinition(),
		estadoFeedsToolDefinition(),
	}
}

// exposeTools returns the entries v1 may expose: the read-only ones (S2).
// A mutating entry is absent from the server tools/list, which is what the
// session test pins over the wire, not only over the registry slice.
func exposeTools(entries []ToolDefinition) []ToolDefinition {
	exposed := make([]ToolDefinition, 0, len(entries))
	for _, entry := range entries {
		if entry.Mutating {
			continue
		}
		exposed = append(exposed, entry)
	}
	return exposed
}

// registerTools installs every exposed entry on the server.
func registerTools(server *mcp.Server, deps Deps, entries []ToolDefinition) {
	for _, entry := range entries {
		entry.Register(server, deps)
	}
}

// readOnlyTool builds the *mcp.Tool of a read-only entry, with the
// annotations clients read as a hint that the call changes nothing.
func readOnlyTool(name, description string) *mcp.Tool {
	return &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}
}
