package cli

import (
	"github.com/aam9063/trafico-cli/internal/mcp"

	"github.com/spf13/cobra"
)

// newMCPCommand builds `trafico mcp` (docs/mcp.md): the Model Context
// Protocol server over stdio, the fourth presentation of the same
// application layer (OLE ola-7-mcp T1). It owns argument parsing only, like
// every command: the server itself lives in internal/mcp, isolated from
// cobra the same way internal/tui is, so a change of tool cannot break a
// contract of output (P-01: stdout is the transport channel from here on,
// diagnostics go to stderr).
//
// There is no transport flag in v1 (S1): stdio is the transport Claude
// Desktop, Cursor, Codex and Pi consume, and every extra flag is surface a
// server does not need.
//
// The command blocks until the client closes the session (mcp.Run returns
// then), the normal lifecycle of a stdio server started by the agent's own
// config. The call chain errors map to the usual exit codes (ARRANQUE §6.3):
// a configuration error is written as the JSON error envelope on stderr once
// and the process exits; a healthy start never writes stdout — the stdout
// stream belongs to the protocol from the first byte.
func newMCPCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve the Model Context Protocol over stdio for AI agents (read-only)",
		Long: "Serve the Model Context Protocol (MCP) over stdio for AI agents.\n\n" +
			"Runs a read-only MCP server on stdin/stdout: AI agents can query incidents, " +
			"variable message signs, radars, road sections, saved routes and feed status " +
			"without shell commands, and get the same JSON contract the --json flag prints.\n\n" +
			"The v1 tools are read-only by design; saved-route and watch writes stay CLI " +
			"affairs. Connect your agent with the command `trafico mcp` (docs/mcp.md).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, fetcher, err := LoadFeedsContext()
			if err != nil {
				return err
			}
			return mcp.Run(cmd.Context(), mcp.Deps{
				Config:  cfg,
				Fetcher: fetcher,
				Version: version,
				Diag:    cmd.ErrOrStderr(),
			})
		},
	}
}
