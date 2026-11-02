package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aam9063/trafico-cli/internal/datex2"
)

// TestSectionFromInvive pins the INVIVE contract (docs/feeds.md §7): the
// kilometre points arrive as referencePointDistance in metres and are
// converted to kilometres, pk_source is "reference" (the numbers come from
// the reference points, not from an identifier), the kind is "invive", the
// province is the published administrative area name and the model version
// is the v1.0 dialect of the feed.
func TestSectionFromInvive(t *testing.T) {
	section := SectionFromInvive(datex2.InviveSection{
		ID:                "GUID_Tramo_Invive_1",
		Road:              "CM-220",
		Province:          "ALBACETE",
		DirectionRelative: "both",
		FromDistance:      floatPtr(17300.0),
		ToDistance:        floatPtr(34800.0),
	})

	if section.Kind != SectionKindInvive {
		t.Errorf("kind = %q, want %q", section.Kind, SectionKindInvive)
	}
	if section.ID != "dgt:GUID_Tramo_Invive_1" {
		t.Errorf("id = %q, want dgt:GUID_Tramo_Invive_1", section.ID)
	}
	if section.SourceVersion != "1.0" {
		t.Errorf("source_version = %q, want 1.0 (the v1.0 dialect)", section.SourceVersion)
	}
	if section.Road != "CM-220" {
		t.Errorf("road = %q, want CM-220", section.Road)
	}
	if section.PK == nil || section.PK.From == nil || *section.PK.From != 17.3 {
		t.Errorf("pk.from = %+v, want 17.3 km (17300.0 metres)", section.PK)
	}
	if section.PK == nil || section.PK.To == nil || *section.PK.To != 34.8 {
		t.Errorf("pk.to = %+v, want 34.8 km (34800.0 metres)", section.PK)
	}
	if section.PKSource == nil || *section.PKSource != PKSourceReference {
		t.Errorf("pk_source = %v, want %q", section.PKSource, PKSourceReference)
	}
	if section.Direction == nil || *section.Direction != "both" {
		t.Errorf("direction = %v, want both", section.Direction)
	}
	if section.Province == nil || *section.Province != "ALBACETE" {
		t.Errorf("province = %v, want the published administrative area name ALBACETE", section.Province)
	}
	if section.LengthKM != nil {
		t.Errorf("length_km = %v, want null (INVIVE publishes no lengthAffected)", section.LengthKM)
	}
	if section.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version = %d, want %d", section.SchemaVersion, SchemaVersion)
	}
}

// TestSectionFromInviveWithoutDistances pins that an INVIVE section the feed
// publishes without referencePointDistance serialises pk and pk_source as
// null: nothing is derived from thin air (S6).
func TestSectionFromInviveWithoutDistances(t *testing.T) {
	section := SectionFromInvive(datex2.InviveSection{ID: "GUID_Tramo_Invive_9", Road: "CM-220"})
	if section.PK != nil {
		t.Errorf("pk = %+v, want null", section.PK)
	}
	if section.PKSource != nil {
		t.Errorf("pk_source = %v, want null", section.PKSource)
	}
}

// TestSectionFromTefivaDerivesPKFromIdentifier pins the decided PK rule for
// TEFIVA (docs/feeds.md §8): the kilometre point exists only inside the id
// (PROVINCIA_CARRETERA_PK_EN_METROS), so pk.from is the trailing metres
// divided by 1000, pk.to is from + lengthAffected, and pk_source is
// "identifier" so the consumer can tell how trustworthy the number is. The
// province prefix of the id is never used for province: TEFIVA carries no
// INE code, so province stays null.
func TestSectionFromTefivaDerivesPKFromIdentifier(t *testing.T) {
	section := SectionFromTefiva(datex2.TefivaSection{
		ID:                "HUELVA_N-433_71400.0",
		Road:              "N-433",
		DirectionRelative: "both",
		LengthKM:          floatPtr(1.7),
	})

	if section.Kind != SectionKindAnimals {
		t.Errorf("kind = %q, want %q", section.Kind, SectionKindAnimals)
	}
	if section.ID != "dgt:HUELVA_N-433_71400.0" {
		t.Errorf("id = %q, want dgt:HUELVA_N-433_71400.0", section.ID)
	}
	if section.SourceVersion != "3" {
		t.Errorf("source_version = %q, want 3 (the v3 dialect)", section.SourceVersion)
	}
	if section.Road != "N-433" {
		t.Errorf("road = %q, want N-433 (the published descriptor, not the id)", section.Road)
	}
	if section.PK == nil || section.PK.From == nil || *section.PK.From != 71.4 {
		t.Errorf("pk.from = %+v, want 71.4 km (71400.0 metres from the id)", section.PK)
	}
	if section.PK == nil || section.PK.To == nil || *section.PK.To != 73.1 {
		t.Errorf("pk.to = %+v, want 73.1 km (71.4 + 1.7 lengthAffected)", section.PK)
	}
	if section.PKSource == nil || *section.PKSource != PKSourceIdentifier {
		t.Errorf("pk_source = %v, want %q", section.PKSource, PKSourceIdentifier)
	}
	if section.Province != nil {
		t.Errorf("province = %v, want null (TEFIVA carries no INE code and the id prefix is not a province)", section.Province)
	}
	if section.LengthKM == nil || *section.LengthKM != 1.7 {
		t.Errorf("length_km = %v, want 1.7", section.LengthKM)
	}
}

// TestSectionFromTefivaWithoutLength pins that a TEFIVA id matching the
// pattern without lengthAffected still derives pk.from and marks it
// "identifier", leaving pk.to null: the rule is per bound, never guessed.
func TestSectionFromTefivaWithoutLength(t *testing.T) {
	section := SectionFromTefiva(datex2.TefivaSection{ID: "HUELVA_N-433_71400.0", Road: "N-433"})
	if section.PK == nil || section.PK.From == nil || *section.PK.From != 71.4 {
		t.Errorf("pk.from = %+v, want 71.4", section.PK)
	}
	if section.PK == nil || section.PK.To != nil {
		t.Errorf("pk.to = %+v, want null without lengthAffected", section.PK)
	}
	if section.PKSource == nil || *section.PKSource != PKSourceIdentifier {
		t.Errorf("pk_source = %v, want %q", section.PKSource, PKSourceIdentifier)
	}
}

// TestSectionFromTefivaUnmatchedIdentifier pins the never-guess half of the
// PK decision: an id that does not match the documented
// PROVINCIA_CARRETERA_PK shape serialises pk and pk_source as null, in every
// malformed variant.
func TestSectionFromTefivaUnmatchedIdentifier(t *testing.T) {
	for _, id := range []string{"", "HUELVA", "N-433_71400.0", "HUELVA_N-433_sinpk", "HUELVA_N-433_"} {
		section := SectionFromTefiva(datex2.TefivaSection{ID: id, Road: "N-433", LengthKM: floatPtr(1.7)})
		if section.PK != nil {
			t.Errorf("id %q: pk = %+v, want null", id, section.PK)
		}
		if section.PKSource != nil {
			t.Errorf("id %q: pk_source = %v, want null", id, section.PKSource)
		}
	}
}

// TestSectionJSONKeys pins the wire keys of the contract: absent optional
// fields serialise as null, never omitted (S6), and pk_source is top level.
func TestSectionJSONKeys(t *testing.T) {
	section := SectionFromTefiva(datex2.TefivaSection{ID: "HUELVA_N-433_71400.0", Road: "N-433", LengthKM: floatPtr(1.7)})
	raw, err := json.Marshal(section)
	if err != nil {
		t.Fatalf("marshalling the section: %v", err)
	}
	for _, key := range []string{"schema_version", "id", "kind", "source", "source_version", "road", "pk", "pk_source", "direction", "province", "location", "length_km"} {
		if !strings.Contains(string(raw), `"`+key+`"`) {
			t.Errorf("the JSON contract is missing the key %q: %s", key, raw)
		}
	}
}
