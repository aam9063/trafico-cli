package cli

import (
	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/output"

	"github.com/spf13/cobra"
)

// newDoctorCommand builds `trafico doctor` (CMD-02): configuration, feeds
// and cache health plus the not-an-official-source warning (ARRANQUE §13).
// It is a diagnostic: a failing feed turns ok false but never fails the
// command.
func newDoctorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check configuration, feeds and cache health",
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := config.ResolvePaths(config.OSEnv)
			if err != nil {
				return errs.NewInternal(err.Error())
			}
			cfg, cfgErr := config.Load(paths.ConfigPath)
			var fetcher *feeds.Fetcher
			if cfgErr != nil {
				// The doctor reports the broken config itself (ok = false)
				// instead of failing: there is no feed to fetch without it.
				cfg = nil
			} else {
				fetcher = feeds.NewFetcher(cfg, paths.CacheDir, version)
			}
			report, err := app.Doctor(cmd.Context(), cfg, paths, fetcher)
			if err != nil {
				return err
			}
			// Cache-fallback warnings travel on stderr (ARRANQUE §5.6, §6.1);
			// stdout stays one pure JSON value (P-01).
			for _, status := range report.Feeds {
				if status.Warning != nil {
					output.Diag(cmd.ErrOrStderr(), "%s", *status.Warning)
				}
			}
			pretty, _ := cmd.Flags().GetBool("pretty")
			return output.Write(cmd.OutOrStdout(), report, output.Options{Pretty: pretty})
		},
	}
	addPrettyFlag(cmd)
	return cmd
}
