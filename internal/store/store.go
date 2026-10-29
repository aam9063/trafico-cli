// Package store persists watch state and watch events in a local SQLite
// database (docs/ARRANQUE.md §7: $XDG_DATA_HOME/trafico-cli/trafico.db,
// mode 0600). Only internal/store touches the database; the watch layer
// (internal/watch) reads and writes through this API.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/errs"

	// Pure-Go SQLite driver: no CGO (ARRANQUE §8.1, CGO_ENABLED=0 must build).
	_ "modernc.org/sqlite"
)

// schemaVersion is the current version of the database schema, recorded in
// PRAGMA user_version. Bump it and add a migration step in applyMigrations
// whenever the schema changes; a database written by a NEWER binary must be
// refused rather than silently misread.
const schemaVersion = 4

// migrationSteps, indexed by the version they migrate FROM, bring a database
// one version forward. Step 0 creates the base (version-1) schema; every
// later step is an ALTER-style migration. A database written by a NEWER
// binary is refused before any step runs.
var migrationSteps = map[int]func(*sql.Tx) error{
	0: func(tx *sql.Tx) error {
		if _, err := tx.Exec(schemaSQL); err != nil {
			return fmt.Errorf("no se pudo aplicar el esquema: %w", err)
		}
		return nil
	},
	// Version 1 → 2: the describing fields of an incident (road, kilometre
	// point, type) join the state, so an incident.cleared notification can
	// say what ended. Existing rows keep empty strings, exactly what the old
	// binary knew about them.
	1: func(tx *sql.Tx) error {
		for _, column := range []string{"road", "pk", "type"} {
			if _, err := tx.Exec("ALTER TABLE watch_state ADD COLUMN " + column + " TEXT NOT NULL DEFAULT ''"); err != nil {
				return fmt.Errorf("no se pudo añadir la columna %s: %w", column, err)
			}
		}
		return nil
	},
	// Version 2 → 3: the watches table. Watch definitions move out of the
	// config file into the database (docs/ARRANQUE.md §7), so `watch add`
	// works without rewriting the 0600 config and `watch run` reads the
	// definitions through the same store as the state and the events. The
	// table starts empty: versions 1-2 carried no watch definitions, so
	// there is nothing to carry over.
	2: func(tx *sql.Tx) error {
		if _, err := tx.Exec(watchesTableSQL); err != nil {
			return fmt.Errorf("no se pudo crear la tabla watches: %w", err)
		}
		return nil
	},
	// Version 3 → 4: the last_check_at column of the watches table. The run
	// cycle (S6) must enforce the polling interval itself — "safe to run
	// from a scheduler every minute" only holds if the cycle remembers when
	// each watch was last really checked. Existing rows keep NULL (they have
	// never run under this rule, so they are due).
	3: func(tx *sql.Tx) error {
		if _, err := tx.Exec("ALTER TABLE watches ADD COLUMN last_check_at TEXT"); err != nil {
			return fmt.Errorf("no se pudo añadir la columna last_check_at: %w", err)
		}
		return nil
	},
}

// Event kinds of the append-only watch event log and target kinds of the
// watches table (docs/ARRANQUE.md §5.5 and §7).
const (
	EventIncidentNew     = "incident.new"
	EventIncidentUpdated = "incident.updated"
	EventIncidentCleared = "incident.cleared"

	// Watch kinds, the target shapes a watch can take. The kind column
	// selects which Target fields are meaningful.
	WatchKindRoute = "route" // a saved route by name
	WatchKindRoad  = "road"  // one road with an optional PK range
	WatchKindNear  = "near"  // a radius around a place or coordinates
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
	// Road is the road the incident affects ("A-7"), remembered so an
	// incident.cleared notification can still say what ended.
	Road string
	// PK is the kilometre-point interval as one stored string that
	// round-trips the model's {from,to} shape: "102.3-108" with both
	// bounds, the open end alone with one, and empty when the incident
	// has no PK (P-04). One column, one representation.
	PK string
	// Type is the normalised incident type ("roadworks", "accident",
	// ...), remembered for the same reason as Road.
	Type string
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
	// DeliveredAt is when a sink accepted the notification (or, for an
	// event that can never be delivered, when it was stamped out of the
	// retry queue by MarkSkipped); nil while it is still pending.
	DeliveredAt *time.Time
}

// Store is a handle to the local SQLite database. It is safe for concurrent
// use (database/sql serialises access) and several handles on the same file
// are legal: `watch run` and `watch check` may overlap, so Open sets a busy
// timeout instead of failing on a locked database.
type Store struct {
	db *sql.DB
}

// schemaSQL is the base (version-1) schema, written once so the migration
// path stays obvious: applyMigrations applies it when user_version is 0 and
// then runs every numbered step up to schemaVersion. Every later version
// adds its own step in migrationSteps.
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

// watchesTableSQL is the version-3 addition: the watch definitions, one row
// per watch (docs/ARRANQUE.md §7: the database holds the vigilancias, not
// the config file). name is the primary key, so upserting a watch replaces
// its definition; kind selects which Target fields are meaningful; windows
// and sinks are JSON arrays of strings; target is the JSON serialisation of
// Target. last_check_at is added by migration step 3 (both on a fresh
// install, which runs every step, and on an upgrade), and is the runtime
// state of the interval gate (S6), written only by MarkWatchChecked — an
// upsert of the definition never resets it. The definition layer
// (internal/app) validates every field before SaveWatch sees it; the store
// only enforces its storage invariants.
const watchesTableSQL = `
CREATE TABLE IF NOT EXISTS watches (
	name          TEXT PRIMARY KEY,
	kind          TEXT NOT NULL,   -- route | road | near
	target        TEXT NOT NULL,   -- JSON, shape depends on kind
	windows       TEXT NOT NULL,   -- JSON array of window spec strings
	min_severity  TEXT NOT NULL DEFAULT '',
	sinks         TEXT NOT NULL,   -- JSON array of sink names from the config
	interval_secs INTEGER NOT NULL,
	created_at    TEXT NOT NULL
);
`

// Target is the stored target of a watch: what the watch looks at. It is a
// tagged union; the watch's kind column (WatchKind*) says which fields are
// meaningful, and the JSON stored in the target column is exactly this
// shape:
//
//	route: {"route":"trabajo"}
//	road:  {"road":"AP-7","pk_from":600,"pk_to":700}   (PK bounds optional)
//	near:  {"place":"casa","radius_km":15}
//	near:  {"lat":38.3452,"lon":-0.481,"radius_km":15}
//
// The store never interprets the fields; the definition layer
// (internal/app) owns validation and translation into filter criteria.
type Target struct {
	// Route is the saved route name (kind "route").
	Route string `json:"route,omitempty"`
	// Road is the road name (kind "road"), already normalised by the
	// definition layer.
	Road string `json:"road,omitempty"`
	// PKFrom and PKTo are the optional kilometre bounds (kind "road"); a
	// nil bound is an open end, exactly the filter.Range convention.
	PKFrom *float64 `json:"pk_from,omitempty"`
	PKTo   *float64 `json:"pk_to,omitempty"`
	// Place is the saved place name centring the radius (kind "near").
	// Mutually exclusive with Lat/Lon by construction.
	Place string `json:"place,omitempty"`
	// Lat and Lon are the radius centre when no place name is used
	// (kind "near"). Pointers, so a legitimate 0.0 coordinate never reads
	// as "absent".
	Lat *float64 `json:"lat,omitempty"`
	Lon *float64 `json:"lon,omitempty"`
	// RadiusKm is the radius of the near watch in kilometres (kind "near").
	RadiusKm float64 `json:"radius_km,omitempty"`
}

// Watch is one saved watch definition, mirroring the watches columns. The
// JSON columns are typed here (Target, window specs, sink names) so callers
// never handle the raw storage shapes.
type Watch struct {
	// Name identifies the watch; storage metadata, never sent to any sink
	// (P-07). It is the primary key: SaveWatch upserts by name.
	Name string
	// Kind is one of the WatchKind* constants.
	Kind string
	// Target is what the watch looks at; fields not meaningful for Kind
	// stay at their zero values.
	Target Target
	// Windows are the window spec strings ("lun-vie 07:00-09:00") the
	// definition layer parsed; they are stored verbatim and re-parsed on
	// read, so a stored watch round-trips through internal/window.
	Windows []string
	// MinSeverity is the severity floor ("", "low", "medium" or "high").
	MinSeverity string
	// Sinks are the notification destinations, by config sink name.
	Sinks []string
	// IntervalSecs is the polling interval in seconds.
	IntervalSecs int
	// CreatedAt is when the watch was saved.
	CreatedAt time.Time
	// LastCheckAt is when the run cycle last REALLY checked the watch (the
	// interval gate of S6); the zero time means it never ran. It is runtime
	// state written by MarkWatchChecked: saving the definition never
	// touches it.
	LastCheckAt time.Time
}

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
// an empty database: apply the base schema and then every step. A version
// greater than the compiled-in one means an older binary is reading a newer
// database, which must not be silently misread. All steps and the version
// bump run inside one transaction, so a database is never left half-migrated.
//
// Two processes can open the same old database at once. Each attempt
// re-reads the version — outside the transaction to refuse a newer database,
// inside it to skip steps another process already applied — and a short
// bounded retry absorbs the window where the other process still holds the
// write lock, so the loser skips instead of failing.
func (s *Store) applyMigrations() error {
	for attempt := 0; ; attempt++ {
		err := s.migrateOnce()
		if err == nil {
			return nil
		}
		if attempt >= migrationRetries || !isBusyError(err) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// migrationRetries is how many times a migration attempt that lost the write
// lock to another process is repeated. Each attempt re-reads the schema
// version, so once the other process commits, the next attempt skips instead
// of failing; the budget is generous next to a five-second busy timeout.
const migrationRetries = 10

// migrateOnce performs one migration attempt: refuse a newer database, skip
// when it is already current, and otherwise run every step up to
// schemaVersion inside one transaction together with the version bump.
func (s *Store) migrateOnce() error {
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
	// Re-read the version INSIDE the transaction: two processes opening the
	// same old database can both read the old version outside, and the
	// loser would otherwise apply its step twice and fail with a
	// duplicate-column error. This is the first read of the deferred
	// transaction, so it sees whatever another process already committed.
	// If the other process still holds the write lock, the ALTER below may
	// fail with SQLITE_BUSY — the caller retries the whole attempt and then
	// lands on this skip.
	var current int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("store: no se pudo leer la versión del esquema: %w", err)
	}
	if current > schemaVersion {
		return fmt.Errorf("store: la base de datos usa el esquema %d, más nuevo que el soportado (%d)", current, schemaVersion)
	}
	if current == schemaVersion {
		// Another process migrated while we were opening: nothing to do.
		return nil
	}
	for v := current; v < schemaVersion; v++ {
		step, ok := migrationSteps[v]
		if !ok {
			return fmt.Errorf("store: no hay migración del esquema %d al %d", v, v+1)
		}
		if err := step(tx); err != nil {
			return fmt.Errorf("store: migración del esquema %d al %d falló: %w", v, v+1, err)
		}
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("store: no se pudo registrar la versión del esquema: %w", err)
	}
	return tx.Commit()
}

// isBusyError reports whether the error is SQLite refusing a read or write
// because another connection holds the database (SQLITE_BUSY or
// SQLITE_LOCKED): the expected outcome when two processes race to migrate
// the same database, and the one case a migration attempt is retried.
func isBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") ||
		strings.Contains(msg, "SQLITE_LOCKED") ||
		strings.Contains(msg, "database is locked")
}

// SaveWatch inserts or replaces one watch definition, keyed by its name
// (the watches primary key): the second SaveWatch of a name is an upsert
// that overwrites every DEFINITION column, so re-adding a watch redefines
// it. last_check_at is not one of them: the recorded last check is runtime
// state of the interval gate, and redefining a watch must not let it hammer
// the DGT again before its interval has elapsed. The definition layer
// validates the fields; the store enforces only its storage invariants (a
// name, a known kind, a positive interval, targets and string lists that
// serialise).
func (s *Store) SaveWatch(w Watch) error {
	if w.Name == "" {
		return errs.NewInternal("la vigilancia no tiene nombre")
	}
	switch w.Kind {
	case WatchKindRoute, WatchKindRoad, WatchKindNear:
	default:
		return errs.NewInternal(fmt.Sprintf("la vigilancia %q tiene una clase de objetivo desconocida %q", w.Name, w.Kind))
	}
	if w.IntervalSecs <= 0 {
		return errs.NewInternal(fmt.Sprintf("la vigilancia %q tiene un intervalo no positivo (%d)", w.Name, w.IntervalSecs))
	}
	target, err := json.Marshal(w.Target)
	if err != nil {
		return fmt.Errorf("store: no se pudo serializar el objetivo de la vigilancia %q: %w", w.Name, err)
	}
	windows, err := json.Marshal(nonNilStrings(w.Windows))
	if err != nil {
		return fmt.Errorf("store: no se pudo serializar las ventanas de la vigilancia %q: %w", w.Name, err)
	}
	sinks, err := json.Marshal(nonNilStrings(w.Sinks))
	if err != nil {
		return fmt.Errorf("store: no se pudo serializar los destinos de la vigilancia %q: %w", w.Name, err)
	}
	if _, err := s.db.Exec(`INSERT INTO watches
		(name, kind, target, windows, min_severity, sinks, interval_secs, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			kind = excluded.kind,
			target = excluded.target,
			windows = excluded.windows,
			min_severity = excluded.min_severity,
			sinks = excluded.sinks,
			interval_secs = excluded.interval_secs,
			created_at = excluded.created_at`,
		w.Name, w.Kind, string(target), string(windows), w.MinSeverity, string(sinks),
		w.IntervalSecs, storeTime(w.CreatedAt)); err != nil {
		return fmt.Errorf("store: no se pudo guardar la vigilancia %q: %w", w.Name, err)
	}
	return nil
}

// MarkWatchChecked records that the run cycle really checked the watch at
// the given instant: the runtime state of the interval gate (S6). An absent
// name is a not_found error, never a silent success.
func (s *Store) MarkWatchChecked(name string, at time.Time) error {
	result, err := s.db.Exec("UPDATE watches SET last_check_at = ? WHERE name = ?", storeTime(at), name)
	if err != nil {
		return fmt.Errorf("store: no se pudo registrar la comprobación de la vigilancia %q: %w", name, err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("store: no se pudo verificar la vigilancia %q: %w", name, err)
	} else if affected == 0 {
		return errs.NewNotFound(fmt.Sprintf("no existe la vigilancia %q", name))
	}
	return nil
}

// Watch returns the stored definition of one watch. An absent name is a
// not_found error (exit 4, ARRANQUE §6.3: "vigilancia inexistente").
func (s *Store) Watch(name string) (Watch, error) {
	row := s.db.QueryRow(`SELECT kind, target, windows, min_severity, sinks, interval_secs, created_at, last_check_at
		FROM watches WHERE name = ?`, name)
	var (
		w           Watch
		target      string
		windows     string
		sinks       string
		createdAt   string
		lastCheckAt sql.NullString
	)
	err := row.Scan(&w.Kind, &target, &windows, &w.MinSeverity, &sinks, &w.IntervalSecs, &createdAt, &lastCheckAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Watch{}, errs.NewNotFound(fmt.Sprintf("no existe la vigilancia %q", name))
		}
		return Watch{}, fmt.Errorf("store: no se pudo leer la vigilancia %q: %w", name, err)
	}
	if err := decodeWatchFields(&w, target, windows, sinks, createdAt, lastCheckAt, name); err != nil {
		return Watch{}, err
	}
	w.Name = name
	return w, nil
}

// Watches returns every stored watch definition, ordered by name, so the
// listing is stable whatever order the rows come back in.
func (s *Store) Watches() ([]Watch, error) {
	rows, err := s.db.Query(`SELECT name, kind, target, windows, min_severity, sinks, interval_secs, created_at, last_check_at
		FROM watches ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("store: no se pudo leer las vigilancias: %w", err)
	}
	defer rows.Close()

	watches := []Watch{}
	for rows.Next() {
		var (
			name        string
			w           Watch
			target      string
			windows     string
			sinks       string
			createdAt   string
			lastCheckAt sql.NullString
		)
		if err := rows.Scan(&name, &w.Kind, &target, &windows, &w.MinSeverity, &sinks, &w.IntervalSecs, &createdAt, &lastCheckAt); err != nil {
			return nil, fmt.Errorf("store: vigilancia ilegible: %w", err)
		}
		if err := decodeWatchFields(&w, target, windows, sinks, createdAt, lastCheckAt, name); err != nil {
			return nil, err
		}
		w.Name = name
		watches = append(watches, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: no se pudo leer las vigilancias: %w", err)
	}
	return watches, nil
}

// DeleteWatch removes the definition of one watch. An absent name is a
// not_found error, never a silent success: deleting a watch that does not
// exist is a caller mistake worth reporting. The stored state and events of
// the watch are history, not definition; pruning them is not this method's
// job.
func (s *Store) DeleteWatch(name string) error {
	result, err := s.db.Exec("DELETE FROM watches WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("store: no se pudo eliminar la vigilancia %q: %w", name, err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("store: no se pudo verificar la vigilancia %q: %w", name, err)
	} else if affected == 0 {
		return errs.NewNotFound(fmt.Sprintf("no existe la vigilancia %q", name))
	}
	return nil
}

// decodeWatchFields unmarshals the JSON columns and the stored timestamps
// of one watches row into w, wrapping every failure with the watch name so
// the error says which row is unreadable.
func decodeWatchFields(w *Watch, target, windows, sinks, createdAt string, lastCheckAt sql.NullString, name string) error {
	if err := json.Unmarshal([]byte(target), &w.Target); err != nil {
		return fmt.Errorf("store: la vigilancia %q tiene un objetivo ilegible: %w", name, err)
	}
	if err := json.Unmarshal([]byte(windows), &w.Windows); err != nil {
		return fmt.Errorf("store: la vigilancia %q tiene ventanas ilegibles: %w", name, err)
	}
	if err := json.Unmarshal([]byte(sinks), &w.Sinks); err != nil {
		return fmt.Errorf("store: la vigilancia %q tiene destinos ilegibles: %w", name, err)
	}
	created, err := parseStoredTime(createdAt)
	if err != nil {
		return fmt.Errorf("store: la vigilancia %q tiene una fecha de creación ilegible: %w", name, err)
	}
	w.CreatedAt = created
	if lastCheckAt.Valid {
		parsed, err := parseStoredTime(lastCheckAt.String)
		if err != nil {
			return fmt.Errorf("store: last_check_at ilegible en %q: %w", name, err)
		}
		w.LastCheckAt = parsed
	}
	return nil
}

// nonNilStrings normalises a nullable string list to a slice that always
// serialises as a JSON array, never null: the columns are NOT NULL and a
// "null" column would read back as nil and break range loops in callers.
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// PreviousState returns the stored snapshot of one watch: one entry per
// incident still tracked. A watch with no history yields an empty map.
func (s *Store) PreviousState(watch string) (map[string]IncidentState, error) {
	rows, err := s.db.Query(`SELECT incident_id, severity, lanes, road, pk, type, end_time, last_seen_at, missing_count
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
		if err := rows.Scan(&id, &state.Severity, &state.Lanes, &state.Road, &state.PK, &state.Type,
			&endTime, &lastSeenAt, &state.MissingCount); err != nil {
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
		(watch_name, incident_id, severity, lanes, road, pk, type, end_time, last_seen_at, missing_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("store: no se pudo preparar el reemplazo de estado: %w", err)
	}
	defer insert.Close()

	for id, state := range states {
		var endTime any
		if state.EndTime != nil {
			endTime = storeTime(*state.EndTime)
		}
		if _, err := insert.Exec(watch, id, state.Severity, state.Lanes, state.Road, state.PK, state.Type,
			endTime, storeTime(state.LastSeenAt), state.MissingCount); err != nil {
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
	return s.stampEvents(ids)
}

// MarkSkipped stamps the given event ids so they stop blocking the retry
// queue. It is for undeliverable DATA — a payload that can never render, so
// every retry would fail and stall the events behind it forever — never for
// outages: a delivery that may succeed later must stay pending (use
// MarkDelivered only after a sink accepted the notification). The stamp is
// the same delivered_at column UndeliveredEvents filters on, so a skipped
// event never comes back as pending; its row stays in the log (the log is
// append-only, pruning is PruneEvents' job) for diagnostics.
func (s *Store) MarkSkipped(ids []int64) error {
	return s.stampEvents(ids)
}

// stampEvents sets delivered_at on the given event ids, the one store
// operation shared by MarkDelivered and MarkSkipped.
func (s *Store) stampEvents(ids []int64) error {
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
