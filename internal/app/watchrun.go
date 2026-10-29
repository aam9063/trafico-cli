package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/datex2"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/filter"
	"github.com/aam9063/trafico-cli/internal/geo"
	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/output"
	"github.com/aam9063/trafico-cli/internal/store"
	"github.com/aam9063/trafico-cli/internal/watch"
	"github.com/aam9063/trafico-cli/internal/window"
)

// The run path of WATCH-01 (docs/ARRANQUE.md §5.5, specs S3-S8): it loads a
// stored watch, fetches the situations feed ONCE (the same fetch/parse path
// `trafico incidents` uses, so the cache and the fallback behave identically),
// filters per criterion, feeds the engine (internal/watch) and drains the
// delivery queue through the configured sinks.
//
// This layer owns WHERE the data comes from and WHEN a check may run; the
// engine (internal/watch) owns WHAT changed (baseline, incident.new/updated/
// cleared, the window gate inside Check as the last line of defence).

// CheckReport is what one check of one watch reported.
type CheckReport struct {
	// Name is the watch the report belongs to (storage metadata, never sent
	// to any sink, P-07).
	Name string
	// Skipped reports that the check did not run because the instant fell
	// outside every window of the watch (S4). Nothing is fetched, nothing is
	// written and nothing is notified.
	Skipped bool
	// SkippedForInterval reports that the check did not run because the
	// watch's polling interval has not elapsed since its last real check
	// (S6). Distinct from Skipped: the window matches, but the cycle
	// refuses to hammer the DGT faster than the watch allows. Nothing is
	// fetched, nothing is written and nothing is notified.
	SkippedForInterval bool
	// Baseline reports that this check recorded the silent baseline (S3):
	// the current snapshot was stored and no event was emitted.
	Baseline bool
	// Events are the events the check produced (empty on a baseline or a
	// skipped check). On a real run they are already appended to the log and
	// pending delivery; on a manual check they are the WOULD-BE events of a
	// computation that never wrote anything (see CheckWatch).
	Events []store.Event
	// Meta is the freshness block of the fetch that fed the check (nil when
	// nothing was fetched, i.e. a skipped check): the CLI puts it in
	// meta.published_at and friends (P-08).
	Meta *output.Meta
	// Warning is the cache-fallback note of the fetch, for stderr (§5.6,
	// P-01). Empty on a skipped check.
	Warning string
}

// RunResult is the outcome of running ONE watch: what its check reported and
// how many pending events the delivery drain delivered for it.
type RunResult struct {
	// Check is the check report.
	Check CheckReport
	// Delivered is how many pending events were delivered to sinks during
	// this run (0 when the check was skipped).
	Delivered int
	// Err carries the per-watch failure of a RunAll cycle: a watch whose
	// stored definition no longer validates, one whose check failed, or one
	// whose configured sink cannot be constructed any more. The cycle
	// continues with the other watches (one broken watch must not wedge
	// every other notification); RunWatch only reports the sink failures of
	// its own watch.
	Err error
}

// RunAllResult is the outcome of `watch run`: one entry per stored watch,
// plus the totals of the whole cycle.
type RunAllResult struct {
	// Runs are the per-watch outcomes, in name order.
	Runs []RunResult
	// Delivered is how many pending events the delivery drain delivered in
	// total (across every watch).
	Delivered int
	// Meta is the freshness block of the feed fetch that fed the due watches
	// (nil when nothing was fetched: every watch was skipped).
	Meta *output.Meta
	// Warning is the cache-fallback note of the fetch, for stderr (§5.6).
	Warning string
}

// SaveWatch stores a parsed watch definition. It exists so the CLI layer
// does not have to know the storage shape: the definition layer already
// validated the spec, and storeWatch serialises it. CreatedAt is stamped
// here when the caller left it at zero.
func SaveWatch(st *store.Store, spec WatchSpec) error {
	if st == nil {
		return errs.NewInternal("no hay almacén para guardar la vigilancia")
	}
	if spec.CreatedAt.IsZero() {
		spec.CreatedAt = time.Now().UTC()
	}
	return st.SaveWatch(spec.storeWatch())
}

// ListWatches returns every stored watch definition, in name order. The CLI
// layer renders them; this wrapper keeps the commands on the app layer.
func ListWatches(st *store.Store) ([]store.Watch, error) {
	if st == nil {
		return nil, errs.NewInternal("no hay almacén para listar las vigilancias")
	}
	return st.Watches()
}

// GetWatch returns one stored watch definition. An absent name is the
// not_found error of the store (exit 4).
func GetWatch(st *store.Store, name string) (store.Watch, error) {
	if st == nil {
		return store.Watch{}, errs.NewInternal("no hay almacén para leer la vigilancia")
	}
	return st.Watch(name)
}

// RemoveWatch deletes one stored watch definition. Its stored state and
// events are history, not definition; pruning them is not this wrapper's
// job.
func RemoveWatch(st *store.Store, name string) error {
	if st == nil {
		return errs.NewInternal("no hay almacén para eliminar la vigilancia")
	}
	return st.DeleteWatch(name)
}

// RunWatch runs ONE watch: load the stored definition, resolve its criteria,
// fetch the feed once, filter, feed the engine (which appends the events and
// replaces the state) and then drain the pending delivery queue. The window
// gate (S4) and the interval gate (S6) run BEFORE the fetch: a skipped watch
// spends no request on the DGT. RunWatch is the single-watch building block
// of `watch run`; RunAll is the every-watch cycle the scheduler calls.
func RunWatch(ctx context.Context, cfg *config.Config, fetcher *feeds.Fetcher, st *store.Store, name string) (RunResult, error) {
	spec, stored, err := loadWatchSpec(st, cfg, name)
	if err != nil {
		return RunResult{}, err
	}
	now := fetcher.Now()
	// S4: outside the window there is no check — and no request to the DGT.
	if !windowMatches(spec.Windows, now) {
		return RunResult{Check: CheckReport{Name: name, Skipped: true}}, nil
	}
	// S6: inside the window but inside the interval too, the watch waits.
	if intervalPending(stored.LastCheckAt, spec.Interval, now) {
		return RunResult{Check: CheckReport{Name: name, SkippedForInterval: true}}, nil
	}
	result, publication, incidents, err := fetchSituationsIncidents(ctx, fetcher)
	if err != nil {
		return RunResult{}, err
	}
	matches, meta, err := matchWatch(spec, cfg, incidents, result, publication, fetcher)
	if err != nil {
		return RunResult{}, err
	}
	warning := result.Warning
	check, err := checkOnStore(st, spec, matches, now, warning, meta)
	if err != nil {
		return RunResult{}, err
	}
	// The check really ran: stamp it as the watch's last check (S6) before
	// delivering, so a failed delivery retries after the interval too.
	if err := st.MarkWatchChecked(spec.Name, now); err != nil {
		return RunResult{}, err
	}
	delivered, sinkErrs, err := DeliverPending(ctx, cfg, st)
	if err != nil {
		return RunResult{Check: check, Delivered: delivered}, err
	}
	outcome := RunResult{Check: check, Delivered: delivered}
	if sinkErr := sinkErrs[name]; sinkErr != nil {
		outcome.Err = sinkErr
	}
	return outcome, nil
}

// CheckWatch is the manual `watch check` (docs/ARRANQUE.md §5.5).
//
// The rule this layer implements (a decision, documented here once): a
// manual check SHOWS what would be notified and never claims that a
// notification happened. Concretely:
//
//   - Outside every window (S4) it reports Skipped, fetches nothing and
//     writes nothing: "fuera de la ventana no se comprueba ni se avisa".
//   - Inside the window it fetches, filters and runs the REAL engine
//     decision — the same state machine, over a scratch copy of the stored
//     state — so the report is exactly what a real run would emit (baseline,
//     incident.new, incident.updated, incident.cleared). But it appends no
//     event to the log and replaces no stored state: the events it reports
//     never existed and no delivery is attempted. The next real `watch run`
//     performs the detection and delivery for itself.
//
// Running the engine on a scratch store (instead of duplicating its rules
// here) keeps internal/watch the single source of truth for what a
// notification is; the scratch database lives in the system temp directory
// and is removed before returning.
func CheckWatch(ctx context.Context, cfg *config.Config, fetcher *feeds.Fetcher, st *store.Store, name string) (CheckReport, error) {
	spec, _, err := loadWatchSpec(st, cfg, name)
	if err != nil {
		return CheckReport{}, err
	}
	now := fetcher.Now()
	if !windowMatches(spec.Windows, now) {
		return CheckReport{Name: name, Skipped: true}, nil
	}
	result, publication, incidents, err := fetchSituationsIncidents(ctx, fetcher)
	if err != nil {
		return CheckReport{}, err
	}
	matches, meta, err := matchWatch(spec, cfg, incidents, result, publication, fetcher)
	if err != nil {
		return CheckReport{}, err
	}
	// The baseline verdict comes from the REAL stored state: if the watch has
	// never run, the scratch check records the baseline and emits nothing.
	previous, err := st.PreviousState(name)
	if err != nil {
		return CheckReport{}, err
	}
	scratch, err := openScratchStore()
	if err != nil {
		return CheckReport{}, err
	}
	defer scratch.Close()
	defer scratch.cleanup()
	// Copy the watch's current state onto the scratch store so the engine
	// decides over exactly what a real run would see.
	if err := scratch.ReplaceState(name, previous); err != nil {
		return CheckReport{}, err
	}
	check, err := checkOnStore(scratch.Store, spec, matches, now, result.Warning, meta)
	if err != nil {
		return CheckReport{}, err
	}
	check.Baseline = len(previous) == 0
	return check, nil
}

// RunAll is the `watch run` cycle, safe to invoke from a scheduler every
// minute: it checks every stored watch whose window matches now and whose
// polling interval has elapsed (one single feed fetch for all of them),
// leaves the rest untouched — reported skipped for the window or for the
// interval, distinctly — and finally drains the pending delivery queue. Two
// overlapping invocations cannot corrupt or duplicate: the CLI holds the
// single-instance lock (WithWatchLock) around the whole cycle, so the loser
// fails cleanly instead of checking the same snapshot twice.
//
// A watch whose stored definition no longer validates (a route or sink the
// config dropped), or whose sink can no longer be constructed, is reported
// per watch in its RunResult.Err and the cycle continues: one broken watch
// must not wedge every other notification.
func RunAll(ctx context.Context, cfg *config.Config, fetcher *feeds.Fetcher, st *store.Store) (RunAllResult, error) {
	watches, err := st.Watches()
	if err != nil {
		return RunAllResult{}, err
	}
	now := fetcher.Now()
	out := RunAllResult{Runs: make([]RunResult, 0, len(watches))}
	// The feed is fetched at most once per cycle, and only if some watch is
	// actually due (S4: outside the window, and S6: inside the interval,
	// there is no check, so no request).
	var (
		fetched      bool
		feedResult   *feeds.Result
		publication  *datex2.Publication
		incidents    []model.Incident
		totalMatches int
		totalUnloc   int
	)
	for _, stored := range watches {
		spec, err := SpecFromStore(stored, cfg)
		if err != nil {
			out.Runs = append(out.Runs, RunResult{Check: CheckReport{Name: stored.Name}, Err: err})
			continue
		}
		if !windowMatches(spec.Windows, now) {
			out.Runs = append(out.Runs, RunResult{Check: CheckReport{Name: spec.Name, Skipped: true}})
			continue
		}
		// S6: inside the window but inside the interval too, the watch waits.
		if intervalPending(stored.LastCheckAt, spec.Interval, now) {
			out.Runs = append(out.Runs, RunResult{Check: CheckReport{Name: spec.Name, SkippedForInterval: true}})
			continue
		}
		if !fetched {
			feedResult, publication, incidents, err = fetchSituationsIncidents(ctx, fetcher)
			if err != nil {
				return RunAllResult{}, err
			}
			fetched = true
		}
		matches, meta, err := matchWatch(spec, cfg, incidents, feedResult, publication, fetcher)
		if err != nil {
			out.Runs = append(out.Runs, RunResult{Check: CheckReport{Name: spec.Name}, Err: err})
			continue
		}
		totalMatches += len(matches)
		totalUnloc += meta.Unlocated
		out.Warning = feedResult.Warning
		check, err := checkOnStore(st, spec, matches, now, feedResult.Warning, meta)
		if err != nil {
			out.Runs = append(out.Runs, RunResult{Check: CheckReport{Name: spec.Name}, Err: err})
			continue
		}
		// The check really ran: stamp it as the watch's last check (S6).
		if err := st.MarkWatchChecked(spec.Name, now); err != nil {
			out.Runs = append(out.Runs, RunResult{Check: check, Err: err})
			continue
		}
		out.Runs = append(out.Runs, RunResult{Check: check})
	}
	if fetched {
		out.Meta = incidentsMeta(feedResult, publication.PublishedAt, fetcher, totalMatches, totalUnloc)
	}
	delivered, sinkErrs, err := DeliverPending(ctx, cfg, st)
	if err != nil {
		return out, err
	}
	out.Delivered = delivered
	// A sink that can no longer be constructed is a per-watch failure of the
	// drain: attach it to the run of the watch it belongs to.
	for i := range out.Runs {
		if out.Runs[i].Err != nil {
			continue
		}
		if sinkErr, ok := sinkErrs[out.Runs[i].Check.Name]; ok && sinkErr != nil {
			out.Runs[i].Err = sinkErr
		}
	}
	return out, nil
}

// DeliverPending drains the pending event queue through the sinks the events'
// watches configure. Delivery semantics:
//
//   - The oldest pending event is rendered once and delivered to EVERY sink
//     of its watch; only when all sinks accept it is the event stamped
//     delivered. A failure in the middle leaves the event pending for retry,
//     which re-sends to the sinks that already accepted (at-least-once, like
//     every notification retry).
//   - A sink error stops the drain (like watch.DrainPending): the event and
//     everything behind it wait for the next run.
//   - Events that can NEVER be delivered are stamped out of the retry queue
//     (store.MarkSkipped, their rows stay in the log) instead of stalling the
//     queue forever, in exactly two cases: a payload that cannot render
//     (undeliverable data) and a watch with no sinks at all (a watch without
//     --notify only records history). A watch that was REMOVED gets the same
//     stamp: there is no definition left to rebuild a sink from.
//   - A watch whose sinks exist in the config but cannot be CONSTRUCTED (a
//     hand-edited config, a sink removed later) is reported per watch in the
//     returned map instead: its events stay pending for retry, and the error
//     names the unusable sink. The drain keeps going for the other watches.
//
// The returned map carries the per-watch construction errors (empty when no
// watch has one); the caller attaches them to the per-watch run report.
func DeliverPending(ctx context.Context, cfg *config.Config, st *store.Store) (int, map[string]error, error) {
	if cfg == nil {
		return 0, nil, errs.NewInternal("no hay configuración para entregar los avisos")
	}
	events, err := st.UndeliveredEvents(0)
	if err != nil {
		return 0, nil, err
	}
	delivered := 0
	sinkErrs := make(map[string]error)
	type resolvedSinks struct {
		sinks []watch.Sink
		err   error
	}
	sinksByWatch := make(map[string]resolvedSinks)
	for _, event := range events {
		resolved, ok := sinksByWatch[event.WatchName]
		if !ok {
			sinks, err := resolvableSinks(event.WatchName, st, cfg)
			resolved = resolvedSinks{sinks: sinks, err: err}
			sinksByWatch[event.WatchName] = resolved
		}
		if resolved.err != nil {
			// An unusable sink is the user's explicit notification request
			// failing: report it per watch and leave the events queued (the
			// config may be fixed later), never stamped out of the queue.
			if _, seen := sinkErrs[event.WatchName]; !seen {
				sinkErrs[event.WatchName] = resolved.err
			}
			continue
		}
		if len(resolved.sinks) == 0 {
			// Undeliverable by configuration (or the watch is gone): stamp it
			// out of the queue rather than stall everything behind it.
			if err := st.MarkSkipped([]int64{event.ID}); err != nil {
				return delivered, sinkErrs, err
			}
			continue
		}
		message, err := watch.RenderMessage(event.Payload)
		if err != nil {
			if err := st.MarkSkipped([]int64{event.ID}); err != nil {
				return delivered, sinkErrs, err
			}
			continue
		}
		var deliverErr error
		for _, sink := range resolved.sinks {
			if err := sink.Deliver(ctx, message); err != nil {
				deliverErr = fmt.Errorf("no se pudo entregar el aviso de la vigilancia %q: %w", event.WatchName, err)
				break
			}
		}
		if deliverErr != nil {
			// Transient: the event stays queued together with everything
			// behind it for the next retry.
			return delivered, sinkErrs, deliverErr
		}
		if err := st.MarkDelivered([]int64{event.ID}); err != nil {
			return delivered, sinkErrs, err
		}
		delivered++
	}
	return delivered, sinkErrs, nil
}

// resolvableSinks builds the sinks a stored watch delivers through. It reads
// the RAW stored definition (delivery needs the sink names, not the window or
// target validation SpecFromStore re-runs). The outcome is one of:
//
//   - (sinks, nil): every sink was built; empty when the watch has none (a
//     watch without --notify only records history).
//   - (nil, nil): the watch was removed — its events are undeliverable for
//     good and get the same treatment as a sink-less watch.
//   - (nil, err): the watch exists but one of its sinks cannot be built from
//     the current config; its events stay pending and the caller reports err.
func resolvableSinks(name string, st *store.Store, cfg *config.Config) ([]watch.Sink, error) {
	stored, err := st.Watch(name)
	if err != nil {
		var errsErr *errs.Error
		if errors.As(err, &errsErr) && errsErr.Type == "not_found" {
			return nil, nil
		}
		return nil, err
	}
	sinks := make([]watch.Sink, 0, len(stored.Sinks))
	for _, sinkName := range stored.Sinks {
		sink, err := sinkFromConfig(sinkName, cfg.Sinks[sinkName])
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, sink)
	}
	return sinks, nil
}

// ValidateSinksUsable checks that every named sink can actually be built
// from the config, not just that its name exists: a sink whose construction
// fails (an unknown webhook template, a telegram template without a
// chat_id) must fail at `watch add` time, not silently drop every
// notification later. It is the construction half of
// validateSinks (which owns the name half); call it after ParseWatchSpec.
func ValidateSinksUsable(sinks []string, cfg *config.Config) error {
	for _, name := range sinks {
		s, ok := cfg.Sinks[name]
		if !ok {
			// An unknown name is validateSinks' error, already reported by
			// ParseWatchSpec before this runs.
			continue
		}
		if _, err := sinkFromConfig(name, s); err != nil {
			return errs.NewUsage(err.Error())
		}
	}
	return nil
}

// sinkFromConfig builds the engine sink (internal/watch) of one configured
// sink (config [sinks.<name>]). An unusable definition is an error, not a
// panic and not a silent empty sink.
func sinkFromConfig(name string, s config.Sink) (watch.Sink, error) {
	switch s.Type {
	case "ntfy":
		base := strings.TrimSpace(s.URL)
		if base == "" {
			// The compiled default of docs/ARRANQUE.md §7 and §5.5.
			base = "https://ntfy.sh"
		}
		return &watch.NtfySink{BaseURL: base, Topic: s.Topic}, nil
	case "webhook":
		style := watch.WebhookStyle(s.Template)
		switch style {
		case watch.StyleDiscord, watch.StyleSlack:
			return &watch.WebhookSink{URL: s.URL, Style: style}, nil
		case watch.StyleTelegram:
			chatID := strings.TrimSpace(s.ChatID)
			if chatID == "" {
				// A telegram sink without a chat_id can never succeed: the
				// delivery body has no destination to post to. Treat it as
				// unusable, with a message naming the missing field.
				return nil, fmt.Errorf("el destino %q es de tipo telegram y no tiene chat_id configurado: añade chat_id en [sinks.%s]", name, name)
			}
			return &watch.WebhookSink{URL: s.URL, Style: style, ChatID: chatID}, nil
		default:
			return nil, fmt.Errorf("el destino %q tiene una plantilla de webhook desconocida %q: usa discord o slack", name, s.Template)
		}
	case "exec":
		if strings.TrimSpace(s.URL) == "" {
			return nil, fmt.Errorf("el destino %q es de tipo exec y no tiene comando configurado", name)
		}
		return &watch.ExecSink{Command: s.URL}, nil
	default:
		return nil, fmt.Errorf("el destino %q tiene un tipo desconocido %q", name, s.Type)
	}
}

// loadWatchSpec loads and re-validates one stored watch definition, together
// with its raw stored row (the interval gate reads the recorded last check
// from it).
func loadWatchSpec(st *store.Store, cfg *config.Config, name string) (WatchSpec, store.Watch, error) {
	if st == nil {
		return WatchSpec{}, store.Watch{}, errs.NewInternal("no hay almacén para leer la vigilancia")
	}
	if cfg == nil {
		return WatchSpec{}, store.Watch{}, errs.NewInternal("no hay configuración para leer la vigilancia")
	}
	stored, err := st.Watch(name)
	if err != nil {
		return WatchSpec{}, store.Watch{}, err
	}
	spec, err := SpecFromStore(stored, cfg)
	return spec, stored, err
}

// intervalPending reports whether a watch must wait before its next check:
// its polling interval has not elapsed since its last real check (S6, so a
// scheduler firing every minute cannot hammer the DGT). A watch that has
// never run is due.
func intervalPending(lastCheck time.Time, interval time.Duration, now time.Time) bool {
	if lastCheck.IsZero() || interval <= 0 {
		return false
	}
	return now.Sub(lastCheck) < interval
}

// fetchSituationsIncidents fetches the situations feed once (FEED-01 owns
// the cache and the fallback) and converts it to contract incidents. This is
// the same fetch/parse path `trafico incidents` uses (fetchSituations +
// buildIncidents), never a second one.
func fetchSituationsIncidents(ctx context.Context, fetcher *feeds.Fetcher) (*feeds.Result, *datex2.Publication, []model.Incident, error) {
	result, publication, err := fetchSituations(ctx, fetcher)
	if err != nil {
		return nil, nil, nil, err
	}
	return result, publication, buildIncidents(publication, fetcher.Now()), nil
}

// matchWatch applies one watch's criteria to the fetched incidents and
// returns the matches plus the freshness meta of the fetch. The composition
// order is the documented one (docs/ARRANQUE.md §5.2, internal/geo): the
// radius filter first for a near watch, then the common filters (which carry
// the severity floor and, for route and road shapes, the road and PK
// precision).
func matchWatch(spec WatchSpec, cfg *config.Config, incidents []model.Incident, result *feeds.Result, publication *datex2.Publication, fetcher *feeds.Fetcher) ([]model.Incident, *output.Meta, error) {
	var geoUnlocated int
	if spec.Target.Kind == store.WatchKindNear {
		target, err := geo.ResolveTarget(spec.Target.Place, spec.Target.Lat, spec.Target.Lon, spec.Target.RadiusKm, cfg.Places)
		if err != nil {
			return nil, nil, err
		}
		if target != nil {
			incidents, geoUnlocated = geo.FilterRadius(incidents, *target, spec.Target.RadiusKm)
		}
	}
	criteria, err := Criteria(spec, cfg)
	if err != nil {
		return nil, nil, err
	}
	matches := make([]model.Incident, 0)
	unlocated := 0
	for _, criterion := range criteria {
		// filter.Apply owns the road, PK, severity and status semantics
		// (single source of truth); the engine dedupes by incident id, so an
		// incident matching two segments of a route arrives twice and is
		// announced once.
		applied, err := filter.Apply(incidents, criterion)
		if err != nil {
			return nil, nil, err
		}
		matches = append(matches, applied.Incidents...)
		unlocated += applied.Unlocated
	}
	meta := incidentsMeta(result, publication.PublishedAt, fetcher, len(matches), geoUnlocated+unlocated)
	return matches, meta, nil
}

// checkOnStore feeds the engine (internal/watch) on the given store and
// builds the check report. The baseline verdict is decided from the state the
// store carried BEFORE the check (the engine does not expose it): an empty
// previous state means the engine is about to record the silent baseline (S3).
func checkOnStore(st *store.Store, spec WatchSpec, matches []model.Incident, now time.Time, warning string, meta *output.Meta) (CheckReport, error) {
	previous, err := st.PreviousState(spec.Name)
	if err != nil {
		return CheckReport{}, err
	}
	checkResult, err := watch.NewWatcher(st).Check(watch.Definition{Name: spec.Name, Windows: spec.Windows, EmitInitial: spec.EmitInitial}, matches, now)
	if err != nil {
		return CheckReport{}, err
	}
	return CheckReport{
		Name:     spec.Name,
		Skipped:  checkResult.Skipped,
		Baseline: len(previous) == 0,
		Events:   checkResult.Events,
		Meta:     meta,
		Warning:  warning,
	}, nil
}

// windowMatches reports whether the instant falls inside any of the windows
// (Europe/Madrid wall clock; internal/window owns the conventions). The
// engine re-checks the same gate inside Check; this pre-check is what keeps
// a skipped watch from spending a request on the feed (S4).
func windowMatches(windows []window.Window, now time.Time) bool {
	for _, w := range windows {
		if w.Matches(now) {
			return true
		}
	}
	return false
}

// scratch store plumbing of CheckWatch: a throwaway database in the system
// temp directory that carries a copy of one watch's state so the engine can
// decide without writing anything real.

// scratchStore is the throwaway handle plus the directory to remove.
type scratchStore struct {
	*store.Store
	dir string
}

// openScratchStore opens a fresh store in a new system temp directory.
func openScratchStore() (*scratchStore, error) {
	dir, err := os.MkdirTemp("", "trafico-check-")
	if err != nil {
		return nil, errs.NewInternal(fmt.Sprintf("no se pudo crear el almacén temporal de la comprobación: %v", err))
	}
	st, err := store.Open(filepath.Join(dir, "trafico.db"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &scratchStore{Store: st, dir: dir}, nil
}

// cleanup removes the scratch database and its directory.
func (s *scratchStore) cleanup() {
	os.RemoveAll(s.dir)
}

// The single-instance lock of the read-decide-write cycle (WATCH-01): two
// processes landing on the same snapshot (`watch run` from a scheduler and a
// manual `watch check`) would emit duplicated notifications and race the
// state replacement. The lock is one O_CREATE|O_EXCL file in the data
// directory, next to trafico.db:
//
//   - Acquisition is atomic (O_EXCL); the winner writes its pid for
//     diagnostics.
//   - Releasing removes the file only if it still carries our own token: a
//     cycle paused past the staleness timeout may come back to a lock
//     another process legitimately took over, and it must not sabotage it.
//   - A lock older than the staleness timeout is treated as the remains of
//     a crashed process and taken over. The takeover runs under a second
//     O_EXCL gate file (watch.lock.steal) so two processes racing on the
//     same stale lock can never both remove-and-replace it: the gate holder
//     re-verifies the file (same token, same device/inode, still stale)
//     while holding the gate, and only then removes it. The gate is held
//     for microseconds, so its own much shorter staleness timeout can never
//     judge a live gate holder as dead.
//   - A stale lock that cannot be removed (foreign permissions, a file
//     another process keeps open) fails the acquisition with an error naming
//     the removal failure, instead of spinning on it forever.
//   - A second process facing a live lock fails cleanly with an internal
//     error that says what happened; it never corrupts state and never
//     duplicates a notification.
//
// The CLI wraps `watch check` and `watch run` in this lock, so the whole
// check-and-deliver cycle of a command is serialised. Without it, a library
// caller calling RunWatch directly runs unserialised.

// defaultLockStaleAfter is how long a lock file may sit untouched before it
// is treated as the remains of a crashed process. A cycle takes minutes at
// most (one feed fetch plus a handful of sink posts), so ten minutes is a
// generous ceiling that still un-wedges a crashed watcher quickly.
const defaultLockStaleAfter = 10 * time.Minute

// lockFileName is the lock file, created in the same directory as the
// database (docs/ARRANQUE.md §7: the data directory).
const lockFileName = "watch.lock"

// lockStealFileName is the takeover gate, created next to the lock: only one
// process at a time may transition a stale lock (re-verify it and remove it),
// so two processes racing on the same stale lock cannot both take it over
// and run concurrently. The gate is held for microseconds (a re-read plus a
// remove), so defaultGateStaleAfter — much shorter than the lock's own
// staleness timeout — can safely treat an old gate as the remains of a
// crashed takeover: no live holder can look that stale.
const lockStealFileName = "watch.lock.steal"

// defaultGateStaleAfter is how long the takeover gate may sit untouched
// before it is treated as the remains of a crashed takeover.
const defaultGateStaleAfter = 10 * time.Second

// WithWatchLock runs fn while holding the single-instance watch lock in the
// data directory of the database at dataPath. staleAfter <= 0 uses
// defaultLockStaleAfter.
func WithWatchLock(dataPath string, staleAfter time.Duration, fn func() error) error {
	if staleAfter <= 0 {
		staleAfter = defaultLockStaleAfter
	}
	dir := filepath.Dir(dataPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errs.NewInternal(fmt.Sprintf("no se pudo crear el directorio de datos %s: %v", dir, err))
	}
	lockPath := filepath.Join(dir, lockFileName)
	token := fmt.Sprintf("%d\n", os.Getpid())
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprint(file, token)
			file.Close()
			err = fn()
			releaseOwnedLock(lockPath, token)
			return err
		}
		if !os.IsExist(err) {
			return errs.NewInternal(fmt.Sprintf("no se pudo crear el bloqueo %s: %v", lockPath, err))
		}
		// The lock is held: is it a live process, a crashed one, or a file
		// that changed under us since the last look?
		retry, err := examineHeldLock(dir, lockPath, staleAfter)
		if err != nil {
			return err
		}
		if !retry {
			return errs.NewInternal(fmt.Sprintf(
				"otro proceso de trafico está ejecutando una vigilancia: espera a que termine; si el bloqueo %s ha quedado de una ejecución interrumpida, elimínalo y reintenta",
				lockPath))
		}
	}
}

// examineHeldLock inspects the held lock file after an exclusive create
// failed. It reports whether the caller should retry the acquisition (the
// file vanished, or it proved to be a stale lock that was taken over) or
// fail cleanly (the lock is live), and it fails with an error when the lock
// must be taken over but cannot be removed.
func examineHeldLock(dir, lockPath string, staleAfter time.Duration) (retry bool, err error) {
	token, info, err := readLockFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil // the holder released it between create and now
		}
		return false, errs.NewInternal(fmt.Sprintf("no se pudo examinar el bloqueo %s: %v", lockPath, err))
	}
	if time.Since(info.ModTime()) <= staleAfter {
		return false, nil // a live lock: the caller fails cleanly
	}
	return takeOverStaleLock(dir, lockPath, token, info, staleAfter)
}

// takeOverStaleLock removes the stale lock whose token and identity the
// caller examined, under the takeover gate. It reports whether the caller
// should retry the acquisition (the stale lock was removed, or it changed
// under us and must be re-evaluated), or fail cleanly (another process is
// mid-takeover), and it fails with an error when the lock cannot be removed.
func takeOverStaleLock(dir, lockPath, staleToken string, staleInfo os.FileInfo, staleAfter time.Duration) (bool, error) {
	gatePath := filepath.Join(dir, lockStealFileName)
	gateHeld := false
	defer func() {
		if gateHeld {
			os.Remove(gatePath)
		}
	}()
	for {
		file, err := os.OpenFile(gatePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
			file.Close()
			gateHeld = true
			break
		}
		if !os.IsExist(err) {
			return false, errs.NewInternal(fmt.Sprintf("no se pudo crear la puerta de toma de control %s: %v", gatePath, err))
		}
		gateInfo, statErr := os.Stat(gatePath)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue // the gate holder released it; try the create again
			}
			return false, errs.NewInternal(fmt.Sprintf("no se pudo examinar la puerta de toma de control %s: %v", gatePath, statErr))
		}
		if time.Since(gateInfo.ModTime()) <= defaultGateStaleAfter {
			return false, nil // another process is mid-takeover: fail cleanly
		}
		// An abandoned gate (its holder lives for microseconds, so no live
		// holder can look this stale): remove it and take the gate over.
		if err := os.Remove(gatePath); err != nil {
			return false, errs.NewInternal(fmt.Sprintf("no se pudo eliminar la puerta de toma de control obsoleta %s: %v", gatePath, err))
		}
	}
	// Gate held: nobody else may remove or replace the lock now. Re-verify
	// that it is still the same stale file we examined, so we can never
	// delete a lock another process has already replaced with its own live
	// one.
	token, info, err := readLockFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil // gone: retry the exclusive create
		}
		return false, errs.NewInternal(fmt.Sprintf("no se pudo examinar el bloqueo %s: %v", lockPath, err))
	}
	if token != staleToken || !os.SameFile(staleInfo, info) || time.Since(info.ModTime()) <= staleAfter {
		return true, nil // it changed or was refreshed under us: re-evaluate
	}
	if err := removeStaleLock(lockPath); err != nil {
		return false, errs.NewInternal(fmt.Sprintf(
			"no se pudo eliminar el bloqueo obsoleto %s: %v; si ha quedado de una ejecución interrumpida, elimínalo manualmente y reintenta",
			lockPath, err))
	}
	return true, nil
}

// removeStaleLock removes the verified stale lock. On Windows the removal
// fails while any other process holds the file open (a concurrent examiner
// of the same stale lock), so it retries briefly. The budget is bounded —
// a genuinely undeletable lock (foreign permissions, a file another process
// keeps open for good) still fails fast instead of spinning.
func removeStaleLock(lockPath string) error {
	const (
		attempts = 20
		pause    = 5 * time.Millisecond
	)
	var err error
	for i := 0; i < attempts; i++ {
		if err = os.Remove(lockPath); err == nil {
			return nil
		}
		if os.IsNotExist(err) {
			return nil // already gone: the acquisition retry decides
		}
		time.Sleep(pause)
	}
	return err
}

// releaseOwnedLock removes the lock file only if it still carries our own
// token: a cycle paused past the staleness timeout can come back to a lock
// another process legitimately took over, and removing the file would
// sabotage the new holder. Anything unexpected (the file gone, unreadable,
// replaced) is left alone: the worst case is a lock that a later takeover
// cleans up.
func releaseOwnedLock(lockPath, token string) {
	current, _, err := readLockFile(lockPath)
	if err != nil || current != token {
		return
	}
	os.Remove(lockPath)
}

// readLockFile opens the lock file, reads the token written inside (the pid
// of its holder, diagnostics only) and returns it together with the file's
// identity as seen from the open handle.
func readLockFile(path string) (string, os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", nil, err
	}
	buf := make([]byte, 128)
	n, _ := file.Read(buf)
	return string(buf[:n]), info, nil
}
