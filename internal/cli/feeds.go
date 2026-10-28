package cli

import (
	"fmt"
	"time"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/output"

	"github.com/spf13/cobra"
)

// feedsResponse is the JSON contract of `trafico feeds list` and `trafico
// feeds status` (docs/ARRANQUE.md §5.3): the shared envelope with the
// per-feed payload at top level.
type feedsResponse struct {
	output.Envelope
	Feeds []app.FeedStatus `json:"feeds"`
}

// newFeedsCommand builds `trafico feeds` with its list and status
// subcommands (CMD-02).
func newFeedsCommand() *cobra.Command {
	feedsCmd := &cobra.Command{
		Use:   "feeds",
		Short: "Show the configured DGT feeds and their state",
		// Cobra only rejects unknown subcommands under the root: validate
		// here too so `trafico feeds bogus` is an invalid argument (exit 2,
		// ARRANQUE §6.3) instead of a silent help print (exit 0).
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errs.NewUsage(fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()))
			}
			return nil
		},
		// With no subcommand, answer with the usage envelope on stderr (exit
		// 2): help on stdout with exit 0 would break P-01.
		RunE: func(cmd *cobra.Command, args []string) error {
			return errs.NewUsage("indica un subcomando: list o status")
		},
	}
	feedsCmd.AddCommand(newFeedsListCommand(), newFeedsStatusCommand())
	return feedsCmd
}

// newFeedsListCommand builds `trafico feeds list`: every configured feed
// with its URL and enabled state, without touching the network.
func newFeedsListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every configured feed with its URL and enabled state (no network)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, fetcher, err := loadFeedsContext()
			if err != nil {
				return err
			}
			list, err := app.FeedsList(cmd.Context(), cfg, fetcher)
			if err != nil {
				return err
			}
			// feeds list fetches nothing, so meta.stale and meta.from_cache
			// stay false: nothing was fetched, nothing can be stale or cached.
			return writeFeedsResponse(cmd, list, false)
		},
	}
	addPrettyFlag(cmd)
	return cmd
}

// newFeedsStatusCommand builds `trafico feeds status`: fetch every enabled
// feed and report freshness and errors per feed. Per-feed failures never
// fail the command (exit 0): it is a diagnostic.
func newFeedsStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Fetch every enabled feed and report freshness and errors per feed",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, fetcher, err := loadFeedsContext()
			if err != nil {
				return err
			}
			list, err := app.FeedsStatus(cmd.Context(), cfg, fetcher)
			if err != nil {
				return err
			}
			return writeFeedsResponse(cmd, list, true)
		},
	}
	addPrettyFlag(cmd)
	return cmd
}

// loadFeedsContext resolves the storage paths, loads the configuration and
// builds the feed fetcher the feeds commands run with.
func loadFeedsContext() (*config.Config, *feeds.Fetcher, error) {
	paths, err := config.ResolvePaths(config.OSEnv)
	if err != nil {
		return nil, nil, errs.NewInternal(err.Error())
	}
	cfg, err := config.Load(paths.ConfigPath)
	if err != nil {
		return nil, nil, errs.NewInternal(err.Error())
	}
	return cfg, feeds.NewFetcher(cfg, paths.CacheDir, version), nil
}

// writeFeedsResponse renders the envelope with the per-feed payload. The
// meta block carries the newest publication time among the feeds, so every
// output keeps attribution and meta.published_at (AGENTS.md, P-05, P-08).
// fetched reports whether the command actually fetched the feeds (`feeds
// status`): then meta.stale and meta.from_cache are derived from the per-feed
// statuses, so the document never reads as fresh while a served entry is
// stale or cached. Cache-fallback warnings go to stderr (ARRANQUE §5.6, §6.1);
// stdout stays one pure JSON value (P-01).
func writeFeedsResponse(cmd *cobra.Command, list []app.FeedStatus, fetched bool) error {
	for _, status := range list {
		if status.Warning != nil {
			output.Diag(cmd.ErrOrStderr(), "%s", *status.Warning)
		}
	}
	meta := app.DeriveMeta(list, time.Now().UTC(), fetched)
	response := feedsResponse{
		Envelope: output.NewEnvelope(meta, output.NewAttribution(time.Now().UTC())),
		Feeds:    list,
	}
	pretty, _ := cmd.Flags().GetBool("pretty")
	return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
}
