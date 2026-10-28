package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/aam9063/trafico-cli/internal/errs"

	"github.com/spf13/cobra"
)

// TestErrorSubcommandWritesEnvelopeOnce checks that a subcommand failing with
// an *errs.Error produces: empty stdout, exactly one envelope line on stderr
// and the exit code carried by the error.
func TestErrorSubcommandWritesEnvelopeOnce(t *testing.T) {
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	root := NewRootCommand()
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(&cobra.Command{
		Use: "boom",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errs.NewNotFound("la ruta fija no existe")
		},
	})

	code := executeRoot(root, []string{"boom"})

	if code != errs.ExitNotFound {
		t.Fatalf("exit code = %d, want %d", code, errs.ExitNotFound)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout must stay empty on failure, got %q", got)
	}
	want := `{"error":{"type":"not_found","message":"la ruta fija no existe","exit_code":4}}` + "\n"
	if got := stderr.String(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

// TestSuccessSubcommandWritesJSONToStdoutOnly checks the happy path: JSON on
// stdout, nothing on stderr, exit code 0.
func TestSuccessSubcommandWritesJSONToStdoutOnly(t *testing.T) {
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	root := NewRootCommand()
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(&cobra.Command{
		Use: "ok",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), `{"ok":true}`)
			return nil
		},
	})

	code := executeRoot(root, []string{"ok"})

	if code != errs.ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, errs.ExitSuccess)
	}
	if got := strings.TrimSpace(stdout.String()); got != `{"ok":true}` {
		t.Fatalf("stdout = %q, want %q", got, `{"ok":true}`)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr must stay empty on success, got %q", got)
	}
}

// TestPlainErrorFallsBackToInternalExitCode checks that an error which is not
// an *errs.Error still gets an envelope and maps to the internal exit code.
func TestPlainErrorFallsBackToInternalExitCode(t *testing.T) {
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	root := NewRootCommand()
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.AddCommand(&cobra.Command{
		Use: "plain",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("algo se rompió")
		},
	})

	code := executeRoot(root, []string{"plain"})

	if code != errs.ExitInternal {
		t.Fatalf("exit code = %d, want %d", code, errs.ExitInternal)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout must stay empty on failure, got %q", got)
	}
	want := `{"error":{"type":"internal","message":"algo se rompió","exit_code":1}}` + "\n"
	if got := stderr.String(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

// TestUnknownFlagExitsWithUsage checks that an invalid flag is an invalid
// argument: exit code 2 and a usage envelope, never the internal code
// (ARRANQUE section 6.3).
func TestUnknownFlagExitsWithUsage(t *testing.T) {
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	root := NewRootCommand()
	root.SetOut(stdout)
	root.SetErr(stderr)

	code := executeRoot(root, []string{"--bogus"})

	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, errs.ExitUsage)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout must stay empty on failure, got %q", got)
	}
	want := `{"error":{"type":"usage","message":"unknown flag: --bogus","exit_code":2}}` + "\n"
	if got := stderr.String(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

// TestUnknownCommandExitsWithUsage checks the other invalid-argument path:
// cobra reports an unknown command from Find, so it must also exit with 2.
func TestUnknownCommandExitsWithUsage(t *testing.T) {
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	root := NewRootCommand()
	root.SetOut(stdout)
	root.SetErr(stderr)

	code := executeRoot(root, []string{"nosuchcommand"})

	if code != errs.ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, errs.ExitUsage)
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout must stay empty on failure, got %q", got)
	}
	want := `{"error":{"type":"usage","message":"unknown command \"nosuchcommand\" for \"trafico\"","exit_code":2}}` + "\n"
	if got := stderr.String(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}
