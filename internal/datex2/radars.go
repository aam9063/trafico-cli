package datex2

// Parser of the fixed-radar feed of docs/feeds.md §6 (radares), the second
// DATEX II dialect in this package: DATEX II **v1.0**, root
// <d2LogicalModel modelBaseVersion="1.0"> with the default namespace
// http://datex2.eu/schema/1_0/1_0 and elements predefinedLocationSet,
// predefinedLocation and referencePoint — nothing like the two v3-family
// dialects of parse.go and signs.go, which stay untouched.
//
// As everywhere in this package, namespace matching is on the URI, never on
// the prefix: the payload re-declares the same namespace under an arbitrary
// prefix (_0: in the real feed) while the root uses it as the default.

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// nsDatexV1 is the DATEX II v1.0 namespace of the radar feed
// (docs/feeds.md §6).
const nsDatexV1 = "http://datex2.eu/schema/1_0/1_0"

// Radar is one radar of the v1.0 PredefinedLocationsPublication: a segment
// (ini–fin, a GUID_CVM_* cinemómetro de velocidad media) or a point (a
// GUID_CABINACINEMOMETRO_* cabin). Every raw value is preserved as
// published (P-04): the kilometre point is the raw referencePointDistance
// in metres — the kilometre conversion is the model's job — the province is
// the raw INE code, not a name, and absent optional fields stay empty or
// nil. The feed carries no speed limit and no typed radar-kind field
// (docs/feeds.md §6): only the id prefix distinguishes a cabin from a
// segment radar, and no type is derived here.
type Radar struct {
	// ID is the @id of the outer predefinedLocation ("GUID_CVM_161274").
	ID string
	// Road is the road name: the linkName descriptor of the tpeg point
	// names (docs/feeds.md §7: the name travels in name/descriptor/value
	// with tpegDescriptorType), falling back to the primary reference
	// point's roadName and then roadNumber.
	Road string
	// FromDistance and ToDistance are the raw referencePointDistance values
	// in metres of the primary (ini) and secondary (fin) reference points.
	// ToDistance is nil for a cabin, which publishes a single point.
	FromDistance *float64
	ToDistance   *float64
	// Start and End are the coordinates of the tpeg from and to points;
	// End is nil for a cabin.
	Start *Coordinates
	End   *Coordinates
	// DirectionRelative is the primary reference point's
	// directionRelative: positive or negative (docs/feeds.md §6).
	DirectionRelative string
	// DirectionNamed is the primary reference point's directionNamed: the
	// real destination label ("MADRID"), empty when the feed publishes
	// none.
	DirectionNamed string
	// ProvinceINE is the raw provinceINEIdentifier ("50"), a code, never a
	// name.
	ProvinceINE string
	// Singularity is the published road kind ("AUTOPISTA / AUTOVÍA",
	// "TÚNEL", ...), empty when absent.
	Singularity string
}

// The raw structs below mirror only the parts of the v1.0 payload this
// package needs. They are unexported: the public surface is Radar.

type xmlRadarLocation struct {
	ID    string          `xml:"id,attr"`
	Inner xmlRadarRadarIn `xml:"predefinedLocation"`
}

// xmlRadarRadarIn is the inner predefinedLocation, typed Linear (a segment)
// or Point (a cabin) by its xsi:type. The reference points hang directly
// off it for a point and under referencePointLinear for a segment.
type xmlRadarRadarIn struct {
	XsiType   string                  `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	Linear    *xmlRadarLinearLocation `xml:"tpeglinearLocation"`
	PointLoc  *xmlRadarPointLocation  `xml:"tpegpointLocation"`
	RefLinear *xmlRadarRefPointLinear `xml:"referencePointLinear"`
	RefPoint  *xmlRadarReferencePoint `xml:"referencePoint"`
}

type xmlRadarLinearLocation struct {
	To   *xmlRadarTpegPoint `xml:"to"`
	From *xmlRadarTpegPoint `xml:"from"`
}

type xmlRadarPointLocation struct {
	Point *xmlRadarTpegPoint `xml:"point"`
}

type xmlRadarTpegPoint struct {
	Coords *xmlRadarCoordinates `xml:"pointCoordinates"`
	Names  []xmlRadarName       `xml:"name"`
}

type xmlRadarCoordinates struct {
	Latitude  float64 `xml:"latitude"`
	Longitude float64 `xml:"longitude"`
}

type xmlRadarName struct {
	Value          string `xml:"descriptor>value"`
	DescriptorType string `xml:"tpegDescriptorType"`
}

type xmlRadarRefPointLinear struct {
	Primary   *xmlRadarReferencePoint `xml:"referencePointPrimaryLocation>referencePoint"`
	Secondary *xmlRadarReferencePoint `xml:"referencePointSecondaryLocation>referencePoint"`
}

type xmlRadarReferencePoint struct {
	Identifier        string               `xml:"referencePointIdentifier"`
	RoadName          string               `xml:"roadName>value"`
	RoadNumber        string               `xml:"roadNumber"`
	DirectionRelative string               `xml:"directionRelative"`
	Distance          *float64             `xml:"referencePointDistance"`
	Extension         xmlRadarRefExtension `xml:"referencePointExtension"`
}

type xmlRadarRefExtension struct {
	Extended xmlRadarExtendedRef `xml:"ExtendedReferencePoint"`
}

type xmlRadarExtendedRef struct {
	DirectionNamed string `xml:"directionNamed"`
	ProvinceINE    string `xml:"provinceINEIdentifier"`
	RoadIdentifier string `xml:"roadIdentifier"`
	Singularity    string `xml:"singularity"`
}

// radarLocationSetIDs is the named allowlist of the predefinedLocationSet
// ids the DGT fixed-radar inventory publishes (docs/feeds.md §6):
// GUID_Inventario_CinemometrosVelocidadMedia (segments and, as published,
// the cabins too) and GUID_Inventario_CabinasCinemometro (empty in the
// observed payload). Matching on the set id is payload identity, not field
// derivation: it only decides whether the body IS the radar inventory, it
// never derives anything about an individual radar — the earlier decision
// not to infer a radar `type` from an id prefix stands. The check exists
// because the INVIVE sections feed (docs/feeds.md §7) is the same v1.0
// dialect with the same publication type; only the set id tells them
// apart, and without it an INVIVE body would parse as phantom radars and
// `trafico radars` would exit 0 instead of feed_changed.
var radarLocationSetIDs = map[string]bool{
	"GUID_Inventario_CinemometrosVelocidadMedia": true,
	"GUID_Inventario_CabinasCinemometro":         true,
}

// ParseRadars streams a DGT fixed-radar payload (a v1.0
// PredefinedLocationsPublication, docs/feeds.md §6) into Radar values, in
// feed order. It reads token by token and decodes one outer
// predefinedLocation at a time, like the other parsers of this package.
// It does not fetch, filter or convert anything.
//
// Like ParseVmsDevices, it validates the payload identity before returning
// (the review discipline of FEED-03): the root must be a v1.0
// d2LogicalModel, the payload must be a PredefinedLocationsPublication,
// and at least one predefinedLocationSet must carry one of the radar
// inventory ids (radarLocationSetIDs). A body from another DGT dialect —
// the v3.7 situations payload, a v3 tefiva body, a v1.0 body of another
// publication, the v1.0 INVIVE sections — carries no radar
// predefinedLocationSet, so without the check it would parse as zero (or
// phantom) radars and no error, and `trafico radars` would exit 0 with
// radars: [] instead of feed_changed (exit 7).
//
// The publicationTime the payload may carry is deliberately not read here:
// docs/feeds.md §6 and §9 fix this feed's published_at as null because the
// value is not usable (a static inventory whose timestamp is frozen months
// behind the measurement window), and freshness must never be invented.
func ParseRadars(r io.Reader) ([]Radar, error) {
	d := xml.NewDecoder(r)
	var radars []Radar

	var rootName xml.Name
	seenRoot, rootClosed := false, false
	payloadType := ""
	seenPayloadPublication := false
	var locationSetIDs []string
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
			if t.Name.Space == nsDatexV1 && t.Name.Local == "payloadPublication" {
				seenPayloadPublication = true
				payloadType = rootPublicationType(t.Attr)
			}
			if t.Name.Space == nsDatexV1 && t.Name.Local == "predefinedLocationSet" {
				for _, attr := range t.Attr {
					if attr.Name.Local == "id" {
						locationSetIDs = append(locationSetIDs, attr.Value)
					}
				}
			}
			if t.Name.Space == nsDatexV1 && t.Name.Local == "predefinedLocation" {
				var raw xmlRadarLocation
				if err := d.DecodeElement(&raw, &t); err != nil {
					return nil, fmt.Errorf("datex2: decoding radar: %w", err)
				}
				radars = append(radars, convertRadar(raw))
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
	case rootName.Space != nsDatexV1 || rootName.Local != "d2LogicalModel":
		return nil, fmt.Errorf("datex2: payload is not a DATEX II v1.0 d2LogicalModel document (root %q in %q)", rootName.Local, rootName.Space)
	case !seenPayloadPublication:
		return nil, fmt.Errorf("datex2: payload has no payloadPublication")
	case xsiTypeLocal(payloadType) != "PredefinedLocationsPublication":
		return nil, fmt.Errorf("datex2: payload is not a PredefinedLocationsPublication (xsi:type %q)", payloadType)
	case !hasRadarLocationSet(locationSetIDs):
		return nil, fmt.Errorf("datex2: payload is not the fixed-radar inventory (predefinedLocationSet %q)", strings.Join(locationSetIDs, ", "))
	}
	return radars, nil
}

// hasRadarLocationSet reports whether any collected predefinedLocationSet
// id is one of the radar inventory sets (radarLocationSetIDs).
func hasRadarLocationSet(ids []string) bool {
	for _, id := range ids {
		if radarLocationSetIDs[id] {
			return true
		}
	}
	return false
}

// convertRadar flattens one outer predefinedLocation into a Radar. The
// road, direction, destination label, province code and singularity are
// taken from the primary (ini) reference point; the secondary contributes
// only its distance (and, for a segment, the to coordinates already come
// from the tpeg to point).
func convertRadar(raw xmlRadarLocation) Radar {
	inner := raw.Inner
	radar := Radar{ID: raw.ID}

	switch {
	case inner.Linear != nil:
		if from := inner.Linear.From; from != nil {
			radar.Road = linkNameValue(from.Names)
			radar.Start = radarCoordinates(from.Coords)
		}
		if to := inner.Linear.To; to != nil {
			if radar.Road == "" {
				radar.Road = linkNameValue(to.Names)
			}
			radar.End = radarCoordinates(to.Coords)
		}
		if primary := inner.RefLinear.Primary; primary != nil {
			applyPrimaryReferencePoint(&radar, primary)
		}
		if secondary := inner.RefLinear.Secondary; secondary != nil {
			radar.ToDistance = secondary.Distance
		}
	case inner.PointLoc != nil && inner.PointLoc.Point != nil:
		point := inner.PointLoc.Point
		radar.Road = linkNameValue(point.Names)
		radar.Start = radarCoordinates(point.Coords)
		if inner.RefPoint != nil {
			applyPrimaryReferencePoint(&radar, inner.RefPoint)
		}
	}
	return radar
}

// applyPrimaryReferencePoint copies the fields the primary (ini) reference
// point publishes into the radar, falling the road back through roadName
// and roadNumber when the tpeg names carried none.
func applyPrimaryReferencePoint(radar *Radar, point *xmlRadarReferencePoint) {
	if radar.Road == "" {
		radar.Road = strings.TrimSpace(point.RoadName)
	}
	if radar.Road == "" {
		radar.Road = strings.TrimSpace(point.RoadNumber)
	}
	radar.FromDistance = point.Distance
	radar.DirectionRelative = strings.TrimSpace(point.DirectionRelative)
	radar.DirectionNamed = strings.TrimSpace(point.Extension.Extended.DirectionNamed)
	radar.ProvinceINE = strings.TrimSpace(point.Extension.Extended.ProvinceINE)
	radar.Singularity = strings.TrimSpace(point.Extension.Extended.Singularity)
}

// linkNameValue returns the value of the first descriptor typed linkName
// (docs/feeds.md §7: the road name travels there), trimmed; "" when the
// point names carry none.
func linkNameValue(names []xmlRadarName) string {
	for _, name := range names {
		if name.DescriptorType == "linkName" {
			return strings.TrimSpace(name.Value)
		}
	}
	return ""
}

func radarCoordinates(c *xmlRadarCoordinates) *Coordinates {
	if c == nil {
		return nil
	}
	return &Coordinates{Latitude: c.Latitude, Longitude: c.Longitude}
}
