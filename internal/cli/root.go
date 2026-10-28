// Package cli defines the cobra command tree of trafico.
//
// It owns argument parsing only: JSON output formatting and the JSON error
// envelope belong to the application layer (CORE-01).
package cli

import (
	"fmt"

	"github.com/aam9063/trafico-cli/internal/errs"

	"github.com/spf13/cobra"
)

// version is the version string reported by `trafico version`.
// Release builds override it via -ldflags.
var version = "0.0.0-dev"

// NewRootCommand builds the root `trafico` command. Later tasks attach
// their subcommands here.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "trafico",
		Short:         "Query Spanish DGT road traffic open data as JSON for AI agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newVersionCommand())
	return root
}

// Execute runs the root command and returns the process exit code.
// With SilenceErrors set, the application layer owns error output,
// so failures map to the internal exit code for now.
func Execute() errs.ExitCode {
	if err := NewRootCommand().Execute(); err != nil {
		return errs.ExitInternal
	}
	return errs.ExitSuccess
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the trafico version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), version)
			return nil
		},
	}
}
