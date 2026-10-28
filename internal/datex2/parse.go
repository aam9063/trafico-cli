package datex2

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

// Namespaces of the DATEX II v3.7 DGT RTTI profile (docs/feeds.md §1.1, §4).
// Matching is always on the URI, never on the prefix: the feed binds sit:,
// com:, loc: and lse: today, but prefixes are arbitrary per payload.
const (
	nsCommon     = "http://levelC/schema/3/common"
	nsSituation  = "http://levelC/schema/3/situation"
	nsLocation   = "http://levelC/schema/3/locationReferencing"
	nsSpanishExt = "http://levelC/schema/3/locationReferencingSpanishExtension"
	nsXSI        = "http://www.w3.org/2001/XMLSchema-instance"
)

// The raw structs below mirror only the parts of the XML this package needs.
// They are unexported: the public surface is Record/Situation/Publication.

// xmlString captures the chardata of a simple leaf element.
type xmlString struct {
	Value string `xml:",chardata"`
}

// xmlSituation mirrors <sit:situation>.
type xmlSituation struct {
	ID              string               `xml:"id,attr"`
	OverallSeverity string               `xml:"http://levelC/schema/3/situation overallSeverity"`
	Records         []xmlSituationRecord `xml:"http://levelC/schema/3/situation situationRecord"`
}

// xmlSituationRecord mirrors <sit:situationRecord>. The xsi:type attribute
// (e.g. "sit:Accident") selects the record class; the cause child elements
// are read generically because the detailed cause element name varies.
type xmlSituationRecord struct {
	ID           string      `xml:"id,attr"`
	Version      string      `xml:"version,attr"`
	Type         string      `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	CreationTime string      `xml:"http://levelC/schema/3/situation situationRecordCreationTime"`
	VersionTime  string      `xml:"http://levelC/schema/3/situation situationRecordVersionTime"`
	Severity     string      `xml:"http://levelC/schema/3/situation severity"`
	Validity     xmlValidity `xml:"http://levelC/schema/3/situation validity"`
	Cause        xmlCause    `xml:"http://levelC/schema/3/situation cause"`
	// The element is prefixed sit: in the DGT payload: it lives in the
	// situation namespace, while its children live in the location one.
	Location xmlLocRef `xml:"http://levelC/schema/3/situation locationReference"`
}

// xmlValidity mirrors <sit:validity>; the status and window live in the
// common namespace.
type xmlValidity struct {
	Status   string            `xml:"http://levelC/schema/3/common validityStatus"`
	TimeSpec xmlValidityWindow `xml:"http://levelC/schema/3/common validityTimeSpecification"`
}

type xmlValidityWindow struct {
	StartTime string `xml:"http://levelC/schema/3/common overallStartTime"`
	EndTime   string `xml:"http://levelC/schema/3/common overallEndTime"`
}

// xmlNamedValue records the name and text of an element discovered
// generically: <sit:detailedCauseType> wraps a varying child such as
// <sit:roadMaintenanceType>roadworks</sit:roadMaintenanceType>.
type xmlNamedValue struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

type xmlCause struct {
	CauseType string           `xml:"http://levelC/schema/3/situation causeType"`
	Detailed  xmlDetailedCause `xml:"http://levelC/schema/3/situation detailedCauseType"`
}

// xmlDetailedCause wraps the varying child of <sit:detailedCauseType> (e.g.
// <sit:roadMaintenanceType>): the element name and text carry the cause.
type xmlDetailedCause struct {
	Child xmlNamedValue `xml:",any"`
}

// xmlLocRef mirrors <sit:locationReference xsi:type="...">. Exactly one of
// Linear or Point is set, according to the xsi:type.
type xmlLocRef struct {
	Type          string             `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	Supplementary xmlSupplementary   `xml:"http://levelC/schema/3/locationReferencing supplementaryPositionalDescription"`
	Linear        *xmlLinearLocation `xml:"http://levelC/schema/3/locationReferencing tpegLinearLocation"`
	Point         *xmlPointLocation  `xml:"http://levelC/schema/3/locationReferencing tpegPointLocation"`
}

type xmlSupplementary struct {
	Carriageway xmlCarriageway     `xml:"http://levelC/schema/3/locationReferencing carriageway"`
	RoadInfo    xmlRoadInformation `xml:"http://levelC/schema/3/locationReferencing roadInformation"`
}

type xmlCarriageway struct {
	Lane xmlLane `xml:"http://levelC/schema/3/locationReferencing lane"`
}

type xmlLane struct {
	LaneUsages []string `xml:"http://levelC/schema/3/locationReferencing laneUsage"`
}

type xmlRoadInformation struct {
	RoadName string `xml:"http://levelC/schema/3/locationReferencing roadName"`
}

// xmlLinearLocation mirrors loc:tpegLinearLocation inside a
// SingleRoadLinearLocation: a from/to segment. The road direction lives in
// the Spanish extension, three levels down.
type xmlLinearLocation struct {
	Bearing string               `xml:"http://levelC/schema/3/locationReferencing tpegDirection"`
	To      *xmlNonJunctionPoint `xml:"http://levelC/schema/3/locationReferencing to"`
	From    *xmlNonJunctionPoint `xml:"http://levelC/schema/3/locationReferencing from"`
	Ext     xmlLinearExt         `xml:"http://levelC/schema/3/locationReferencing _tpegLinearLocationExtension"`
}

type xmlLinearExt struct {
	Extended xmlExtendedLinear `xml:"http://levelC/schema/3/locationReferencing extendedTpegLinearLocation"`
}

type xmlExtendedLinear struct {
	DirectionRoad string `xml:"http://levelC/schema/3/locationReferencingSpanishExtension tpegDirectionRoad"`
}

// xmlPointLocation mirrors loc:tpegPointLocation inside a PointLocation: a
// single point with the same Spanish extension for the road direction.
type xmlPointLocation struct {
	Bearing string               `xml:"http://levelC/schema/3/locationReferencing tpegDirection"`
	Point   *xmlNonJunctionPoint `xml:"http://levelC/schema/3/locationReferencing point"`
	Ext     xmlSimplePointExt    `xml:"http://levelC/schema/3/locationReferencing _tpegSimplePointExtension"`
}

type xmlSimplePointExt struct {
	Extended xmlExtendedSimplePoint `xml:"http://levelC/schema/3/locationReferencing extendedTpegSimplePoint"`
}

type xmlExtendedSimplePoint struct {
	DirectionRoad string `xml:"http://levelC/schema/3/locationReferencingSpanishExtension tpegDirectionRoad"`
}

// xmlNonJunctionPoint is shared by loc:to, loc:from and loc:point: the
// coordinates plus the Spanish extension with PK, province and municipality.
type xmlNonJunctionPoint struct {
	Coords    xmlCoordinates            `xml:"http://levelC/schema/3/locationReferencing pointCoordinates"`
	Extension xmlNonJunctionPointExtWrp `xml:"http://levelC/schema/3/locationReferencing _tpegNonJunctionPointExtension"`
}

type xmlNonJunctionPointExtWrp struct {
	Extended xmlExtendedNonJunctionPoint `xml:"http://levelC/schema/3/locationReferencing extendedTpegNonJunctionPoint"`
}

type xmlExtendedNonJunctionPoint struct {
	AutonomousCommunity string   `xml:"http://levelC/schema/3/locationReferencingSpanishExtension autonomousCommunity"`
	KilometerPoint      *float64 `xml:"http://levelC/schema/3/locationReferencingSpanishExtension kilometerPoint"`
	Municipality        string   `xml:"http://levelC/schema/3/locationReferencingSpanishExtension municipality"`
	Province            string   `xml:"http://levelC/schema/3/locationReferencingSpanishExtension province"`
}

type xmlCoordinates struct {
	Latitude  float64 `xml:"http://levelC/schema/3/locationReferencing latitude"`
	Longitude float64 `xml:"http://levelC/schema/3/locationReferencing longitude"`
}

// ParseSituations streams a DGT incident payload (sit:SituationPublication,
// DATEX II v3.7) into Publication values. It reads token by token and only
// materialises one situation at a time: the payload is ~7 MB and is decoded
// once per minute (docs/feeds.md §10). It does not filter, does not build
// the output model and does not fetch anything.
//
// publicationTime is mandatory here: it is the only valid freshness source,
// and an empty or truncated body must fail loudly rather than look like a
// payload with zero incidents.
func ParseSituations(r io.Reader) (*Publication, error) {
	d := xml.NewDecoder(r)
	pub := &Publication{}

	var rootName xml.Name
	seenRoot, rootClosed := false, false

	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("datex2: reading XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !seenRoot {
				rootName, seenRoot = t.Name, true
			}
			switch {
			case t.Name.Space == nsCommon && t.Name.Local == "publicationTime":
				var s xmlString
				if err := d.DecodeElement(&s, &t); err != nil {
					return nil, fmt.Errorf("datex2: decoding publicationTime: %w", err)
				}
				tm, err := parseFeedTime(s.Value, "com:publicationTime")
				if err != nil {
					return nil, err
				}
				pub.PublishedAt = tm
			case t.Name.Space == nsSituation && t.Name.Local == "situation":
				var s xmlSituation
				if err := d.DecodeElement(&s, &t); err != nil {
					return nil, fmt.Errorf("datex2: decoding situation: %w", err)
				}
				sit, err := convertSituation(s)
				if err != nil {
					return nil, err
				}
				pub.Situations = append(pub.Situations, sit)
			}
		case xml.EndElement:
			if seenRoot && t.Name == rootName {
				rootClosed = true
			}
		}
	}

	switch {
	case !seenRoot:
		return nil, fmt.Errorf("datex2: empty or non-XML body")
	case !rootClosed:
		return nil, fmt.Errorf("datex2: truncated XML document (root %q not closed)", rootName.Local)
	case pub.PublishedAt.IsZero():
		return nil, fmt.Errorf("datex2: payload has no com:publicationTime")
	}
	return pub, nil
}

func convertSituation(s xmlSituation) (Situation, error) {
	sit := Situation{
		ID:              s.ID,
		OverallSeverity: s.OverallSeverity,
		Records:         make([]Record, 0, len(s.Records)),
	}
	for _, raw := range s.Records {
		rec, err := convertRecord(raw)
		if err != nil {
			return Situation{}, fmt.Errorf("datex2: situation %s record %s: %w", s.ID, raw.ID, err)
		}
		rec.SituationID = s.ID
		// Severity precedence (docs/feeds.md §4.5): the record's own
		// <sit:severity>, then the situation's <sit:overallSeverity>.
		// Never inferred from the cause.
		rec.SeverityRaw = firstNonEmpty(raw.Severity, s.OverallSeverity)
		sit.Records = append(sit.Records, rec)
	}
	return sit, nil
}

func convertRecord(raw xmlSituationRecord) (Record, error) {
	creation, err := parseFeedTime(raw.CreationTime, "situationRecordCreationTime")
	if err != nil {
		return Record{}, err
	}
	version, err := parseFeedTime(raw.VersionTime, "situationRecordVersionTime")
	if err != nil {
		return Record{}, err
	}
	start, err := parseFeedTime(raw.Validity.TimeSpec.StartTime, "overallStartTime")
	if err != nil {
		return Record{}, err
	}
	var end *time.Time
	if t := raw.Validity.TimeSpec.EndTime; strings.TrimSpace(t) != "" {
		parsed, err := parseFeedTime(t, "overallEndTime")
		if err != nil {
			return Record{}, err
		}
		end = &parsed
	}

	rec := Record{
		ID:             raw.ID,
		Version:        raw.Version,
		RecordType:     localName(raw.Type),
		CreationTime:   creation,
		VersionTime:    version,
		ValidityStatus: raw.Validity.Status,
		StartTime:      start,
		EndTime:        end,
		CauseType:      raw.Cause.CauseType,
		// The detailed cause child is namespaced; keep only the local name,
		// which is what mapping.yaml keys on.
		DetailedCauseTypeElement: raw.Cause.Detailed.Child.XMLName.Local,
		DetailedCauseTypeValue:   strings.TrimSpace(raw.Cause.Detailed.Child.Value),
		RoadName:                 raw.Location.Supplementary.RoadInfo.RoadName,
		DirectionBearing:         strings.TrimSpace(firstBearing(raw.Location)),
	}
	rec.LaneUsages = append(rec.LaneUsages, raw.Location.Supplementary.Carriageway.Lane.LaneUsages...)
	applyLocation(&rec, raw.Location)
	return rec, nil
}

// applyLocation fills the spatial fields from whichever encoding the record
// uses (docs/feeds.md §4.8): a from/to segment or a single point. Both carry
// the same Spanish extension.
func applyLocation(rec *Record, loc xmlLocRef) {
	rec.LocationType = localName(loc.Type)
	switch {
	case loc.Linear != nil:
		from, to := loc.Linear.From, loc.Linear.To
		rec.Direction = firstNonEmpty(loc.Linear.Ext.Extended.DirectionRoad)
		rec.PK = pkFromPoints(from, to)
		rec.Start = coordinatesOf(from)
		rec.End = coordinatesOf(to)
		// The extension repeats per point; the from point is authoritative,
		// with the to point as fallback.
		for _, p := range []*xmlNonJunctionPoint{from, to} {
			if p == nil {
				continue
			}
			ext := p.Extension.Extended
			rec.Province = firstNonEmpty(rec.Province, ext.Province)
			rec.Municipality = firstNonEmpty(rec.Municipality, ext.Municipality)
			rec.AutonomousCommunity = firstNonEmpty(rec.AutonomousCommunity, ext.AutonomousCommunity)
		}
	case loc.Point != nil:
		p := loc.Point.Point
		rec.Direction = loc.Point.Ext.Extended.DirectionRoad
		if p != nil {
			rec.PK = pkFromPoints(p, nil)
			rec.Start = coordinatesOf(p)
			ext := p.Extension.Extended
			rec.Province = ext.Province
			rec.Municipality = ext.Municipality
			rec.AutonomousCommunity = ext.AutonomousCommunity
		}
	}
}

func pkFromPoints(from, to *xmlNonJunctionPoint) *PK {
	if from == nil || from.Extension.Extended.KilometerPoint == nil {
		return nil
	}
	pk := &PK{From: from.Extension.Extended.KilometerPoint}
	if to != nil {
		pk.To = to.Extension.Extended.KilometerPoint
	}
	return pk
}

func coordinatesOf(p *xmlNonJunctionPoint) *Coordinates {
	if p == nil {
		return nil
	}
	return &Coordinates{Latitude: p.Coords.Latitude, Longitude: p.Coords.Longitude}
}

func firstBearing(loc xmlLocRef) string {
	switch {
	case loc.Linear != nil:
		return loc.Linear.Bearing
	case loc.Point != nil:
		return loc.Point.Bearing
	default:
		return ""
	}
}

// parseFeedTime parses a DATEX II timestamp (RFC 3339 with fractional
// seconds and a numeric offset) and normalises it to UTC.
func parseFeedTime(s, field string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("datex2: invalid %s value %q: %w", field, s, err)
	}
	return t.UTC(), nil
}

// localName strips an arbitrary namespace prefix ("sit:Accident" ->
// "Accident"); attribute values are not namespace-resolved by encoding/xml,
// so the prefix in xsi:type values is whatever the payload used.
func localName(v string) string {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		return v[i+1:]
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
