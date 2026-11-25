package mcp

import (
	"context"
	"io"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/feeds"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverName is the implementation name the server announces in the
// initialize handshake. The binary is trafico; the MCP server is trafico.
const serverName = "trafico"

// serverInstructions is the instruction text a well-behaved client shows to
// its model next to the tool list. It repeats the reading rules of
// docs/ARRANQUE.md §6.4 so an agent answers without reading the repository.
const serverInstructions = "Servidor de consulta del tráfico de la DGT, solo lectura: " +
	"cada herramienta devuelve el mismo JSON que la opción --json del comando equivalente. " +
	"Antes de dar los datos por vigentes, comprueba meta.published_at y meta.stale; " +
	"si meta.coverage_note aparece, la cobertura no incluye las incidencias gestionadas por la SCT (Cataluña) ni por el Gobierno Vasco. " +
	"Cita la fuente: datos de la DGT. No inventes tiempos de viaje ni retenciones: solo lo que trae el feed."

// Deps carries everything the MCP server needs to answer a query. It is the
// same context the commands run on (config + fetcher, internal/cli's
// LoadFeedsContext), injected so tests never touch the network.
//
// Diag is the process stderr (never stdout: the stdio transport speaks the
// protocol on stdout, P-01). Cache-fallback warnings land there, like the
// commands write them to stderr.
type Deps struct {
	// Config is the loaded user configuration (feeds, places, routes).
	Config *config.Config
	// Fetcher downloads and caches the feeds (60 s cache, network fallback).
	Fetcher *feeds.Fetcher
	// Version is the binary version announced in the initialize handshake.
	Version string
	// Diag is the diagnostic writer; nil disables the warnings.
	Diag io.Writer
}

// NewServer builds the MCP server: the read-only tools (exposeTools drops
// any mutating entry, S2), the four recipes of docs/ARRANQUE.md §6.4 as
// prompts (S5) and the published JSON Schemas as resources (S5).
func NewServer(deps Deps) *mcp.Server {
	return newServer(deps, toolRegistry())
}

// newServer builds the server over an explicit registry slice. Internal:
// the S2 gate test builds its own list with a mutating demo entry to pin
// that the gate refuses the write surface instead of hiding only this
// package's choices.
func newServer(deps Deps, entries []ToolDefinition) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: serverName, Version: deps.Version}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})
	registerTools(srv, deps, exposeTools(entries))
	registerPrompts(srv)
	registerResources(srv)
	return srv
}

// Run serves the MCP protocol over stdio (S1) until the client closes the
// session or the context is cancelled.
func Run(ctx context.Context, deps Deps) error {
	return NewServer(deps).Run(ctx, &mcp.StdioTransport{})
}
