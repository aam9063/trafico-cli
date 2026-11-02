package datex2

import (
	"os"
	"strings"
	"testing"
)

// radarsFixture loads the real trimmed DGT radars snapshot (public CC BY
// data, docs/feeds.md §6) so the tests exercise the actual v1.0 dialect,
// including its default namespace and Spanish extensions.
func radarsFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/feeds/radars/snap-1.xml")
	if err != nil {
		t.Fatalf("reading the radars snapshot: %v", err)
	}
	return data
}

// TestParseRadarsFixture pins the observed shape of the 12 trimmed radars:
// 6 CVM segment radars (GUID_CVM_*) and 6 cabin radars
// (GUID_CABINACINEMOMETRO_*), in feed order, with the kilometre points in
// metres, the province as an INE code, and the real destination label.
func TestParseRadarsFixture(t *testing.T) {
	radars, err := ParseRadars(strings.NewReader(string(radarsFixture(t))))
	if err != nil {
		t.Fatalf("ParseRadars failed: %v", err)
	}
	if len(radars) != 12 {
		t.Fatalf("ParseRadars returned %d radars, want 12", len(radars))
	}
	segments, cabins := 0, 0
	for _, radar := range radars {
		switch {
		case strings.HasPrefix(radar.ID, "GUID_CVM_"):
			segments++
		case strings.HasPrefix(radar.ID, "GUID_CABINACINEMOMETRO_"):
			cabins++
		default:
			t.Errorf("radar id %q matches neither documented prefix", radar.ID)
		}
		if radar.ProvinceINE == "" {
			t.Errorf("radar %s carries no provinceINEIdentifier", radar.ID)
		}
	}
	if segments != 6 || cabins != 6 {
		t.Errorf("segments = %d, cabins = %d; want 6 and 6", segments, cabins)
	}

	first := radars[0]
	if first.ID != "GUID_CVM_161274" {
		t.Errorf("radar[0].ID = %q, want GUID_CVM_161274", first.ID)
	}
	if first.Road != "Z-40" {
		t.Errorf("radar[0].Road = %q, want Z-40 (the linkName descriptor)", first.Road)
	}
	if first.FromDistance == nil || *first.FromDistance != 26600.0 {
		t.Errorf("radar[0].FromDistance = %v, want 26600.0 metres (the primary/ini point)", first.FromDistance)
	}
	if first.ToDistance == nil || *first.ToDistance != 29700.0 {
		t.Errorf("radar[0].ToDistance = %v, want 29700.0 metres (the secondary/fin point)", first.ToDistance)
	}
	if first.Start == nil || first.Start.Latitude != 41.6088 || first.Start.Longitude != -0.915697 {
		t.Errorf("radar[0].Start = %+v, want the from-point coordinates", first.Start)
	}
	if first.End == nil || first.End.Latitude != 41.6192 || first.End.Longitude != -0.9496 {
		t.Errorf("radar[0].End = %+v, want the to-point coordinates", first.End)
	}
	if first.DirectionRelative != "positive" {
		t.Errorf("radar[0].DirectionRelative = %q, want positive", first.DirectionRelative)
	}
	if first.DirectionNamed != "MADRID" {
		t.Errorf("radar[0].DirectionNamed = %q, want MADRID", first.DirectionNamed)
	}
	if first.ProvinceINE != "50" {
		t.Errorf("radar[0].ProvinceINE = %q, want the INE code 50, not a province name", first.ProvinceINE)
	}
	if first.Singularity != "AUTOPISTA / AUTOVÍA" {
		t.Errorf("radar[0].Singularity = %q, want AUTOPISTA / AUTOVÍA", first.Singularity)
	}

	// A cabin is a point: one reference point, no fin distance, one end.
	cabin := radars[6]
	if cabin.ID != "GUID_CABINACINEMOMETRO_120093" {
		t.Errorf("radar[6].ID = %q, want GUID_CABINACINEMOMETRO_120093", cabin.ID)
	}
	if cabin.FromDistance == nil || *cabin.FromDistance != 422900.0 || cabin.ToDistance != nil {
		t.Errorf("cabin distances = %v/%v, want 422900.0 and none", cabin.FromDistance, cabin.ToDistance)
	}
	if cabin.Start == nil || cabin.Start.Latitude != 37.243286 || cabin.End != nil {
		t.Errorf("cabin location = %+v/%+v, want a start point and no end", cabin.Start, cabin.End)
	}
	if cabin.Road != "N-432" {
		t.Errorf("cabin road = %q, want N-432", cabin.Road)
	}
	if cabin.DirectionNamed != "" {
		t.Errorf("cabin directionNamed = %q, want empty (this cabin publishes none)", cabin.DirectionNamed)
	}

	// The feed publishes single-digit INE codes (Almería is "4").
	singleDigit := radars[10]
	if singleDigit.ID != "GUID_CABINACINEMOMETRO_120148" || singleDigit.ProvinceINE != "4" {
		t.Errorf("radar[10] = %s/%q, want GUID_CABINACINEMOMETRO_120148 with the raw code 4",
			singleDigit.ID, singleDigit.ProvinceINE)
	}
}

// minimalRadarsBody is a synthetic v1.0 PredefinedLocationsPublication with
// one segment radar, used to pin the payload identity guard against a
// genuine body of the right dialect. Its predefinedLocationSet id is one of
// the real radar inventory ids (docs/feeds.md §6): the identity guard
// accepts only the radar sets.
const minimalRadarsBody = `<?xml version="1.0" encoding="UTF-8"?>
<d2LogicalModel modelBaseVersion="1.0" xmlns="http://datex2.eu/schema/1_0/1_0" xmlns:_0="http://datex2.eu/schema/1_0/1_0" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <exchange>
    <supplierIdentification>
      <country>es</country>
      <nationalIdentifier>dgt</nationalIdentifier>
    </supplierIdentification>
  </exchange>
  <payloadPublication lang="es" xsi:type="_0:PredefinedLocationsPublication">
    <_0:predefinedLocationSet id="GUID_Inventario_CinemometrosVelocidadMedia">
      <_0:predefinedLocation id="GUID_CVM_1">
        <_0:predefinedLocation xsi:type="_0:Linear">
          <_0:tpeglinearLocation>
            <_0:from xsi:type="_0:TPEGNonJunctionPoint">
              <_0:pointCoordinates>
                <_0:latitude>41.6</_0:latitude>
                <_0:longitude>-0.9</_0:longitude>
              </_0:pointCoordinates>
              <_0:name>
                <_0:descriptor>
                  <_0:value>Z-40</_0:value>
                </_0:descriptor>
                <_0:tpegDescriptorType>linkName</_0:tpegDescriptorType>
              </_0:name>
            </_0:from>
          </_0:tpeglinearLocation>
          <_0:referencePointLinear>
            <_0:referencePointPrimaryLocation>
              <_0:referencePoint>
                <_0:referencePointIdentifier>CVM_1_ini</_0:referencePointIdentifier>
                <_0:roadName>
                  <_0:value lang="es">Z-40</_0:value>
                </_0:roadName>
                <_0:directionRelative>positive</_0:directionRelative>
                <_0:referencePointDistance>26600.0</_0:referencePointDistance>
                <_0:referencePointExtension>
                  <_0:ExtendedReferencePoint>
                    <_0:provinceINEIdentifier>50</_0:provinceINEIdentifier>
                  </_0:ExtendedReferencePoint>
                </_0:referencePointExtension>
              </_0:referencePoint>
            </_0:referencePointPrimaryLocation>
          </_0:referencePointLinear>
        </_0:predefinedLocation>
      </_0:predefinedLocation>
    </_0:predefinedLocationSet>
  </payloadPublication>
</d2LogicalModel>`

// TestParseRadarsAcceptsMinimalPublication pins that the identity check
// demands the dialect, not content: a genuine v1.0 publication with one
// radar parses with the road taken from the linkName descriptor, and a body
// whose point carries no linkName falls back to the primary roadName.
func TestParseRadarsAcceptsMinimalPublication(t *testing.T) {
	radars, err := ParseRadars(strings.NewReader(minimalRadarsBody))
	if err != nil {
		t.Fatalf("ParseRadars failed: %v", err)
	}
	if len(radars) != 1 {
		t.Fatalf("ParseRadars returned %d radars, want 1", len(radars))
	}
	if radars[0].Road != "Z-40" || radars[0].FromDistance == nil || *radars[0].FromDistance != 26600.0 {
		t.Errorf("radar[0] = %+v, want the road and the primary distance", radars[0])
	}
	if radars[0].ToDistance != nil || radars[0].End != nil {
		t.Errorf("radar[0] end = %v/%v, want none (the minimal body has no fin point)", radars[0].ToDistance, radars[0].End)
	}

	// The fallback is pinned with a body whose linkName is absent.
	noLinkName := strings.Replace(minimalRadarsBody,
		`<_0:name>
                <_0:descriptor>
                  <_0:value>Z-40</_0:value>
                </_0:descriptor>
                <_0:tpegDescriptorType>linkName</_0:tpegDescriptorType>
              </_0:name>`, "", 1)
	if noLinkName == minimalRadarsBody {
		t.Fatal("the test fixture edit did not remove the linkName block")
	}
	radars, err = ParseRadars(strings.NewReader(noLinkName))
	if err != nil {
		t.Fatalf("ParseRadars without a linkName failed: %v", err)
	}
	if radars[0].Road != "Z-40" {
		t.Errorf("road without a linkName descriptor = %q, want the primary roadName fallback Z-40", radars[0].Road)
	}
}

// TestParseRadarsRejectsForeignDialect pins the payload identity guard: a
// body from another DGT dialect (v3.7 situations, v3.7 sign locations, v3
// tefiva) must be an error, never a silent empty result — otherwise
// `trafico radars` would exit 0 with radars: [] instead of feed_changed.
func TestParseRadarsRejectsForeignDialect(t *testing.T) {
	for _, rel := range []string{
		"../../testdata/feeds/situations/snap-1.xml",
		"../../testdata/feeds/vms-locations/snap-1.xml",
		"../../testdata/feeds/tefiva/snap-1.xml",
	} {
		body, err := os.ReadFile(rel)
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
		if radars, err := ParseRadars(strings.NewReader(string(body))); err == nil || len(radars) != 0 {
			t.Errorf("%s: got %d radars, err %v; want a feed-changed error", rel, len(radars), err)
		}
	}
}

// TestParseRadarsRejectsInviveBody pins the strongest identity check: the
// INVIVE sections feed (docs/feeds.md §7) is the same v1.0 dialect with the
// same PredefinedLocationsPublication payload type, so only the
// predefinedLocationSet ids distinguish it from the radar inventory. Point
// the radars URL at the INVIVE body and this must still be a feed-changed
// error, never twelve phantom radars and exit 0.
func TestParseRadarsRejectsInviveBody(t *testing.T) {
	body, err := os.ReadFile("../../testdata/feeds/invive/snap-1.xml")
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if rs, err := ParseRadars(strings.NewReader(string(body))); err == nil || len(rs) != 0 {
		t.Errorf("got %d radars, err %v; want a feed-changed error", len(rs), err)
	}
}

func TestParseRadarsTruncatedBodyFails(t *testing.T) {
	body := radarsFixture(t)
	if _, err := ParseRadars(strings.NewReader(string(body[:len(body)/2]))); err == nil {
		t.Fatal("ParseRadars accepted a truncated body, want an error")
	}
}

func TestParseRadarsEmptyBodyFails(t *testing.T) {
	if _, err := ParseRadars(strings.NewReader("")); err == nil {
		t.Fatal("ParseRadars accepted an empty body, want an error")
	}
}

// TestParseRadarsRejectsOtherV1Publication pins that the identity guard
// reaches past the root element: a v1.0 body whose payloadPublication is
// not a PredefinedLocationsPublication is a changed feed, not an empty
// radar list.
func TestParseRadarsRejectsOtherV1Publication(t *testing.T) {
	body := strings.Replace(minimalRadarsBody,
		`xsi:type="_0:PredefinedLocationsPublication"`, `xsi:type="_0:SomethingElsePublication"`, 1)
	if radars, err := ParseRadars(strings.NewReader(body)); err == nil || len(radars) != 0 {
		t.Errorf("got %d radars, err %v; want a feed-changed error", len(radars), err)
	}
}
