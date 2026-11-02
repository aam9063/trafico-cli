package cli

import (
	"fmt"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/output"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// newRadarsCommand builds `trafico radars`, the fixed-radar command
// (FEED-04). It reads the DATEX II v1.0 PredefinedLocationsPublication of
// docs/feeds.md §6 and exposes only the shared filters that make sense for
// radars: --road, --direction and --province. The incident concepts --pk,
// --type and --severity are deliberately absent.
func newRadarsCommand() *cobra.Command {
	radarsCmd := &cobra.Command{
		Use:   "radars",
		Short: "List DGT fixed radars (radares fijos) (text by default, --json for one JSON value)",
		// Cobra only rejects unknown subcommands under the root: validate
		// here too so `trafico radars bogus` is an invalid argument (exit 2)
		// instead of a silent help print (exit 0).
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errs.NewUsage(fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()))
			}
			return nil
		},
		RunE: runRadars,
	}
	registerRadarFlags(radarsCmd.Flags())
	addPrettyFlag(radarsCmd)
	return radarsCmd
}

// registerRadarFlags registers the filters of the radars command.
func registerRadarFlags(flags *pflag.FlagSet) {
	flags.StringArray("road", nil, "carretera a consultar; repetible (--road A-7 --road AP-7)")
	flags.String("direction", "", "dirección de circulación: positive, negative o both")
	flags.StringArray("province", nil, "provincia; repetible (se compara con el nombre resuelto del código INE del feed)")
	flags.Int("limit", 0, "número máximo de radares (0 = sin límite)")
}

// runRadars answers `trafico radars [filtros]`.
func runRadars(cmd *cobra.Command, args []string) error {
	criteria, err := parseRadarCriteria(cmd)
	if err != nil {
		return err
	}
	cfg, fetcher, err := loadFeedsContext()
	if err != nil {
		return err
	}
	response, err := app.Radars(cmd.Context(), cfg, fetcher, criteria)
	if err != nil {
		return err
	}
	return writeRadarsResponse(cmd, response)
}

// writeRadarsResponse renders the §5.3 envelope of the radars command.
// Warnings (cache fallback) go to stderr (ARRANQUE §5.6, P-01); stdout
// stays one pure JSON value.
func writeRadarsResponse(cmd *cobra.Command, response app.RadarsResponse) error {
	if response.Warning != "" {
		output.Diag(cmd.ErrOrStderr(), "%s", response.Warning)
	}
	pretty := prettyFor(cmd)
	return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
}

// parseRadarCriteria translates the flags of the radars command into the
// application criteria. Cross-flag rules (unknown direction, negative
// limit) stay owned by the application layer.
func parseRadarCriteria(cmd *cobra.Command) (app.RadarCriteria, error) {
	flags := cmd.Flags()
	criteria := app.RadarCriteria{}
	criteria.Roads, _ = flags.GetStringArray("road")
	criteria.Direction, _ = flags.GetString("direction")
	criteria.Provinces, _ = flags.GetStringArray("province")
	criteria.Limit, _ = flags.GetInt("limit")
	return criteria, nil
}
