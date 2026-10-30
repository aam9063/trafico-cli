package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/filter"
	"github.com/aam9063/trafico-cli/internal/geo"
	"github.com/aam9063/trafico-cli/internal/road"
	"github.com/aam9063/trafico-cli/internal/store"
	"github.com/aam9063/trafico-cli/internal/watch"
	"github.com/aam9063/trafico-cli/internal/window"
)

// The watch definition layer (docs/ARRANQUE.md §5.5, WATCH-01): it turns
// what the user asked for into a validated WatchSpec, stores it through
// internal/store (§7: the database holds the vigilancias, not the config
// file) and translates it into the filter criteria the watch engine's
// caller applies over the fetched incidents. The engine (internal/watch)
// stays untouched: it decides WHAT changed; this layer decides what the
// watch looks at.

// WatchTarget is what a watch looks at, in one of three shapes selected by
// Kind (the store.WatchKind* constants):
//
//   - route: a saved route by name; the watch covers every declared
//     segment, with route-check precision (one road and PK range per
//     segment).
//   - road: one road with an optional kilometre range; a missing bound is
//     an open end (the filter.Range convention).
//   - near: a radius around a saved place name or around raw coordinates.
//     Place and Lat/Lon are mutually exclusive; RadiusKm must be positive.
//
// The zero value is not a valid target: ParseWatchSpec enforces a kind and
// its fields.
type WatchTarget struct {
	// Kind is one of store.WatchKindRoute, store.WatchKindRoad or
	// store.WatchKindNear.
	Kind string
	// Route is the saved route name (kind route).
	Route string
	// Road is the road name (kind road); it is normalised on parse, so
	// "a7" and "A-7" are the same target.
	Road string
	// PK is the optional kilometre range (kind road).
	PK *filter.Range
	// Place is the saved place name centring the radius (kind near).
	Place string
	// Lat and Lon are the radius centre when no place name is given
	// (kind near); used together or not at all.
	Lat *float64
	Lon *float64
	// RadiusKm is the radius in kilometres (kind near).
	RadiusKm float64
}

// WatchSpec is a validated watch definition: everything `watch run` and
// `watch check` need besides the fetched incidents. The windows are both
// the parsed values (for the engine) and the original spec strings (for the
// store, which keeps them verbatim so a stored watch round-trips through
// internal/window).
type WatchSpec struct {
	// Name identifies the watch in the store; storage metadata, never sent
	// to any sink (P-07).
	Name string
	// Target is the validated target.
	Target WatchTarget
	// Windows are the parsed time windows (Europe/Madrid, internal/window).
	Windows []window.Window
	// WindowSpecs are the original window spec strings, stored verbatim.
	WindowSpecs []string
	// MinSeverity is the severity floor: "", "low", "medium" or "high".
	MinSeverity string
	// EmitInitial is the stored opt-in of --emit-initial (§5.5): the watch
	// announces with incident.new what is already happening at its first
	// check. The default (false) is the silent baseline of S3.
	EmitInitial bool
	// Sinks are the notification destinations, by config sink name.
	Sinks []string
	// Interval is the polling interval (≥ watch.MinInterval, spec S6).
	Interval time.Duration
	// CreatedAt is when the watch was saved (set by the caller).
	CreatedAt time.Time
}

// ParseWatchSpec validates one watch definition and parses it into a
// WatchSpec. Every failure is a usage error (exit 2), except a target that
// names a route or a place the config does not carry: that is not_found
// (exit 4), like the route and place commands. Nothing here touches the
// network: routes, places and sinks all live in the config.
//
// Sinks are optional: a watch defined with zero sinks is accepted and only
// records history (its events are appended to the log and never delivered),
// because the spec does not mandate --notify. `watch list` shows it like
// any other watch, so the decision stays visible to the user.
func ParseWatchSpec(name string, target WatchTarget, windowSpecs []string, minSeverity string, sinks []string, interval time.Duration, emitInitial bool, cfg *config.Config) (WatchSpec, error) {
	if cfg == nil {
		return WatchSpec{}, errs.NewInternal("no hay configuración para definir la vigilancia")
	}
	// Same safe-identifier rule as routes and places (the config and the
	// store keys must never read as a path or a shell fragment); the
	// message is reworded for watches in Spanish.
	if err := config.ValidateRouteName(name); err != nil {
		return WatchSpec{}, errs.NewUsage(fmt.Sprintf(
			"el nombre de la vigilancia %q no es un identificador seguro: usa de 1 a 64 letras, dígitos, puntos, guiones o guiones bajos, empezando por letra o dígito", name))
	}
	if err := validateTarget(target, cfg); err != nil {
		return WatchSpec{}, err
	}
	// S4: a watch without windows would never be silent, so at least one
	// window is required at definition time (same rule on the read path,
	// see errNoWindows).
	if len(windowSpecs) == 0 {
		return WatchSpec{}, errs.NewUsage(errNoWindows)
	}
	windows, err := window.ParseAll(windowSpecs)
	if err != nil {
		return WatchSpec{}, err
	}
	min, err := validateMinSeverity(minSeverity)
	if err != nil {
		return WatchSpec{}, err
	}
	if err := validateSinks(sinks, cfg); err != nil {
		return WatchSpec{}, err
	}
	// S6: the polling floor lives in the engine package, so the CLI layer
	// and this definition layer cannot drift apart.
	if err := watch.ValidateInterval(interval); err != nil {
		return WatchSpec{}, err
	}
	return WatchSpec{
		Name:        name,
		Target:      target,
		Windows:     windows,
		WindowSpecs: windowSpecs,
		MinSeverity: min,
		EmitInitial: emitInitial,
		Sinks:       sinks,
		Interval:    interval,
	}, nil
}

// validateTarget checks one target shape against the config: the route and
// the place must exist (not_found), the road must be a real name and the
// near radius must be positive (usage). Road names are normalised in place
// of the caller, so the stored target already carries "A-7", not "a7".
func validateTarget(target WatchTarget, cfg *config.Config) error {
	switch target.Kind {
	case store.WatchKindRoute:
		if target.Route == "" {
			return errs.NewUsage("indica el nombre de la ruta a vigilar")
		}
		if _, ok := cfg.Routes[target.Route]; !ok {
			return errs.NewNotFound(fmt.Sprintf("no existe la ruta %q", target.Route))
		}
		// Reuse the route-check parser: the segments were validated when
		// the route was saved, but a bad stored segment must surface as
		// usage here too, never as a silent watch on nothing.
		if _, err := parseStoredSegments(target.Route, cfg.Routes[target.Route].Segments); err != nil {
			return err
		}
		return nil
	case store.WatchKindRoad:
		if road.Normalize(target.Road) == "" {
			return errs.NewUsage("indica la carretera a vigilar, por ejemplo --road A-7")
		}
		if target.PK != nil && target.PK.From != nil && target.PK.To != nil && *target.PK.From > *target.PK.To {
			return errs.NewUsage(fmt.Sprintf("el tramo --pk está invertido (%g > %g)", *target.PK.From, *target.PK.To))
		}
		return nil
	case store.WatchKindNear:
		// geo owns the near/lat-lon/radius combination rules (exclusivity,
		// positive radius, coordinate ranges) and the place lookup; reuse
		// them instead of a second copy. The resolved centre is thrown
		// away: the target stores the inputs, resolved again at run time,
		// so a place the user later edits takes effect without re-adding
		// the watch.
		_, err := geo.ResolveTarget(target.Place, target.Lat, target.Lon, target.RadiusKm, cfg.Places)
		return err
	default:
		return errs.NewUsage(fmt.Sprintf("la clase de vigilancia %q no es válida: usa route, road o near", target.Kind))
	}
}

// validMinSeverities are the severity floors a watch accepts; empty means
// "any severity". The floor "low" reads as "any published severity": the
// DGT publishes no severity (unknown) for the large majority of records
// (docs/feeds.md §4.5), so its set includes unknown — a floor the user
// reads as "anything the feed carries" must not quietly drop most of the
// feed. The floors "medium" and "high" exclude unknown, because "not
// published" is not "at least medium".
var validMinSeverities = map[string][]string{
	"":       nil,
	"low":    {"unknown", "low", "medium", "high"},
	"medium": {"medium", "high"},
	"high":   {"high"},
}

// errNoWindows is the usage message shared by the parse path and the read
// path, so a stored watch without windows fails with exactly the error the
// write path would have given.
const errNoWindows = "indica al menos una ventana con --window \"días HH:MM-HH:MM\", por ejemplo --window \"lun-vie 07:00-09:00\""

// validateMinSeverity normalises and validates the severity floor, returning
// the normalised value. The mapping to filter severities lives in
// severitiesAtOrAbove, so the accepted set and its translation cannot drift.
func validateMinSeverity(min string) (string, error) {
	normalised := strings.ToLower(strings.TrimSpace(min))
	if _, ok := validMinSeverities[normalised]; !ok {
		return "", errs.NewUsage(fmt.Sprintf("--min-severity debe ser low, medium o high; recibido %q", min))
	}
	return normalised, nil
}

// validateSinks checks that every named sink exists in the config: a typo
// in --notify must fail at watch add time, not silence the watch forever.
func validateSinks(sinks []string, cfg *config.Config) error {
	for _, sink := range sinks {
		if _, ok := cfg.Sinks[sink]; !ok {
			return errs.NewUsage(fmt.Sprintf("el destino %q no está definido en la configuración: añádelo en [sinks.%s]", sink, sink))
		}
	}
	return nil
}

// Criteria translates a watch spec into the filter criteria its caller
// applies with filter.Apply over the fetched incidents, one criteria per
// entry. The engine (internal/watch) receives already-filtered incidents,
// so the precision of a route check is kept here:
//
//   - route: one criteria per declared segment (normalised road + PK
//     bounds), in declaration order — the same shape `route check` matches
//     with, because filter.Apply only accepts a PK range with exactly one
//     road.
//   - road: one criteria with the road and its optional PK range.
//   - near: one criteria with no road or PK filter. The radius composition
//     is the caller's job, in the same order `incidents` uses (docs/
//     ARRANQUE.md §5.2, internal/geo): geo.ResolveTarget with the target's
//     place/lat-lon/radius, then geo.FilterRadius, then filter.Apply with
//     the returned criteria.
//
// The min severity floor travels as the criteria's Severities set (a floor
// of "medium" is the set medium+high), so the engine never sees incidents
// below it and a severity rising past the floor fires incident.new exactly
// once. A route or place the config no longer carries is a not_found error;
// anything else is usage or internal, as the shape demands.
func Criteria(spec WatchSpec, cfg *config.Config) ([]filter.Criteria, error) {
	if cfg == nil {
		return nil, errs.NewInternal("no hay configuración para traducir la vigilancia a filtros")
	}
	severities, err := severitiesAtOrAbove(spec.MinSeverity)
	if err != nil {
		return nil, err
	}
	base := filter.Criteria{Severities: severities}

	switch spec.Target.Kind {
	case store.WatchKindRoute:
		stored, ok := cfg.Routes[spec.Target.Route]
		if !ok {
			return nil, errs.NewNotFound(fmt.Sprintf("no existe la ruta %q", spec.Target.Route))
		}
		segments, err := parseStoredSegments(spec.Target.Route, stored.Segments)
		if err != nil {
			return nil, err
		}
		criteria := make([]filter.Criteria, 0, len(segments))
		for _, segment := range segments {
			perSegment := base // copy: each segment carries its own road and PK
			perSegment.Roads = []string{segment.Road}
			perSegment.PK = &filter.Range{From: &segment.From, To: &segment.To}
			criteria = append(criteria, perSegment)
		}
		return criteria, nil
	case store.WatchKindRoad:
		perRoad := base
		perRoad.Roads = []string{road.Normalize(spec.Target.Road)}
		if spec.Target.PK != nil {
			perRoad.PK = &filter.Range{From: spec.Target.PK.From, To: spec.Target.PK.To}
		}
		return []filter.Criteria{perRoad}, nil
	case store.WatchKindNear:
		return []filter.Criteria{base}, nil
	default:
		return nil, errs.NewInternal(fmt.Sprintf("la vigilancia %q tiene una clase de objetivo desconocida %q", spec.Name, spec.Target.Kind))
	}
}

// severitiesAtOrAbove maps a validated severity floor to the set of
// severities filter.Apply accepts, so the floor is applied as a filter
// before the engine ever sees an incident. The rule (see validMinSeverities):
// the floor "low" includes unknown — "any published severity" — while
// "medium" and "high" exclude it, and an empty floor sets no restriction.
func severitiesAtOrAbove(min string) ([]string, error) {
	normalised := strings.ToLower(strings.TrimSpace(min))
	severities, ok := validMinSeverities[normalised]
	if !ok {
		return nil, errs.NewUsage(fmt.Sprintf("--min-severity debe ser low, medium o high; recibido %q", min))
	}
	return severities, nil
}

// storeWatch serialises a parsed spec into the storage shape. The window
// specs are stored verbatim and the interval as whole seconds, so the
// store stays free of package semantics it cannot validate.
func (spec WatchSpec) storeWatch() store.Watch {
	return store.Watch{
		Name:         spec.Name,
		Kind:         spec.Target.Kind,
		Target:       storeTarget(spec.Target),
		Windows:      spec.WindowSpecs,
		MinSeverity:  spec.MinSeverity,
		EmitInitial:  spec.EmitInitial,
		Sinks:        spec.Sinks,
		IntervalSecs: int(spec.Interval / time.Second),
		CreatedAt:    spec.CreatedAt,
	}
}

// storeTarget flattens the three target shapes into the store's tagged
// union, keeping only the fields the kind uses.
func storeTarget(t WatchTarget) store.Target {
	switch t.Kind {
	case store.WatchKindRoute:
		return store.Target{Route: t.Route}
	case store.WatchKindRoad:
		target := store.Target{Road: road.Normalize(t.Road)}
		if t.PK != nil {
			target.PKFrom = t.PK.From
			target.PKTo = t.PK.To
		}
		return target
	case store.WatchKindNear:
		return store.Target{Place: t.Place, Lat: t.Lat, Lon: t.Lon, RadiusKm: t.RadiusKm}
	default:
		return store.Target{}
	}
}

// SpecFromStore rebuilds a parsed spec from its stored definition, running
// the same validation as ParseWatchSpec: a stored watch is re-parsed, never
// trusted blindly, so a watch saved by an older version (or a row corrupted
// outside the CLI) fails loudly instead of silently watching nothing. The
// config supplies the current routes, places and sinks, exactly what a
// `watch run` needs. The re-run rules include the read-path counterparts of
// every write-path check: the target is revalidated against the current
// config (a route or place the config no longer carries stays not_found;
// an inconsistent near or road target is usage), and a watch without
// windows is rejected instead of silently running 24/7.
func SpecFromStore(w store.Watch, cfg *config.Config) (WatchSpec, error) {
	if cfg == nil {
		return WatchSpec{}, errs.NewInternal("no hay configuración para leer la vigilancia")
	}
	target, err := targetFromStore(w.Target, w.Kind)
	if err != nil {
		return WatchSpec{}, err
	}
	// The same target rules the write path enforces, re-run against the
	// current config: a corrupt or stale row must fail where it is read,
	// with the same error (usage, or not_found for a route or place the
	// config no longer carries) `watch add` would have given.
	if err := validateTarget(target, cfg); err != nil {
		return WatchSpec{}, err
	}
	// S4 on the read path: a row with no windows ("null" or [] after a
	// hand edit or a corrupt write) would run 24/7, because the engine
	// treats an empty window list as "always". Reject it like the parse
	// path does.
	if len(w.Windows) == 0 {
		return WatchSpec{}, errs.NewUsage(errNoWindows)
	}
	windows, err := window.ParseAll(w.Windows)
	if err != nil {
		return WatchSpec{}, err
	}
	min, err := validateMinSeverity(w.MinSeverity)
	if err != nil {
		return WatchSpec{}, err
	}
	if err := validateSinks(w.Sinks, cfg); err != nil {
		return WatchSpec{}, err
	}
	interval := time.Duration(w.IntervalSecs) * time.Second
	if err := watch.ValidateInterval(interval); err != nil {
		return WatchSpec{}, err
	}
	return WatchSpec{
		Name:        w.Name,
		Target:      target,
		Windows:     windows,
		WindowSpecs: w.Windows,
		MinSeverity: min,
		EmitInitial: w.EmitInitial,
		Sinks:       w.Sinks,
		Interval:    interval,
		CreatedAt:   w.CreatedAt,
	}, nil
}

// targetFromStore rebuilds the validated target shape from the store's
// tagged union, checking that the stored fields match the kind: a row whose
// target does not agree with its kind column is unreadable state, reported
// as an internal error instead of a spec that would silently watch nothing.
func targetFromStore(t store.Target, kind string) (WatchTarget, error) {
	switch kind {
	case store.WatchKindRoute:
		if t.Route == "" {
			return WatchTarget{}, errs.NewInternal(fmt.Sprintf("la vigilancia de clase route no nombra ninguna ruta (objetivo %q)", routeTargetEcho(t)))
		}
		return WatchTarget{Kind: kind, Route: t.Route}, nil
	case store.WatchKindRoad:
		if t.Road == "" {
			return WatchTarget{}, errs.NewInternal("la vigilancia de clase road no nombra ninguna carretera")
		}
		target := WatchTarget{Kind: kind, Road: t.Road}
		if t.PKFrom != nil || t.PKTo != nil {
			target.PK = &filter.Range{From: t.PKFrom, To: t.PKTo}
		}
		return target, nil
	case store.WatchKindNear:
		if t.Place == "" && (t.Lat == nil || t.Lon == nil) {
			return WatchTarget{}, errs.NewInternal("la vigilancia de clase near no tiene ni lugar ni coordenadas")
		}
		if t.RadiusKm <= 0 {
			return WatchTarget{}, errs.NewInternal("la vigilancia de clase near tiene un radio no positivo")
		}
		return WatchTarget{Kind: kind, Place: t.Place, Lat: t.Lat, Lon: t.Lon, RadiusKm: t.RadiusKm}, nil
	default:
		return WatchTarget{}, errs.NewInternal(fmt.Sprintf("la vigilancia tiene una clase de objetivo desconocida %q", kind))
	}
}

// routeTargetEcho renders a stored target for an error message; it is
// diagnostics only and never reaches a sink (P-07).
func routeTargetEcho(t store.Target) string {
	if t.Route != "" {
		return t.Route
	}
	return "{}"
}
