package model

import (
	"time"

	"github.com/aam9063/trafico-cli/internal/datex2"
	"github.com/aam9063/trafico-cli/internal/road"
)

// SchemaVersion is the version of the JSON contract this package emits
// (P-09). Schema changes land in a dedicated PR with a schema_version bump;
// schemas/incident.schema.json must stay in step with this constant.
const SchemaVersion = 1

// Source is the only incident source in v1: the DGT national access point.
const Source = "dgt"

// SourceVersion is the DATEX II model base version of the incident feed
// (docs/feeds.md §1.1: modelBaseVersion 3, profile 3.7).
const SourceVersion = "3"

// Incident is the normalized incident of the JSON contract (ARRANQUE §4.1).
// The struct fields serialise the §4.1 keys, in that order, with the
// corrections measured by SPIKE-01 (docs/feeds.md §4.3, §4.4):
//
//   - situation_id was added because one DATEX II situation holds several
//     records and only the record id is unique per location and cause;
//   - direction_bearing replaced the sketched direction_label: the feed
//     carries no destination label, only a compass bearing (§4.4);
//   - lanes_affected is a []string of raw loc:laneUsage values, NOT a "1 de 3"
//     string: the feed publishes no lane total, only which lanes are affected
//     (§4.3). It serialises as null when the record lists none;
//   - raw_description is always null for now: the feed carries no free text
//     (§4.3). The field stays so the contract is stable if the DGT adds one;
//     the original cause text is preserved in subtype_raw instead;
//   - severity is the DGT-published severity mapped to low|medium|high;
//     unknown (also serialised as a string) means the DGT publishes none
//     (87% of records, §4.5) and is never inferred from the cause.
//
// P-04 governs every field: what the feed does not carry serialises as null;
// nothing is ever synthesised.
type Incident struct {
	SchemaVersion    int      `json:"schema_version"`
	ID               string   `json:"id"`
	SituationID      string   `json:"situation_id"`
	Source           string   `json:"source"`
	SourceVersion    string   `json:"source_version"`
	Type             string   `json:"type"`
	SubtypeRaw       string   `json:"subtype_raw"`
	Severity         string   `json:"severity"`
	Status           string   `json:"status"`
	Road             string   `json:"road"`
	Direction        *string  `json:"direction"`
	DirectionBearing *string  `json:"direction_bearing"`
	PK               *PK      `json:"pk"`
	Location         Location `json:"location"`
	LanesAffected    []string `json:"lanes_affected"`
	StartTime        string   `json:"start_time"`
	EndTime          *string  `json:"end_time"`
	RawDescription   *string  `json:"raw_description"`
	UpdatedAt        string   `json:"updated_at"`
}

// PK is the kilometre-point interval ("punto kilométrico") of a location.
// Either bound is null when the record is a single point.
type PK struct {
	From *float64 `json:"from"`
	To   *float64 `json:"to"`
}

// Location is the geographic context of an incident. End is null for point
// locations. Province and Municipality keep the source text as published
// (P-04); filters normalise them with NormalizeProvince/ProvinceEqual.
type Location struct {
	Start        *Coordinates `json:"start"`
	End          *Coordinates `json:"end"`
	Province     string       `json:"province"`
	Municipality string       `json:"municipality"`
}

// Coordinates is a WGS84 position.
type Coordinates struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
}

// FromRecord converts a parsed DATEX II record into the contract incident.
// m resolves the normalised type and severity; nil means the embedded
// datex2.Default() mapping. now is the injected clock: status must never be
// derived from wall time inside this package, so tests stay deterministic.
func FromRecord(rec datex2.Record, m *datex2.Mapping, now time.Time) Incident {
	if m == nil {
		m = datex2.Default()
	}
	status := computeStatus(rec, now)
	return Incident{
		SchemaVersion:    SchemaVersion,
		ID:               "dgt:" + rec.ID,
		SituationID:      rec.SituationID,
		Source:           Source,
		SourceVersion:    SourceVersion,
		Type:             m.Type(rec),
		SubtypeRaw:       subtypeRaw(rec),
		Severity:         m.Severity(rec.SeverityRaw),
		Status:           status,
		Road:             road.Normalize(rec.RoadName),
		Direction:        direction(rec.Direction),
		DirectionBearing: directionBearing(rec.DirectionBearing),
		PK:               pk(rec.PK),
		Location: Location{
			Start:        coordinates(rec.Start),
			End:          coordinates(rec.End),
			Province:     rec.Province,
			Municipality: rec.Municipality,
		},
		LanesAffected: rec.LaneUsages,
		StartTime:     rec.StartTime.UTC().Format(time.RFC3339),
		EndTime:       rfc3339(rec.EndTime),
		// The incident feed carries no free text (docs/feeds.md §4.3), so
		// raw_description is always null. The field remains in the contract
		// in case the DGT adds one; the original cause text lives in
		// subtype_raw (P-04).
		RawDescription: nil,
		UpdatedAt:      rec.VersionTime.UTC().Format(time.RFC3339),
	}
}

// computeStatus derives status from the validity window, never from the
// feed's validityStatus string (a stale cache must not label a finished
// incident as active):
//
//	start in the future          -> planned
//	end present and in the past  -> ended
//	otherwise                    -> active
//
// An incident whose end instant equals now is still active; it ends the
// moment now moves past it.
func computeStatus(rec datex2.Record, now time.Time) string {
	if !rec.StartTime.IsZero() && now.Before(rec.StartTime) {
		return "planned"
	}
	if rec.EndTime != nil && now.After(*rec.EndTime) {
		return "ended"
	}
	return "active"
}

// subtypeRaw keeps the original DATEX II cause value (P-04): the value of the
// sit:detailedCauseType child when the feed publishes one, otherwise the
// sit:causeType. Both are always present in one of the two positions.
func subtypeRaw(rec datex2.Record) string {
	if rec.DetailedCauseTypeValue != "" {
		return rec.DetailedCauseTypeValue
	}
	return rec.CauseType
}

// direction passes through the travel direction of lse:tpegDirectionRoad
// (positive|negative|both, §4.4) and serialises as null when the feed omits
// it or carries any other value. It is never merged with the compass bearing.
func direction(v string) *string {
	switch v {
	case "positive", "negative", "both":
		s := v
		return &s
	default:
		return nil
	}
}

// directionBearing exposes the compass bearing of loc:tpegDirection
// (westBound, northEastBound, ...) in its own field, exactly as the feed
// publishes it, including DATEX II's "unknown". It is a geographic heading,
// not a destination label: the incident feed has no destination (§4.4).
func directionBearing(v string) *string {
	if v == "" {
		return nil
	}
	s := v
	return &s
}

// pk copies the kilometre-point interval; nil when the record carries no
// kilometre point at all.
func pk(p *datex2.PK) *PK {
	if p == nil {
		return nil
	}
	return &PK{From: p.From, To: p.To}
}

// coordinates copies a position; nil when absent.
func coordinates(c *datex2.Coordinates) *Coordinates {
	if c == nil {
		return nil
	}
	return &Coordinates{Latitude: c.Latitude, Longitude: c.Longitude}
}

// rfc3339 formats a time as RFC3339 in UTC; nil serialises as null.
func rfc3339(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
