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
var (
	// version, commit and buildDate are injected by GoReleaser through
	// -ldflags (see .goreleaser.yaml). A plain `go build` leaves them at their
	// defaults, so `trafico version` says 0.0.0-dev and no more: the binary
	// never claims a release it is not.
	version   = "0.0.0-dev"
	commit    = ""
	buildDate = ""
)

// NewRootCommand builds the root `trafico` command. Later tasks attach
// their subcommands here.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "trafico",
		Short:         "Query Spanish DGT road traffic open data for humans and AI agents",
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
	root.AddCommand(newSignsCommand())
	root.AddCommand(newRadarsCommand())
	root.AddCommand(newSectionsCommand())
	root.AddCommand(newPlaceCommand())
	root.AddCommand(newRouteCommand())
	root.AddCommand(newFeedsCommand())
	root.AddCommand(newDoctorCommand())
	root.AddCommand(newWatchCommand())
	return root
}

// addPrettyFlag registers the output-format flags on a command that renders an
// envelope (ARRANQUE section 6.1). It is deliberately not a root persistent
// flag: a command that prints plain text, such as `trafico version`, must
// reject the flag as unknown (exit 2) instead of silently ignoring it (CMD-02
// review).
//
// The human-readable text is the DEFAULT (FORMAT-01 S1): the binary exists to
// be read. --json restores the pure JSON value, byte for byte the contract in
// schemas/, which is what an agent parses. --pretty is kept and hidden so a
// script that already passes it keeps working; it selects nothing now.
func addPrettyFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "emite el JSON puro documentado en schemas/ en lugar del texto legible")
	cmd.Flags().Bool("pretty", true, "obsoleto: el texto legible ya es el valor por defecto, usa --json")
	_ = cmd.Flags().MarkHidden("pretty")
}

// prettyFor reports whether the command renders human-readable output.
func prettyFor(cmd *cobra.Command) bool {
	forced, err := cmd.Flags().GetBool("json")
	if err != nil {
		return true
	}
	return !forced
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
			line := version
			if commit != "" {
				line += " (" + commit + ")"
			}
			if buildDate != "" {
				line += " built " + buildDate
			}
			fmt.Fprintln(cmd.OutOrStdout(), line)
			return nil
		},
	}
}
