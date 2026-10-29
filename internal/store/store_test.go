package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/errs"
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

// v1SchemaSQL is the schema of store version 1, kept verbatim so the
// migration test can hand-craft a database exactly as the previous binary
// wrote it.
const v1SchemaSQL = `
CREATE TABLE IF NOT EXISTS watch_state (
	watch_name    TEXT    NOT NULL,
	incident_id   TEXT    NOT NULL,
	severity      TEXT    NOT NULL DEFAULT '',
	lanes         TEXT    NOT NULL DEFAULT '',
	end_time      TEXT,
	last_seen_at  TEXT    NOT NULL,
	missing_count INTEGER NOT NULL DEFAULT 0 CHECK (missing_count >= 0),
	PRIMARY KEY (watch_name, incident_id)
);

CREATE TABLE IF NOT EXISTS watch_events (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	watch_name   TEXT NOT NULL,
	incident_id  TEXT NOT NULL,
	kind         TEXT NOT NULL,
	payload      TEXT NOT NULL,
	created_at   TEXT NOT NULL,
	delivered_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_watch_events_pending
	ON watch_events (created_at)
	WHERE delivered_at IS NULL;
`

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
		"SIT-open": func() IncidentState {
			s := stateFixture("medium", nil, 1)
			s.Road = "A-7"
			s.PK = "102.3-108"
			s.Type = "roadworks"
			return s
		}(),
		"SIT-ended": stateFixture("high", &end, 0),
		"SIT-cleared": {
			Severity:     "low",
			Lanes:        "",
			Road:         "N-340",
			PK:           "102.3",
			Type:         "accident",
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
		if gotState.Road != wantState.Road {
			t.Errorf("%s road = %q, want %q", id, gotState.Road, wantState.Road)
		}
		if gotState.PK != wantState.PK {
			t.Errorf("%s pk = %q, want %q", id, gotState.PK, wantState.PK)
		}
		if gotState.Type != wantState.Type {
			t.Errorf("%s type = %q, want %q", id, gotState.Type, wantState.Type)
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

// TestOpenMigratesV1Database hand-crafts a database exactly as the schema-1
// binary wrote it (no road/pk/type columns, user_version = 1) and checks that
// Open migrates it in place: the version is bumped, the pre-existing row
// survives with empty description fields, and a subsequent ReplaceState +
// PreviousState round-trips the new fields.
func TestOpenMigratesV1Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trafico.db")

	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(v1SchemaSQL); err != nil {
		t.Fatalf("create v1 schema: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO watch_state
		(watch_name, incident_id, severity, lanes, end_time, last_seen_at, missing_count)
		VALUES ('trabajo', 'dgt:SIT-old', 'high', '1 de 3', NULL,
			'2026-10-28T09:00:00.000000000Z', 1)`); err != nil {
		t.Fatalf("seed v1 row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close v1 database: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open on a v1 database: %v", err)
	}
	defer s.Close()

	if got := queryUserVersion(t, s); got != schemaVersion {
		t.Errorf("user_version after migration = %d, want %d", got, schemaVersion)
	}

	// The pre-existing row survives the migration, its old fields intact and
	// its new description fields empty (the old binary could not know them).
	prev, err := s.PreviousState("trabajo")
	if err != nil {
		t.Fatalf("PreviousState after migration: %v", err)
	}
	old, ok := prev["dgt:SIT-old"]
	if !ok {
		t.Fatalf("pre-existing row lost in the migration: %+v", prev)
	}
	if old.Severity != "high" || old.Lanes != "1 de 3" || old.MissingCount != 1 {
		t.Errorf("migrated row changed: %+v", old)
	}
	if old.Road != "" || old.PK != "" || old.Type != "" {
		t.Errorf("migrated row invented description fields: %+v", old)
	}

	// New writes round-trip the description fields through the migrated
	// schema.
	lastSeen := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	want := IncidentState{
		Severity:   "medium",
		Lanes:      "lane1",
		Road:       "A-7",
		PK:         "102.3-108",
		Type:       "roadworks",
		LastSeenAt: lastSeen,
	}
	if err := s.ReplaceState("trabajo", map[string]IncidentState{"dgt:SIT-new": want}); err != nil {
		t.Fatalf("ReplaceState after migration: %v", err)
	}
	prev, err = s.PreviousState("trabajo")
	if err != nil {
		t.Fatalf("PreviousState of the new row: %v", err)
	}
	got, ok := prev["dgt:SIT-new"]
	if !ok {
		t.Fatalf("new row missing after replace: %+v", prev)
	}
	if got != want {
		t.Errorf("round trip = %+v, want %+v", got, want)
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

// TestConcurrentMigrationsDoNotCollide covers the migration guard: several
// processes opening the same old database at the same time must not make the
// losers fail with a duplicate-column error. The version must be re-checked
// inside the migration transaction, so a process that already applied the
// step makes the others skip it. The children re-execute this test binary
// with TRAFICO_MIGRATION_RACE_CHILD=1; a start barrier releases them all at
// once so they read the old schema version together.
func TestConcurrentMigrationsDoNotCollide(t *testing.T) {
	if os.Getenv("TRAFICO_MIGRATION_RACE_CHILD") != "" {
		runMigrationRaceChild()
		return
	}
	path := filepath.Join(t.TempDir(), "trafico.db")

	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(v1SchemaSQL); err != nil {
		t.Fatalf("create v1 schema: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close v1 database: %v", err)
	}

	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	const children = 4
	cmds := make([]*exec.Cmd, children)
	for i := range cmds {
		ready := filepath.Join(dir, fmt.Sprintf("ready-%d", i))
		result := filepath.Join(dir, fmt.Sprintf("result-%d", i))
		cmds[i] = exec.Command(executable, "-test.run", "^"+t.Name()+"$")
		cmds[i].Env = append(os.Environ(),
			"TRAFICO_MIGRATION_RACE_CHILD=1",
			"TRAFICO_MIGRATION_RACE_DB="+path,
			"TRAFICO_MIGRATION_RACE_READY="+ready,
			"TRAFICO_MIGRATION_RACE_RESULT="+result,
		)
		if err := cmds[i].Start(); err != nil {
			t.Fatalf("start child %d: %v", i, err)
		}
	}
	// Barrier: every child signals ready, then the parent opens the gate so
	// all of them read the old schema version at the same time.
	waitForFiles(t, dir, "ready-", children)
	if err := os.WriteFile(filepath.Join(dir, "start"), nil, 0o600); err != nil {
		t.Fatalf("write start barrier: %v", err)
	}
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Errorf("child %d: %v", i, err)
		}
	}

	// No child may have failed to open the database.
	for i := range cmds {
		out, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("result-%d", i)))
		if err != nil {
			t.Fatalf("child %d wrote no result: %v", i, err)
		}
		if strings.HasPrefix(string(out), "error") {
			t.Errorf("child %d failed to open the shared database: %s", i, out)
		}
	}

	// The database must come out intact at the current schema version.
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open after the race: %v", err)
	}
	defer s.Close()
	if got := queryUserVersion(t, s); got != schemaVersion {
		t.Errorf("user_version after the race = %d, want %d", got, schemaVersion)
	}
}

// runMigrationRaceChild is the body of TestConcurrentMigrationsDoNotCollide
// in a child process: signal ready, wait for the start barrier, open the
// shared database like a second process, and report the outcome through the
// result file the parent reads.
func runMigrationRaceChild() {
	ready := os.Getenv("TRAFICO_MIGRATION_RACE_READY")
	result := os.Getenv("TRAFICO_MIGRATION_RACE_RESULT")
	dbPath := os.Getenv("TRAFICO_MIGRATION_RACE_DB")
	fail := func(err error) {
		os.WriteFile(result, []byte("error: "+err.Error()), 0o600)
	}
	if err := os.WriteFile(ready, nil, 0o600); err != nil {
		fail(err)
		return
	}
	start := filepath.Join(filepath.Dir(ready), "start")
	deadline := time.Now().Add(60 * time.Second)
	for {
		if _, err := os.Stat(start); err == nil {
			break
		}
		if time.Now().After(deadline) {
			fail(fmt.Errorf("start barrier timed out"))
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	s, err := Open(dbPath)
	if err != nil {
		fail(err)
		return
	}
	if err := s.Close(); err != nil {
		fail(err)
		return
	}
	os.WriteFile(result, []byte("ok"), 0o600)
}

// waitForFiles waits until count files named prefix<i> exist in dir.
func waitForFiles(t *testing.T, dir, prefix string, count int) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		ready := 0
		for i := 0; i < count; i++ {
			if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("%s%d", prefix, i))); err == nil {
				ready++
			}
		}
		if ready == count {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d %s files in %s", count, prefix, dir)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// v2SchemaSQL is the schema of store version 2: the version-1 base schema
// plus the three describing columns the version-2 step added, exactly as
// the previous binary left the database after migrating.
const v2SchemaSQL = v1SchemaSQL + `
ALTER TABLE watch_state ADD COLUMN road TEXT NOT NULL DEFAULT '';
ALTER TABLE watch_state ADD COLUMN pk TEXT NOT NULL DEFAULT '';
ALTER TABLE watch_state ADD COLUMN type TEXT NOT NULL DEFAULT '';
`

// ptr returns a pointer to v, a helper for the nullable PK bounds.
func ptr(v float64) *float64 { return &v }

// sameWatch compares two Watch values field by field: the struct carries
// slices, so it is not comparable with ==.
func sameWatch(a, b Watch) bool { return reflect.DeepEqual(a, b) }

// watchFixture builds a deterministic Watch for tests.
func watchFixture(name, kind string) Watch {
	created := time.Date(2026, 10, 29, 8, 30, 0, 0, time.UTC)
	return Watch{
		Name:         name,
		Kind:         kind,
		Windows:      []string{"lun-vie 07:00-09:00", "lun-vie 17:30-19:30"},
		MinSeverity:  "medium",
		Sinks:        []string{"phone"},
		IntervalSecs: 300,
		CreatedAt:    created,
	}
}

// TestWatchRoundTripAllKinds covers the three target shapes: every column
// of the watches table survives a save/read cycle unchanged, including the
// nullable PK bounds of the road kind and both centre variants of the near
// kind (a saved place name and raw coordinates).
func TestWatchRoundTripAllKinds(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	zero := 0.0
	want := []Watch{
		func() Watch {
			w := watchFixture("ruta", WatchKindRoute)
			w.Target = Target{Route: "trabajo"}
			return w
		}(),
		func() Watch {
			w := watchFixture("carretera", WatchKindRoad)
			w.Target = Target{Road: "AP-7", PKFrom: ptr(600.5), PKTo: ptr(700.0)}
			return w
		}(),
		func() Watch {
			w := watchFixture("carretera-abierta", WatchKindRoad)
			w.Target = Target{Road: "A-7", PKFrom: ptr(102.3)}
			return w
		}(),
		func() Watch {
			w := watchFixture("lugar", WatchKindNear)
			w.Target = Target{Place: "casa", RadiusKm: 15}
			return w
		}(),
		func() Watch {
			w := watchFixture("coordenadas", WatchKindNear)
			w.Target = Target{Lat: ptr(38.3452), Lon: &zero, RadiusKm: 10}
			return w
		}(),
	}
	for _, w := range want {
		if err := s.SaveWatch(w); err != nil {
			t.Fatalf("SaveWatch(%s): %v", w.Name, err)
		}
	}

	for _, wantWatch := range want {
		got, err := s.Watch(wantWatch.Name)
		if err != nil {
			t.Fatalf("Watch(%s): %v", wantWatch.Name, err)
		}
		if !sameWatch(got, wantWatch) {
			t.Errorf("Watch(%s) = %+v, want %+v", wantWatch.Name, got, wantWatch)
		}
	}
}

// TestSaveWatchUpsertReplaces checks that saving an existing name replaces
// every column instead of failing on the primary key.
func TestSaveWatchUpsertReplaces(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	first := watchFixture("trabajo", WatchKindRoute)
	first.Target = Target{Route: "casa"}
	if err := s.SaveWatch(first); err != nil {
		t.Fatalf("first SaveWatch: %v", err)
	}

	second := watchFixture("trabajo", WatchKindRoad)
	second.Target = Target{Road: "AP-7"}
	second.MinSeverity = ""
	second.Sinks = []string{"casa", "oficina"}
	second.IntervalSecs = 600
	if err := s.SaveWatch(second); err != nil {
		t.Fatalf("second SaveWatch: %v", err)
	}

	got, err := s.Watch("trabajo")
	if err != nil {
		t.Fatalf("Watch after upsert: %v", err)
	}
	if !sameWatch(got, second) {
		t.Errorf("Watch after upsert = %+v, want the replaced %+v", got, second)
	}
	all, err := s.Watches()
	if err != nil {
		t.Fatalf("Watches after upsert: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("Watches after upsert has %d rows, want 1", len(all))
	}
}

// TestWatchesOrderedByName checks the name ordering of the full listing.
func TestWatchesOrderedByName(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	for _, name := range []string{"casa", "ap7", "trabajo"} {
		w := watchFixture(name, WatchKindRoute)
		w.Target = Target{Route: name}
		if err := s.SaveWatch(w); err != nil {
			t.Fatalf("SaveWatch(%s): %v", name, err)
		}
	}

	all, err := s.Watches()
	if err != nil {
		t.Fatalf("Watches: %v", err)
	}
	want := []string{"ap7", "casa", "trabajo"}
	if len(all) != len(want) {
		t.Fatalf("Watches returned %d rows, want %d", len(all), len(want))
	}
	for i, name := range want {
		if all[i].Name != name {
			t.Errorf("Watches[%d] = %q, want %q", i, all[i].Name, name)
		}
	}
}

// TestWatchUnknownIsNotFound pins the not_found error (exit 4) of §6.3 for
// a watch name that was never saved.
func TestWatchUnknownIsNotFound(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	_, err := s.Watch("inexistente")
	if err == nil {
		t.Fatal("Watch on an unknown name succeeded, want not_found")
	}
	var errsErr *errs.Error
	if !errors.As(err, &errsErr) || errsErr.Type != "not_found" {
		t.Fatalf("Watch error = %v, want a not_found errs.Error", err)
	}
}

// TestDeleteWatchRemovesAndReportsNotFound covers the delete path: an
// existing watch disappears, and deleting an absent name (never saved or
// already deleted) is a not_found error, not a silent success.
func TestDeleteWatchRemovesAndReportsNotFound(t *testing.T) {
	s := openTestStore(t)
	defer s.Close()

	w := watchFixture("trabajo", WatchKindRoute)
	w.Target = Target{Route: "casa"}
	if err := s.SaveWatch(w); err != nil {
		t.Fatalf("SaveWatch: %v", err)
	}
	if err := s.DeleteWatch("trabajo"); err != nil {
		t.Fatalf("DeleteWatch: %v", err)
	}
	if _, err := s.Watch("trabajo"); err == nil {
		t.Error("Watch after delete succeeded, want not_found")
	}
	if err := s.DeleteWatch("trabajo"); err == nil {
		t.Error("DeleteWatch of a deleted name succeeded, want not_found")
	}
	if err := s.DeleteWatch("nunca-guardada"); err == nil {
		t.Error("DeleteWatch of an unknown name succeeded, want not_found")
	}
}

// TestOpenMigratesV2Database hand-crafts a database exactly as the
// schema-2 binary wrote it (watch_state with the road/pk/type columns, no
// watches table, user_version = 2) and checks that Open migrates it in
// place: the version is bumped to 3, the pre-existing state and events
// survive untouched, and the new watches table accepts writes.
func TestOpenMigratesV2Database(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trafico.db")

	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(v2SchemaSQL); err != nil {
		t.Fatalf("create v2 schema: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO watch_state
		(watch_name, incident_id, severity, lanes, road, pk, type, end_time, last_seen_at, missing_count)
		VALUES ('trabajo', 'dgt:SIT-old', 'high', '1 de 3', 'A-7', '102.3-108', 'roadworks', NULL,
			'2026-10-28T09:00:00.000000000Z', 1)`); err != nil {
		t.Fatalf("seed v2 state row: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO watch_events
		(watch_name, incident_id, kind, payload, created_at, delivered_at)
		VALUES ('trabajo', 'dgt:SIT-old', 'incident.new', '{"id":"dgt:SIT-old"}',
			'2026-10-28T09:00:00.000000000Z', NULL)`); err != nil {
		t.Fatalf("seed v2 event row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close v2 database: %v", err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open on a v2 database: %v", err)
	}
	defer s.Close()

	if got := queryUserVersion(t, s); got != schemaVersion {
		t.Errorf("user_version after migration = %d, want %d", got, schemaVersion)
	}

	// The pre-existing state survives the migration untouched.
	states, err := s.PreviousState("trabajo")
	if err != nil {
		t.Fatalf("PreviousState after migration: %v", err)
	}
	state, ok := states["dgt:SIT-old"]
	if !ok {
		t.Fatalf("pre-existing state lost in the migration: %+v", states)
	}
	if state.Severity != "high" || state.Road != "A-7" || state.PK != "102.3-108" || state.Type != "roadworks" || state.MissingCount != 1 {
		t.Errorf("migrated state changed: %+v", state)
	}

	// The pre-existing pending event survives too.
	events, err := s.UndeliveredEvents(10)
	if err != nil {
		t.Fatalf("UndeliveredEvents after migration: %v", err)
	}
	if len(events) != 1 || events[0].IncidentID != "dgt:SIT-old" {
		t.Errorf("events after migration = %+v, want the pre-existing one", events)
	}

	// The new watches table accepts writes and reads after the migration.
	w := watchFixture("nueva", WatchKindRoute)
	w.Target = Target{Route: "casa"}
	if err := s.SaveWatch(w); err != nil {
		t.Fatalf("SaveWatch after migration: %v", err)
	}
	got, err := s.Watch("nueva")
	if err != nil {
		t.Fatalf("Watch after migration: %v", err)
	}
	if !sameWatch(got, w) {
		t.Errorf("Watch after migration = %+v, want %+v", got, w)
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
