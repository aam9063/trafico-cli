package watch

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/store"
	"github.com/aam9063/trafico-cli/internal/window"
)

// Polling limits of docs/ARRANQUE.md §5.5 and spec S6: the feed changes every
// minute, so asking more often than every two minutes is abuse of the DGT
// server, not vigilance. Five minutes is the default.
const (
	// MinInterval is the smallest polling interval a watch run accepts.
	MinInterval = 2 * time.Minute
	// DefaultInterval is the polling interval used when the caller does not
	// configure one.
	DefaultInterval = 5 * time.Minute
)

// baselineKey is the sentinel incident id that marks a watch as having a
// baseline. It exists because store.PreviousState returns an empty map both
// for "this watch never ran" and for "its last check found zero incidents";
// without the sentinel, the first real incident after an empty check would be
// silently swallowed by the baseline rule and, worse, would eventually be
// announced as incident.cleared for an incident.new that never fired.
// The sentinel is private to this package: Check skips any incident whose id
// equals it, so no id shape promised by another package is relied upon. It
// never leaves this package and never reaches an event or a notification.
const baselineKey = "__baseline__"

// Definition is what the caller knows about one watch: its name (storage
// metadata, never sent to any sink, P-07) and the time windows it is active
// in. An empty Windows list means the watch runs at every check, 24/7.
type Definition struct {
	// Name identifies the watch in the store.
	Name string
	// Windows are the time windows (Europe/Madrid, parsed by internal/window)
	// the check may run in. Empty means no restriction.
	Windows []window.Window
}

// CheckResult is the outcome of one check. Events are already in the store's
// append-only log, still pending delivery; the caller delivers them through a
// sink (sinks.go) and the store stamps delivered_at on success.
type CheckResult struct {
	// Skipped reports that the check did not run because the instant fell
	// outside every window of the watch (spec S4). The caller logs this
	// explicitly instead of inventing an event: outside the window there is
	// no check and no notification.
	Skipped bool
	// Events are the events this check produced, empty on a baseline or
	// skipped check.
	Events []store.Event
}

// Watcher detects watch events between two checks. It is a thin state machine
// over the store: the store persists, the watcher decides (what gets notified
// and what does not).
type Watcher struct {
	st *store.Store
}

// NewWatcher returns a watcher persisting through the given store.
func NewWatcher(st *store.Store) *Watcher {
	return &Watcher{st: st}
}

// ValidateInterval rejects a polling interval below MinInterval with a usage
// error (exit 2), per spec S6 and ARRANQUE P-06.
func ValidateInterval(d time.Duration) error {
	if d < MinInterval {
		return errs.NewUsage(fmt.Sprintf(
			"el intervalo %s está por debajo del mínimo de %s: la DGT actualiza el feed cada minuto y comprobar más a menudo es abuso con el servidor, no vigilancia",
			d, MinInterval))
	}
	return nil
}

// Check runs one check of the watch against the incidents currently matching
// it (the caller fetched and filtered them) at instant now, and returns the
// events to deliver.
//
// Rules this engine implements (docs/ARRANQUE.md §5.5, specs S3-S7):
//
//   - Baseline (S3): a watch with no previous state records the current
//     snapshot and emits nothing. The first run never dumps "new incident"
//     alerts for what was already happening; --emit-initial (CLI layer,
//     WATCH-01d) is an opt-in made possible by exactly this behaviour.
//   - incident.new: present now, absent from the previous snapshot.
//   - incident.updated: present in both and the severity, the lane list or
//     the end time changed. Nothing else counts as an update: a driver cares
//     about how bad it is, which lanes are affected and when it ends.
//   - incident.cleared: absent for two consecutive checks (MissingCount >= 2,
//     the store keeps the counter). One blip in the feed must not produce a
//     "cleared" notification; a reappearance resets the counter.
//   - Window (S4): outside every window the check does not run, stores
//     nothing and returns Skipped.
//   - Dedupe: the caller may hand the same incident more than once in one
//     Check (a route watch maps one incident to one criteria per segment, so
//     an incident whose PK range overlaps two segments arrives twice). The
//     incident id is the identity: within one Check an id produces at most
//     one event of each kind, so no watch notifies twice for the same thing.
//     Duplicates are not deduped by content, only by id; the first
//     occurrence wins.
//
// Events are appended to the log BEFORE the snapshot is replaced, so a crash
// in between can at worst duplicate a notification on the next run, never
// silently lose one.
func (w *Watcher) Check(def Definition, incidents []model.Incident, now time.Time) (CheckResult, error) {
	if w == nil || w.st == nil {
		return CheckResult{}, errs.NewInternal("no hay almacén para el estado de las vigilancias")
	}
	if def.Name == "" {
		return CheckResult{}, errs.NewInternal("la vigilancia no tiene nombre")
	}

	// S4: outside the window the check does not run and does not notify.
	if len(def.Windows) > 0 && !anyWindowMatches(def.Windows, now) {
		return CheckResult{Skipped: true}, nil
	}

	previous, err := w.st.PreviousState(def.Name)
	if err != nil {
		return CheckResult{}, err
	}

	// S3: silent baseline. The sentinel marks the watch as initialised even
	// when the snapshot is empty.
	if _, hasBaseline := previous[baselineKey]; !hasBaseline {
		snapshot := currentSnapshot(incidents, now)
		snapshot[baselineKey] = store.IncidentState{LastSeenAt: now}
		return CheckResult{}, w.st.ReplaceState(def.Name, snapshot)
	}
	delete(previous, baselineKey)

	var events []store.Event
	next := make(map[string]store.IncidentState, len(incidents)+1)
	// Dedupe by id (see the Check rules): the first occurrence of an
	// incident id wins, later ones are the same incident seen through
	// another segment.
	seen := make(map[string]struct{}, len(incidents))

	for _, incident := range incidents {
		if incident.ID == baselineKey {
			// Local guard: no feed incident may collide with the baseline
			// sentinel, whatever another package promises about id shapes.
			continue
		}
		if _, duplicate := seen[incident.ID]; duplicate {
			continue
		}
		seen[incident.ID] = struct{}{}
		state := incidentStateOf(incident, now)
		before, seen := previous[incident.ID]
		switch {
		case !seen:
			events = append(events, newEvent(def.Name, incident, store.EventIncidentNew, now))
		case incidentChanged(before, state):
			events = append(events, newEvent(def.Name, incident, store.EventIncidentUpdated, now))
		}
		next[incident.ID] = state
	}

	// Absences: the counter decides (S5). At two consecutive misses the
	// incident is cleared and leaves the state; before that it stays, so a
	// reappearance resets the counter instead of firing a spurious new.
	for id, before := range previous {
		if _, present := next[id]; present {
			continue
		}
		before.MissingCount++
		if before.MissingCount >= 2 {
			events = append(events, clearedEvent(def.Name, id, before, now))
			continue
		}
		next[id] = before
	}
	next[baselineKey] = store.IncidentState{LastSeenAt: now}

	if err := w.st.AppendEvents(events); err != nil {
		return CheckResult{}, err
	}
	if err := w.st.ReplaceState(def.Name, next); err != nil {
		return CheckResult{}, err
	}
	return CheckResult{Events: events}, nil
}

// anyWindowMatches reports whether the instant falls inside any of the watch
// windows (Europe/Madrid wall clock, end-exclusive; internal/window owns the
// conventions).
func anyWindowMatches(windows []window.Window, now time.Time) bool {
	for _, w := range windows {
		if w.Matches(now) {
			return true
		}
	}
	return false
}

// currentSnapshot builds the state of a baseline check: every incident seen,
// no absence counted yet. The sentinel guard keeps the marker row private
// even if a feed incident ever carried its id, and the same id dedupe as
// Check applies: the first occurrence of an id wins.
func currentSnapshot(incidents []model.Incident, now time.Time) map[string]store.IncidentState {
	snapshot := make(map[string]store.IncidentState, len(incidents)+1)
	seen := make(map[string]struct{}, len(incidents))
	for _, incident := range incidents {
		if incident.ID == baselineKey {
			continue
		}
		if _, duplicate := seen[incident.ID]; duplicate {
			continue
		}
		seen[incident.ID] = struct{}{}
		snapshot[incident.ID] = incidentStateOf(incident, now)
	}
	return snapshot
}

// incidentStateOf reduces a contract incident to what the watch tracks
// between two checks: the severity, a stable serialisation of the lane list,
// the road, the kilometre point, the type and the announced end. The road,
// PK and type are not update triggers (S5), but they are remembered so an
// incident.cleared notification can still say what ended. Everything else is
// deliberately forgotten: not allowed to leave the machine (coordinates,
// P-07).
func incidentStateOf(incident model.Incident, now time.Time) store.IncidentState {
	return store.IncidentState{
		Severity:     incident.Severity,
		Lanes:        serializeLanes(incident.LanesAffected),
		Road:         incident.Road,
		PK:           serializePK(incident.PK),
		Type:         incident.Type,
		EndTime:      parseEndTime(incident.EndTime),
		LastSeenAt:   now,
		MissingCount: 0,
	}
}

// serializePK produces the single stored string of the kilometre-point
// interval, the shape IncidentState.PK round-trips: "102.3-108" with both
// bounds, the open end alone with one, and empty when the incident has no
// PK (P-04). formatKM keeps the values identical to what a notification
// prints, so the stored text renders back unchanged.
func serializePK(pk *model.PK) string {
	if pk == nil {
		return ""
	}
	from, to := "", ""
	if pk.From != nil {
		from = formatKM(*pk.From)
	}
	if pk.To != nil {
		to = formatKM(*pk.To)
	}
	switch {
	case from != "" && to != "":
		return from + "-" + to
	case from != "":
		return from
	case to != "":
		return to
	default:
		return ""
	}
}

// serializeLanes produces a stable serialisation of the lane list: sorted, so
// the same lanes in another order are the same state.
func serializeLanes(lanes []string) string {
	if len(lanes) == 0 {
		return ""
	}
	sorted := append([]string(nil), lanes...)
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

// parseEndTime reads the announced end of an incident as an instant. An
// unparsable value is treated as absent (P-04: the feed said something the
// contract cannot interpret; it is never invented back).
func parseEndTime(end *string) *time.Time {
	if end == nil {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, *end)
	if err != nil {
		return nil
	}
	return &parsed
}

// incidentChanged reports whether an update must fire (S5): severity, lanes
// or end time changed. Nothing else counts.
func incidentChanged(before, now store.IncidentState) bool {
	if before.Severity != now.Severity {
		return true
	}
	if before.Lanes != now.Lanes {
		return true
	}
	return !endTimeEqual(before.EndTime, now.EndTime)
}

// endTimeEqual compares nullable instants.
func endTimeEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// newEvent builds an incident.new or incident.updated event. The payload is
// rendered later, at delivery time, from these fields only (see message.go).
func newEvent(watchName string, incident model.Incident, kind string, now time.Time) store.Event {
	p := payload{
		Kind:       kind,
		IncidentID: incident.ID,
		Road:       incident.Road,
		Type:       incident.Type,
		Severity:   incident.Severity,
	}
	if incident.PK != nil {
		p.PKFrom = incident.PK.From
		p.PKTo = incident.PK.To
	}
	data, err := marshalPayload(p)
	if err != nil {
		// json.Marshal of this fixed struct cannot fail except for a
		// non-finite kilometre point (a NaN the feed carried). If it ever
		// fails, store the marker the message layer recognises instead of
		// an empty payload: the bug surfaces as an explicit internal error
		// at delivery time and, with the poison handling of DrainPending,
		// never stalls the queue.
		data = []byte(unserialisablePayloadJSON)
	}
	return store.Event{
		WatchName:  watchName, // storage metadata, never sent (P-07)
		IncidentID: incident.ID,
		Kind:       kind,
		Payload:    string(data),
		CreatedAt:  now,
	}
}

// clearedEvent builds the incident.cleared event. The incident is gone from
// the feed, so the payload carries what the last known state still remembers
// — severity, road, kilometre point and type — which is the only way the
// notification can say WHAT ended (and still nothing about the user's
// configuration, P-07).
func clearedEvent(watchName, incidentID string, before store.IncidentState, now time.Time) store.Event {
	p := payload{
		Kind:       store.EventIncidentCleared,
		IncidentID: incidentID,
		Road:       before.Road,
		Type:       before.Type,
		Severity:   before.Severity,
	}
	if from, to, ok := parsePK(before.PK); ok {
		p.PKFrom, p.PKTo = from, to
	}
	data, err := marshalPayload(p)
	if err != nil {
		// Same guard as newEvent: the marker keeps the failure visible and
		// the queue flowing.
		data = []byte(unserialisablePayloadJSON)
	}
	return store.Event{
		WatchName:  watchName,
		IncidentID: incidentID,
		Kind:       store.EventIncidentCleared,
		Payload:    string(data),
		CreatedAt:  now,
	}
}

// parsePK reads back what serializePK stored into the payload's nullable
// bounds. ok is false for an empty string (the incident had no PK, P-04) and
// for a value that cannot be parsed, which is omitted rather than invented.
func parsePK(s string) (from, to *float64, ok bool) {
	if s == "" {
		return nil, nil, false
	}
	before, after, found := strings.Cut(s, "-")
	if f, err := strconv.ParseFloat(before, 64); err == nil {
		from = &f
	}
	if found {
		if t, err := strconv.ParseFloat(after, 64); err == nil {
			to = &t
		}
	}
	if from == nil && to == nil {
		return nil, nil, false
	}
	return from, to, true
}
