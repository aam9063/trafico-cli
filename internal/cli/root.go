// Package cli defines the cobra command tree of trafico.
//
// It owns argument parsing only: JSON output formatting and the JSON error
// envelope belong to the application layer (CORE-01).
package cli

import (
	"fmt"
	"os"

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
	// An invalid flag is an invalid argument: it must exit with code 2, never
	// with the internal code (ARRANQUE section 6.3).
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return errs.NewUsage(err.Error())
	})
	root.AddCommand(newVersionCommand())
	root.AddCommand(newIncidentsCommand())
	root.AddCommand(newPlaceCommand())
	root.AddCommand(newRouteCommand())
	root.AddCommand(newFeedsCommand())
	root.AddCommand(newDoctorCommand())
	return root
}

// addPrettyFlag registers --pretty on a command that renders its output as a
// JSON envelope (ARRANQUE section 6.1). It is deliberately not a root
// persistent flag: a command that prints plain text, such as `trafico
// version`, must reject the flag as unknown (exit 2) instead of silently
// ignoring it (CMD-02 review).
func addPrettyFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("pretty", false, "render output in human-readable Spanish text instead of JSON")
}

// Execute runs the root command and returns the process exit code.
// With SilenceErrors set, the application layer owns error output: on error
// it writes the JSON error envelope (ARRANQUE section 6.3) to stderr exactly
// once and returns the exit code the error carries.
func Execute() errs.ExitCode {
	return executeRoot(NewRootCommand(), os.Args[1:])
}

// executeRoot runs root with args and maps its error to the JSON envelope and
// the process exit code. stdout stays empty on failure; stderr carries exactly
// one envelope line.
//
// cobra reports an unknown command from Find rather than from Execute, so both
// paths are checked to keep invalid arguments on exit code 2.
func executeRoot(root *cobra.Command, args []string) errs.ExitCode {
	root.SetArgs(args)
	if _, _, err := root.Find(args); err != nil {
		return report(root, errs.NewUsage(err.Error()))
	}
	if err := root.Execute(); err != nil {
		return report(root, err)
	}
	return errs.ExitSuccess
}

// report writes the error envelope once and returns the exit code it carries.
func report(root *cobra.Command, err error) errs.ExitCode {
	if wErr := errs.WriteError(root.ErrOrStderr(), err); wErr != nil {
		fmt.Fprintln(root.ErrOrStderr(), wErr)
	}
	return errs.ExitCodeFor(err)
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
