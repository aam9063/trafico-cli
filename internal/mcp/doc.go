// Package mcp is the Model Context Protocol server of trafico-cli: the
// fourth presentation of the same application layer, after the CLI, the TUI
// and the embedded recipes (ODD ola-7-mcp). It is deliberately isolated from
// internal/cli the same way internal/tui is: it shares only internal/app —
// the single door of every query (S3) — and internal/output, the contract
// holders, and it never re-implements a parser or a filter.
//
// The v1 surface is read-only by design (S2): every tool declares whether it
// writes (ToolDefinition.Mutating), the server exposes only the read-only
// entries, and a test pins that no mutating tool reaches the wire. Writing
// routes and watches is the documented extension point: app.AddRoute,
// app.RemoveRoute and the watch machinery exist, but the MCP server never
// calls them in v1.
//
// Transport: stdio only (S1), the transport Claude Desktop, Cursor, Codex and
// Pi consume. HTTP would add a port, authentication and network surface to a
// project whose contract is "no network dependency beyond nap.dgt.es".
//
// Every tool result is exactly the JSON contract the command's --json prints,
// attribution and meta.published_at included (S4): an agent learns one format,
// the one frozen in schemas/.
package mcp
