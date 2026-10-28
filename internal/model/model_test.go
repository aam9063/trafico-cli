package model

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/datex2"
)

// update regenerates the golden file when explicitly requested:
// go test ./internal/model/ -update. It is never automatic.
var update = flag.Bool("update", false, "rewrite testdata/incident.golden.json")

func ptrTime(t time.Time) *time.Time { return &t }

// --- status computation -------------------------------------------------

func statusRecord(start time.Time, end *time.Time) datex2.Record {
	return datex2.Record{StartTime: start, EndTime: end}
}

// TestStatusComputation is table-driven over an injected clock: status comes
// from the validity window, never from the feed's validityStatus string.
func TestStatusComputation(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		rec  datex2.Record
		now  time.Time
		want string
	}{
		{
			name: "active: started in the past, no end time",
			rec:  statusRecord(now.Add(-2*time.Hour), nil),
			now:  now,
			want: "active",
		},
		{
			name: "planned: start in the future",
			rec:  statusRecord(now.Add(24*time.Hour), nil),
			now:  now,
			want: "planned",
		},
		{
			name: "planned: start in the future with a future end",
			rec:  statusRecord(now.Add(24*time.Hour), ptrTime(now.Add(48*time.Hour))),
			now:  now,
			want: "planned",
		},
		{
			name: "ended: end time in the past",
			rec:  statusRecord(now.Add(-48*time.Hour), ptrTime(now.Add(-2*time.Hour))),
			now:  now,
			want: "ended",
		},
		{
			name: "active at the exact end instant",
			rec:  statusRecord(now.Add(-2*time.Hour), ptrTime(now)),
			now:  now,
			want: "active",
		},
		{
			name: "ended one second after the end",
			rec:  statusRecord(now.Add(-2*time.Hour), ptrTime(now.Add(-time.Second))),
			now:  now,
			want: "ended",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inc := FromRecord(tt.rec, nil, tt.now)
			if inc.Status != tt.want {
				t.Errorf("status = %q, want %q", inc.Status, tt.want)
			}
		})
	}
}

// --- FromRecord ----------------------------------------------------------

// goldenRecord returns the first record of testdata/feeds/situations/snap-1.xml
// (situation 2816645, record 18811074): a real feed record, not a synthetic one.
func goldenRecord(t *testing.T) datex2.Record {
	t.Helper()
	data, err := os.ReadFile("../../testdata/feeds/situations/snap-1.xml")
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	pub, err := datex2.ParseSituations(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	if len(pub.Situations) == 0 || len(pub.Situations[0].Records) == 0 {
		t.Fatal("snapshot has no records")
	}
	return pub.Situations[0].Records[0]
}

// TestFromRecordGolden asserts the serialisation of a real record byte for
// byte. Regeneration is explicit: go test ./internal/model/ -update.
func TestFromRecordGolden(t *testing.T) {
	rec := goldenRecord(t)
	// The record started 2025-12-12 and has no end time: with this clock the
	// incident is active.
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	inc := FromRecord(rec, nil, now)

	got, err := json.Marshal(inc)
	if err != nil {
		t.Fatalf("marshal incident: %v", err)
	}
	got = append(got, '\n')

	const goldenPath = "testdata/incident.golden.json"
	if *update {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (regenerate with go test ./internal/model/ -update): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestFromRecordRealValues pins the contract decisions of MODEL-01 on the real
// record behind the golden (docs/feeds.md §4.2-§4.4).
func TestFromRecordRealValues(t *testing.T) {
	rec := goldenRecord(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	inc := FromRecord(rec, nil, now)

	if inc.ID != "dgt:18811074" {
		t.Errorf("id = %q, want dgt:18811074", inc.ID)
	}
	if inc.SituationID != "2816645" {
		t.Errorf("situation_id = %q, want 2816645", inc.SituationID)
	}
	if inc.Type != "roadworks" {
		t.Errorf("type = %q, want roadworks", inc.Type)
	}
	if inc.SubtypeRaw != "roadworks" {
		t.Errorf("subtype_raw = %q, want roadworks (the detailed cause value)", inc.SubtypeRaw)
	}
	if inc.Severity != "unknown" {
		t.Errorf("severity = %q, want unknown (the feed publishes none here)", inc.Severity)
	}
	if inc.Road != "N-400" {
		t.Errorf("road = %q, want N-400", inc.Road)
	}
	if inc.Direction == nil || *inc.Direction != "both" {
		t.Errorf("direction = %v, want both", inc.Direction)
	}
	if inc.DirectionBearing == nil || *inc.DirectionBearing != "unknown" {
		t.Errorf("direction_bearing = %v, want unknown (raw loc:tpegDirection, §4.4)", inc.DirectionBearing)
	}
	if inc.LanesAffected == nil || len(inc.LanesAffected) != 1 || inc.LanesAffected[0] != "allLanesCompleteCarriageway" {
		t.Errorf("lanes_affected = %v, want [allLanesCompleteCarriageway]", inc.LanesAffected)
	}
	if inc.PK == nil || inc.PK.From == nil || inc.PK.To == nil || *inc.PK.From != 37.1 || *inc.PK.To != 37.3 {
		t.Errorf("pk = %v, want from 37.1 to 37.3", inc.PK)
	}
	if inc.Location.Start == nil || inc.Location.End == nil {
		t.Fatal("linear location must carry start and end coordinates")
	}
	if inc.Location.Province != "Toledo" || inc.Location.Municipality != "Ontígola" {
		t.Errorf("location = %+v, want province Toledo, municipality Ontígola", inc.Location)
	}
	if inc.StartTime != "2025-12-12T07:05:19Z" {
		t.Errorf("start_time = %q, want 2025-12-12T07:05:19Z", inc.StartTime)
	}
	if inc.EndTime != nil {
		t.Errorf("end_time = %q, want null", *inc.EndTime)
	}
	if inc.RawDescription != nil {
		t.Errorf("raw_description = %q, want null (the feed has no free text, §4.3)", *inc.RawDescription)
	}
	if inc.UpdatedAt != "2025-12-12T07:11:34Z" {
		t.Errorf("updated_at = %q, want 2025-12-12T07:11:34Z", inc.UpdatedAt)
	}
}

// TestFromRecordOmittedValues proves P-04 on constructed records: what the
// feed does not carry serialises as null, and the compass bearing is never
// merged into direction (§4.4).
func TestFromRecordOmittedValues(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	pk := 5.0
	rec := datex2.Record{
		ID:           "42",
		SituationID:  "7",
		LocationType: "PointLocation",
		CauseType:    "accident",
		StartTime:    now.Add(-time.Hour),
		PK:           &datex2.PK{From: &pk},
		Start:        &datex2.Coordinates{Latitude: 38.3, Longitude: -0.55},
		Province:     "Alacant/Alicante",
		Municipality: "Elche",
	}
	inc := FromRecord(rec, nil, now)

	raw, err := json.Marshal(inc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"subtype_raw":"accident"`,
		`"type":"accident"`,
		`"pk":{"from":5,"to":null}`,
		`"location":{"start":{"lat":38.3,"lon":-0.55},"end":null,"province":"Alacant/Alicante","municipality":"Elche"}`,
		`"lanes_affected":null`,
		`"raw_description":null`,
		`"end_time":null`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("marshalled incident misses %s:\n%s", want, raw)
		}
	}
	if inc.Direction != nil {
		t.Errorf("direction = %q, want null when the feed omits it", *inc.Direction)
	}
	if inc.DirectionBearing != nil {
		t.Errorf("direction_bearing = %q, want null when the feed omits it", *inc.DirectionBearing)
	}
	if inc.Type != "accident" {
		t.Errorf("type = %q, want accident resolved from the cause type", inc.Type)
	}
}

// TestFromRecordDirectionBearingSeparate proves the two direction concepts of
// §4.4 never merge: direction keeps the travel direction, the compass bearing
// lives in direction_bearing.
func TestFromRecordDirectionBearingSeparate(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	rec := datex2.Record{
		ID:               "1",
		StartTime:        now.Add(-time.Hour),
		Direction:        "negative",
		DirectionBearing: "westBound",
	}
	inc := FromRecord(rec, nil, now)
	if inc.Direction == nil || *inc.Direction != "negative" {
		t.Errorf("direction = %v, want negative", inc.Direction)
	}
	if inc.DirectionBearing == nil || *inc.DirectionBearing != "westBound" {
		t.Errorf("direction_bearing = %v, want westBound", inc.DirectionBearing)
	}
}

// --- schemas -------------------------------------------------------------

// TestSchemasAreValidJSON asserts both schema files parse; the response
// envelope schema is exercised structurally here because this package must not
// import internal/output (MODEL-01 constraint).
func TestSchemasAreValidJSON(t *testing.T) {
	for _, path := range []string{"../../schemas/incident.schema.json", "../../schemas/incidents-response.schema.json"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Errorf("%s is not valid JSON: %v", path, err)
		}
	}
}

// TestGoldenAgainstIncidentSchema validates the real golden output against
// schemas/incident.schema.json with the focused validator below (draft
// 2020-12 subset: type, required, properties, additionalProperties, items).
func TestGoldenAgainstIncidentSchema(t *testing.T) {
	schemaData, err := os.ReadFile("../../schemas/incident.schema.json")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var schema schemaNode
	if err := json.Unmarshal(schemaData, &schema); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	golden, err := os.ReadFile("testdata/incident.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var value any
	if err := json.Unmarshal(golden, &value); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	if errs := schema.validate(value, ""); len(errs) > 0 {
		t.Errorf("golden does not validate against incident.schema.json: %s", strings.Join(errs, "; "))
	}
}

// --- focused schema validator --------------------------------------------

// schemaNode is a validator for the subset of JSON Schema draft 2020-12 the
// contract schemas use: type (single or array, covering ["string","null"]),
// const, enum, required, properties, additionalProperties and items. No JSON
// Schema library is added (MODEL-01 constraint).
type schemaNode struct {
	Type                 any                    `json:"type"`
	Const                any                    `json:"const"`
	Enum                 []any                  `json:"enum"`
	Properties           map[string]*schemaNode `json:"properties"`
	Required             []string               `json:"required"`
	AdditionalProperties *bool                  `json:"additionalProperties"`
	Items                *schemaNode            `json:"items"`
}

// validate returns the list of violations of v against this schema node.
func (s *schemaNode) validate(v any, path string) []string {
	var errs []string
	at := path
	if at == "" {
		at = "(root)"
	}
	if s.Const != nil && !jsonEqual(v, s.Const) {
		errs = append(errs, fmt.Sprintf("%s: %v does not equal const %v", at, v, s.Const))
	}
	if s.Type != nil && !s.typeMatches(v) {
		errs = append(errs, fmt.Sprintf("%s: %v does not match type %v", at, v, s.Type))
	}
	if len(s.Enum) > 0 {
		ok := false
		for _, e := range s.Enum {
			if jsonEqual(v, e) {
				ok = true
				break
			}
		}
		if !ok {
			errs = append(errs, fmt.Sprintf("%s: %v is not in enum", at, v))
		}
	}
	obj, isObj := v.(map[string]any)
	if isObj {
		for _, req := range s.Required {
			if _, ok := obj[req]; !ok {
				errs = append(errs, fmt.Sprintf("%s: missing required property %q", at, req))
			}
		}
		if s.AdditionalProperties != nil && !*s.AdditionalProperties {
			for key := range obj {
				if _, ok := s.Properties[key]; !ok {
					errs = append(errs, fmt.Sprintf("%s: unexpected property %q", at, key))
				}
			}
		}
		for key, sub := range s.Properties {
			if val, ok := obj[key]; ok {
				errs = append(errs, sub.validate(val, path+"/"+key)...)
			}
		}
	}
	if arr, ok := v.([]any); ok && s.Items != nil {
		for i, item := range arr {
			errs = append(errs, s.Items.validate(item, fmt.Sprintf("%s[%d]", path, i))...)
		}
	}
	return errs
}

// typeMatches checks the JSON type of v against the declared type, which may
// be a single string or an array such as ["string","null"].
func (s *schemaNode) typeMatches(v any) bool {
	check := func(t string) bool {
		switch t {
		case "string":
			_, ok := v.(string)
			return ok
		case "number":
			_, ok := v.(float64)
			return ok
		case "integer":
			f, ok := v.(float64)
			return ok && f == math.Trunc(f)
		case "boolean":
			_, ok := v.(bool)
			return ok
		case "null":
			return v == nil
		case "object":
			_, ok := v.(map[string]any)
			return ok
		case "array":
			_, ok := v.([]any)
			return ok
		}
		return false
	}
	switch t := s.Type.(type) {
	case string:
		return check(t)
	case []any:
		for _, e := range t {
			if str, ok := e.(string); ok && check(str) {
				return true
			}
		}
	}
	return false
}

// jsonEqual compares two decoded JSON values for deep equality; decoded
// numbers are always float64, so a plain == on them is safe.
func jsonEqual(a, b any) bool {
	aa, aok := a.(map[string]any)
	bb, bok := b.(map[string]any)
	if aok && bok {
		if len(aa) != len(bb) {
			return false
		}
		for k, av := range aa {
			bv, ok := bb[k]
			if !ok || !jsonEqual(av, bv) {
				return false
			}
		}
		return true
	}
	return a == b
}

// TestResponseSchemaShape pins the §5.3 envelope keys of
// schemas/incidents-response.schema.json.
func TestResponseSchemaShape(t *testing.T) {
	data, err := os.ReadFile("../../schemas/incidents-response.schema.json")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	for _, key := range []string{"schema_version", "query", "meta", "incidents", "attribution"} {
		if _, ok := doc.Properties[key]; !ok {
			t.Errorf("response schema misses property %q", key)
		}
	}
	for _, key := range []string{"schema_version", "meta", "incidents", "attribution"} {
		found := false
		for _, r := range doc.Required {
			if r == key {
				found = true
			}
		}
		if !found {
			t.Errorf("response schema misses required %q", key)
		}
	}
}
