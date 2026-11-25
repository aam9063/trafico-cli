package cli

import (
	"testing"

	"github.com/aam9063/trafico-cli/internal/errs"

	"github.com/spf13/pflag"
)

// TestMCPCommandRegisteredHelp pins the registration of `trafico mcp` (S1)
// at CLI level: the command is on the tree and its help still works. There
// is deliberately no test that RUNS the command: a MCP stdio server blocks
// until a client connects, so its behavior is pinned by
// internal/mcp/tests.go over the SDK in-memory transport instead; running it
// here would only hang.
func TestMCPCommandRegisteredHelp(t *testing.T) {
	setCLIEnv(t)
	// runCLIDefaultFormat: the mcp command takes no format flag — it streams
	// the protocol, not a response (S1).
	code, stdout, stderr := runCLIDefaultFormat(t, "mcp", "--help")
	if code != errs.ExitSuccess {
		t.Fatalf("trafico mcp --help = exit %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatal("trafico mcp --help printed nothing on stdout")
	}
}

// TestMCPCommandRejectsArguments pins that a bare `trafico mcp extra` is a
// usage error (exit 2, ARRANQUE §6.3), not a silently ignored argument.
func TestMCPCommandRejectsArguments(t *testing.T) {
	setCLIEnv(t)
	code, _, stderr := runCLI(t, "mcp", "extra")
	if code != errs.ExitUsage {
		t.Fatalf("trafico mcp extra = exit %d, want 2 (stderr: %s)", code, stderr.String())
	}
}

// TestMCPCommandHasNoFlags pins the v1 transport decision (S1): stdio, no
// transport flag, no token. A transport flag that appeared later would be a
// network surface, not a convenience.
func TestMCPCommandHasNoFlags(t *testing.T) {
	root := NewRootCommand()
	mcpCmd, _, err := root.Find([]string{"mcp"})
	if err != nil {
		t.Fatalf("finding the mcp command on the tree: %v", err)
	}
	flags := mcpCmd.Flags()
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Name != "help" {
			t.Errorf("the mcp command carries the unexpected flag --%s (v1 is stdio only, S1)", f.Name)
		}
	})
}
