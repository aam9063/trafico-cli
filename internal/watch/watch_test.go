package watch

import (
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/store"
	"github.com/aam9063/trafico-cli/internal/window"
)

// checkNow is a deterministic Wednesday 2026-11-04 07:30 UTC (08:30 Madrid,
// CET after the 2026-10-25 fall-back): inside "lun-vie 07:00-09:00".
var checkNow = time.Date(2026, 11, 4, 7, 30, 0, 0, time.UTC)

// checkNowSunday is Sunday 2026-11-08 07:30 UTC, outside any "lun-vie" window.
var checkNowSunday = time.Date(2026, 11, 8, 7, 30, 0, 0, time.UTC)

// openTestStore opens a watch store in a per-test temporary directory.
func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "trafico.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// incidentFixture builds a deterministic incident with road, PK, lanes, an
// end time and coordinates (the coordinates must never reach a notification,
// which the privacy test in message_test.go asserts).
func incidentFixture(id, severity string) model.Incident {
	from := 102.3
	to := 108.0
	end := "2026-11-04T14:00:00Z"
	lat, lon := 38.30, -0.55
	return model.Incident{
		ID:            "dgt:" + id,
		Type:          "roadworks",
		Severity:      severity,
		Road:          "A-7",
		PK:            &model.PK{From: &from, To: &to},
		LanesAffected: []string{"lane1"},
		EndTime:       &end,
		Location:      model.Location{Start: &model.Coordinates{Latitude: lat, Longitude: lon}},
	}
}

// runBaseline stores the silent first snapshot of a watch and returns nothing.
func runBaseline(t *testing.T, st *store.Store, def Definition, incidents []model.Incident) CheckResult {
	t.Helper()
	result, err := NewWatcher(st).Check(def, incidents, checkNow)
	if err != nil {
		t.Fatalf("baseline Check: %v", err)
	}
	return result
}

// TestCheckBaselineStoresSnapshotAndEmitsNothing covers S3: the first check
// of a watch with no previous state records the snapshot and emits zero
// events, so a newly created watch never dumps alerts for what was already
// happening. --emit-initial (a CLI-layer opt-in, WATCH-01d) is only possible
// because this baseline exists.
func TestCheckBaselineStoresSnapshotAndEmitsNothing(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}

	result := runBaseline(t, st, def, []model.Incident{incidentFixture("SIT-1", "medium")})

	if result.Skipped {
		t.Fatalf("baseline check reported Skipped = true, want false")
	}
	if len(result.Events) != 0 {
		t.Fatalf("baseline emitted %d events, want 0: %+v", len(result.Events), result.Events)
	}
	prev, err := st.PreviousState("trabajo")
	if err != nil {
		t.Fatalf("PreviousState: %v", err)
	}
	state, ok := prev["dgt:SIT-1"]
	if !ok {
		t.Fatalf("baseline did not store the incident snapshot: %+v", prev)
	}
	if state.Severity != "medium" {
		t.Errorf("stored severity = %q, want %q", state.Severity, "medium")
	}
	if state.MissingCount != 0 {
		t.Errorf("stored MissingCount = %d, want 0", state.MissingCount)
	}
}

// TestCheckSecondSnapshotEmitsExactlyOneNew covers S10: two consecutive
// snapshots where one incident appears produce exactly one incident.new.
func TestCheckSecondSnapshotEmitsExactlyOneNew(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}
	runBaseline(t, st, def, []model.Incident{incidentFixture("SIT-1", "medium")})

	result, err := NewWatcher(st).Check(def, []model.Incident{
		incidentFixture("SIT-1", "medium"),
		incidentFixture("SIT-2", "high"),
	}, checkNow.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("second Check: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("got %d events, want exactly 1: %+v", len(result.Events), result.Events)
	}
	event := result.Events[0]
	if event.Kind != store.EventIncidentNew {
		t.Errorf("event kind = %q, want %q", event.Kind, store.EventIncidentNew)
	}
	if event.IncidentID != "dgt:SIT-2" {
		t.Errorf("event incident id = %q, want %q", event.IncidentID, "dgt:SIT-2")
	}
	if event.WatchName != "trabajo" {
		t.Errorf("event watch name = %q, want %q (storage metadata)", event.WatchName, "trabajo")
	}
	// The events must be in the log, still pending delivery.
	pending, err := st.UndeliveredEvents(0)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 1 || pending[0].IncidentID != "dgt:SIT-2" {
		t.Fatalf("pending events = %+v, want the single SIT-2 event", pending)
	}
}

// TestCheckUpdatedOnSeverityChange covers S5: a severity change is an update.
func TestCheckUpdatedOnSeverityChange(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}
	runBaseline(t, st, def, []model.Incident{incidentFixture("SIT-1", "medium")})

	changed := incidentFixture("SIT-1", "high")
	result, err := NewWatcher(st).Check(def, []model.Incident{changed}, checkNow.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != store.EventIncidentUpdated {
		t.Fatalf("events = %+v, want exactly one incident.updated", result.Events)
	}
}

// TestCheckUpdatedOnLaneChange covers S5: a lane-list change is an update.
func TestCheckUpdatedOnLaneChange(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}
	runBaseline(t, st, def, []model.Incident{incidentFixture("SIT-1", "medium")})

	changed := incidentFixture("SIT-1", "medium")
	changed.LanesAffected = []string{"lane1", "lane2"}
	result, err := NewWatcher(st).Check(def, []model.Incident{changed}, checkNow.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != store.EventIncidentUpdated {
		t.Fatalf("events = %+v, want exactly one incident.updated", result.Events)
	}
}

// TestCheckUpdatedOnLaneReorderingIsNotAnUpdate pins the lane comparison: the
// lane list is compared as a set (stable serialisation), so the same lanes in
// another order are not a change.
func TestCheckUpdatedOnLaneReorderingIsNotAnUpdate(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}
	changed := incidentFixture("SIT-1", "medium")
	baseline := incidentFixture("SIT-1", "medium")
	// The same two lanes in another order are the same state.
	baseline.LanesAffected = []string{"lane1", "lane2"}
	changed.LanesAffected = []string{"lane2", "lane1"}
	runBaseline(t, st, def, []model.Incident{baseline})

	result, err := NewWatcher(st).Check(def, []model.Incident{changed}, checkNow.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Events) != 0 {
		t.Fatalf("events = %+v, want none (same lanes, other order)", result.Events)
	}
}

// TestCheckUpdatedOnEndTimeChange covers S5: a changing expected end is what
// a driver cares about, so an end_time change is an update.
func TestCheckUpdatedOnEndTimeChange(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}
	runBaseline(t, st, def, []model.Incident{incidentFixture("SIT-1", "medium")})

	changed := incidentFixture("SIT-1", "medium")
	newEnd := "2026-11-04T16:30:00Z"
	changed.EndTime = &newEnd
	result, err := NewWatcher(st).Check(def, []model.Incident{changed}, checkNow.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != store.EventIncidentUpdated {
		t.Fatalf("events = %+v, want exactly one incident.updated", result.Events)
	}
}

// TestCheckIgnoresOtherChanges covers S5: nothing else counts as an update.
// Direction, status, province, municipality, coordinates and updated_at
// changing produce no event.
func TestCheckIgnoresOtherChanges(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}
	runBaseline(t, st, def, []model.Incident{incidentFixture("SIT-1", "medium")})

	changed := incidentFixture("SIT-1", "medium")
	direction := "both"
	changed.Direction = &direction
	changed.Status = "ended"
	changed.Location.Province = "Alicante"
	changed.Location.Municipality = "Elche"
	changed.UpdatedAt = "2026-11-04T09:00:00Z"
	changed.Location.Start = &model.Coordinates{Latitude: 99.9, Longitude: 9.9}
	result, err := NewWatcher(st).Check(def, []model.Incident{changed}, checkNow.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Events) != 0 {
		t.Fatalf("events = %+v, want none (fields that are not severity, lanes or end_time)", result.Events)
	}
}

// TestCheckClearedAfterTwoConsecutiveAbsences covers S5: one absence emits
// nothing (a feed blip must not produce a "cleared" notification); the second
// consecutive absence emits exactly one incident.cleared and drops the
// incident from the state.
func TestCheckClearedAfterTwoConsecutiveAbsences(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}
	runBaseline(t, st, def, []model.Incident{incidentFixture("SIT-1", "medium")})
	w := NewWatcher(st)

	// First absence: the blip case, no event, missing_count incremented.
	first, err := w.Check(def, nil, checkNow.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("first absence Check: %v", err)
	}
	if len(first.Events) != 0 {
		t.Fatalf("one absence emitted %d events, want 0: %+v", len(first.Events), first.Events)
	}
	prev, err := st.PreviousState("trabajo")
	if err != nil {
		t.Fatalf("PreviousState: %v", err)
	}
	if state, ok := prev["dgt:SIT-1"]; !ok || state.MissingCount != 1 {
		t.Fatalf("state after one absence = %+v, want SIT-1 kept with MissingCount 1", prev)
	}

	// Second consecutive absence: exactly one incident.cleared.
	second, err := w.Check(def, nil, checkNow.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("second absence Check: %v", err)
	}
	if len(second.Events) != 1 || second.Events[0].Kind != store.EventIncidentCleared {
		t.Fatalf("events = %+v, want exactly one incident.cleared", second.Events)
	}
	if second.Events[0].IncidentID != "dgt:SIT-1" {
		t.Errorf("cleared incident id = %q, want %q", second.Events[0].IncidentID, "dgt:SIT-1")
	}

	// The cleared incident left the state, and a third absence emits nothing.
	prev, err = st.PreviousState("trabajo")
	if err != nil {
		t.Fatalf("PreviousState: %v", err)
	}
	if _, ok := prev["dgt:SIT-1"]; ok {
		t.Fatalf("cleared incident still tracked in state: %+v", prev)
	}
	third, err := w.Check(def, nil, checkNow.Add(15*time.Minute))
	if err != nil {
		t.Fatalf("third absence Check: %v", err)
	}
	if len(third.Events) != 0 {
		t.Fatalf("third absence emitted %d events, want 0: %+v", len(third.Events), third.Events)
	}
}

// TestCheckClearedCarriesLastKnownDescription pins that an incident.cleared
// event carries the last known road, kilometre point and type from the stored
// state: the incident is gone from the feed, so the stored description is the
// only way the notification can say WHAT ended.
func TestCheckClearedCarriesLastKnownDescription(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}
	runBaseline(t, st, def, []model.Incident{incidentFixture("SIT-1", "medium")})
	w := NewWatcher(st)

	if _, err := w.Check(def, nil, checkNow.Add(5*time.Minute)); err != nil {
		t.Fatalf("first absence Check: %v", err)
	}
	result, err := w.Check(def, nil, checkNow.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("second absence Check: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != store.EventIncidentCleared {
		t.Fatalf("events = %+v, want exactly one incident.cleared", result.Events)
	}

	var p payload
	if err := json.Unmarshal([]byte(result.Events[0].Payload), &p); err != nil {
		t.Fatalf("cleared payload is not JSON: %v", err)
	}
	if p.Road != "A-7" {
		t.Errorf("cleared payload road = %q, want %q", p.Road, "A-7")
	}
	if p.PKFrom == nil || *p.PKFrom != 102.3 || p.PKTo == nil || *p.PKTo != 108.0 {
		t.Errorf("cleared payload pk = %v-%v, want 102.3-108", p.PKFrom, p.PKTo)
	}
	if p.Type != "roadworks" {
		t.Errorf("cleared payload type = %q, want %q", p.Type, "roadworks")
	}
	if p.Severity != "medium" {
		t.Errorf("cleared payload severity = %q, want %q", p.Severity, "medium")
	}
}

// TestCheckReappearanceResetsMissingCount covers S5: a disappearance followed
// by a reappearance resets the counter and produces no cleared; two more
// consecutive absences are needed before the incident clears.
func TestCheckReappearanceResetsMissingCount(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}
	runBaseline(t, st, def, []model.Incident{incidentFixture("SIT-1", "medium")})
	w := NewWatcher(st)
	incident := incidentFixture("SIT-1", "medium")

	steps := []struct {
		name      string
		incidents []model.Incident
		wantKinds []string
	}{
		{"one absence", nil, nil},
		{"reappearance", []model.Incident{incident}, nil},
		{"second absence", nil, nil},
		{"third consecutive absence", nil, []string{store.EventIncidentCleared}},
	}
	for i, step := range steps {
		result, err := w.Check(def, step.incidents, checkNow.Add(time.Duration(5*(i+1))*time.Minute))
		if err != nil {
			t.Fatalf("step %q: Check: %v", step.name, err)
		}
		if len(result.Events) != len(step.wantKinds) {
			t.Fatalf("step %q: got %d events, want %d: %+v", step.name, len(result.Events), len(step.wantKinds), result.Events)
		}
		for j, want := range step.wantKinds {
			if result.Events[j].Kind != want {
				t.Errorf("step %q event %d: kind = %q, want %q", step.name, j, result.Events[j].Kind, want)
			}
		}
	}
}

// TestCheckOutsideWindowSkipsAndEmitsNothing covers S4: outside the window
// the check does not run and does not notify; the result says so explicitly.
func TestCheckOutsideWindowSkipsAndEmitsNothing(t *testing.T) {
	st := openTestStore(t)
	windows, err := window.ParseAll([]string{"lun-vie 07:00-09:00"})
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	def := Definition{Name: "trabajo", Windows: windows}

	result, err := NewWatcher(st).Check(def, []model.Incident{incidentFixture("SIT-1", "medium")}, checkNowSunday)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.Skipped {
		t.Fatalf("result.Skipped = false, want true: the caller must be able to see the check did not run")
	}
	if len(result.Events) != 0 {
		t.Fatalf("skipped check emitted %d events, want 0: %+v", len(result.Events), result.Events)
	}
	// The check did not run, so no snapshot may have been stored either.
	prev, err := st.PreviousState("trabajo")
	if err != nil {
		t.Fatalf("PreviousState: %v", err)
	}
	if len(prev) != 0 {
		t.Fatalf("skipped check stored a snapshot: %+v", prev)
	}
}

// TestCheckInsideWindowRuns covers S4 from the other side: a matching window
// lets the check run normally.
func TestCheckInsideWindowRuns(t *testing.T) {
	st := openTestStore(t)
	windows, err := window.ParseAll([]string{"lun-vie 07:00-09:00"})
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	def := Definition{Name: "trabajo", Windows: windows}

	result, err := NewWatcher(st).Check(def, []model.Incident{incidentFixture("SIT-1", "medium")}, checkNow)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Skipped {
		t.Fatalf("result.Skipped = true inside the window, want false")
	}
	if len(result.Events) != 0 {
		t.Fatalf("baseline inside the window emitted %d events, want 0", len(result.Events))
	}
}

// TestCheckWithoutWindowsAlwaysRuns pins that a watch defined with no window
// runs at every check (24/7), which every other test also relies on.
func TestCheckWithoutWindowsAlwaysRuns(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}

	result, err := NewWatcher(st).Check(def, []model.Incident{incidentFixture("SIT-1", "medium")}, checkNowSunday)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Skipped {
		t.Fatalf("a watch with no windows reported Skipped on a Sunday, want false")
	}
}

// TestCheckStoresMarkerWhenPayloadCannotBeSerialised covers the marshal
// guard of newEvent: a payload json.Marshal cannot serialise (a NaN
// kilometre point is the one realistic way) must be stored as the marker the
// message layer recognises, not as an empty payload that would only ever
// render a bare unmarshalling error.
func TestCheckStoresMarkerWhenPayloadCannotBeSerialised(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}
	runBaseline(t, st, def, nil)

	nan := math.NaN()
	broken := incidentFixture("SIT-NAN", "medium")
	broken.PK = &model.PK{From: &nan}
	result, err := NewWatcher(st).Check(def, []model.Incident{broken}, checkNow.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("events = %+v, want exactly one", result.Events)
	}
	var p payload
	if err := json.Unmarshal([]byte(result.Events[0].Payload), &p); err != nil {
		t.Fatalf("payload %q is not JSON: %v", result.Events[0].Payload, err)
	}
	if p.Kind != "internal.unserialisable-payload" {
		t.Errorf("payload kind = %q, want the unserialisable-payload marker", p.Kind)
	}
	if _, err := RenderMessage(result.Events[0].Payload); err == nil {
		t.Errorf("RenderMessage of the marker = nil error, want an explicit internal error")
	}
}

// TestCheckClearedStoresMarkerWhenPayloadCannotBeSerialised covers the same
// marshal guard on clearedEvent: the stored kilometre point "NaN" parses
// back as a NaN float (strconv accepts it), which json.Marshal then rejects.
func TestCheckClearedStoresMarkerWhenPayloadCannotBeSerialised(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}
	nan := math.NaN()
	broken := incidentFixture("SIT-NAN", "medium")
	broken.PK = &model.PK{From: &nan}
	runBaseline(t, st, def, []model.Incident{broken})
	w := NewWatcher(st)

	if _, err := w.Check(def, nil, checkNow.Add(5*time.Minute)); err != nil {
		t.Fatalf("first absence Check: %v", err)
	}
	result, err := w.Check(def, nil, checkNow.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("second absence Check: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != store.EventIncidentCleared {
		t.Fatalf("events = %+v, want exactly one incident.cleared", result.Events)
	}
	var p payload
	if err := json.Unmarshal([]byte(result.Events[0].Payload), &p); err != nil {
		t.Fatalf("payload %q is not JSON: %v", result.Events[0].Payload, err)
	}
	if p.Kind != "internal.unserialisable-payload" {
		t.Errorf("payload kind = %q, want the unserialisable-payload marker", p.Kind)
	}
	if _, err := RenderMessage(result.Events[0].Payload); err == nil {
		t.Errorf("RenderMessage of the marker = nil error, want an explicit internal error")
	}
}

// TestCheckSkipsIncidentWithBaselineSentinelID pins the local guard on the
// baseline sentinel: no feed incident may be tracked or notified under the
// sentinel id, whatever another package promises about incident id shapes.
func TestCheckSkipsIncidentWithBaselineSentinelID(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}
	w := NewWatcher(st)

	runBaseline(t, st, def, nil)

	sentinel := incidentFixture("SIT-1", "medium")
	sentinel.ID = baselineKey
	for i := 1; i <= 3; i++ {
		var incidents []model.Incident
		if i == 1 {
			// First check after the baseline: the sentinel incident is
			// "new" from the engine's point of view and must be ignored,
			// not announced.
			incidents = []model.Incident{sentinel}
		}
		result, err := w.Check(def, incidents, checkNow.Add(time.Duration(5*i)*time.Minute))
		if err != nil {
			t.Fatalf("check %d: %v", i, err)
		}
		for _, event := range result.Events {
			if event.IncidentID == baselineKey {
				t.Errorf("check %d emitted an event for the baseline sentinel id: %+v", i, event)
			}
		}
	}
}

// TestCheckDeduplicatesRepeatedIncidentIDInOneCheck pins the engine-level
// dedupe invariant: a route watch maps one incident to one criteria per
// segment, so a caller (route check precision, internal/app) may hand the
// same incident to one Check more than once when its PK range overlaps
// several segments. The incident id is the identity: within one Check an id
// produces at most one event of each kind, so a watch never notifies twice
// for the same thing.
func TestCheckDeduplicatesRepeatedIncidentIDInOneCheck(t *testing.T) {
	st := openTestStore(t)
	def := Definition{Name: "trabajo"}
	w := NewWatcher(st)
	runBaseline(t, st, def, []model.Incident{incidentFixture("SIT-1", "medium")})

	// New: the same unseen incident handed twice is one incident.new.
	duplicate := incidentFixture("SIT-2", "high")
	result, err := w.Check(def, []model.Incident{duplicate, duplicate}, checkNow.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("got %d events for one incident handed twice, want exactly 1: %+v", len(result.Events), result.Events)
	}
	if result.Events[0].Kind != store.EventIncidentNew || result.Events[0].IncidentID != "dgt:SIT-2" {
		t.Fatalf("event = %+v, want one incident.new for dgt:SIT-2", result.Events[0])
	}

	// Updated: the same changed incident handed twice is one incident.updated
	// (SIT-1 stays present so no incident.cleared fires in this check).
	kept := incidentFixture("SIT-1", "medium")
	changed := incidentFixture("SIT-2", "high")
	changed.LanesAffected = []string{"lane1", "lane2"}
	result, err = w.Check(def, []model.Incident{kept, changed, changed}, checkNow.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("second Check: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("got %d events for one changed incident handed twice, want exactly 1: %+v", len(result.Events), result.Events)
	}
	if result.Events[0].Kind != store.EventIncidentUpdated || result.Events[0].IncidentID != "dgt:SIT-2" {
		t.Fatalf("event = %+v, want one incident.updated for dgt:SIT-2", result.Events[0])
	}
}

// TestValidateIntervalRejectsBelowTwoMinutes covers S6: an interval below two
// minutes is a usage error (exit 2).
func TestValidateIntervalRejectsBelowTwoMinutes(t *testing.T) {
	err := ValidateInterval(90 * time.Second)
	if err == nil {
		t.Fatalf("ValidateInterval(90s) = nil, want a usage error")
	}
	var target *errs.Error
	if !errors.As(err, &target) {
		t.Fatalf("ValidateInterval(90s) error type = %T, want *errs.Error", err)
	}
	if target.ExitCode != errs.ExitUsage {
		t.Errorf("exit code = %d, want %d", target.ExitCode, errs.ExitUsage)
	}
}

// TestValidateIntervalAcceptsMinimumAndDefault covers S6: two minutes is the
// accepted minimum and five minutes the default.
func TestValidateIntervalAcceptsMinimumAndDefault(t *testing.T) {
	if err := ValidateInterval(MinInterval); err != nil {
		t.Errorf("ValidateInterval(MinInterval) = %v, want nil", err)
	}
	if err := ValidateInterval(DefaultInterval); err != nil {
		t.Errorf("ValidateInterval(DefaultInterval) = %v, want nil", err)
	}
	if MinInterval != 2*time.Minute {
		t.Errorf("MinInterval = %v, want 2m", MinInterval)
	}
	if DefaultInterval != 5*time.Minute {
		t.Errorf("DefaultInterval = %v, want 5m", DefaultInterval)
	}
}
