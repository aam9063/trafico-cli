package cli

import (
	"fmt"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/output"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// newSectionsCommand builds `trafico sections`, the road-section command
// (FEED-05). --kind selects the dataset — invive (the v1.0 INVIVE
// inventory, docs/feeds.md §7) or animals (the v3 TEFIVA/SPFRIA black spots
// for animal collisions, docs/feeds.md §8) — and is required: there is no
// sensible default between two different datasets. The remaining flags are
// the shared filters that make sense for sections.
func newSectionsCommand() *cobra.Command {
	sectionsCmd := &cobra.Command{
		Use:   "sections",
		Short: "List DGT road sections (INVIVE inventory and animal-collision black spots) as JSON",
		// Cobra only rejects unknown subcommands under the root: validate
		// here too so `trafico sections bogus` is an invalid argument (exit 2)
		// instead of a silent help print (exit 0).
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errs.NewUsage(fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()))
			}
			return nil
		},
		RunE: runSections,
	}
	registerSectionFlags(sectionsCmd.Flags())
	addPrettyFlag(sectionsCmd)
	return sectionsCmd
}

// registerSectionFlags registers the flags of the sections command. The
// flag help is user-facing and stays in Spanish.
func registerSectionFlags(flags *pflag.FlagSet) {
	flags.String("kind", "", "tipo de tramos: invive o animals (obligatorio)")
	flags.StringArray("road", nil, "carretera a consultar; repetible (--road A-7 --road AP-7)")
	flags.String("direction", "", "dirección de circulación: positive, negative o both")
	flags.StringArray("province", nil, "provincia; repetible (el feed animals no publica provincia y cuenta como no localizable)")
	flags.Int("limit", 0, "número máximo de tramos (0 = sin límite)")
}

// runSections answers `trafico sections --kind <kind> [filtros]`.
func runSections(cmd *cobra.Command, args []string) error {
	criteria, err := parseSectionCriteria(cmd)
	if err != nil {
		return err
	}
	cfg, fetcher, err := loadFeedsContext()
	if err != nil {
		return err
	}
	response, err := app.Sections(cmd.Context(), cfg, fetcher, criteria)
	if err != nil {
		return err
	}
	return writeSectionsResponse(cmd, response)
}

// writeSectionsResponse renders the §5.3 envelope of the sections command.
// Warnings (cache fallback) go to stderr (ARRANQUE §5.6, P-01); stdout
// stays one pure JSON value.
func writeSectionsResponse(cmd *cobra.Command, response app.SectionsResponse) error {
	if response.Warning != "" {
		output.Diag(cmd.ErrOrStderr(), "%s", response.Warning)
	}
	pretty, _ := cmd.Flags().GetBool("pretty")
	return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
}

// parseSectionCriteria translates the flags of the sections command into
// the application criteria. Cross-flag rules (a missing or unknown kind,
// unknown direction, negative limit) stay owned by the application layer.
func parseSectionCriteria(cmd *cobra.Command) (app.SectionCriteria, error) {
	flags := cmd.Flags()
	criteria := app.SectionCriteria{}
	criteria.Kind, _ = flags.GetString("kind")
	criteria.Roads, _ = flags.GetStringArray("road")
	criteria.Direction, _ = flags.GetString("direction")
	criteria.Provinces, _ = flags.GetStringArray("province")
	criteria.Limit, _ = flags.GetInt("limit")
	return criteria, nil
}
