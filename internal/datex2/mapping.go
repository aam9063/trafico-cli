package datex2

import (
	"fmt"
	"strings"
	"sync"

	_ "embed"

	"gopkg.in/yaml.v3"
)

// mapping.yaml maps the DATEX II cause and severity values actually observed
// in the feed (SPIKE-01, 2026-10-28) to the normalised values of the JSON
// contract. It is read-only data: values missing from it must be reported,
// not patched silently.
//
//go:embed mapping.yaml
var mappingYAML []byte

// Mapping is the parsed cause/severity mapping.
type Mapping struct {
	// DefaultType is the normalised type used when neither the detailed
	// cause nor the cause type is in the mapping ("other").
	DefaultType string `yaml:"default_type"`
	// Types lists the normalised types of the JSON contract.
	Types []string `yaml:"types"`
	// CauseTypes maps <sit:causeType> to a normalised type.
	CauseTypes map[string]string `yaml:"cause_types"`
	// DetailedCauseTypes maps the element name of the child of
	// <sit:detailedCauseType> to its values' normalised types. Most
	// specific, wins first.
	DetailedCauseTypes map[string]map[string]string `yaml:"detailed_cause_types"`
	// SeverityTable maps raw <sit:severity>/<sit:overallSeverity> values to
	// normalised severities (YAML key: severity).
	SeverityTable map[string]string `yaml:"severity"`
	// DefaultSeverity is used when the feed publishes no severity or an
	// unmapped one; the incident severity is never inferred from the cause.
	DefaultSeverity string `yaml:"default_severity"`
}

var (
	defaultMappingOnce sync.Once
	defaultMapping     *Mapping
)

// Load parses mapping data. It fails loudly on broken input — a mapping that
// silently degraded every incident to `other` would hide a real defect.
func Load(data []byte) (*Mapping, error) {
	var m Mapping
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("datex2: cannot parse mapping: %w", err)
	}
	if strings.TrimSpace(m.DefaultType) == "" {
		return nil, fmt.Errorf("datex2: mapping has no default_type")
	}
	if len(m.CauseTypes) == 0 {
		return nil, fmt.Errorf("datex2: mapping has no cause_types")
	}
	if len(m.DetailedCauseTypes) == 0 {
		return nil, fmt.Errorf("datex2: mapping has no detailed_cause_types")
	}
	if len(m.SeverityTable) == 0 {
		return nil, fmt.Errorf("datex2: mapping has no severity table")
	}
	if m.DefaultSeverity == "" {
		m.DefaultSeverity = "unknown"
	}
	if m.SeverityTable == nil {
		m.SeverityTable = map[string]string{}
	}
	return &m, nil
}

// Default returns the mapping loaded from the embedded mapping.yaml. The
// file is validated at Load time; if the embedded copy is broken this panics
// loudly at first use instead of mislabelling incidents.
func Default() *Mapping {
	defaultMappingOnce.Do(func() {
		m, err := Load(mappingYAML)
		if err != nil {
			panic(fmt.Sprintf("datex2: embedded mapping.yaml is broken: %v", err))
		}
		defaultMapping = m
	})
	return defaultMapping
}

// Type resolves the normalised incident type of a record with the default
// mapping (see Mapping.Type).
func Type(rec Record) string { return Default().Type(rec) }

// Severity normalises a raw severity with the default mapping (see
// Mapping.Severity).
func Severity(raw string) string { return Default().Severity(raw) }

// Type resolves the normalised incident type of a record, in this order:
// detailed cause (element name + value), cause type, default_type. The raw
// values stay on the record either way (P-04).
func (m *Mapping) Type(rec Record) string {
	if rec.DetailedCauseTypeElement != "" {
		if values := m.DetailedCauseTypes[rec.DetailedCauseTypeElement]; values != nil {
			if t := values[rec.DetailedCauseTypeValue]; t != "" {
				return t
			}
		}
	}
	if t := m.CauseTypes[rec.CauseType]; t != "" {
		return t
	}
	return m.DefaultType
}

// Severity normalises a raw severity value from the feed; absent or
// unmapped values become default_severity ("unknown").
func (m *Mapping) Severity(raw string) string {
	if v := m.SeverityTable[strings.ToLower(strings.TrimSpace(raw))]; v != "" {
		return v
	}
	return m.DefaultSeverity
}
