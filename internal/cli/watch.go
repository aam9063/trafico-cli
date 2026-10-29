package cli

import (
	"fmt"
	"time"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/output"
	"github.com/aam9063/trafico-cli/internal/store"
	"github.com/aam9063/trafico-cli/internal/watch"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The watch commands of docs/ARRANQUE.md §5.5 and WATCH-01d (specs S3-S8):
// add/list/show/remove manage the stored definitions through the definition
// layer (internal/app, which validates and translates them), check shows
// what a real run would notify WITHOUT claiming it happened, and run is the
// scheduler entry point: one feed fetch, every due watch, pending delivery.
// All of them keep the contract shape of the other commands: one JSON
// envelope on stdout (P-01), attribution and meta on every output (P-05),
// cache-fallback warnings on stderr (§5.6), and the confirmation gate of the
// other deletions on remove.

// watchInfo is one saved watch of the JSON contract of `trafico watch
// add/list/show/remove`: the stored definition rendered for agents. The name
// and target are storage metadata the CLI displays locally; they never reach
// a notification (P-07 — the sink payload has no field for them).
type watchInfo struct {
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Target      watchTargetInfo `json:"target"`
	Windows     []string        `json:"windows"`
	MinSeverity string          `json:"min_severity,omitempty"`
	Sinks       []string        `json:"sinks,omitempty"`
	Interval    string          `json:"interval"`
	CreatedAt   time.Time       `json:"created_at"`
}

// watchPKRange echoes a kilometre interval with the lowercase keys of the
// incidents contract (filter.Range is a domain value without JSON tags).
type watchPKRange struct {
	From *float64 `json:"from,omitempty"`
	To   *float64 `json:"to,omitempty"`
}

// watchTargetInfo echoes what the watch looks at, per kind, reusing the pk
// echo shape of the incidents contract.
type watchTargetInfo struct {
	Route    string        `json:"route,omitempty"`
	Road     string        `json:"road,omitempty"`
	PK       *watchPKRange `json:"pk,omitempty"`
	Place    string        `json:"place,omitempty"`
	Lat      *float64      `json:"lat,omitempty"`
	Lon      *float64      `json:"lon,omitempty"`
	RadiusKm float64       `json:"radius_km,omitempty"`
}

// watchesResponse is the JSON contract of `trafico watch add/list/remove`:
// the shared envelope with the full stored-watch list, so a mutation shows
// its result and list stays a plain query.
type watchesResponse struct {
	output.Envelope
	Watches []watchInfo `json:"watches"`
}

// watchResponse is the JSON contract of `trafico watch show <name>`.
type watchResponse struct {
	output.Envelope
	Watch watchInfo `json:"watch"`
}

// watchCheckResponse is the JSON contract of `trafico watch check <name>`:
// what a real run would notify (events counts and kinds), or skipped.
type watchCheckResponse struct {
	output.Envelope
	Watch    string   `json:"watch"`
	Skipped  bool     `json:"skipped"`
	Baseline bool     `json:"baseline"`
	Events   int      `json:"events"`
	Kinds    []string `json:"kinds,omitempty"`
}

// watchRunResponse is the JSON contract of `trafico watch run`: one entry
// per stored watch, plus the delivery total of the cycle.
type watchRunResponse struct {
	output.Envelope
	Runs      []watchRunEntry `json:"runs"`
	Delivered int             `json:"delivered"`
}

// watchRunEntry is the per-watch outcome of a run.
type watchRunEntry struct {
	Watch           string   `json:"watch"`
	Skipped         bool     `json:"skipped,omitempty"`
	SkippedInterval bool     `json:"skipped_interval,omitempty"`
	Baseline        bool     `json:"baseline,omitempty"`
	Events          int      `json:"events,omitempty"`
	Kinds           []string `json:"kinds,omitempty"`
	Error           string   `json:"error,omitempty"`
}

// newWatchCommand builds `trafico watch` with its add, list, show, remove,
// check and run subcommands (docs/ARRANQUE.md §5.1, §5.5).
func newWatchCommand() *cobra.Command {
	watchCmd := &cobra.Command{
		Use:   "watch",
		Short: "Manage saved watches, check them and run the vigilance cycle",
		// Cobra only rejects unknown subcommands under the root: validate
		// here too so `trafico watch bogus` is an invalid argument (exit 2,
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
			return errs.NewUsage("indica un subcomando: add, list, show, remove, check o run")
		},
	}
	watchCmd.AddCommand(
		newWatchAddCommand(),
		newWatchListCommand(),
		newWatchShowCommand(),
		newWatchRemoveCommand(),
		newWatchCheckCommand(),
		newWatchRunCommand(),
	)
	return watchCmd
}

// newWatchAddCommand builds `trafico watch add <name> (route <ruta> |
// road <carretera> [--pk from-to] | near <lugar> [--radius 15km])
// --window <spec> [--window ...] [--min-severity low|medium|high]
// [--notify <sink>] [--interval 5m]`.
func newWatchAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <nombre> (route <ruta> | road <carretera> | near <lugar>)",
		Short: "Save a watch over a route, a road or the surroundings of a place (docs/ARRANQUE.md §5.5)",
		Long: "Save a watch over a route, a road or the surroundings of a place.\n\n" +
			"A watch needs at least one --window (outside it there is no check and no\n" +
			"notification). The first check after adding records the baseline silently:\n" +
			"what was already happening does not notify. An existing watch with the same\n" +
			"name is REPLACED, which is the only way to edit one.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 3 {
				return errs.NewUsage("indica el nombre, la clase y el objetivo: trafico watch add <nombre> (route <ruta> | road <carretera> | near <lugar>)")
			}
			return nil
		},
		RunE: runWatchAdd,
	}
	cmd.Flags().StringArray("window", nil, "ventana horaria de la vigilancia, repetible (--window \"lun-vie 07:00-09:00\")")
	cmd.Flags().String("pk", "", "tramo de punto kilométrico, solo con road (ejemplo: 600-700)")
	cmd.Flags().String("radius", "", "radio en kilómetros, solo con near (ejemplo: 15 o 15km)")
	cmd.Flags().String("min-severity", "", "severidad mínima: low, medium o high")
	cmd.Flags().StringArray("notify", nil, "destino del aviso definido en [sinks.<nombre>], repetible (--notify phone)")
	cmd.Flags().String("interval", "", "intervalo mínimo entre comprobaciones, 2 minutos como mínimo y 5 por defecto (--interval 5m)")
	addPrettyFlag(cmd)
	return cmd
}

// runWatchAdd validates the flags through the definition layer and stores
// the watch. A rejected add writes nothing.
func runWatchAdd(cmd *cobra.Command, args []string) error {
	flags := cmd.Flags()
	windowSpecs, _ := flags.GetStringArray("window")
	minSeverity, _ := flags.GetString("min-severity")
	sinks, _ := flags.GetStringArray("notify")

	interval := watch.DefaultInterval
	if value, _ := flags.GetString("interval"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return errs.NewUsage(fmt.Sprintf("--interval debe ser una duración, por ejemplo 5m o 10m (recibido %q)", value))
		}
		interval = parsed
	}

	name, kind, value := args[0], args[1], args[2]
	target, err := parseWatchTarget(kind, value, flags)
	if err != nil {
		return err
	}

	_, cfg, st, err := loadStoreContext()
	if err != nil {
		return err
	}
	defer st.Close()
	spec, err := app.ParseWatchSpec(name, target, windowSpecs, minSeverity, sinks, interval, cfg)
	if err != nil {
		return err
	}
	// The definition layer checked that every sink NAME exists; check too
	// that every sink can be BUILT from the config, so a sink that can never
	// deliver (a telegram template without a chat_id field, say) is rejected
	// here with a usage error instead of silently dropping every
	// notification later.
	if err := app.ValidateSinksUsable(sinks, cfg); err != nil {
		return err
	}
	if err := app.SaveWatch(st, spec); err != nil {
		return err
	}
	return writeWatchesResponse(cmd, st)
}

// parseWatchTarget translates the positional target of `watch add` into the
// validated target shape of the definition layer. The road name is
// normalised by ParseWatchSpec, so the stored target carries "A-7", not
// "a7".
func parseWatchTarget(kind, value string, flags *pflag.FlagSet) (app.WatchTarget, error) {
	switch kind {
	case store.WatchKindRoute:
		return app.WatchTarget{Kind: kind, Route: value}, nil
	case store.WatchKindRoad:
		target := app.WatchTarget{Kind: kind, Road: value}
		if pk, _ := flags.GetString("pk"); pk != "" {
			rng, err := parsePKRange(pk)
			if err != nil {
				return app.WatchTarget{}, err
			}
			target.PK = rng
		}
		return target, nil
	case store.WatchKindNear:
		target := app.WatchTarget{Kind: kind, Place: value}
		if radius, _ := flags.GetString("radius"); radius != "" {
			km, err := parseRadiusKm(radius)
			if err != nil {
				return app.WatchTarget{}, err
			}
			target.RadiusKm = km
		}
		return target, nil
	default:
		return app.WatchTarget{}, errs.NewUsage(fmt.Sprintf(
			"la clase de vigilancia %q no es válida: usa route, road o near", kind))
	}
}

// newWatchListCommand builds `trafico watch list`.
func newWatchListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every saved watch with its target, windows and sinks",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, st, err := loadStoreContext()
			if err != nil {
				return err
			}
			defer st.Close()
			return writeWatchesResponse(cmd, st)
		},
	}
	addPrettyFlag(cmd)
	return cmd
}

// newWatchShowCommand builds `trafico watch show <name>`.
func newWatchShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <nombre>",
		Short: "Show one saved watch with its target, windows and sinks",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errs.NewUsage("indica exactamente el nombre de la vigilancia: trafico watch show <nombre>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, st, err := loadStoreContext()
			if err != nil {
				return err
			}
			defer st.Close()
			stored, err := app.GetWatch(st, args[0])
			if err != nil {
				return err
			}
			meta := &output.Meta{FetchedAt: time.Now().UTC(), Total: 1}
			response := watchResponse{
				Envelope: output.NewEnvelope(meta, output.NewAttribution(time.Now().UTC())),
				Watch:    watchView(stored),
			}
			pretty, _ := cmd.Flags().GetBool("pretty")
			return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
		},
	}
	addPrettyFlag(cmd)
	return cmd
}

// newWatchRemoveCommand builds `trafico watch remove <name> [--yes]`. On a
// non-interactive invocation there is no prompt at all: without --yes the
// command refuses with a confirmation_required envelope (exit 2) that names
// the flag, and deletes nothing.
func newWatchRemoveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <nombre>",
		Short: "Remove a saved watch (requires --yes)",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errs.NewUsage("indica exactamente el nombre de la vigilancia: trafico watch remove <nombre>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check the confirmation before anything else: a refused removal
			// must not delete anything.
			yes, _ := cmd.Flags().GetBool("yes")
			if !yes {
				return errs.NewConfirmationRequired(fmt.Sprintf(
					"el borrado de una vigilancia necesita confirmación: añade --yes para eliminar la vigilancia %q", args[0]))
			}
			_, _, st, err := loadStoreContext()
			if err != nil {
				return err
			}
			defer st.Close()
			if err := app.RemoveWatch(st, args[0]); err != nil {
				return err
			}
			return writeWatchesResponse(cmd, st)
		},
	}
	cmd.Flags().Bool("yes", false, "confirma el borrado (sin este flag el comando no elimina nada)")
	addPrettyFlag(cmd)
	return cmd
}

// newWatchCheckCommand builds `trafico watch check <name>`: the manual
// check. It shows what a real run WOULD notify and writes nothing (see
// app.CheckWatch), guarded by the single-instance lock so it can never land
// on the snapshot a concurrent run is deciding over.
func newWatchCheckCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <nombre>",
		Short: "Show what a watch would notify now, without recording or delivering anything",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return errs.NewUsage("indica exactamente el nombre de la vigilancia: trafico watch check <nombre>")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, cfg, st, err := loadStoreContext()
			if err != nil {
				return err
			}
			defer st.Close()
			var report app.CheckReport
			checkErr := app.WithWatchLock(paths.DataPath, 0, func() error {
				var inner error
				report, inner = app.CheckWatch(cmd.Context(), cfg, loadFetcherOf(cfg), st, args[0])
				return inner
			})
			if checkErr != nil {
				return checkErr
			}
			return writeWatchCheckResponse(cmd, report)
		},
	}
	addPrettyFlag(cmd)
	return cmd
}

// newWatchRunCommand builds `trafico watch run`: the vigilance cycle, safe
// to invoke from a scheduler every minute. It checks every stored watch
// whose window matches now (one feed fetch in total), delivers the pending
// events and reports per watch what happened — under the single-instance
// lock, so an overlapping invocation fails cleanly instead of duplicating
// notifications.
func newWatchRunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the vigilance cycle: check every watch inside its window and deliver pending events",
		Long: "Run the vigilance cycle.\n\n" +
			"Checks every stored watch whose time window matches now (one feed fetch in\n" +
			"total), delivers the events still pending and reports per watch what\n" +
			"happened. Safe to run from a scheduler every minute: two overlapping runs\n" +
			"cannot duplicate notifications, the second fails cleanly until the first\n" +
			"releases the lock.",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return errs.NewUsage(fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, cfg, st, err := loadStoreContext()
			if err != nil {
				return err
			}
			defer st.Close()
			var result app.RunAllResult
			runErr := app.WithWatchLock(paths.DataPath, 0, func() error {
				var inner error
				result, inner = app.RunAll(cmd.Context(), cfg, loadFetcherOf(cfg), st)
				return inner
			})
			if runErr != nil {
				return runErr
			}
			return writeWatchRunResponse(cmd, result)
		},
	}
	addPrettyFlag(cmd)
	return cmd
}

// loadStoreContext resolves the storage paths, loads the configuration and
// opens the watch database: the store commands and the vigilance cycle all
// run through it.
func loadStoreContext() (config.Paths, *config.Config, *store.Store, error) {
	paths, err := config.ResolvePaths(config.OSEnv)
	if err != nil {
		return config.Paths{}, nil, nil, errs.NewInternal(err.Error())
	}
	cfg, err := config.Load(paths.ConfigPath)
	if err != nil {
		return config.Paths{}, nil, nil, errs.NewInternal(err.Error())
	}
	st, err := store.Open(paths.DataPath)
	if err != nil {
		return config.Paths{}, nil, nil, err
	}
	return paths, cfg, st, nil
}

// loadFetcherOf builds the feed fetcher over an already-loaded config with
// the resolved cache directory.
func loadFetcherOf(cfg *config.Config) *feeds.Fetcher {
	return feeds.NewFetcher(cfg, cacheDirOf(), version)
}

// cacheDirOf resolves the cache directory again for the fetcher (ResolvePaths
// never touches the filesystem and its only failure mode is a missing home).
func cacheDirOf() string {
	paths, err := config.ResolvePaths(config.OSEnv)
	if err != nil {
		// ResolvePaths failed for the same lookup it already passed in
		// loadStoreContext; the empty dir only degrades to no caching.
		return ""
	}
	return paths.CacheDir
}

// writeWatchesResponse renders the envelope with the stored-watch list. The
// definition commands touch no feed, so meta carries no published_at
// (nothing was fetched and nothing can be stale) and total counts the
// watches.
func writeWatchesResponse(cmd *cobra.Command, st *store.Store) error {
	stored, err := app.ListWatches(st)
	if err != nil {
		return err
	}
	views := make([]watchInfo, 0, len(stored))
	for _, w := range stored {
		views = append(views, watchView(w))
	}
	meta := &output.Meta{FetchedAt: time.Now().UTC(), Total: len(views)}
	response := watchesResponse{
		Envelope: output.NewEnvelope(meta, output.NewAttribution(time.Now().UTC())),
		Watches:  views,
	}
	pretty, _ := cmd.Flags().GetBool("pretty")
	return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
}

// writeWatchCheckResponse renders the check report. Cache-fallback warnings
// go to stderr (ARRANQUE §5.6, §6.1); stdout stays one pure JSON value
// (P-01). A skipped check fetched nothing, so its meta has no published_at.
func writeWatchCheckResponse(cmd *cobra.Command, report app.CheckReport) error {
	if report.Warning != "" {
		output.Diag(cmd.ErrOrStderr(), "%s", report.Warning)
	}
	meta := report.Meta
	if meta == nil {
		meta = &output.Meta{FetchedAt: time.Now().UTC()}
	}
	response := watchCheckResponse{
		Envelope: output.NewEnvelope(meta, output.NewAttribution(time.Now().UTC())),
		Watch:    report.Name,
		Skipped:  report.Skipped,
		Baseline: report.Baseline,
		Events:   len(report.Events),
		Kinds:    eventKinds(report.Events),
	}
	pretty, _ := cmd.Flags().GetBool("pretty")
	return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
}

// writeWatchRunResponse renders the run report: one entry per stored watch
// plus the delivery total. Cache-fallback warnings go to stderr (§5.6);
// per-watch errors are part of the report, because one broken watch must not
// fail the whole cycle.
func writeWatchRunResponse(cmd *cobra.Command, result app.RunAllResult) error {
	if result.Warning != "" {
		output.Diag(cmd.ErrOrStderr(), "%s", result.Warning)
	}
	entries := make([]watchRunEntry, 0, len(result.Runs))
	for _, run := range result.Runs {
		entry := watchRunEntry{
			Watch:           run.Check.Name,
			Skipped:         run.Check.Skipped,
			SkippedInterval: run.Check.SkippedForInterval,
			Baseline:        run.Check.Baseline,
			Events:          len(run.Check.Events),
			Kinds:           eventKinds(run.Check.Events),
		}
		if run.Err != nil {
			entry.Error = run.Err.Error()
		}
		entries = append(entries, entry)
	}
	meta := result.Meta
	if meta == nil {
		// Nothing was fetched: every watch was outside its window (S4).
		meta = &output.Meta{FetchedAt: time.Now().UTC(), Total: len(entries)}
	}
	response := watchRunResponse{
		Envelope:  output.NewEnvelope(meta, output.NewAttribution(time.Now().UTC())),
		Runs:      entries,
		Delivered: result.Delivered,
	}
	pretty, _ := cmd.Flags().GetBool("pretty")
	return output.Write(cmd.OutOrStdout(), response, output.Options{Pretty: pretty})
}

// eventKinds lists the kinds of the given events, in emission order.
func eventKinds(events []store.Event) []string {
	if len(events) == 0 {
		return nil
	}
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

// watchView renders one stored watch for the JSON contract.
func watchView(w store.Watch) watchInfo {
	info := watchInfo{
		Name:        w.Name,
		Kind:        w.Kind,
		Windows:     w.Windows,
		MinSeverity: w.MinSeverity,
		Sinks:       w.Sinks,
		Interval:    config.Duration(time.Duration(w.IntervalSecs) * time.Second).String(),
		CreatedAt:   w.CreatedAt,
	}
	switch w.Kind {
	case store.WatchKindRoute:
		info.Target.Route = w.Target.Route
	case store.WatchKindRoad:
		info.Target.Road = w.Target.Road
		if w.Target.PKFrom != nil || w.Target.PKTo != nil {
			info.Target.PK = &watchPKRange{From: w.Target.PKFrom, To: w.Target.PKTo}
		}
	case store.WatchKindNear:
		info.Target.Place = w.Target.Place
		info.Target.Lat = w.Target.Lat
		info.Target.Lon = w.Target.Lon
		info.Target.RadiusKm = w.Target.RadiusKm
	}
	if info.Windows == nil {
		info.Windows = []string{}
	}
	if info.Sinks == nil {
		info.Sinks = []string{}
	}
	return info
}
