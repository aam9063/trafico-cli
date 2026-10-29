package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// stateFixture builds a deterministic IncidentState for tests.
func stateFixture(severity string, end *time.Time, missing int) IncidentState {
	return IncidentState{
		Severity:     severity,
		Lanes:        "1 de 3",
		EndTime:      end,
		LastSeenAt:   time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC),
		MissingCount: missing,
	}
}

// TestOpenCreatesFileAndDirWithPrivatePermissions checks ARRANQUE §7: the
// database file is created 0600 and its directory 0700, because the database
// holds personal locations (P-07). The POSIX assertion is skipped on Windows,
// which has no executable permission bits; CI covers it on Linux and macOS.
func TestOpenCreatesFileAndDirWithPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not exist on Windows; CI runs this test on Linux and macOS")
	}
	dir := filepath.Join(t.TempDir(), "trafico-cli")
	path := filepath.Join(dir, "trafico.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("directory mode = %o, want 700", dirInfo.Mode().Perm())
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %o, want 600", fileInfo.Mode().Perm())
	}
}

// TestOpenSetsSchemaVersion checks that PRAGMA user_version carries the
// schema version so a later migration can detect its starting point.
func TestOpenSetsSchemaVersion(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	got := queryUserVersion(t, s)
	if got != schemaVersion {
		t.Errorf("user_version = %d, want %d", got, schemaVersion)
	}
}

// TestReopenPreservesData checks that opening an existing database does not
// recreate the schema or lose state and events.
func TestReopenPreservesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trafico.db")

	end := time.Date(2026, 10, 28, 14, 0, 0, 0, time.UTC)
	first := map[string]IncidentState{
		"SIT-1": stateFixture("high", &end, 0),
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s.ReplaceState("trabajo", first); err != nil {
		t.Fatalf("ReplaceState: %v", err)
	}
	if err := s.AppendEvents([]Event{{
		WatchName:  "trabajo",
		IncidentID: "SIT-1",
		Kind:       EventIncidentNew,
		Payload:    `{"id":"SIT-1"}`,
		CreatedAt:  time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC),
	}}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer reopened.Close()

	if got := queryUserVersion(t, reopened); got != schemaVersion {
		t.Errorf("user_version after reopen = %d, want %d", got, schemaVersion)
	}
	states, err := reopened.PreviousState("trabajo")
	if err != nil {
		t.Fatalf("PreviousState: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("states after reopen = %d entries, want 1", len(states))
	}
	events, err := reopened.UndeliveredEvents(10)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(events) != 1 || events[0].IncidentID != "SIT-1" {
		t.Fatalf("events after reopen = %+v, want the appended event", events)
	}
}

// TestPreviousStateRoundTrip covers severity, lanes, nullable end_time,
// last_seen_at and missing_count.
func TestPreviousStateRoundTrip(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	end := time.Date(2026, 10, 28, 14, 30, 0, 0, time.UTC)
	want := map[string]IncidentState{
		"SIT-open":  stateFixture("medium", nil, 1),
		"SIT-ended": stateFixture("high", &end, 0),
		"SIT-cleared": {
			Severity:     "low",
			Lanes:        "",
			EndTime:      nil,
			LastSeenAt:   time.Date(2026, 10, 27, 8, 15, 30, 123456789, time.UTC),
			MissingCount: 2,
		},
	}
	if err := s.ReplaceState("ruta", want); err != nil {
		t.Fatalf("ReplaceState: %v", err)
	}

	got, err := s.PreviousState("ruta")
	if err != nil {
		t.Fatalf("PreviousState: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d states, want %d", len(got), len(want))
	}
	for id, wantState := range want {
		gotState, ok := got[id]
		if !ok {
			t.Errorf("missing state for %s", id)
			continue
		}
		if gotState.Severity != wantState.Severity {
			t.Errorf("%s severity = %q, want %q", id, gotState.Severity, wantState.Severity)
		}
		if gotState.Lanes != wantState.Lanes {
			t.Errorf("%s lanes = %q, want %q", id, gotState.Lanes, wantState.Lanes)
		}
		if !sameNullableTime(gotState.EndTime, wantState.EndTime) {
			t.Errorf("%s end_time = %v, want %v", id, gotState.EndTime, wantState.EndTime)
		}
		if !gotState.LastSeenAt.Equal(wantState.LastSeenAt) {
			t.Errorf("%s last_seen_at = %v, want %v", id, gotState.LastSeenAt, wantState.LastSeenAt)
		}
		if gotState.MissingCount != wantState.MissingCount {
			t.Errorf("%s missing_count = %d, want %d", id, gotState.MissingCount, wantState.MissingCount)
		}
	}
}

// TestPreviousStateUnknownWatchReturnsEmpty checks a watch with no stored
// history reads as an empty snapshot, not an error.
func TestPreviousStateUnknownWatchReturnsEmpty(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	got, err := s.PreviousState("inexistente")
	if err != nil {
		t.Fatalf("PreviousState: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("PreviousState unknown watch = %#v, want empty non-nil map", got)
	}
}

// TestReplaceStateRemovesGoneIncidents checks a new snapshot fully replaces
// the previous one, including incidents that disappeared.
func TestReplaceStateRemovesGoneIncidents(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	first := map[string]IncidentState{
		"SIT-1": stateFixture("high", nil, 0),
		"SIT-2": stateFixture("low", nil, 0),
	}
	if err := s.ReplaceState("ruta", first); err != nil {
		t.Fatalf("first ReplaceState: %v", err)
	}
	second := map[string]IncidentState{
		"SIT-2": stateFixture("medium", nil, 0),
		"SIT-3": stateFixture("high", nil, 0),
	}
	if err := s.ReplaceState("ruta", second); err != nil {
		t.Fatalf("second ReplaceState: %v", err)
	}

	got, err := s.PreviousState("ruta")
	if err != nil {
		t.Fatalf("PreviousState: %v", err)
	}
	if _, ok := got["SIT-1"]; ok {
		t.Errorf("SIT-1 survived the replace, want it gone")
	}
	if len(got) != 2 {
		t.Fatalf("got %d states, want 2", len(got))
	}
}

// TestReplaceStateIsAtomicOnFailure injects a failure in the middle of the
// batch (a row violating the missing_count CHECK constraint, after valid
// rows were already inserted): the previous snapshot must survive untouched,
// otherwise a crash would turn every incident into a spurious incident.new.
func TestReplaceStateIsAtomicOnFailure(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	previous := map[string]IncidentState{
		"SIT-1": stateFixture("high", nil, 0),
		"SIT-2": stateFixture("medium", nil, 0),
	}
	if err := s.ReplaceState("ruta", previous); err != nil {
		t.Fatalf("seeding ReplaceState: %v", err)
	}

	broken := map[string]IncidentState{
		"SIT-3":       stateFixture("low", nil, 0),
		"SIT-bad":     {Severity: "high", MissingCount: -1, LastSeenAt: time.Now().UTC()},
		"SIT-cleared": stateFixture("low", nil, 2),
	}
	if err := s.ReplaceState("ruta", broken); err == nil {
		t.Fatal("ReplaceState with an invalid row succeeded, want an error")
	}

	got, err := s.PreviousState("ruta")
	if err != nil {
		t.Fatalf("PreviousState after failure: %v", err)
	}
	if len(got) != len(previous) {
		t.Fatalf("after the failed replace there are %d states, want the previous %d", len(got), len(previous))
	}
	for id, wantState := range previous {
		gotState, ok := got[id]
		if !ok {
			t.Errorf("previous incident %s lost after a failed replace", id)
			continue
		}
		if gotState != wantState {
			t.Errorf("previous incident %s changed: got %+v, want %+v", id, gotState, wantState)
		}
	}
}

// TestEventsRoundTrip covers the append-only event log: appending, the
// nullable delivered_at, oldest-first undelivered reads, the limit,
// MarkDelivered and PruneEvents.
func TestEventsRoundTrip(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	base := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	events := []Event{
		{WatchName: "ruta", IncidentID: "SIT-1", Kind: EventIncidentNew, Payload: `{"id":"SIT-1","severity":"high"}`, CreatedAt: base},
		{WatchName: "ruta", IncidentID: "SIT-2", Kind: EventIncidentUpdated, Payload: `{"id":"SIT-2","severity":"low"}`, CreatedAt: base.Add(time.Minute)},
		{WatchName: "ruta", IncidentID: "SIT-3", Kind: EventIncidentCleared, Payload: `{"id":"SIT-3"}`, CreatedAt: base.Add(2 * time.Minute)},
	}
	if err := s.AppendEvents(events); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	// All three are undelivered, oldest first, with delivered_at null.
	pending, err := s.UndeliveredEvents(10)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("got %d undelivered events, want 3", len(pending))
	}
	wantOrder := []string{"SIT-1", "SIT-2", "SIT-3"}
	for i, wantID := range wantOrder {
		if pending[i].IncidentID != wantID {
			t.Errorf("pending[%d] incident = %q, want %q (oldest first)", i, pending[i].IncidentID, wantID)
		}
		if pending[i].DeliveredAt != nil {
			t.Errorf("pending[%d] delivered_at = %v, want null", i, pending[i].DeliveredAt)
		}
		if pending[i].ID == 0 {
			t.Errorf("pending[%d] has no assigned id", i)
		}
		if pending[i].WatchName != "ruta" || pending[i].Kind != events[i].Kind || pending[i].Payload != events[i].Payload {
			t.Errorf("pending[%d] = %+v, want the appended event %+v", i, pending[i], events[i])
		}
		if !pending[i].CreatedAt.Equal(events[i].CreatedAt) {
			t.Errorf("pending[%d] created_at = %v, want %v", i, pending[i].CreatedAt, events[i].CreatedAt)
		}
	}

	// The limit keeps only the oldest entries.
	limited, err := s.UndeliveredEvents(2)
	if err != nil {
		t.Fatalf("UndeliveredEvents(2): %v", err)
	}
	if len(limited) != 2 || limited[0].IncidentID != "SIT-1" || limited[1].IncidentID != "SIT-2" {
		t.Fatalf("limited events = %+v, want the two oldest", limited)
	}

	// MarkDelivered removes them from the pending set without deleting rows.
	if err := s.MarkDelivered([]int64{pending[0].ID, pending[1].ID}); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	stillPending, err := s.UndeliveredEvents(10)
	if err != nil {
		t.Fatalf("UndeliveredEvents after mark: %v", err)
	}
	if len(stillPending) != 1 || stillPending[0].IncidentID != "SIT-3" {
		t.Fatalf("pending after mark = %+v, want only SIT-3", stillPending)
	}

	// PruneEvents deletes only rows older than the cutoff.
	pruned, err := s.PruneEvents(base.Add(90 * time.Second))
	if err != nil {
		t.Fatalf("PruneEvents: %v", err)
	}
	if pruned != 2 {
		t.Errorf("pruned = %d rows, want 2", pruned)
	}
	remaining, err := s.UndeliveredEvents(10)
	if err != nil {
		t.Fatalf("UndeliveredEvents after prune: %v", err)
	}
	if len(remaining) != 1 || remaining[0].IncidentID != "SIT-3" {
		t.Fatalf("undelivered after prune = %+v, want only SIT-3", remaining)
	}
}

// TestMarkDeliveredKeepsRows checks that delivered rows survive in the log
// (the payload may be needed for audits or retries diagnostics).
func TestMarkDeliveredKeepsRows(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	created := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	if err := s.AppendEvents([]Event{{
		WatchName:  "ruta",
		IncidentID: "SIT-1",
		Kind:       EventIncidentNew,
		Payload:    `{"id":"SIT-1"}`,
		CreatedAt:  created,
	}}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}
	pending, err := s.UndeliveredEvents(10)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if err := s.MarkDelivered([]int64{pending[0].ID}); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	row := queryEventByID(t, s, pending[0].ID)
	if row == nil {
		t.Fatal("delivered event row was deleted, want it kept")
	}
	if row.DeliveredAt == nil {
		t.Error("delivered_at still null after MarkDelivered")
	}
	if row.Payload != `{"id":"SIT-1"}` {
		t.Errorf("payload = %q, want it preserved", row.Payload)
	}
}

// TestTwoOpenHandlesSamePath checks that two Store handles on the same file
// work concurrently in one process: `watch run` and `watch check` may overlap.
func TestTwoOpenHandlesSamePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trafico.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	defer s1.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	// Write through one handle, read through the other.
	if err := s1.ReplaceState("ruta", map[string]IncidentState{
		"SIT-1": stateFixture("high", nil, 0),
	}); err != nil {
		t.Fatalf("ReplaceState via handle 1: %v", err)
	}
	got, err := s2.PreviousState("ruta")
	if err != nil {
		t.Fatalf("PreviousState via handle 2: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("handle 2 saw %d states, want 1", len(got))
	}

	// Interleaved writes must not deadlock or fail (busy_timeout).
	if err := s2.AppendEvents([]Event{{
		WatchName:  "ruta",
		IncidentID: "SIT-1",
		Kind:       EventIncidentNew,
		Payload:    "{}",
		CreatedAt:  time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("AppendEvents via handle 2: %v", err)
	}
	if err := s1.AppendEvents([]Event{{
		WatchName:  "ruta",
		IncidentID: "SIT-2",
		Kind:       EventIncidentNew,
		Payload:    "{}",
		CreatedAt:  time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("AppendEvents via handle 1: %v", err)
	}
}

// TestPruneEventsKeepsUndelivered checks that PruneEvents never destroys an
// undelivered event: a pending event older than the cutoff is a notification
// the user never received, so only delivered rows may be pruned.
func TestPruneEventsKeepsUndelivered(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	base := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	if err := s.AppendEvents([]Event{
		{WatchName: "ruta", IncidentID: "SIT-pending", Kind: EventIncidentNew, Payload: `{"id":"SIT-pending"}`, CreatedAt: base},
		{WatchName: "ruta", IncidentID: "SIT-delivered", Kind: EventIncidentNew, Payload: `{"id":"SIT-delivered"}`, CreatedAt: base.Add(time.Minute)},
	}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}
	pending, err := s.UndeliveredEvents(10)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	var deliveredID int64
	for _, event := range pending {
		if event.IncidentID == "SIT-delivered" {
			deliveredID = event.ID
		}
	}
	if deliveredID == 0 {
		t.Fatal("SIT-delivered not found among the pending events")
	}
	if err := s.MarkDelivered([]int64{deliveredID}); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	// Both events are older than the cutoff; only the delivered one may go.
	cutoff := base.Add(2 * time.Hour)
	pruned, err := s.PruneEvents(cutoff)
	if err != nil {
		t.Fatalf("PruneEvents: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d rows, want 1 (only the delivered event)", pruned)
	}
	if row := queryEventByID(t, s, deliveredID); row != nil {
		t.Error("delivered event older than the cutoff survived the prune, want it gone")
	}
	kept, err := s.UndeliveredEvents(10)
	if err != nil {
		t.Fatalf("UndeliveredEvents after prune: %v", err)
	}
	if len(kept) != 1 || kept[0].IncidentID != "SIT-pending" {
		t.Fatalf("undelivered after prune = %+v, want SIT-pending to survive", kept)
	}
}

// TestUndeliveredEventsOrdersChronologically pins the retry order across
// whole-second and fractional timestamps in the same second: the storage
// format keeps a fixed nine-digit fractional part, because RFC 3339Nano
// drops trailing zeros and would make "...:00Z" sort after "...:00.5Z".
func TestUndeliveredEventsOrdersChronologically(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	whole := time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC)
	if err := s.AppendEvents([]Event{
		{WatchName: "ruta", IncidentID: "SIT-whole", Kind: EventIncidentNew, Payload: `{"id":"SIT-whole"}`, CreatedAt: whole},
		{WatchName: "ruta", IncidentID: "SIT-fraction", Kind: EventIncidentNew, Payload: `{"id":"SIT-fraction"}`, CreatedAt: whole.Add(500 * time.Millisecond)},
	}); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}

	pending, err := s.UndeliveredEvents(10)
	if err != nil {
		t.Fatalf("UndeliveredEvents: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("got %d undelivered events, want 2", len(pending))
	}
	if pending[0].IncidentID != "SIT-whole" || pending[1].IncidentID != "SIT-fraction" {
		t.Errorf("order = [%s, %s], want [SIT-whole, SIT-fraction] (chronological)",
			pending[0].IncidentID, pending[1].IncidentID)
	}
}

// TestOpenRefusesNewerSchema checks that a database written by a newer
// schema version is refused instead of silently misread.
func TestOpenRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trafico.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := s.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err = Open(path)
	if err == nil {
		t.Fatal("Open on a newer-schema database succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "99") {
		t.Errorf("error %q does not mention the database version 99", err)
	}
}

// TestOpenTightensLoosePermissions checks that Open enforces the §7 modes on
// an existing database: a file restored from a backup or created by another
// tool must come back 0600, and its directory 0700. Skipped on Windows,
// which has no POSIX permission bits.
func TestOpenTightensLoosePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits do not exist on Windows; CI runs this test on Linux and macOS")
	}
	dir := filepath.Join(t.TempDir(), "trafico-cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "trafico.db")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("directory mode = %o, want 700 after Open", dirInfo.Mode().Perm())
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %o, want 600 after Open", fileInfo.Mode().Perm())
	}
}

// TestMarkDeliveredUnknownIDFails pins that marking an unknown event id
// fails instead of silently succeeding, whatever RowsAffected reports.
func TestMarkDeliveredUnknownIDFails(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	if err := s.MarkDelivered([]int64{4242}); err == nil {
		t.Fatal("MarkDelivered with an unknown id succeeded, want an error")
	}
}

// openTestStore opens a database inside t.TempDir() and registers cleanup.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "trafico.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// sameNullableTime compares two nullable times, treating nil and zero alike.
func sameNullableTime(a, b *time.Time) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Equal(*b)
	}
}

// queryUserVersion reads PRAGMA user_version through the store handle.
func queryUserVersion(t *testing.T, s *Store) int {
	t.Helper()
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("PRAGMA user_version: %v", err)
	}
	return version
}

// queryEventByID loads one event row by id, or nil when it is gone.
func queryEventByID(t *testing.T, s *Store, id int64) *Event {
	t.Helper()
	row := s.db.QueryRow(`SELECT watch_name, incident_id, kind, payload, created_at, delivered_at
		FROM watch_events WHERE id = ?`, id)
	event := &Event{}
	var deliveredAt sql.NullString
	var createdAt string
	if err := row.Scan(&event.WatchName, &event.IncidentID, &event.Kind, &event.Payload,
		&createdAt, &deliveredAt); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		t.Fatalf("query event %d: %v", id, err)
	}
	created, err := parseStoredTime(createdAt)
	if err != nil {
		t.Fatalf("created_at %q: %v", createdAt, err)
	}
	event.CreatedAt = created
	if deliveredAt.Valid {
		parsed, err := parseStoredTime(deliveredAt.String)
		if err != nil {
			t.Fatalf("delivered_at %q: %v", deliveredAt.String, err)
		}
		event.DeliveredAt = &parsed
	}
	return event
}
