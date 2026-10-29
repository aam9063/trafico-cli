// Package store persists watch state and watch events in a local SQLite
// database (docs/ARRANQUE.md §7: $XDG_DATA_HOME/trafico-cli/trafico.db,
// mode 0600). Only internal/store touches the database; the watch layer
// (internal/watch) reads and writes through this API.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	// Pure-Go SQLite driver: no CGO (ARRANQUE §8.1, CGO_ENABLED=0 must build).
	_ "modernc.org/sqlite"
)

// schemaVersion is the current version of the database schema, recorded in
// PRAGMA user_version. Bump it and add a migration step in applyMigrations
// whenever the schema changes; a database written by a NEWER binary must be
// refused rather than silently misread.
const schemaVersion = 1

// Event kinds of the append-only watch event log (docs/ARRANQUE.md §5.5).
const (
	EventIncidentNew     = "incident.new"
	EventIncidentUpdated = "incident.updated"
	EventIncidentCleared = "incident.cleared"
)

// IncidentState is what the watch layer remembers about one incident of a
// watch between two checks, so it can detect incident.new, incident.updated
// (severity, lanes or end time changed) and incident.cleared (absent in two
// consecutive checks, via MissingCount).
type IncidentState struct {
	// Severity is the incident severity as published ("low", "medium",
	// "high" or whatever the feed carried).
	Severity string
	// Lanes is a stable serialisation of the lane list; empty when the
	// feed carries none.
	Lanes string
	// EndTime is the announced end of the incident; nil when the feed
	// carries none (P-04: absent, not invented).
	EndTime *time.Time
	// LastSeenAt is when the incident was last present in a check.
	LastSeenAt time.Time
	// MissingCount is how many consecutive checks the incident has been
	// absent; incident.cleared fires after two.
	MissingCount int
}

// Event is one entry of the append-only watch event log. Payload carries the
// JSON the notification will deliver; DeliveredAt stays null until a sink
// accepts it, so a failed delivery can be retried.
type Event struct {
	// ID is the row id, assigned by the database on append (0 before).
	ID int64
	// WatchName is the watch that produced the event. It is storage
	// metadata: notifications never carry the watch name (P-07).
	WatchName string
	// IncidentID is the DGT incident identifier (e.g. "dgt:SIT-123").
	IncidentID string
	// Kind is one of the Event* constants.
	Kind string
	// Payload is the JSON body the notification will carry.
	Payload string
	// CreatedAt is when the watcher detected the change.
	CreatedAt time.Time
	// DeliveredAt is when a sink accepted the notification; nil while it
	// is still pending.
	DeliveredAt *time.Time
}

// Store is a handle to the local SQLite database. It is safe for concurrent
// use (database/sql serialises access) and several handles on the same file
// are legal: `watch run` and `watch check` may overlap, so Open sets a busy
// timeout instead of failing on a locked database.
type Store struct {
	db *sql.DB
}

// schemaSQL is the schema of version 1, written once so the migration path
// stays obvious: applyMigrations applies it when user_version is 0 and then
// records schemaVersion. Every later version adds its own numbered step.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS watch_state (
	watch_name    TEXT    NOT NULL,
	incident_id   TEXT    NOT NULL,
	severity      TEXT    NOT NULL DEFAULT '',
	lanes         TEXT    NOT NULL DEFAULT '',
	end_time      TEXT,             -- RFC 3339 UTC, NULL when the feed carries none
	last_seen_at  TEXT    NOT NULL, -- RFC 3339 UTC
	missing_count INTEGER NOT NULL DEFAULT 0 CHECK (missing_count >= 0),
	PRIMARY KEY (watch_name, incident_id)
);

CREATE TABLE IF NOT EXISTS watch_events (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	watch_name   TEXT NOT NULL,
	incident_id  TEXT NOT NULL,
	kind         TEXT NOT NULL,
	payload      TEXT NOT NULL,
	created_at   TEXT NOT NULL, -- RFC 3339 UTC
	delivered_at TEXT           -- RFC 3339 UTC, NULL while delivery is pending
);

CREATE INDEX IF NOT EXISTS idx_watch_events_pending
	ON watch_events (created_at)
	WHERE delivered_at IS NULL;
`

// Open opens (and creates when missing) the SQLite database at path. The
// file is created with mode 0600 and its directory with 0700, because the
// database holds personal locations (ARRANQUE §7, P-07). Open applies the
// schema when the database is empty and refuses a database written by a
// newer schema version.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("store: la ruta de la base de datos está vacía")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("store: no se pudo crear el directorio %s: %w", dir, err)
	}
	// The driver would create the file with its default mode; create it
	// ourselves first so it is born 0600 (ARRANQUE §7).
	if err := touchPrivateFile(path); err != nil {
		return nil, err
	}
	// touchPrivateFile and MkdirAll only apply the modes at creation, so a
	// database restored from a backup or created by another tool could keep
	// a wider mode; §7 requires 0600/0700 on every open.
	if err := tightenPermissions(path, dir); err != nil {
		return nil, err
	}

	// busy_timeout lets two overlapping handles (`watch run` and a `watch
	// check`) wait for each other's write lock instead of failing.
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("store: no se pudo abrir %s: %w", path, err)
	}
	s := &Store{db: db}
	if err := s.applyMigrations(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// applyMigrations brings the database up to schemaVersion. Version 0 means
// an empty database: apply the version-1 schema. A version greater than the
// compiled-in one means an older binary is reading a newer database, which
// must not be silently misread.
func (s *Store) applyMigrations() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("store: no se pudo leer la versión del esquema: %w", err)
	}
	switch {
	case version > schemaVersion:
		return fmt.Errorf("store: la base de datos usa el esquema %d, más nuevo que el soportado (%d)", version, schemaVersion)
	case version == schemaVersion:
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: no se pudo iniciar la migración: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schemaSQL); err != nil {
		return fmt.Errorf("store: no se pudo aplicar el esquema: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("store: no se pudo registrar la versión del esquema: %w", err)
	}
	return tx.Commit()
}

// PreviousState returns the stored snapshot of one watch: one entry per
// incident still tracked. A watch with no history yields an empty map.
func (s *Store) PreviousState(watch string) (map[string]IncidentState, error) {
	rows, err := s.db.Query(`SELECT incident_id, severity, lanes, end_time, last_seen_at, missing_count
		FROM watch_state WHERE watch_name = ?`, watch)
	if err != nil {
		return nil, fmt.Errorf("store: no se pudo leer el estado de la vigilancia %q: %w", watch, err)
	}
	defer rows.Close()

	states := make(map[string]IncidentState)
	for rows.Next() {
		var (
			id         string
			state      IncidentState
			endTime    sql.NullString
			lastSeenAt string
		)
		if err := rows.Scan(&id, &state.Severity, &state.Lanes, &endTime, &lastSeenAt, &state.MissingCount); err != nil {
			return nil, fmt.Errorf("store: fila de estado ilegible en %q: %w", watch, err)
		}
		lastSeen, err := parseStoredTime(lastSeenAt)
		if err != nil {
			return nil, fmt.Errorf("store: last_seen_at ilegible en %q: %w", watch, err)
		}
		state.LastSeenAt = lastSeen
		if endTime.Valid {
			parsed, err := parseStoredTime(endTime.String)
			if err != nil {
				return nil, fmt.Errorf("store: end_time ilegible en %q: %w", watch, err)
			}
			state.EndTime = &parsed
		}
		states[id] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: no se pudo leer el estado de la vigilancia %q: %w", watch, err)
	}
	return states, nil
}

// ReplaceState atomically replaces the stored snapshot of one watch with the
// given states: everything happens inside one transaction, so a crash can
// never leave half a snapshot that would turn every incident into a spurious
// incident.new on the next run.
func (s *Store) ReplaceState(watch string, states map[string]IncidentState) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: no se pudo iniciar el reemplazo de estado: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM watch_state WHERE watch_name = ?", watch); err != nil {
		return fmt.Errorf("store: no se pudo vaciar el estado de la vigilancia %q: %w", watch, err)
	}
	insert, err := tx.Prepare(`INSERT INTO watch_state
		(watch_name, incident_id, severity, lanes, end_time, last_seen_at, missing_count)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("store: no se pudo preparar el reemplazo de estado: %w", err)
	}
	defer insert.Close()

	for id, state := range states {
		var endTime any
		if state.EndTime != nil {
			endTime = storeTime(*state.EndTime)
		}
		if _, err := insert.Exec(watch, id, state.Severity, state.Lanes, endTime,
			storeTime(state.LastSeenAt), state.MissingCount); err != nil {
			return fmt.Errorf("store: no se pudo guardar el estado de %q en la vigilancia %q: %w", id, watch, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: no se pudo confirmar el reemplazo de estado: %w", err)
	}
	return nil
}

// AppendEvents appends events to the log in one transaction. CreatedAt and
// the pending DeliveredAt (null) are stored as given; ID is filled in.
func (s *Store) AppendEvents(events []Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: no se pudo iniciar la escritura de eventos: %w", err)
	}
	defer tx.Rollback()

	insert, err := tx.Prepare(`INSERT INTO watch_events
		(watch_name, incident_id, kind, payload, created_at, delivered_at)
		VALUES (?, ?, ?, ?, ?, NULL)`)
	if err != nil {
		return fmt.Errorf("store: no se pudo preparar la escritura de eventos: %w", err)
	}
	defer insert.Close()

	for i := range events {
		result, err := insert.Exec(events[i].WatchName, events[i].IncidentID, events[i].Kind,
			events[i].Payload, storeTime(events[i].CreatedAt))
		if err != nil {
			return fmt.Errorf("store: no se pudo guardar el evento %q de %q: %w", events[i].Kind, events[i].IncidentID, err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("store: no se pudo recuperar el id del evento: %w", err)
		}
		events[i].ID = id
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: no se pudo confirmar la escritura de eventos: %w", err)
	}
	return nil
}

// UndeliveredEvents returns up to limit events whose delivery is still
// pending, oldest first, so retries drain in order. limit <= 0 means no limit.
func (s *Store) UndeliveredEvents(limit int) ([]Event, error) {
	query := `SELECT id, watch_name, incident_id, kind, payload, created_at, delivered_at
		FROM watch_events WHERE delivered_at IS NULL ORDER BY created_at ASC, id ASC`
	args := []any{}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: no se pudo leer los eventos pendientes: %w", err)
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var (
			event       Event
			createdAt   string
			deliveredAt sql.NullString
		)
		if err := rows.Scan(&event.ID, &event.WatchName, &event.IncidentID, &event.Kind,
			&event.Payload, &createdAt, &deliveredAt); err != nil {
			return nil, fmt.Errorf("store: evento pendiente ilegible: %w", err)
		}
		if event.CreatedAt, err = parseStoredTime(createdAt); err != nil {
			return nil, fmt.Errorf("store: created_at ilegible: %w", err)
		}
		if deliveredAt.Valid {
			parsed, err := parseStoredTime(deliveredAt.String)
			if err != nil {
				return nil, fmt.Errorf("store: delivered_at ilegible: %w", err)
			}
			event.DeliveredAt = &parsed
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: no se pudo leer los eventos pendientes: %w", err)
	}
	return events, nil
}

// MarkDelivered stamps the given event ids as delivered now, keeping the
// rows: the log is append-only and pruning is PruneEvents' job.
func (s *Store) MarkDelivered(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: no se pudo iniciar la marca de entrega: %w", err)
	}
	defer tx.Rollback()

	now := storeTime(time.Now())
	for _, id := range ids {
		result, err := tx.Exec("UPDATE watch_events SET delivered_at = ? WHERE id = ?", now, id)
		if err != nil {
			return fmt.Errorf("store: no se pudo marcar el evento %d como entregado: %w", id, err)
		}
		if affected, err := result.RowsAffected(); err != nil {
			return fmt.Errorf("store: no se pudo verificar el evento %d: %w", id, err)
		} else if affected == 0 {
			return fmt.Errorf("store: el evento %d no existe", id)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: no se pudo confirmar la marca de entrega: %w", err)
	}
	return nil
}

// PruneEvents deletes events created before the cutoff whose delivery is
// over (delivered_at set) and returns how many rows it removed, so the log
// cannot grow forever. Undelivered events are never pruned: a pending event
// older than the cutoff is a notification the user never received, and
// deleting it would lose it silently; it stays queued for the retry loop.
func (s *Store) PruneEvents(olderThan time.Time) (int, error) {
	result, err := s.db.Exec("DELETE FROM watch_events WHERE created_at < ? AND delivered_at IS NOT NULL", storeTime(olderThan))
	if err != nil {
		return 0, fmt.Errorf("store: no se pudo podar el registro de eventos: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: no se pudo contar la poda: %w", err)
	}
	return int(removed), nil
}

// touchPrivateFile creates path with mode 0600 when it does not exist; an
// existing file is left untouched, mode included.
func touchPrivateFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("store: no se pudo crear %s: %w", path, err)
	}
	return file.Close()
}

// tightenPermissions enforces the §7 modes on an existing database file and
// its directory, because touchPrivateFile and MkdirAll only apply the modes
// at creation: a file restored from a backup, or created by another tool,
// must not keep a wider mode than 0600/0700. Windows has no POSIX
// permission bits, so there this is a no-op.
func tightenPermissions(path, dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if info, err := os.Stat(path); err != nil {
		return fmt.Errorf("store: no se pudo examinar %s: %w", path, err)
	} else if info.Mode().Perm() != 0o600 {
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("store: no se pudo ajustar el permiso de %s a 0600: %w", path, err)
		}
	}
	if info, err := os.Stat(dir); err != nil {
		return fmt.Errorf("store: no se pudo examinar %s: %w", dir, err)
	} else if info.Mode().Perm() != 0o700 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("store: no se pudo ajustar el permiso de %s a 0700: %w", dir, err)
		}
	}
	return nil
}

// storedTimeFormat is RFC 3339 with a fixed nine-digit fractional part, the
// single storage format of every timestamp column. The width is fixed
// because timestamps are compared and ordered as TEXT: RFC 3339Nano drops
// trailing zeros, so a whole second ("...:00Z") would sort after the same
// second with a fraction ("...:00.5Z"), breaking the chronological order of
// UndeliveredEvents (the retry order) and the PruneEvents cutoff comparison.
const storedTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// storeTime serialises a time in the fixed-width storedTimeFormat, in UTC.
func storeTime(t time.Time) string {
	return t.UTC().Format(storedTimeFormat)
}

// parseStoredTime reads back what storeTime wrote.
func parseStoredTime(value string) (time.Time, error) {
	t, err := time.Parse(storedTimeFormat, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("fecha %q ilegible: %w", value, err)
	}
	return t, nil
}
