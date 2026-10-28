package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// SchemaVersion is the version of the output contract (ARRANQUE §5.3, P-09).
// Schema changes land in a dedicated PR with a schema_version bump.
const SchemaVersion = 1

// Attribution is the mandatory CC BY attribution block (ARRANQUE §2.1, P-05).
// It is embedded in every JSON response and shown as a closing line in pretty output.
type Attribution struct {
	Source    string    `json:"source"`
	License   string    `json:"license"`
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetched_at"`
}

// MarshalJSON serialises fetched_at as RFC3339 in UTC, regardless of the
// location the time was created in.
func (a Attribution) MarshalJSON() ([]byte, error) {
	type alias Attribution
	return json.Marshal(alias{
		Source:    a.Source,
		License:   a.License,
		URL:       a.URL,
		FetchedAt: a.FetchedAt.UTC(),
	})
}

// NewAttribution returns the attribution block for data fetched at fetchedAt.
func NewAttribution(fetchedAt time.Time) Attribution {
	return Attribution{
		Source:    "DGT, Punto de Acceso Nacional",
		License:   "CC BY",
		URL:       "https://nap.dgt.es/",
		FetchedAt: fetchedAt,
	}
}

// Meta holds the generic response metadata (ARRANQUE §5.3). It is a plain data
// holder: deriving published_at, age_seconds or stale is the caller's job.
type Meta struct {
	Feed         string     `json:"feed"`
	PublishedAt  *time.Time `json:"published_at"`
	FetchedAt    time.Time  `json:"fetched_at"`
	AgeSeconds   *int64     `json:"age_seconds"`
	Stale        bool       `json:"stale"`
	FromCache    bool       `json:"from_cache"`
	Total        int        `json:"total"`
	Unlocated    int        `json:"unlocated"`
	CoverageNote string     `json:"coverage_note,omitempty"`
}

// Envelope carries the fields every response shares (ARRANQUE §5.3). Commands
// embed it in their own response struct so schema_version, meta and attribution
// appear at top level next to the command payload:
//
//	type incidentsResponse struct {
//	    output.Envelope
//	    Query     []queryFilter  `json:"query"`
//	    Incidents []model.Incident `json:"incidents"`
//	}
type Envelope struct {
	SchemaVersion int         `json:"schema_version"`
	Meta          *Meta       `json:"meta,omitempty"`
	Attribution   Attribution `json:"attribution"`
}

// NewEnvelope returns an envelope with schema_version pinned to SchemaVersion.
// Commands embed the returned value in their response struct.
func NewEnvelope(meta *Meta, attribution Attribution) Envelope {
	return Envelope{
		SchemaVersion: SchemaVersion,
		Meta:          meta,
		Attribution:   attribution,
	}
}

// PrettyFunc renders v in human-readable form. Implementations must end their
// output with the line "Fuente: DGT (CC BY)".
type PrettyFunc func(w io.Writer, v any) error

// Options controls Write. With Pretty set and no PrettyFunc attached, Write
// falls back to indented JSON plus the attribution closing line.
type Options struct {
	Pretty     bool
	PrettyFunc PrettyFunc
}

// Write emits exactly one JSON value: compact single-line JSON with a single
// trailing newline by default, or pretty rendering when Options.Pretty is set.
func Write(w io.Writer, v any, opts Options) error {
	if opts.Pretty {
		if opts.PrettyFunc != nil {
			return opts.PrettyFunc(w, v)
		}
		return writePretty(w, v)
	}
	return json.NewEncoder(w).Encode(v)
}

// writePretty is the fallback pretty renderer: indented JSON followed by the
// mandatory attribution closing line (ARRANQUE §2.1).
func writePretty(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return err
	}
	_, err = io.WriteString(w, "Fuente: DGT (CC BY)\n")
	return err
}

// WriteLines emits items as JSONL (one compact JSON value per line, no
// envelope), for commands that stream results.
func WriteLines(w io.Writer, items []any) error {
	enc := json.NewEncoder(w)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return err
		}
	}
	return nil
}

// IsTTY reports whether w is a character device (an interactive terminal).
// It is used to decide whether pretty output and stderr diagnostics apply.
func IsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// Diag writes a diagnostic line to w, which callers pass as os.Stderr, so
// diagnostics never touch the JSON on stdout (P-01). Callers that suppress
// diagnostics on a terminal follow the same rule the other way round.
func Diag(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, format+"\n", args...)
}
