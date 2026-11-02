package datex2

import (
	"os"
	"strings"
	"testing"
)

// sectionsFixture loads one of the real trimmed DGT snapshots (public CC BY
// data) so the tests exercise the actual dialects of docs/feeds.md §7 and §8.
func sectionsFixture(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return data
}

// TestParseInviveSectionsFixture pins the observed shape of the real INVIVE
// snapshot (docs/feeds.md §7): three trimmed v1.0 sections, every one a
// segment with from and to reference points, the PK distances in metres, the
// road from the linkName descriptor, the administrative area name and the
// relative direction of the primary reference point.
func TestParseInviveSectionsFixture(t *testing.T) {
	body := sectionsFixture(t, "../../testdata/feeds/invive/snap-1.xml")
	sections, err := ParseInviveSections(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("ParseInviveSections failed: %v", err)
	}
	if len(sections) != 3 {
		t.Fatalf("ParseInviveSections returned %d sections, want 3 (the fixture holds 3 of the 1331 published)", len(sections))
	}

	first := sections[0]
	if first.ID != "GUID_Tramo_Invive_1" {
		t.Errorf("section[0].ID = %q, want GUID_Tramo_Invive_1", first.ID)
	}
	if first.Road != "CM-220" {
		t.Errorf("section[0].Road = %q, want CM-220 (the linkName descriptor)", first.Road)
	}
	if first.FromDistance == nil || *first.FromDistance != 17300.0 {
		t.Errorf("section[0].FromDistance = %v, want 17300.0 metres (the primary/ini point)", first.FromDistance)
	}
	if first.ToDistance == nil || *first.ToDistance != 34800.0 {
		t.Errorf("section[0].ToDistance = %v, want 34800.0 metres (the secondary/fin point)", first.ToDistance)
	}
	if first.Start == nil || first.Start.Latitude != 39.25851 || first.Start.Longitude != -1.91995 {
		t.Errorf("section[0].Start = %+v, want the from-point coordinates", first.Start)
	}
	if first.End == nil || first.End.Latitude != 39.11986 || first.End.Longitude != -1.99897 {
		t.Errorf("section[0].End = %+v, want the to-point coordinates", first.End)
	}
	if first.Province != "ALBACETE" {
		t.Errorf("section[0].Province = %q, want the published administrative area name ALBACETE", first.Province)
	}
	if first.DirectionRelative != "both" {
		t.Errorf("section[0].DirectionRelative = %q, want the primary reference point's directionRelative", first.DirectionRelative)
	}

	second := sections[1]
	if second.ID != "GUID_Tramo_Invive_2" || second.Road != "N-301" {
		t.Errorf("section[1] = %s on %s, want GUID_Tramo_Invive_2 on N-301", second.ID, second.Road)
	}
	if second.FromDistance == nil || *second.FromDistance != 277510.0 || second.ToDistance == nil || *second.ToDistance != 300900.0 {
		t.Errorf("section[1] distances = %v/%v, want 277510.0/300900.0 metres", second.FromDistance, second.ToDistance)
	}
}

// TestParseTefivaSectionsFixture pins the observed shape of the real TEFIVA
// snapshot (docs/feeds.md §8): three trimmed v3 sections keyed by
// loc:predefinedLocationReference ids of the form
// PROVINCIA_CARRETERA_PK_EN_METROS, the affected length in kilometres and
// the from/to coordinates of the TpegJunction points.
func TestParseTefivaSectionsFixture(t *testing.T) {
	body := sectionsFixture(t, "../../testdata/feeds/tefiva/snap-1.xml")
	sections, err := ParseTefivaSections(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("ParseTefivaSections failed: %v", err)
	}
	if len(sections) != 3 {
		t.Fatalf("ParseTefivaSections returned %d sections, want 3 (the fixture holds 3 of the 150 published)", len(sections))
	}

	first := sections[0]
	if first.ID != "HUELVA_N-433_71400.0" {
		t.Errorf("section[0].ID = %q, want HUELVA_N-433_71400.0", first.ID)
	}
	if first.Road != "N-433" {
		t.Errorf("section[0].Road = %q, want N-433 (the ilc descriptor)", first.Road)
	}
	if first.LengthKM == nil || *first.LengthKM != 1.7 {
		t.Errorf("section[0].LengthKM = %v, want 1.7 km (lengthAffected)", first.LengthKM)
	}
	if first.Start == nil || first.Start.Latitude != 37.815742 || first.Start.Longitude != -6.436001 {
		t.Errorf("section[0].Start = %+v, want the from-point coordinates", first.Start)
	}
	if first.End == nil || first.End.Latitude != 37.82849 || first.End.Longitude != -6.442522 {
		t.Errorf("section[0].End = %+v, want the to-point coordinates", first.End)
	}
	if first.DirectionRelative != "both" {
		t.Errorf("section[0].DirectionRelative = %q, want the published directionRelativeOnLinearSection", first.DirectionRelative)
	}

	third := sections[2]
	if third.ID != "GRANADA_A-44_156400.0" || third.Road != "A-44" {
		t.Errorf("section[2] = %s on %s, want GRANADA_A-44_156400.0 on A-44", third.ID, third.Road)
	}
	if third.LengthKM == nil || *third.LengthKM != 1.6 {
		t.Errorf("section[2].LengthKM = %v, want 1.6", third.LengthKM)
	}
}

// TestParseInviveSectionsIdentityGuard pins the payload identity guard of
// the v1.0 parser: a body of another dialect (the v3 TEFIVA payload, the
// v3.7 situations payload) and a body of the wrong v1.0 feed (the radars
// inventory, whose set id is not GUID_Inventario_Tramos_Invive) are
// feed_changed-class errors, never an empty list.
func TestParseInviveSectionsIdentityGuard(t *testing.T) {
	for name, body := range map[string]string{
		"tefiva body":     string(sectionsFixture(t, "../../testdata/feeds/tefiva/snap-1.xml")),
		"radars body":     string(sectionsFixture(t, "../../testdata/feeds/radars/snap-1.xml")),
		"situations body": string(sectionsFixture(t, "../../testdata/feeds/situations/snap-1.xml")),
		"empty body":      "",
	} {
		sections, err := ParseInviveSections(strings.NewReader(body))
		if err == nil {
			t.Errorf("%s: ParseInviveSections accepted the body as %d sections, want a feed_changed-class error", name, len(sections))
		}
	}
}

// TestParseTefivaSectionsIdentityGuard pins the payload identity guard of
// the v3 parser: a body of another dialect (the v1.0 INVIVE payload, the
// v1.0 radars payload, the v3.7 situations payload) is a feed_changed-class
// error, never an empty list.
func TestParseTefivaSectionsIdentityGuard(t *testing.T) {
	for name, body := range map[string]string{
		"invive body":     string(sectionsFixture(t, "../../testdata/feeds/invive/snap-1.xml")),
		"radars body":     string(sectionsFixture(t, "../../testdata/feeds/radars/snap-1.xml")),
		"situations body": string(sectionsFixture(t, "../../testdata/feeds/situations/snap-1.xml")),
		"empty body":      "",
	} {
		sections, err := ParseTefivaSections(strings.NewReader(body))
		if err == nil {
			t.Errorf("%s: ParseTefivaSections accepted the body as %d sections, want a feed_changed-class error", name, len(sections))
		}
	}
}
