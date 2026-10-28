package datex2

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fixture expectations measured on testdata/feeds/situations (see
// testdata/feeds/README.md): 2 situations per observed causeType, 18
// situations and 24 records per snapshot, 17 linear + 7 point locations.
const (
	wantSituations  = 18
	wantRecords     = 24
	wantLinearLocs  = 17
	wantPointLocs   = 7
	wantPublishedAt = "2026-10-28T12:12:17.098Z" // feed 13:12:17.098+01:00, normalised to UTC
)

// publicationTime of each snapshot (docs/feeds.md §3), normalised to UTC.
var wantPublishedAtBySnapshot = map[string]string{
	"snap-1.xml": "2026-10-28T12:12:17.098Z",
	"snap-2.xml": "2026-10-28T12:13:17.099Z",
	"snap-3.xml": "2026-10-28T12:14:17.099Z",
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "feeds", "situations", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestParseSituations_Fixtures(t *testing.T) {
	for _, name := range []string{"snap-1.xml", "snap-2.xml", "snap-3.xml"} {
		t.Run(name, func(t *testing.T) {
			pub, err := ParseSituations(bytes.NewReader(fixture(t, name)))
			if err != nil {
				t.Fatalf("ParseSituations: %v", err)
			}

			if got := len(pub.Situations); got != wantSituations {
				t.Fatalf("situations = %d, want %d", got, wantSituations)
			}
			records := 0
			linear, point := 0, 0
			for _, sit := range pub.Situations {
				if sit.ID == "" {
					t.Errorf("situation with empty ID")
				}
				records += len(sit.Records)
				for _, rec := range sit.Records {
					switch rec.LocationType {
					case "SingleRoadLinearLocation":
						linear++
					case "PointLocation":
						point++
					default:
						t.Errorf("record %s: unexpected LocationType %q", rec.ID, rec.LocationType)
					}
					if rec.SituationID != sit.ID {
						t.Errorf("record %s: SituationID = %q, want %q", rec.ID, rec.SituationID, sit.ID)
					}
					if rec.RoadName == "" {
						t.Errorf("record %s: empty RoadName (docs/feeds.md §4.2 says 100%% coverage)", rec.ID)
					}
					if rec.Province == "" {
						t.Errorf("record %s: empty Province", rec.ID)
					}
					if rec.Municipality == "" {
						t.Errorf("record %s: empty Municipality", rec.ID)
					}
					if rec.AutonomousCommunity == "" {
						t.Errorf("record %s: empty AutonomousCommunity", rec.ID)
					}
					if rec.PK == nil {
						t.Errorf("record %s: nil PK", rec.ID)
					} else if rec.PK.From == nil {
						t.Errorf("record %s: nil PK.From (kilometerPoint is present in 100%% of points)", rec.ID)
					}
					if rec.Start == nil {
						t.Errorf("record %s: nil Start coordinates", rec.ID)
					}
					if rec.CreationTime.IsZero() {
						t.Errorf("record %s: zero CreationTime", rec.ID)
					}
					if rec.VersionTime.IsZero() {
						t.Errorf("record %s: zero VersionTime", rec.ID)
					}
					if rec.StartTime.IsZero() {
						t.Errorf("record %s: zero StartTime", rec.ID)
					}
					if rec.ValidityStatus == "" {
						t.Errorf("record %s: empty ValidityStatus", rec.ID)
					}
					if rec.LocationType == "SingleRoadLinearLocation" {
						if rec.PK.To == nil {
							t.Errorf("record %s: linear location with nil PK.To", rec.ID)
						}
						if rec.End == nil {
							t.Errorf("record %s: linear location with nil End coordinates", rec.ID)
						}
					}
				}
			}
			if records != wantRecords {
				t.Errorf("records = %d, want %d", records, wantRecords)
			}
			if linear != wantLinearLocs {
				t.Errorf("linear locations = %d, want %d", linear, wantLinearLocs)
			}
			if point != wantPointLocs {
				t.Errorf("point locations = %d, want %d", point, wantPointLocs)
			}

			want, err := time.Parse(time.RFC3339, wantPublishedAtBySnapshot[name])
			if err != nil {
				t.Fatalf("bad test constant: %v", err)
			}
			if !pub.PublishedAt.Equal(want) {
				t.Errorf("PublishedAt = %v, want %v", pub.PublishedAt, want)
			}
			if pub.PublishedAt.Location() != time.UTC {
				t.Errorf("PublishedAt location = %v, want UTC", pub.PublishedAt.Location())
			}
		})
	}
}

// The first record of snap-1.xml, verified by hand against the fixture.
func TestParseSituations_SpotCheckFirstRecord(t *testing.T) {
	pub, err := ParseSituations(bytes.NewReader(fixture(t, "snap-1.xml")))
	if err != nil {
		t.Fatalf("ParseSituations: %v", err)
	}
	rec := pub.Situations[0].Records[0]

	if rec.ID != "18811074" {
		t.Errorf("ID = %q, want 18811074", rec.ID)
	}
	if rec.SituationID != "2816645" {
		t.Errorf("SituationID = %q, want 2816645", rec.SituationID)
	}
	if rec.Version != "2" {
		t.Errorf("Version = %q, want 2", rec.Version)
	}
	if rec.RecordType != "RoadOrCarriagewayOrLaneManagement" {
		t.Errorf("RecordType = %q, want RoadOrCarriagewayOrLaneManagement (xsi:type without prefix)", rec.RecordType)
	}
	if !rec.CreationTime.Equal(time.Date(2025, 12, 12, 7, 11, 34, 0, time.UTC)) {
		t.Errorf("CreationTime = %v, want 2025-12-12T07:11:34Z", rec.CreationTime)
	}
	if !rec.VersionTime.Equal(time.Date(2025, 12, 12, 7, 11, 34, 0, time.UTC)) {
		t.Errorf("VersionTime = %v, want 2025-12-12T07:11:34Z", rec.VersionTime)
	}
	if rec.CauseType != "roadMaintenance" {
		t.Errorf("CauseType = %q, want roadMaintenance", rec.CauseType)
	}
	if rec.DetailedCauseTypeElement != "roadMaintenanceType" {
		t.Errorf("DetailedCauseTypeElement = %q, want roadMaintenanceType", rec.DetailedCauseTypeElement)
	}
	if rec.DetailedCauseTypeValue != "roadworks" {
		t.Errorf("DetailedCauseTypeValue = %q, want roadworks", rec.DetailedCauseTypeValue)
	}
	if rec.ValidityStatus != "active" {
		t.Errorf("ValidityStatus = %q, want active", rec.ValidityStatus)
	}
	if !rec.StartTime.Equal(time.Date(2025, 12, 12, 7, 5, 19, 0, time.UTC)) {
		t.Errorf("StartTime = %v, want 2025-12-12T07:05:19Z", rec.StartTime)
	}
	if rec.EndTime != nil {
		t.Errorf("EndTime = %v, want nil (record has no overallEndTime)", rec.EndTime)
	}
	if rec.RoadName != "N-400" {
		t.Errorf("RoadName = %q, want N-400", rec.RoadName)
	}
	// Direction is lse:tpegDirectionRoad (§4.4); the compass bearing is separate.
	if rec.Direction != "both" {
		t.Errorf("Direction = %q, want both", rec.Direction)
	}
	if rec.DirectionBearing != "unknown" {
		t.Errorf("DirectionBearing = %q, want unknown (loc:tpegDirection)", rec.DirectionBearing)
	}
	if len(rec.LaneUsages) != 1 || rec.LaneUsages[0] != "allLanesCompleteCarriageway" {
		t.Errorf("LaneUsages = %v, want [allLanesCompleteCarriageway]", rec.LaneUsages)
	}
	if rec.PK == nil || rec.PK.From == nil || rec.PK.To == nil {
		t.Fatalf("PK = %+v, want From=37.1 To=37.3", rec.PK)
	}
	if *rec.PK.From != 37.1 {
		t.Errorf("PK.From = %v, want 37.1", *rec.PK.From)
	}
	if *rec.PK.To != 37.3 {
		t.Errorf("PK.To = %v, want 37.3", *rec.PK.To)
	}
	if rec.Start == nil || rec.Start.Latitude != 39.993732 || rec.Start.Longitude != -3.6074991 {
		t.Errorf("Start = %+v, want {39.993732 -3.6074991}", rec.Start)
	}
	if rec.End == nil || rec.End.Latitude != 39.994446 || rec.End.Longitude != -3.6054852 {
		t.Errorf("End = %+v, want {39.994446 -3.6054852}", rec.End)
	}
	if rec.Province != "Toledo" {
		t.Errorf("Province = %q, want Toledo", rec.Province)
	}
	if rec.Municipality != "Ontígola" {
		t.Errorf("Municipality = %q, want Ontígola", rec.Municipality)
	}
	if rec.AutonomousCommunity != "Castilla - La Mancha" {
		t.Errorf("AutonomousCommunity = %q, want Castilla - La Mancha", rec.AutonomousCommunity)
	}
	if got := Default().Type(rec); got != "roadworks" {
		t.Errorf("Type = %q, want roadworks", got)
	}
}

// A point location: single point, no PK.To and no End coordinates.
func TestParseSituations_PointLocation(t *testing.T) {
	pub, err := ParseSituations(bytes.NewReader(fixture(t, "snap-1.xml")))
	if err != nil {
		t.Fatalf("ParseSituations: %v", err)
	}
	var rec *Record
outer:
	for i := range pub.Situations {
		for j := range pub.Situations[i].Records {
			if pub.Situations[i].Records[j].LocationType == "PointLocation" {
				rec = &pub.Situations[i].Records[j]
				break outer
			}
		}
	}
	if rec == nil {
		t.Fatal("no PointLocation record in first situation")
	}
	if rec.PK == nil || rec.PK.From == nil {
		t.Fatalf("PK = %+v, want From set from lse:kilometerPoint", rec.PK)
	}
	if rec.PK.To != nil {
		t.Errorf("PK.To = %v, want nil for a point location", *rec.PK.To)
	}
	if rec.End != nil {
		t.Errorf("End = %+v, want nil for a point location", rec.End)
	}
	if rec.Direction == "" {
		t.Error("Direction empty; lse:tpegDirectionRoad also lives in the point extension")
	}
}

// Severity precedence (docs/feeds.md §4.5): record severity wins, then the
// situation's overallSeverity, and it is never inferred from the cause.
// No fixture has overallSeverity without record severity, so this uses a
// minimal inline payload with the real feed shape.
func TestParseSituations_SeverityFallsBackToSituation(t *testing.T) {
	const body = `<?xml version="1.0" encoding="UTF-8"?>
<d2:payload xsi:type="sit:SituationPublication" xmlns:com="http://levelC/schema/3/common" xmlns:sit="http://levelC/schema/3/situation" xmlns:loc="http://levelC/schema/3/locationReferencing" xmlns:lse="http://levelC/schema/3/locationReferencingSpanishExtension" xmlns:d2="http://levelC/schema/3/d2Payload" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <com:publicationTime>2026-10-28T13:12:17.098+01:00</com:publicationTime>
  <sit:situation id="s1">
    <sit:overallSeverity>highest</sit:overallSeverity>
    <sit:situationRecord xsi:type="sit:Accident" id="r1" version="1">
      <sit:situationRecordCreationTime>2025-12-12T08:11:34.000+01:00</sit:situationRecordCreationTime>
      <sit:situationRecordVersionTime>2025-12-12T08:11:34.000+01:00</sit:situationRecordVersionTime>
      <sit:validity>
        <com:validityStatus>active</com:validityStatus>
        <com:validityTimeSpecification>
          <com:overallStartTime>2025-12-12T08:05:19.000+01:00</com:overallStartTime>
        </com:validityTimeSpecification>
      </sit:validity>
      <sit:cause><sit:causeType>accident</sit:causeType></sit:cause>
      <sit:locationReference xsi:type="loc:PointLocation">
        <loc:tpegPointLocation xsi:type="loc:TpegSimplePoint">
          <loc:point xsi:type="loc:TpegNonJunctionPoint">
            <loc:pointCoordinates><loc:latitude>1.0</loc:latitude><loc:longitude>2.0</loc:longitude></loc:pointCoordinates>
            <loc:_tpegNonJunctionPointExtension>
              <loc:extendedTpegNonJunctionPoint>
                <lse:province>Huesca</lse:province>
                <lse:municipality>Villanueva de Sigena</lse:municipality>
                <lse:kilometerPoint>1.2</lse:kilometerPoint>
              </loc:extendedTpegNonJunctionPoint>
            </loc:_tpegNonJunctionPointExtension>
          </loc:point>
          <loc:_tpegSimplePointExtension>
            <loc:extendedTpegSimplePoint><lse:tpegDirectionRoad>both</lse:tpegDirectionRoad></loc:extendedTpegSimplePoint>
          </loc:_tpegSimplePointExtension>
        </loc:tpegPointLocation>
      </sit:locationReference>
    </sit:situationRecord>
  </sit:situation>
</d2:payload>`

	pub, err := ParseSituations(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseSituations: %v", err)
	}
	if len(pub.Situations) != 1 || len(pub.Situations[0].Records) != 1 {
		t.Fatalf("got %d situations / %d records, want 1/1", len(pub.Situations), len(pub.Situations[0].Records))
	}
	rec := pub.Situations[0].Records[0]
	if rec.SeverityRaw != "highest" {
		t.Errorf("SeverityRaw = %q, want highest (from sit:overallSeverity)", rec.SeverityRaw)
	}
	if got := Default().Severity(rec.SeverityRaw); got != "high" {
		t.Errorf("Severity = %q, want high", got)
	}
	if got := Default().Type(rec); got != "accident" {
		t.Errorf("Type = %q, want accident", got)
	}
}
