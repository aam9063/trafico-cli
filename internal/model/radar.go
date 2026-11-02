package model

import (
	"github.com/aam9063/trafico-cli/internal/datex2"
	"github.com/aam9063/trafico-cli/internal/road"
)

// Radar is the normalized fixed radar ("radar fijo") of the JSON contract
// (docs/feeds.md §6). A radar is a segment (ini–fin) of road, not a point:
// pk carries both kilometre points when the feed publishes the two
// reference points, and location both ends. This feed does not order the
// kilometre points: some segment radars publish the fin reference point at
// a lower distance than the ini one (CVM_163929: ini 2650 m, fin 1400 m),
// so from > to occurs and is preserved unordered — the from <= to
// expectation of the incident contract (§4.3) does not hold here. Every
// absent field serialises as null (S6: nothing is invented):
//
//   - province is resolved from the INE table (province_ine.go) of the
//     published provinceINEIdentifier; it is null when the code is missing
//     or unknown, while province_code always carries the published value;
//   - direction_label is _0:directionNamed, the real destination label this
//     feed publishes (unlike the incident feed, §4.3). It is read only
//     from the primary (ini) reference point: a label published only on
//     the fin point (CVM_164860 publishes GUARDO there) is dropped, never
//     merged with the secondary (primary-authority rule);
//   - type is always null today: the feed carries no typed radar-kind
//     field. The id prefix (CABINACINEMOMETRO vs CVM) is the only signal,
//     and deriving a type from an identifier is a documented decision this
//     contract deliberately does not make; the field exists so the schema
//     stays stable if the DGT adds a kind;
//   - speed_limit is always null today for the same reason: the feed does
//     not publish a speed limit (docs/feeds.md §6). The key stays in the
//     contract so the schema is stable if the DGT ever adds one;
//   - singularity is the published road kind of the segment ("AUTOPISTA /
//     AUTOVÍA", "TÚNEL", ...), null when the feed omits it.
type Radar struct {
	SchemaVersion  int           `json:"schema_version"`
	ID             string        `json:"id"`
	Source         string        `json:"source"`
	SourceVersion  string        `json:"source_version"`
	Road           string        `json:"road"`
	PK             *PK           `json:"pk"`
	Direction      *string       `json:"direction"`
	DirectionLabel *string       `json:"direction_label"`
	Province       *string       `json:"province"`
	ProvinceCode   *string       `json:"province_code"`
	Location       RadarLocation `json:"location"`
	Type           *string       `json:"type"`
	SpeedLimit     *float64      `json:"speed_limit"`
	Singularity    *string       `json:"singularity"`
}

// RadarLocation is the geographic shape of a radar: both ends of the
// segment when the feed publishes them (a CVM segment radar), start only
// for a cabin (a point radar).
type RadarLocation struct {
	Start *Coordinates `json:"start"`
	End   *Coordinates `json:"end"`
}

// RadarFromParsed converts a parsed v1.0 radar into the contract radar.
// The kilometre points arrive as referencePointDistance in metres
// (docs/feeds.md §6: 26600.0 → PK 26.6) and are converted to kilometres,
// the unit of the rest of the contract. The segment direction and labels
// are taken from the primary (ini) reference point, the unit the feed
// publishes them on; the secondary's differing values are never merged.
func RadarFromParsed(parsed datex2.Radar) Radar {
	radar := Radar{
		SchemaVersion: SchemaVersion,
		ID:            "dgt:" + parsed.ID,
		Source:        Source,
		SourceVersion: "1.0",
		Road:          road.Normalize(parsed.Road),
		Direction:     direction(parsed.DirectionRelative),
		// The feed publishes no speed limit and no radar-kind field
		// (docs/feeds.md §6): both stay null, the keys stay in the contract.
		Type:         nil,
		SpeedLimit:   nil,
		ProvinceCode: optionalString(parsed.ProvinceINE),
		Location: RadarLocation{
			Start: coordinates(parsed.Start),
			End:   coordinates(parsed.End),
		},
		DirectionLabel: optionalString(parsed.DirectionNamed),
		Singularity:    optionalString(parsed.Singularity),
	}
	if parsed.FromDistance != nil || parsed.ToDistance != nil {
		pk := &PK{}
		if parsed.FromDistance != nil {
			from := *parsed.FromDistance / 1000
			pk.From = &from
		}
		if parsed.ToDistance != nil {
			to := *parsed.ToDistance / 1000
			pk.To = &to
		}
		radar.PK = pk
	}
	if name, ok := ProvinceFromINE(parsed.ProvinceINE); ok {
		radar.Province = &name
	}
	return radar
}
