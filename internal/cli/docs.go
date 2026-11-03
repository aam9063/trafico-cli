package cli

import "github.com/spf13/cobra"

// newDocsCommand is the hidden generator behind docs/cli-spec.md. It is not
// in the help output because nobody needs it to use the tool; its callers are
// the maintainer and the staleness test.
//
//	trafico docs > docs/cli-spec.md
//
// The test is the real gate — it compares the rendered text with the committed
// file — but having the command means a maintainer never has to reach for a
// build tag to regenerate.
func newDocsCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "docs",
		Short:  "Print the generated command reference",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeCLISpec(cmd.OutOrStdout(), cmd.Root())
		},
	}
}
