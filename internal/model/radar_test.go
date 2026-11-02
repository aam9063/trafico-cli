package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aam9063/trafico-cli/internal/datex2"
)

func floatPtr(v float64) *float64 { return &v }

// TestRadarFromParsedSegment pins the wire shape of a segment radar
// (GUID_CVM_*, docs/feeds.md §6): the kilometre points converted from
// metres, the province resolved from the INE table, the real destination
// label and the both-ends location.
func TestRadarFromParsedSegment(t *testing.T) {
	parsed := datex2.Radar{
		ID:                "GUID_CVM_161274",
		Road:              "Z-40",
		FromDistance:      floatPtr(26600.0),
		ToDistance:        floatPtr(29700.0),
		Start:             &datex2.Coordinates{Latitude: 41.6088, Longitude: -0.915697},
		End:               &datex2.Coordinates{Latitude: 41.6192, Longitude: -0.9496},
		DirectionRelative: "positive",
		DirectionNamed:    "MADRID",
		ProvinceINE:       "50",
		Singularity:       "AUTOPISTA / AUTOVÍA",
	}
	radar := RadarFromParsed(parsed)
	if radar.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version = %d, want %d", radar.SchemaVersion, SchemaVersion)
	}
	if radar.ID != "dgt:GUID_CVM_161274" || radar.Source != Source || radar.SourceVersion != "1.0" {
		t.Errorf("id/source/source_version = %q/%q/%q, want dgt:GUID_CVM_161274/dgt/1.0", radar.ID, radar.Source, radar.SourceVersion)
	}
	if radar.Road != "Z-40" {
		t.Errorf("road = %q, want Z-40", radar.Road)
	}
	if radar.PK == nil || radar.PK.From == nil || *radar.PK.From != 26.6 || radar.PK.To == nil || *radar.PK.To != 29.7 {
		t.Errorf("pk = %+v, want from 26.6 to 29.7 (metres converted to kilometres)", radar.PK)
	}
	if radar.Direction == nil || *radar.Direction != "positive" {
		t.Errorf("direction = %v, want positive", radar.Direction)
	}
	if radar.DirectionLabel == nil || *radar.DirectionLabel != "MADRID" {
		t.Errorf("direction_label = %v, want MADRID", radar.DirectionLabel)
	}
	if radar.Province == nil || *radar.Province != "Zaragoza" {
		t.Errorf("province = %v, want Zaragoza (resolved from INE code 50)", radar.Province)
	}
	if radar.ProvinceCode == nil || *radar.ProvinceCode != "50" {
		t.Errorf("province_code = %v, want 50", radar.ProvinceCode)
	}
	if radar.Location.Start == nil || radar.Location.Start.Latitude != 41.6088 ||
		radar.Location.End == nil || radar.Location.End.Longitude != -0.9496 {
		t.Errorf("location = %+v, want both ends", radar.Location)
	}
	if radar.Singularity == nil || *radar.Singularity != "AUTOPISTA / AUTOVÍA" {
		t.Errorf("singularity = %v, want AUTOPISTA / AUTOVÍA", radar.Singularity)
	}
}

// TestRadarFromParsedTypeAndSpeedLimitStayNull pins the S6 rule of the radar
// feed (docs/feeds.md §6): the feed publishes no speed limit and no typed
// radar kind, so type and speed_limit are always null — but the JSON keys
// exist, so the contract stays stable if the DGT adds them.
func TestRadarFromParsedTypeAndSpeedLimitStayNull(t *testing.T) {
	radar := RadarFromParsed(datex2.Radar{ID: "GUID_X", Road: "A-7", ProvinceINE: "50"})
	if radar.Type != nil {
		t.Errorf("type = %v, want nil (the feed has no typed radar-kind field)", radar.Type)
	}
	if radar.SpeedLimit != nil {
		t.Errorf("speed_limit = %v, want nil (the feed does not publish it)", radar.SpeedLimit)
	}
	data, err := json.Marshal(radar)
	if err != nil {
		t.Fatalf("marshalling the radar: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshalling the radar: %v", err)
	}
	for _, key := range []string{"type", "speed_limit", "province", "province_code", "pk", "location", "direction_label", "singularity"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("the JSON contract is missing the key %q: %s", key, data)
		}
	}
	if doc["type"] != nil || doc["speed_limit"] != nil {
		t.Errorf("type/speed_limit = %v/%v, want both null", doc["type"], doc["speed_limit"])
	}
}

// TestRadarFromParsedUnknownINECodeIsNull pins that an INE code outside the
// table yields province null while province_code still carries the published
// value — never a guessed name.
func TestRadarFromParsedUnknownINECodeIsNull(t *testing.T) {
	radar := RadarFromParsed(datex2.Radar{ID: "GUID_X", Road: "A-7", ProvinceINE: "99"})
	if radar.Province != nil {
		t.Errorf("province = %v, want nil for the unknown code 99", radar.Province)
	}
	if radar.ProvinceCode == nil || *radar.ProvinceCode != "99" {
		t.Errorf("province_code = %v, want 99 (the feed publishes it)", radar.ProvinceCode)
	}
}

// TestRadarFromParsedPointRadar pins the cabin shape (GUID_CABINACINEMOMETRO_*,
// docs/feeds.md §6): one reference point only, so pk.to and location.end are
// null, and absent labels serialise as null.
func TestRadarFromParsedPointRadar(t *testing.T) {
	parsed := datex2.Radar{
		ID:                "GUID_CABINACINEMOMETRO_120093",
		Road:              "N-432",
		FromDistance:      floatPtr(422900.0),
		Start:             &datex2.Coordinates{Latitude: 37.243286, Longitude: -3.739078},
		DirectionRelative: "positive",
		ProvinceINE:       "18",
	}
	radar := RadarFromParsed(parsed)
	if radar.PK == nil || radar.PK.From == nil || *radar.PK.From != 422.9 || radar.PK.To != nil {
		t.Errorf("pk = %+v, want from 422.9 and to null (a cabin is a point)", radar.PK)
	}
	if radar.Location.Start == nil || radar.Location.End != nil {
		t.Errorf("location = %+v, want start only", radar.Location)
	}
	if radar.DirectionLabel != nil {
		t.Errorf("direction_label = %v, want nil (the feed publishes none here)", radar.DirectionLabel)
	}
	if radar.Singularity != nil {
		t.Errorf("singularity = %v, want nil", radar.Singularity)
	}
	if radar.Province == nil || *radar.Province != "Granada" {
		t.Errorf("province = %v, want Granada (INE 18, single-digit input tolerated)", radar.Province)
	}
}

// TestRadarFromParsedNormalisesRoad pins that the road is normalised with
// the shared road rules (docs/feeds.md §4.7), the same as every command.
func TestRadarFromParsedNormalisesRoad(t *testing.T) {
	radar := RadarFromParsed(datex2.Radar{ID: "GUID_X", Road: "z 40", ProvinceINE: "50"})
	if radar.Road != "Z-40" {
		t.Errorf("road = %q, want the normalised Z-40", radar.Road)
	}
}

// TestRadarFromParsedUnknownDirectionIsNull pins that a directionRelative
// outside the documented set serialises as null instead of the raw string.
func TestRadarFromParsedUnknownDirectionIsNull(t *testing.T) {
	radar := RadarFromParsed(datex2.Radar{ID: "GUID_X", Road: "A-7", DirectionRelative: "northBound"})
	if radar.Direction != nil {
		t.Errorf("direction = %v, want nil", radar.Direction)
	}
}

// TestRadarFromParsedPreservesReversedPK pins that the kilometre points
// are preserved unordered: some segment radars publish the fin reference
// point at a lower distance than the ini one (CVM_163929: ini 2650 m, fin
// 1400 m), and the faithful reading keeps that as-is instead of reordering
// — the radar feed, unlike the incident feed, does not guarantee
// from <= to (docs/feeds.md §6).
func TestRadarFromParsedPreservesReversedPK(t *testing.T) {
	radar := RadarFromParsed(datex2.Radar{
		ID:           "GUID_CVM_163929",
		Road:         "A-62",
		FromDistance: floatPtr(2650.0),
		ToDistance:   floatPtr(1400.0),
	})
	if radar.PK == nil || radar.PK.From == nil || *radar.PK.From != 2.65 || radar.PK.To == nil || *radar.PK.To != 1.4 {
		t.Errorf("pk = %+v, want from 2.65 to 1.4 unchanged, not reordered", radar.PK)
	}
}

// TestRadarFromParsedDropsLabelOnlyOnFinPoint pins the primary-authority
// rule end to end: the datex2 parser reads directionNamed from the primary
// (ini) reference point only, so a segment radar whose destination label is
// published only on the fin point (CVM_164860 publishes GUARDO there)
// arrives here with no label and serialises direction_label null — the
// secondary's label is never merged, and direction still carries the
// primary's relative value.
func TestRadarFromParsedDropsLabelOnlyOnFinPoint(t *testing.T) {
	radar := RadarFromParsed(datex2.Radar{
		ID:                "GUID_CVM_164860",
		Road:              "AV-100",
		FromDistance:      floatPtr(1000.0),
		ToDistance:        floatPtr(2000.0),
		DirectionRelative: "negative",
	})
	if radar.Direction == nil || *radar.Direction != "negative" {
		t.Errorf("direction = %v, want negative (the primary point's value)", radar.Direction)
	}
	if radar.DirectionLabel != nil {
		t.Errorf("direction_label = %v, want nil: a label carried only by the fin point never reaches the model", radar.DirectionLabel)
	}
}

// TestRadarJSONProvinceAndCodeBothSerialised pins via raw JSON that a radar
// carries province and province_code side by side and that location keeps
// its start/end keys even when a bound is null.
func TestRadarJSONProvinceAndCodeBothSerialised(t *testing.T) {
	radar := RadarFromParsed(datex2.Radar{ID: "GUID_X", Road: "A-7", ProvinceINE: "50", FromDistance: floatPtr(1000)})
	data, err := json.Marshal(radar)
	if err != nil {
		t.Fatalf("marshalling the radar: %v", err)
	}
	for _, fragment := range []string{`"province":"Zaragoza"`, `"province_code":"50"`, `"start"`, `"end"`, `"pk":`} {
		if !strings.Contains(string(data), fragment) {
			t.Errorf("the JSON output is missing %s: %s", fragment, data)
		}
	}
}
