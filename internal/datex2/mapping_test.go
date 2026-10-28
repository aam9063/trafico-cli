package datex2

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// rawMappingShape mirrors mapping.yaml so the tests can iterate over every
// entry without hardcoding the table. If mapping.yaml grows a new value the
// loop below automatically covers it.
type rawMappingShape struct {
	DefaultType        string                       `yaml:"default_type"`
	Types              []string                     `yaml:"types"`
	CauseTypes         map[string]string            `yaml:"cause_types"`
	DetailedCauseTypes map[string]map[string]string `yaml:"detailed_cause_types"`
	Severity           map[string]string            `yaml:"severity"`
	DefaultSeverity    string                       `yaml:"default_severity"`
}

func loadRawShape(t *testing.T) rawMappingShape {
	t.Helper()
	var raw rawMappingShape
	if err := yaml.Unmarshal(mappingYAML, &raw); err != nil {
		t.Fatalf("unmarshal mapping.yaml: %v", err)
	}
	return raw
}

// Every cause_type entry in mapping.yaml must resolve through the mapping.
func TestMapping_Type_CauseTypesTable(t *testing.T) {
	raw := loadRawShape(t)
	if len(raw.CauseTypes) == 0 {
		t.Fatal("mapping.yaml has no cause_types; the table test would be vacuous")
	}
	for cause, want := range raw.CauseTypes {
		rec := Record{CauseType: cause}
		if got := Default().Type(rec); got != want {
			t.Errorf("Type({CauseType: %q}) = %q, want %q", cause, got, want)
		}
	}
}

// Every detailed_cause_type element/value pair in mapping.yaml must resolve
// with the highest priority, even when the cause type is also present.
func TestMapping_Type_DetailedCauseTypesTable(t *testing.T) {
	raw := loadRawShape(t)
	if len(raw.DetailedCauseTypes) == 0 {
		t.Fatal("mapping.yaml has no detailed_cause_types; the table test would be vacuous")
	}
	for element, values := range raw.DetailedCauseTypes {
		if len(values) == 0 {
			t.Errorf("detailed_cause_types.%s has no values", element)
			continue
		}
		for value, want := range values {
			rec := Record{
				CauseType:                "accident", // also mapped; the detailed cause must win
				DetailedCauseTypeElement: element,
				DetailedCauseTypeValue:   value,
			}
			if got := Default().Type(rec); got != want {
				t.Errorf("Type({%s: %s}) = %q, want %q", element, value, got, want)
			}
		}
	}
}

func TestMapping_Type_UnknownFallsBackToDefault(t *testing.T) {
	raw := loadRawShape(t)
	tests := []struct {
		name string
		rec  Record
	}{
		{"unknown cause type", Record{CauseType: "noSuchCauseType"}},
		{
			"unknown detailed cause with unknown cause type",
			Record{CauseType: "noSuchCauseType", DetailedCauseTypeElement: "noSuchElement", DetailedCauseTypeValue: "nope"},
		},
		{
			"known element with unknown value falls through to default",
			Record{DetailedCauseTypeElement: "accidentType", DetailedCauseTypeValue: "noSuchValue"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Default().Type(tt.rec); got != raw.DefaultType {
				t.Errorf("Type = %q, want default %q", got, raw.DefaultType)
			}
		})
	}
}

// An element that exists in the feed but has no detailed entry must fall
// through to the cause type, not to the default.
func TestMapping_Type_UnknownDetailFallsThroughToCauseType(t *testing.T) {
	rec := Record{
		CauseType:                "accident",
		DetailedCauseTypeElement: "noSuchElement",
		DetailedCauseTypeValue:   "nope",
	}
	if got := Default().Type(rec); got != "accident" {
		t.Errorf("Type = %q, want accident", got)
	}
}

func TestMapping_Severity_Table(t *testing.T) {
	raw := loadRawShape(t)
	if len(raw.Severity) == 0 {
		t.Fatal("mapping.yaml has no severity table; the table test would be vacuous")
	}
	for in, want := range raw.Severity {
		if got := Default().Severity(in); got != want {
			t.Errorf("Severity(%q) = %q, want %q", in, got, want)
		}
	}
	// docs/feeds.md §4.5: highest normalises to high.
	if got := Default().Severity("highest"); got != "high" {
		t.Errorf("Severity(highest) = %q, want high", got)
	}
	// Unmapped and absent severities are unknown, never inferred (P-04).
	for _, in := range []string{"noSuchSeverity", "", "  "} {
		if got := Default().Severity(in); got != raw.DefaultSeverity {
			t.Errorf("Severity(%q) = %q, want %q", in, got, raw.DefaultSeverity)
		}
	}
	// The feed sends lowercase, but the lookup tolerates source casing.
	if got := Default().Severity("HIGH"); got != "high" {
		t.Errorf("Severity(HIGH) = %q, want high", got)
	}
}

// Load must fail loudly on broken input instead of returning a mapping that
// silently degrades every incident to `other`.
func TestMapping_Load_Errors(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"invalid yaml", "::: not yaml :::"},
		{"missing default_type", "cause_types:\n  accident: accident\n"},
		{"missing cause tables", "default_type: other\nseverity:\n  low: low\n"},
		{"missing severity table", "default_type: other\ncause_types:\n  accident: accident\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := Load([]byte(tt.data))
			if err == nil {
				t.Fatalf("Load succeeded with %+v, want error", m)
			}
		})
	}
}

func TestMapping_Load_Valid(t *testing.T) {
	m, err := Load(mappingYAML)
	if err != nil {
		t.Fatalf("Load(mappingYAML): %v", err)
	}
	if m.DefaultType == "" || len(m.CauseTypes) == 0 || len(m.SeverityTable) == 0 {
		t.Fatalf("Load lost tables: %+v", m)
	}
	if !reflect.DeepEqual(m, Default()) {
		t.Error("Load(mappingYAML) and Default() differ")
	}
}
