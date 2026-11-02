package model

import (
	"math"
	"strconv"
	"strings"

	"github.com/aam9063/trafico-cli/internal/datex2"
	"github.com/aam9063/trafico-cli/internal/road"
)

// Section kinds of the JSON contract. They are the two datasets the
// `trafico sections` command serves, and they are deliberately spelled in
// the command's vocabulary: invive is the DGT road-section inventory
// (docs/feeds.md §7), animals the TEFIVA/SPFRIA black spots for animal
// collisions (docs/feeds.md §8).
const (
	SectionKindInvive  = "invive"
	SectionKindAnimals = "animals"
)

// Sources of a section's kilometre points (pk_source). The value tells the
// consumer how trustworthy the number is: "reference" means the DGT
// published the PK as a referencePointDistance element (INVIVE), while
// "identifier" means the PK was derived from the fragile id encoding of
// TEFIVA (docs/feeds.md §8) and only pk.from is really published — pk.to
// is from plus the affected length.
const (
	PKSourceReference  = "reference"
	PKSourceIdentifier = "identifier"
)

// Section is the normalized road section of the JSON contract (FEED-05).
// Every absent field serialises as null (S6: nothing is invented):
//
//   - pk is null when neither bound is known; pk_source is null whenever
//     no bound was derived;
//   - direction is the published relative direction (positive, negative or
//     both) and null when the feed omits it;
//   - province is null for TEFIVA (the feed carries no INE code and the
//     uppercase prefix of the id is never lifted into one) and carries the
//     published administrative area name for INVIVE;
//   - length_km is the published lengthAffected in kilometres (TEFIVA
//     only); it is never derived from the PK bounds for INVIVE.
type Section struct {
	SchemaVersion int             `json:"schema_version"`
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	Source        string          `json:"source"`
	SourceVersion string          `json:"source_version"`
	Road          string          `json:"road"`
	PK            *PK             `json:"pk"`
	PKSource      *string         `json:"pk_source"`
	Direction     *string         `json:"direction"`
	Province      *string         `json:"province"`
	Location      SectionLocation `json:"location"`
	LengthKM      *float64        `json:"length_km"`
}

// SectionLocation is the geographic shape of a section: both ends of the
// segment.
type SectionLocation struct {
	Start *Coordinates `json:"start"`
	End   *Coordinates `json:"end"`
}

// SectionFromInvive converts a parsed v1.0 INVIVE section into the
// contract section. The kilometre points arrive as referencePointDistance
// in metres (docs/feeds.md §7) and are converted to kilometres, the unit
// of the rest of the contract; the feed publishes both bounds for every
// measured section, but either bound stays null when the feed omits it.
func SectionFromInvive(parsed datex2.InviveSection) Section {
	section := Section{
		SchemaVersion: SchemaVersion,
		ID:            "dgt:" + parsed.ID,
		Kind:          SectionKindInvive,
		Source:        Source,
		SourceVersion: "1.0",
		Road:          road.Normalize(parsed.Road),
		Direction:     direction(parsed.DirectionRelative),
		Province:      optionalString(parsed.Province),
		Location: SectionLocation{
			Start: coordinates(parsed.Start),
			End:   coordinates(parsed.End),
		},
		// The INVIVE feed publishes no lengthAffected and no identifier
		// encoding: length_km stays null (S6).
		LengthKM: nil,
	}
	if parsed.FromDistance != nil || parsed.ToDistance != nil {
		section.PK = &PK{}
		if parsed.FromDistance != nil {
			from := *parsed.FromDistance / 1000
			section.PK.From = &from
		}
		if parsed.ToDistance != nil {
			to := *parsed.ToDistance / 1000
			section.PK.To = &to
		}
		source := PKSourceReference
		section.PKSource = &source
	}
	return section
}

// SectionFromTefiva converts a parsed v3 TEFIVA section into the contract
// section, applying the decided PK rule (docs/feeds.md §8): the kilometre
// point exists only encoded in the id
// (PROVINCIA_CARRETERA_PK_EN_METROS, e.g. HUELVA_N-433_71400.0), so
// pk.from is the trailing metres divided by 1000 and pk.to is
// pk.from + lengthAffected. The derivation is exposed as pk_source
// "identifier"; an id that does not match the documented pattern derives
// nothing — both bounds and pk_source stay null, never a guess. The
// uppercase province prefix of the id is NOT used for province: TEFIVA
// carries no INE code, so province is null.
func SectionFromTefiva(parsed datex2.TefivaSection) Section {
	section := Section{
		SchemaVersion: SchemaVersion,
		ID:            "dgt:" + parsed.ID,
		Kind:          SectionKindAnimals,
		Source:        Source,
		SourceVersion: "3",
		Road:          road.Normalize(parsed.Road),
		Direction:     direction(parsed.DirectionRelative),
		Province:      nil,
		Location: SectionLocation{
			Start: coordinates(parsed.Start),
			End:   coordinates(parsed.End),
		},
		LengthKM: parsed.LengthKM,
	}
	// The published loc:distanceAlong elements are the authority for the
	// kilometre point. docs/feeds.md §8 said this feed carried no PK elements;
	// it does, and they agree with the metre the id encodes — which is why the
	// mistake survived until the fixture was read element by element.
	if parsed.FromMetres != nil {
		from := *parsed.FromMetres / 1000
		pk := &PK{From: &from}
		if parsed.ToMetres != nil {
			to := *parsed.ToMetres / 1000
			pk.To = &to
		}
		section.PK = pk
		source := PKSourceReference
		section.PKSource = &source
		return section
	}
	// No location block: fall back to the metre the identifier encodes.
	if metres, ok := pkMetresFromIdentifier(parsed.ID); ok {
		from := metres / 1000
		pk := &PK{From: &from}
		if parsed.LengthKM != nil {
			// The sum runs in kilometre space because lengthAffected is
			// published in kilometres; the result is rounded to the
			// millimetre so the float noise of 71.4 + 1.7 never serialises
			// as 73.09999999999999 — the value itself is unchanged.
			to := math.Round((from+*parsed.LengthKM)*1e6) / 1e6
			pk.To = &to
		}
		section.PK = pk
		source := PKSourceIdentifier
		section.PKSource = &source
	}
	return section
}

// pkMetresFromIdentifier extracts the kilometre point in metres that the
// TEFIVA identifier encodes as its trailing underscore field
// (HUELVA_N-433_71400.0 → 71400.0). An id with fewer than the three
// documented fields (province, road, PK), an empty field or a PK that is
// not a finite number does not match the pattern: ok is false and the
// caller serialises nulls, never a guess.
func pkMetresFromIdentifier(id string) (float64, bool) {
	parts := strings.Split(strings.TrimSpace(id), "_")
	if len(parts) < 3 || parts[0] == "" || parts[len(parts)-2] == "" {
		return 0, false
	}
	metres, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	if err != nil || math.IsNaN(metres) || math.IsInf(metres, 0) {
		return 0, false
	}
	return metres, true
}
