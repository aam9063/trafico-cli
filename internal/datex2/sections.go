package datex2

// Parser of the two section feeds of FEED-05, one per DATEX II dialect:
//
//   - INVIVE ("tramos_invive", docs/feeds.md §7): the DGT road-section
//     inventory, DATEX II v1.0, the same dialect as the radar parser of
//     radars.go. The payload identity is the predefinedLocationSet id
//     GUID_Inventario_Tramos_Invive: the radars inventory is the same
//     dialect and publication type, and only the set id tells them apart.
//   - TEFIVA / SPFRIA ("tefiva", docs/feeds.md §8): the road sections with
//     a special frequency of animal collisions, DATEX II v3 (namespace
//     datex2.eu/schema/3, NOT the levelC/schema/3 family of signs.go).
//     Its identity guard is partly prose: see tefivaDescriptionMatches.
//
// As everywhere in this package, namespace matching is on the URI, never on
// the prefix, because prefixes are arbitrary per payload.
//
// Neither feed publishes a usable publicationTime (docs/feeds.md §9: the
// value exists but is frozen), so neither parser reads one and published_at
// stays null downstream. The TEFIVA kilometre point is read from the
// published loc:distanceAlong elements, with the raw id preserved so the
// conversion can fall back to the metre the id encodes when a payload omits
// the location block.
// the model's documented decision.

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// inviveLocationSetID is the predefinedLocationSet id of the DGT INVIVE
// section inventory (docs/feeds.md §7). It is the payload identity of the
// v1.0 section feed: the radar parser (radarLocationSetIDs) rejects it and
// this parser accepts only it.
const inviveLocationSetID = "GUID_Inventario_Tramos_Invive"

// Namespaces of the DATEX II v3 dialect the TEFIVA feed uses
// (docs/feeds.md §1.1 and §8). They are the standard schema/3 namespaces,
// not the levelC/schema/3 family the incident and sign feeds share.
const (
	nsDatexV3Payload   = "http://datex2.eu/schema/3/d2Payload"
	nsDatexV3Location  = "http://datex2.eu/schema/3/locationReferencing"
	nsDatexV3CommonLoc = "http://datex2.eu/schema/3/common"
)

// InviveSection is one section of the v1.0 INVIVE inventory
// (docs/feeds.md §7). Every raw value is preserved as published (P-04):
// the kilometre points are the raw referencePointDistance values in
// metres — the kilometre conversion is the model's job — and the province
// is the published administrative area name (uppercase), not an INE code:
// this feed publishes no provinceINEIdentifier.
type InviveSection struct {
	// ID is the @id of the outer predefinedLocation
	// ("GUID_Tramo_Invive_1").
	ID string
	// Road is the road name: the linkName descriptor of the tpeg point
	// names (docs/feeds.md §7: names travel in name/descriptor/value with
	// tpegDescriptorType), falling back to the primary reference point's
	// roadName and then roadNumber.
	Road string
	// Province is the primary reference point's administrativeArea value
	// as published ("ALBACETE"); empty when the feed omits it.
	Province string
	// FromDistance and ToDistance are the raw referencePointDistance
	// values in metres of the primary (ini) and secondary (fin) reference
	// points; either is nil when the feed omits it.
	FromDistance *float64
	ToDistance   *float64
	// Start and End are the coordinates of the tpeg from and to points.
	Start *Coordinates
	End   *Coordinates
	// DirectionRelative is the primary reference point's
	// directionRelative: positive, negative or both (docs/feeds.md §7).
	DirectionRelative string
}

// TefivaSection is one section of the v3 TEFIVA/SPFRIA feed
// (docs/feeds.md §8). The kilometre point is NOT parsed from the id here:
// the parser keeps the raw identifier and the published lengthAffected,
// and the model layer applies the documented derivation. The feed carries
// no province: the uppercase prefix of the id is a fragile identifier
// fragment, not a published province field, so it is never lifted into
// one.
type TefivaSection struct {
	// ID is the @id of the loc:predefinedLocationReference
	// ("HUELVA_N-433_71400.0"): province, road and PK in metres joined by
	// underscores (docs/feeds.md §8).
	ID string
	// Road is the published road name: the ilc descriptor value, falling
	// back to the otherName descriptor typed linkName.
	Road string
	// LengthKM is the published loc:lengthAffected in kilometres; nil
	// when the feed omits it.
	LengthKM *float64
	// FromMetres and ToMetres are the published loc:distanceAlong values of
	// the linear element, in metres. They are the authority for the kilometre
	// point; nil when the payload omits the location block.
	FromMetres *float64
	ToMetres   *float64
	// Start and End are the coordinates of the tpeg from and to points.
	Start *Coordinates
	End   *Coordinates
	// DirectionRelative is the published directionRelativeOnLinearSection
	// ("both"); empty when the feed omits it.
	DirectionRelative string
}

// The raw structs below mirror only the parts of the two payloads this
// package needs. They are unexported: the public surface is InviveSection
// and TefivaSection.

type xmlInviveLocation struct {
	ID    string          `xml:"id,attr"`
	Inner xmlInviveLinear `xml:"predefinedLocation"`
}

// xmlInviveLinear is the inner predefinedLocation, typed Linear: an INVIVE
// section is always a segment with a tpeg linear location and two
// reference points.
type xmlInviveLinear struct {
	Linear    *xmlV1TpegLinear     `xml:"tpeglinearLocation"`
	RefLinear *xmlV1RefPointLinear `xml:"referencePointLinear"`
}

// xmlV1TpegLinear is the v1.0 tpeglinearLocation shared shape: a to and a
// from point, each with coordinates and named descriptors.
type xmlV1TpegLinear struct {
	To   *xmlV1TpegPoint `xml:"to"`
	From *xmlV1TpegPoint `xml:"from"`
}

type xmlV1TpegPoint struct {
	Coords *xmlV1Coordinates `xml:"pointCoordinates"`
	Names  []xmlV1Name       `xml:"name"`
}

type xmlV1Coordinates struct {
	Latitude  float64 `xml:"latitude"`
	Longitude float64 `xml:"longitude"`
}

type xmlV1Name struct {
	Value          string `xml:"descriptor>value"`
	DescriptorType string `xml:"tpegDescriptorType"`
}

type xmlV1RefPointLinear struct {
	Primary   *xmlV1ReferencePoint `xml:"referencePointPrimaryLocation>referencePoint"`
	Secondary *xmlV1ReferencePoint `xml:"referencePointSecondaryLocation>referencePoint"`
}

type xmlV1ReferencePoint struct {
	Identifier        string   `xml:"referencePointIdentifier"`
	AdminArea         string   `xml:"administrativeArea>value"`
	RoadName          string   `xml:"roadName>value"`
	RoadNumber        string   `xml:"roadNumber"`
	DirectionRelative string   `xml:"directionRelative"`
	Distance          *float64 `xml:"referencePointDistance"`
}

type xmlTefivaLocationReference struct {
	ID       string          `xml:"id,attr"`
	Location xmlTefivaLocate `xml:"location"`
}

type xmlTefivaLocate struct {
	Supplementary xmlTefivaSupplementary `xml:"supplementaryPositionalDescription"`
	Tpeg          xmlTefivaTpegLinear    `xml:"tpegLinearLocation"`
	WithinLinear  xmlTefivaWithinLinear  `xml:"linearWithinLinearElement"`
}

type xmlTefivaSupplementary struct {
	LengthKM *float64 `xml:"lengthAffected"`
}

type xmlTefivaTpegLinear struct {
	To   *xmlTefivaTpegPoint `xml:"to"`
	From *xmlTefivaTpegPoint `xml:"from"`
}

type xmlTefivaTpegPoint struct {
	Coords    *xmlTefivaCoordinates `xml:"pointCoordinates"`
	Ilc       *xmlTefivaIlc         `xml:"ilc"`
	OtherName []xmlTefivaOtherName  `xml:"otherName"`
}

type xmlTefivaCoordinates struct {
	Latitude  float64 `xml:"latitude"`
	Longitude float64 `xml:"longitude"`
}

type xmlTefivaIlc struct {
	Value string `xml:"descriptor>values>value"`
}

type xmlTefivaOtherName struct {
	Value          string `xml:"descriptor>values>value"`
	DescriptorType string `xml:"tpegOtherPointDescriptorType"`
}

type xmlTefivaWithinLinear struct {
	DirectionRelative string               `xml:"directionRelativeOnLinearSection"`
	From              xmlTefivaLinearPoint `xml:"fromPoint"`
	To                xmlTefivaLinearPoint `xml:"toPoint"`
}

// xmlTefivaLinearPoint is one end of the linear element. distanceAlong is the
// published kilometre point in metres, which the payload does carry — the id
// only repeats the starting one.
type xmlTefivaLinearPoint struct {
	DistanceAlong *float64 `xml:"distanceAlong"`
}

// xmlTefivaFeedDescription mirrors com:feedDescription of a v3 payload: one
// com:values block holding one com:value per language, each with a lang
// attribute. The TEFIVA feed publishes exactly one, lang="es".
type xmlTefivaFeedDescription struct {
	Values struct {
		Value []struct {
			Lang  string `xml:"lang,attr"`
			Value string `xml:",chardata"`
		} `xml:"value"`
	} `xml:"values"`
}

// tefivaDescriptionValue returns the feed description text of the payload,
// preferring the lang="es" value and falling back to the first published one;
// "" when the element carries none. Whitespace is trimmed: the feed wraps the
// prose across lines.
func tefivaDescriptionValue(desc xmlTefivaFeedDescription) string {
	es, first := "", ""
	for _, v := range desc.Values.Value {
		if first == "" {
			first = strings.TrimSpace(v.Value)
		}
		if v.Lang == "es" && es == "" {
			es = strings.TrimSpace(v.Value)
		}
	}
	if es != "" {
		return es
	}
	return first
}

// tefivaDescriptionMatches reports whether the payload's com:feedDescription
// identifies the TEFIVA feed. THIS IS A PROSE MATCH, NOT A STRUCTURAL ONE:
// the motos feed (tramosriesgomotos) is the same v3 d2:payload, the same
// PredefinedLocationsPublication type, the same loc:PredefinedLocation
// reference shape and the same PROVINCIA_CARRETERA_METROS id pattern (both
// verified against the real feeds), so no element, attribute or identifier
// tells them apart. The only measured signal that differs is the frozen
// description text:
//
//	tefiva: "Publicación TEFIVA: Tramos con especial frecuencia de ..."
//	motos:  "Publicación de los tramos de elevado riesgo para motoristas"
//
// The trade: match the stable brand token TEFIVA, case-insensitively, and
// nothing else. Generic words ("tramos", "animales", "publicación") are
// deliberately NOT matched, because the motos description uses them too and
// would false-positive. The cost is the fragility documented in docs/feeds.md
// §8: if the DGT ever rewords the description and drops the brand token, this
// parser rejects a legitimate TEFIVA payload (feed_changed, exit 7) instead of
// silently accepting a foreign one as animal sections (exit 0). Failing closed
// is the chosen direction.
func tefivaDescriptionMatches(description string) bool {
	return strings.Contains(strings.ToLower(description), "tefiva")
}

// ParseInviveSections streams a DGT INVIVE payload (a v1.0
// PredefinedLocationsPublication whose predefinedLocationSet is the INVIVE
// inventory, docs/feeds.md §7) into InviveSection values, in feed order.
// It reads token by token and decodes one outer predefinedLocation at a
// time, like ParseRadars. It does not fetch, filter or convert anything.
//
// Like ParseRadars, it validates the payload identity before returning:
// the root must be a v1.0 d2LogicalModel, the payload must be a
// PredefinedLocationsPublication, and at least one predefinedLocationSet
// must carry the INVIVE set id. A body from another DGT dialect — the v3
// tefiva payload, the v3.7 situations payload, a v1.0 body of another
// publication such as the radar inventory — carries no INVIVE set id, so
// without the check it would parse as zero (or phantom) sections and no
// error, and `trafico sections --kind invive` would exit 0 with sections:
// [] instead of feed_changed (exit 7).
func ParseInviveSections(r io.Reader) ([]InviveSection, error) {
	d := xml.NewDecoder(r)
	var sections []InviveSection

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
				var raw xmlInviveLocation
				if err := d.DecodeElement(&raw, &t); err != nil {
					return nil, fmt.Errorf("datex2: decoding invive section: %w", err)
				}
				sections = append(sections, convertInviveSection(raw))
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
	case !containsString(locationSetIDs, inviveLocationSetID):
		return nil, fmt.Errorf("datex2: payload is not the INVIVE section inventory (predefinedLocationSet %q)", strings.Join(locationSetIDs, ", "))
	}
	return sections, nil
}

// ParseTefivaSections streams a DGT TEFIVA payload (a v3
// PredefinedLocationsPublication, docs/feeds.md §8) into TefivaSection
// values, in feed order. The kilometre point travels as published, from the
// loc:distanceAlong elements; the identifier is carried raw so the model can
// fall back to the metre it encodes when a payload omits the location block.
//
// Like ParseInviveSections it validates the payload identity before
// returning: the root must be a v3 d2:payload typed
// PredefinedLocationsPublication, the com:feedDescription must name TEFIVA,
// and at least one id must match the documented form. Two guards are prose-
// adjacent: the feed description is frozen prose (tefivaDescriptionMatches),
// and the id pattern is a shape shared with the motos feed, which is why the
// description guard runs first. A body from another DGT dialect — the v1.0
// INVIVE or radar payload, the v3.7 situations payload, or the v3 motos
// payload — fails one of them, so without the checks it would parse as zero
// (or, for motos, plausible) sections and `trafico sections --kind animals`
// would exit 0 instead of feed_changed (exit 7).
func ParseTefivaSections(r io.Reader) ([]TefivaSection, error) {
	d := xml.NewDecoder(r)
	var sections []TefivaSection

	var rootName xml.Name
	var rootXsiTypeValue string
	seenRoot, rootClosed := false, false
	seenFeedDescription := false
	var feedDescription xmlTefivaFeedDescription
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
				rootName, rootXsiTypeValue, seenRoot = t.Name, rootPublicationType(t.Attr), true
			}
			if t.Name.Space == nsDatexV3CommonLoc && t.Name.Local == "feedDescription" {
				seenFeedDescription = true
				if err := d.DecodeElement(&feedDescription, &t); err != nil {
					return nil, fmt.Errorf("datex2: decoding tefiva feed description: %w", err)
				}
			}
			if t.Name.Space == nsDatexV3Location && t.Name.Local == "predefinedLocationReference" {
				var raw xmlTefivaLocationReference
				if err := d.DecodeElement(&raw, &t); err != nil {
					return nil, fmt.Errorf("datex2: decoding tefiva section: %w", err)
				}
				sections = append(sections, convertTefivaSection(raw))
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
	case rootName.Space != nsDatexV3Payload || rootName.Local != "payload":
		return nil, fmt.Errorf("datex2: payload is not a DATEX II v3 d2:payload document (root %q in %q)", rootName.Local, rootName.Space)
	case xsiTypeLocal(rootXsiTypeValue) != "PredefinedLocationsPublication":
		return nil, fmt.Errorf("datex2: payload is not a PredefinedLocationsPublication (root xsi:type %q)", rootXsiTypeValue)
	case !seenFeedDescription:
		return nil, fmt.Errorf("datex2: payload carries no com:feedDescription, so it cannot be identified as the TEFIVA animal-section feed; rejecting a payload of unknown origin")
	case !tefivaDescriptionMatches(tefivaDescriptionValue(feedDescription)):
		return nil, fmt.Errorf("datex2: payload is not the TEFIVA animal-section feed (com:feedDescription %q does not name TEFIVA)", tefivaDescriptionValue(feedDescription))
	case !hasTefivaIdentifiers(sections):
		return nil, fmt.Errorf("datex2: payload is not the SPFRIA/TEFIVA animal-section inventory (no predefinedLocationReference id in the PROVINCIA_CARRETERA_METROS form)")
	}
	return sections, nil
}

// hasTefivaIdentifiers reports whether at least one reference id matches the
// documented PROVINCIA_CARRETERA_METROS form. It is the second identity
// signal, kept after the feed-description prose guard: the motos feed's ids
// match this pattern too (verified against the real payload), so on its own
// it discriminates nothing — but it catches a description reword that kept
// the TEFIVA token while the body changed into something else.
func hasTefivaIdentifiers(sections []TefivaSection) bool {
	for _, section := range sections {
		if tefivaIDMetres(section.ID) {
			return true
		}
	}
	return false
}

// tefivaIDMetres reports whether id ends in underscore-separated metres.
func tefivaIDMetres(id string) bool {
	parts := strings.Split(strings.TrimSpace(id), "_")
	if len(parts) < 3 || parts[len(parts)-2] == "" {
		return false
	}
	metres, err := strconv.ParseFloat(parts[len(parts)-1], 64)
	return err == nil && !math.IsNaN(metres) && !math.IsInf(metres, 0)
}

// containsString reports whether list carries value. A tiny helper shared
// by the identity guards.
func containsString(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

// convertInviveSection flattens one outer predefinedLocation into an
// InviveSection. The road, administrative area and direction are taken
// from the primary (ini) reference point; the from/to names and
// coordinates come from the tpeg points.
func convertInviveSection(raw xmlInviveLocation) InviveSection {
	section := InviveSection{ID: raw.ID}
	inner := raw.Inner
	if inner.Linear != nil {
		if from := inner.Linear.From; from != nil {
			section.Road = v1LinkName(from.Names)
			section.Start = v1Coordinates(from.Coords)
		}
		if to := inner.Linear.To; to != nil {
			if section.Road == "" {
				section.Road = v1LinkName(to.Names)
			}
			section.End = v1Coordinates(to.Coords)
		}
	}
	if inner.RefLinear != nil {
		if primary := inner.RefLinear.Primary; primary != nil {
			if section.Road == "" {
				section.Road = strings.TrimSpace(primary.RoadName)
			}
			if section.Road == "" {
				section.Road = strings.TrimSpace(primary.RoadNumber)
			}
			section.Province = strings.TrimSpace(primary.AdminArea)
			section.FromDistance = primary.Distance
			section.DirectionRelative = strings.TrimSpace(primary.DirectionRelative)
		}
		if secondary := inner.RefLinear.Secondary; secondary != nil {
			section.ToDistance = secondary.Distance
		}
	}
	return section
}

// convertTefivaSection flattens one loc:predefinedLocationReference into a
// TefivaSection. The road comes from the published descriptors (the ilc
// name first, then the otherName typed linkName), never from the id.
func convertTefivaSection(raw xmlTefivaLocationReference) TefivaSection {
	section := TefivaSection{
		ID:       raw.ID,
		LengthKM: raw.Location.Supplementary.LengthKM,
	}
	section.DirectionRelative = strings.TrimSpace(raw.Location.WithinLinear.DirectionRelative)
	section.FromMetres = raw.Location.WithinLinear.From.DistanceAlong
	section.ToMetres = raw.Location.WithinLinear.To.DistanceAlong

	tpeg := raw.Location.Tpeg
	if tpeg.From != nil {
		section.Start = tefivaCoordinates(tpeg.From.Coords)
		section.Road = strings.TrimSpace(tpeg.From.Ilc.Value)
	}
	if tpeg.To != nil {
		section.End = tefivaCoordinates(tpeg.To.Coords)
		if section.Road == "" {
			section.Road = strings.TrimSpace(tpeg.To.Ilc.Value)
		}
	}
	if section.Road == "" {
		section.Road = tefivaLinkName(append(append([]xmlTefivaOtherName{}, tefivaPointNames(tpeg.From)...), tefivaPointNames(tpeg.To)...))
	}
	return section
}

// tefivaPointNames returns the otherName descriptors of a point, nil for a
// missing point.
func tefivaPointNames(point *xmlTefivaTpegPoint) []xmlTefivaOtherName {
	if point == nil {
		return nil
	}
	return point.OtherName
}

// tefivaLinkName returns the value of the first otherName descriptor typed
// linkName, trimmed; "" when none carries it.
func tefivaLinkName(names []xmlTefivaOtherName) string {
	for _, name := range names {
		if name.DescriptorType == "linkName" {
			return strings.TrimSpace(name.Value)
		}
	}
	return ""
}

// v1LinkName returns the value of the first descriptor typed linkName of a
// v1.0 tpeg point (docs/feeds.md §7: the road name travels there), trimmed;
// "" when the point names carry none. It mirrors linkNameValue of the radar
// parser on the section raw types.
func v1LinkName(names []xmlV1Name) string {
	for _, name := range names {
		if name.DescriptorType == "linkName" {
			return strings.TrimSpace(name.Value)
		}
	}
	return ""
}

func v1Coordinates(c *xmlV1Coordinates) *Coordinates {
	if c == nil {
		return nil
	}
	return &Coordinates{Latitude: c.Latitude, Longitude: c.Longitude}
}

func tefivaCoordinates(c *xmlTefivaCoordinates) *Coordinates {
	if c == nil {
		return nil
	}
	return &Coordinates{Latitude: c.Latitude, Longitude: c.Longitude}
}
