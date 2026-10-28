package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// incidentResponse shows the composition pattern a command uses: embed the
// envelope so its fields land at top level next to the command payload.
type incidentResponse struct {
	Envelope
	Query     []string `json:"query"`
	Incidents []string `json:"incidents"`
}

func TestCompactWriteSingleLineJSON(t *testing.T) {
	fetched := time.Date(2026, 10, 28, 9, 30, 12, 0, time.UTC)
	resp := incidentResponse{
		Envelope: NewEnvelope(
			&Meta{Feed: "dgt-situations", FetchedAt: fetched},
			NewAttribution(fetched),
		),
		Incidents: []string{"one"},
	}

	var buf bytes.Buffer
	if err := Write(&buf, resp, Options{}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	out := buf.String()
	if !strings.HasSuffix(out, "}\n") {
		t.Fatalf("output must end with exactly one newline, got %q", out)
	}
	body := strings.TrimSuffix(out, "\n")
	if strings.Contains(body, "\n") {
		t.Fatalf("compact output must be a single line, got %d lines", strings.Count(body, "\n")+1)
	}
	for _, esc := range []string{"\x1b", "\u001b"} {
		if strings.Contains(out, esc) {
			t.Fatalf("output must not contain ANSI escape %q: %q", esc, out)
		}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got := parsed["schema_version"]; got != float64(SchemaVersion) {
		t.Fatalf("schema_version = %v, want %d", got, SchemaVersion)
	}
	if _, ok := parsed["attribution"]; !ok {
		t.Fatalf("output must contain attribution: %s", out)
	}
}

func TestPrettyOutputEndsWithAttributionLine(t *testing.T) {
	fetched := time.Date(2026, 10, 28, 9, 30, 12, 0, time.UTC)
	resp := incidentResponse{Envelope: NewEnvelope(nil, NewAttribution(fetched))}

	var buf bytes.Buffer
	if err := Write(&buf, resp, Options{Pretty: true}); err != nil {
		t.Fatalf("Write pretty: %v", err)
	}

	out := buf.String()
	if !strings.HasSuffix(out, "Fuente: DGT (CC BY)\n") {
		t.Fatalf("pretty output must end with the attribution line, got %q", out)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSuffix(out, "\nFuente: DGT (CC BY)\n")), &parsed); err != nil {
		t.Fatalf("pretty JSON body must still parse: %v", err)
	}
}

func TestPrettyFuncAttached(t *testing.T) {
	fetched := time.Date(2026, 10, 28, 9, 30, 12, 0, time.UTC)
	resp := incidentResponse{Envelope: NewEnvelope(nil, NewAttribution(fetched))}

	var buf bytes.Buffer
	opts := Options{Pretty: true, PrettyFunc: func(w io.Writer, v any) error {
		_, err := io.WriteString(w, "custom pretty\n")
		return err
	}}
	if err := Write(&buf, resp, opts); err != nil {
		t.Fatalf("Write pretty func: %v", err)
	}
	if buf.String() != "custom pretty\n" {
		t.Fatalf("attached PrettyFunc must own pretty rendering, got %q", buf.String())
	}
}

func TestNewAttributionConstants(t *testing.T) {
	fetched := time.Date(2026, 10, 28, 9, 30, 12, 0, time.UTC)
	a := NewAttribution(fetched)

	if a.Source != "DGT, Punto de Acceso Nacional" {
		t.Fatalf("source = %q", a.Source)
	}
	if a.License != "CC BY" {
		t.Fatalf("license = %q", a.License)
	}
	if a.URL != "https://nap.dgt.es/" {
		t.Fatalf("url = %q", a.URL)
	}

	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal attribution: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatalf("unmarshal attribution: %v", err)
	}
	wantOrder := []string{"source", "license", "url", "fetched_at"}
	if len(fields) != len(wantOrder) {
		t.Fatalf("attribution fields = %v, want %v", fields, wantOrder)
	}
	if got := fields["fetched_at"]; got != "2026-10-28T09:30:12Z" {
		t.Fatalf("fetched_at = %v, want RFC3339 UTC %q", got, "2026-10-28T09:30:12Z")
	}

	// Non-UTC input is normalised to UTC on serialisation.
	zone := time.FixedZone("test", 2*3600)
	local := time.Date(2026, 10, 28, 11, 30, 12, 0, zone)
	b, err = json.Marshal(NewAttribution(local))
	if err != nil {
		t.Fatalf("marshal local attribution: %v", err)
	}
	if !strings.Contains(string(b), `"fetched_at":"2026-10-28T09:30:12Z"`) {
		t.Fatalf("fetched_at must serialise as RFC3339 UTC, got %s", b)
	}
}

func TestAttributionFieldOrder(t *testing.T) {
	a := NewAttribution(time.Date(2026, 10, 28, 9, 30, 12, 0, time.UTC))
	b, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	iSource := strings.Index(s, `"source":`)
	iLicense := strings.Index(s, `"license":`)
	iURL := strings.Index(s, `"url":`)
	iFetched := strings.Index(s, `"fetched_at":`)
	if !(iSource < iLicense && iLicense < iURL && iURL < iFetched) {
		t.Fatalf("attribution field order must be source,license,url,fetched_at, got %s", s)
	}
}

func TestPayloadAtTopLevelNextToMeta(t *testing.T) {
	fetched := time.Date(2026, 10, 28, 9, 30, 12, 0, time.UTC)
	resp := incidentResponse{
		Envelope: NewEnvelope(
			&Meta{Feed: "dgt-situations", FetchedAt: fetched, Total: 1},
			NewAttribution(fetched),
		),
		Query:     []string{"A-7"},
		Incidents: []string{"x"},
	}

	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(b, &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"schema_version", "meta", "attribution", "query", "incidents"} {
		if _, ok := top[key]; !ok {
			t.Fatalf("key %q must appear at top level, got %v", key, string(b))
		}
	}
	if _, nested := top["meta"].(map[string]any)["envelope"]; nested {
		t.Fatalf("envelope must not be nested under meta")
	}
}

func TestMetaNullableFields(t *testing.T) {
	meta := Meta{Feed: "dgt-situations", FetchedAt: time.Now().UTC()}
	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := parsed["published_at"]; !ok || v != nil {
		t.Fatalf("published_at must be present and null, got %v (ok=%v)", v, ok)
	}
	if v, ok := parsed["age_seconds"]; !ok || v != nil {
		t.Fatalf("age_seconds must be present and null, got %v (ok=%v)", v, ok)
	}
	if _, ok := parsed["coverage_note"]; ok {
		t.Fatalf("empty coverage_note must be omitted, got %s", b)
	}
}

func TestWriteLinesJSONL(t *testing.T) {
	items := []any{
		map[string]string{"id": "a"},
		map[string]string{"id": "b"},
		map[string]string{"id": "c"},
	}

	var buf bytes.Buffer
	if err := WriteLines(&buf, items); err != nil {
		t.Fatalf("WriteLines: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != len(items) {
		t.Fatalf("got %d lines, want %d: %q", len(lines), len(items), buf.String())
	}
	for i, line := range lines {
		var parsed map[string]string
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("line %d does not parse independently: %v (%q)", i, err, line)
		}
		want := fmt.Sprintf("%c", 'a'+i)
		if parsed["id"] != want {
			t.Fatalf("line %d id = %q, want %q", i, parsed["id"], want)
		}
	}
}

func TestIsTTYFalseForBuffer(t *testing.T) {
	var buf bytes.Buffer
	if IsTTY(&buf) {
		t.Fatal("IsTTY must be false for bytes.Buffer")
	}
	if IsTTY(nil) {
		t.Fatal("IsTTY must be false for nil writer")
	}
}

func TestWritePropagatesError(t *testing.T) {
	want := errors.New("disk full")
	err := Write(failWriter{want}, map[string]string{"a": "b"}, Options{})
	if !errors.Is(err, want) {
		t.Fatalf("Write must propagate writer error, got %v", err)
	}
}

type failWriter struct{ err error }

func (f failWriter) Write(p []byte) (int, error) { return 0, f.err }
