package cli

import (
	"fmt"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/output"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// newSignsCommand builds `trafico signs`, the variable message signs
// command (FEED-03). It joins the two panel feeds of docs/feeds.md §5 and
// exposes only the shared filters that make sense for panels: --road,
// --direction and --province. The incident concepts --pk, --type and
// --severity are deliberately absent.
func newSignsCommand() *cobra.Command {
	signsCmd := &cobra.Command{
		Use:   "signs",
		Short: "List DGT variable message signs (paneles de mensaje variable) as JSON",
		// Cobra only rejects unknown subcommands under the root: validate
		// here too so `trafico signs bogus` is an invalid argument (exit 2)
		// instead of a silent help print (exit 0).
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errs.NewUsage(fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()))
			}
			return nil
		},
		RunE: runSigns,
	}
	registerSignFlags(signsCmd.Flags())
	addPrettyFlag(signsCmd)
	return signsCmd
}

// registerSignFlags registers the filters of the signs command.
func registerSignFlags(flags *pflag.FlagSet) {
	flags.StringArray("road", nil, "carretera a consultar; repetible (--road A-7 --road AP-7)")
	flags.String("direction", "", "dirección de circulación: positive, negative o both")
	flags.StringArray("province", nil, "provincia; repetible (acepta cualquiera de los nombres del par bilingüe)")
	flags.Int("limit", 0, "número máximo de paneles (0 = sin límite)")
}

// runSigns answers `trafico signs [filtros]`.
func runSigns(cmd *cobra.Command, args []string) error {
	criteria, err := parseSignCriteria(cmd)
	if err != nil {
		return err
	}
	cfg, fetcher, err := loadFeedsContext()
	if err != nil {
		return err
	}
	response, err := app.Signs(cmd.Context(), cfg, fetcher, criteria)
	if err != nil {
		return err
	}
	return writeSignsResponse(cmd, response)
}

// writeSignsResponse renders the §5.3 envelope of the signs command.
// Warnings (cache fallback, orphan controllers) go to stderr (ARRANQUE
// §5.6, P-01); stdout stays one pure JSON value.
func writeSignsResponse(cmd *cobra.Command, response app.SignsResponse) error {
	if response.Warning != "" {
		output.Diag(cmd.ErrOrStderr(), "%s", response.Warning)
	}
	pretty, _ := cmd.Flags().GetBool("pretty")
	return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
}

// parseSignCriteria translates the flags of the signs command into the
// application criteria. Cross-flag rules (unknown direction, negative
// limit) stay owned by the application layer.
func parseSignCriteria(cmd *cobra.Command) (app.SignCriteria, error) {
	flags := cmd.Flags()
	criteria := app.SignCriteria{}
	criteria.Roads, _ = flags.GetStringArray("road")
	criteria.Direction, _ = flags.GetString("direction")
	criteria.Provinces, _ = flags.GetStringArray("province")
	criteria.Limit, _ = flags.GetInt("limit")
	return criteria, nil
}
