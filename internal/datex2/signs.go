package datex2

// Parser of the two variable message sign (VMS) feeds of docs/feeds.md §5.
// They are two DATEX II payloads that must be joined by id downstream:
//
//   - DevicePublication ("dgt-signs", §5.1): where every panel is. The DGT
//     publishes it under profileName="ProfileDGT" with xsi:type
//     ns2:DevicePublication, but its namespaces are the levelC/schema/3
//     family, like the incident feed — NOT the datex2.eu/schema/3 dialect
//     listed in docs/feeds.md §1.1 (see the xsi:schemaLocation of the real
//     payload). It is a second dialect in this package; the v3.7 incident
//     parser of parse.go is untouched.
//   - VmsPublication ("dgt-signs-messages", §5.2): what every panel shows.
//
// As everywhere in this package, namespace matching is on the URI, never on
// the prefix, because prefixes are arbitrary per payload.

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Namespaces of the two sign feeds (docs/feeds.md §5). nsFaultStatus holds
// ns2:device and ns2:typeOfDevice of the DevicePublication; nsVms holds the
// whole VmsPublication. nsCommon, nsLocation and nsSpanishExt are shared
// with the incident parser (parse.go).
const (
	nsFaultStatus = "http://levelC/schema/3/faultAndStatus"
	nsVms         = "http://levelC/schema/3/vms"
)

// VmsDevice is one <ns2:device> of the DevicePublication: where a panel
// stands. Every raw value is preserved as published (P-04): the road name is
// not normalised here, the province keeps the feed's UPPERCASE spelling
// (docs/feeds.md §5.1, unlike the incident feed), and absent optional fields
// stay empty or nil.
type VmsDevice struct {
	// ID is <ns2:device/@id>: the join key with VmsController.ID.
	ID string
	// Version is the @version attribute of the device element.
	Version string
	// TypeOfDevice is <ns2:typeOfDevice>; it is "vms" for every device
	// measured so far (2517/2517, docs/feeds.md §5.1).
	TypeOfDevice string
	// LastUpdate is <ns2:lastUpdateOfDeviceInformation> in UTC; zero when
	// the element is absent.
	LastUpdate time.Time
	// RoadName is loc:roadInformation/loc:roadName as published.
	RoadName string
	// RoadDestination is loc:roadInformation/loc:roadDestination: the real
	// destination label of the panel (1545 of 2517 devices carry it, §5.1).
	// Empty when absent.
	RoadDestination string
	// KilometerPoint is lse:kilometerPoint; nil when absent.
	KilometerPoint *float64
	// Province is lse:province as published (UPPERCASE in this feed).
	Province string
	// DirectionRoad is lse:tpegDirectionRoad: positive, negative or both —
	// the direction of travel (§4.4). Empty when absent.
	DirectionRoad string
	// Coordinates is loc:pointCoordinates; nil when absent.
	Coordinates *Coordinates
}

// VmsMessage is one <vms:vmsMessage> of a controller: what a panel displays.
// The message is the concatenation of the text lines and the pictogram URLs
// (docs/feeds.md §5.2), in display-area index order.
type VmsMessage struct {
	// TimeLastSet is <vms:timeLastSet> in UTC; zero when absent.
	TimeLastSet time.Time
	// TextLines are the <vms:textLine> values of the message. The DGT nests
	// the text three elements deep and wraps the rows of a line with
	// newlines; the whitespace is collapsed so the value is the rendered
	// text ("VELOCIDAD CONTROLADA POR RADAR"), and a blank line serialises
	// as "".
	TextLines []string
	// Pictograms are the <vms:pictogramDisplayUrl> values in display-area
	// index order (the payload publishes areas out of order: 1, 3, 2). The
	// blank glyph the DGT serves for an empty area is pictogramas/0.png.
	Pictograms []string
	// PictogramCodes are the <vms:customPictogramCode> values parallel to
	// Pictograms; the code "0" marks the blank pictogram of an off panel.
	PictogramCodes []string
}

// VmsController is one <vms:vmsControllerStatus> of the VmsPublication: the
// messages currently set on one panel, keyed by the same id as VmsDevice.
type VmsController struct {
	// ID is <vms:vmsControllerReference/@id>: the join key with
	// VmsDevice.ID (§5.2: same identifier, no heuristic).
	ID string
	// Messages are the messages of the controller in feed order.
	Messages []VmsMessage
}

// The raw structs below mirror only the parts of the two payloads this
// package needs. They are unexported: the public surface is VmsDevice,
// VmsMessage and VmsController.

type xmlVmsDevice struct {
	ID            string            `xml:"id,attr"`
	Version       string            `xml:"version,attr"`
	TypeOfDevice  string            `xml:"http://levelC/schema/3/faultAndStatus typeOfDevice"`
	LastUpdate    string            `xml:"http://levelC/schema/3/faultAndStatus lastUpdateOfDeviceInformation"`
	PointLocation xmlVmsPointHeader `xml:"http://levelC/schema/3/faultAndStatus pointLocation"`
}

type xmlVmsPointHeader struct {
	Supplementary xmlVmsSupplementary `xml:"http://levelC/schema/3/locationReferencing supplementaryPositionalDescription"`
	Tpeg          xmlVmsTpegPoint     `xml:"http://levelC/schema/3/locationReferencing tpegPointLocation"`
}

type xmlVmsSupplementary struct {
	RoadInfo xmlVmsRoadInformation `xml:"http://levelC/schema/3/locationReferencing roadInformation"`
}

type xmlVmsRoadInformation struct {
	RoadName        string `xml:"http://levelC/schema/3/locationReferencing roadName"`
	RoadDestination string `xml:"http://levelC/schema/3/locationReferencing roadDestination"`
}

type xmlVmsTpegPoint struct {
	Point *xmlVmsTpegPointInner `xml:"http://levelC/schema/3/locationReferencing point"`
	Ext   xmlSimplePointExt     `xml:"http://levelC/schema/3/locationReferencing _tpegSimplePointExtension"`
}

type xmlVmsTpegPointInner struct {
	Coords    *xmlCoordinates           `xml:"http://levelC/schema/3/locationReferencing pointCoordinates"`
	Extension xmlNonJunctionPointExtWrp `xml:"http://levelC/schema/3/locationReferencing _tpegNonJunctionPointExtension"`
}

type xmlVmsControllerStatus struct {
	Reference xmlVmsControllerReference `xml:"http://levelC/schema/3/vms vmsControllerReference"`
	Status    xmlVmsStatusOuter         `xml:"http://levelC/schema/3/vms vmsStatus"`
}

type xmlVmsControllerReference struct {
	ID string `xml:"id,attr"`
}

// xmlVmsStatusOuter is the outer <vms:vmsStatus vmsIndex="...">; the
// messages live one level deeper, in the inner <vms:vmsStatus>.
type xmlVmsStatusOuter struct {
	Inner xmlVmsStatusInner `xml:"http://levelC/schema/3/vms vmsStatus"`
}

type xmlVmsStatusInner struct {
	Messages []xmlVmsMessageOuter `xml:"http://levelC/schema/3/vms vmsMessage"`
}

type xmlVmsMessageOuter struct {
	Index  string        `xml:"messageIndex,attr"`
	Detail xmlVmsMessage `xml:"http://levelC/schema/3/vms vmsMessage"`
}

type xmlVmsMessage struct {
	TimeLastSet  string              `xml:"http://levelC/schema/3/vms timeLastSet"`
	DisplayAreas []xmlVmsDisplayArea `xml:"http://levelC/schema/3/vms displayAreaSettings"`
}

// xmlVmsDisplayArea is the outer <vms:displayAreaSettings
// displayAreaIndex="...">; the typed content is one level deeper in another
// displayAreaSettings element carrying the xsi:type.
type xmlVmsDisplayArea struct {
	Index   int                     `xml:"displayAreaIndex,attr"`
	Content xmlVmsDisplayAreaDetail `xml:"http://levelC/schema/3/vms displayAreaSettings"`
}

type xmlVmsDisplayAreaDetail struct {
	Type         string              `xml:"http://www.w3.org/2001/XMLSchema-instance type,attr"`
	PictogramURL string              `xml:"http://levelC/schema/3/vms pictogramDisplayUrl"`
	Pictogram    xmlVmsPictogram     `xml:"http://levelC/schema/3/vms pictogram"`
	TextLineMids []xmlVmsTextLineMid `xml:"http://levelC/schema/3/vms textLine"`
}

type xmlVmsPictogram struct {
	Code string `xml:"http://levelC/schema/3/vms customPictogramCode"`
}

// xmlVmsTextLineMid is the outer <vms:textLine lineIndex="1"> of the
// three-level nesting. The DGT wraps the text two elements deeper; when
// fewer levels are ever published, the direct chardata of the outer or
// middle element is the text. All three positions are read so either shape
// parses.
type xmlVmsTextLineMid struct {
	LineIndex string             `xml:"lineIndex,attr"`
	Text      string             `xml:",chardata"`
	Inner     xmlVmsTextLineCore `xml:"http://levelC/schema/3/vms textLine"`
}

// xmlVmsTextLineCore is the middle <vms:textLine>; it either carries the
// text directly or wraps the innermost element.
type xmlVmsTextLineCore struct {
	Text  string              `xml:",chardata"`
	Inner xmlVmsTextLineInner `xml:"http://levelC/schema/3/vms textLine"`
}

type xmlVmsTextLineInner struct {
	Text string `xml:",chardata"`
}

// ParseVmsDevices streams a DGT sign-location payload (ns2:DevicePublication,
// docs/feeds.md §5.1) into VmsDevice values, in feed order. It reads token by
// token, like ParseSituations: the payload is ~4.5 MB. It does not fetch,
// filter or join anything.
//
// Like ParseSituations, it validates the payload identity before returning
// (review FEED-03): the root must carry the DevicePublication xsi:type and
// the body must carry com:publicationTime (docs/feeds.md §9). A body from
// another DGT dialect — the v3.7 situations payload, a v1.0 radar body, a
// v3 tefiva body — carries no ns2:device, so without the check it would
// parse as zero devices and no error, and `trafico signs` would exit 0 with
// signs: [] instead of feed_changed (exit 7).
func ParseVmsDevices(r io.Reader) ([]VmsDevice, error) {
	d := xml.NewDecoder(r)
	var devices []VmsDevice

	var rootName xml.Name
	var rootXsiTypeValue string
	seenRoot, rootClosed, seenPublicationTime := false, false, false
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
			if t.Name.Space == nsCommon && t.Name.Local == "publicationTime" {
				seenPublicationTime = true
			}
			if t.Name.Space == nsFaultStatus && t.Name.Local == "device" {
				var raw xmlVmsDevice
				if err := d.DecodeElement(&raw, &t); err != nil {
					return nil, fmt.Errorf("datex2: decoding vms device: %w", err)
				}
				dev, err := convertVmsDevice(raw)
				if err != nil {
					return nil, err
				}
				devices = append(devices, dev)
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
	case xsiTypeLocal(rootXsiTypeValue) != "DevicePublication":
		return nil, fmt.Errorf("datex2: payload is not a DevicePublication (root xsi:type %q)", rootXsiTypeValue)
	case !seenPublicationTime:
		return nil, fmt.Errorf("datex2: payload has no com:publicationTime")
	}
	return devices, nil
}

// ParseVmsMessages streams a DGT sign-message payload (vms:VmsPublication,
// docs/feeds.md §5.2) into VmsController values, in feed order. It does not
// join the two feeds. Like ParseVmsDevices it validates the payload identity
// before returning (review FEED-03): the root must carry the VmsPublication
// xsi:type and the body must carry com:publicationTime (docs/feeds.md §9).
// The publicationTime value itself is still read separately with
// ParseVmsPublicationTime, the freshness source.
func ParseVmsMessages(r io.Reader) ([]VmsController, error) {
	d := xml.NewDecoder(r)
	var controllers []VmsController

	var rootName xml.Name
	var rootXsiTypeValue string
	seenRoot, rootClosed, seenPublicationTime := false, false, false
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
			if t.Name.Space == nsCommon && t.Name.Local == "publicationTime" {
				seenPublicationTime = true
			}
			if t.Name.Space == nsVms && t.Name.Local == "vmsControllerStatus" {
				var raw xmlVmsControllerStatus
				if err := d.DecodeElement(&raw, &t); err != nil {
					return nil, fmt.Errorf("datex2: decoding vms controller: %w", err)
				}
				controller, err := convertVmsController(raw)
				if err != nil {
					return nil, err
				}
				controllers = append(controllers, controller)
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
	case xsiTypeLocal(rootXsiTypeValue) != "VmsPublication":
		return nil, fmt.Errorf("datex2: payload is not a VmsPublication (root xsi:type %q)", rootXsiTypeValue)
	case !seenPublicationTime:
		return nil, fmt.Errorf("datex2: payload has no com:publicationTime")
	}
	return controllers, nil
}

// xsiNamespace is the namespace of the xsi:type attribute that names the
// publication of a v3 DATEX II payload.
const xsiNamespace = "http://www.w3.org/2001/XMLSchema-instance"

// rootPublicationType returns the raw xsi:type attribute of the root
// element (for example "ns2:DevicePublication"), or "" when the root
// carries none.
func rootPublicationType(attrs []xml.Attr) string {
	for _, a := range attrs {
		if a.Name.Space == xsiNamespace && a.Name.Local == "type" {
			return a.Value
		}
	}
	return ""
}

// xsiTypeLocal strips the arbitrary prefix of an xsi:type value
// ("vms:VmsPublication" → "VmsPublication"), because prefixes are arbitrary
// per payload while the type name is the payload's identity.
func xsiTypeLocal(v string) string {
	if i := strings.LastIndex(v, ":"); i >= 0 {
		return v[i+1:]
	}
	return v
}

// ParseVmsPublicationTime returns the <com:publicationTime> of a sign
// payload (either feed) in UTC. It is the only valid freshness source
// (docs/feeds.md §2 and §9); a body without one reads as the zero time, and
// the caller must not invent a value.
func ParseVmsPublicationTime(r io.Reader) (time.Time, error) {
	d := xml.NewDecoder(r)
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return time.Time{}, nil
		}
		if err != nil {
			return time.Time{}, fmt.Errorf("datex2: reading XML: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Space == nsCommon && se.Name.Local == "publicationTime" {
			var s xmlString
			if err := d.DecodeElement(&s, &se); err != nil {
				return time.Time{}, fmt.Errorf("datex2: decoding publicationTime: %w", err)
			}
			return parseFeedTime(s.Value, "com:publicationTime")
		}
	}
}

func convertVmsDevice(raw xmlVmsDevice) (VmsDevice, error) {
	lastUpdate, err := parseFeedTime(raw.LastUpdate, "lastUpdateOfDeviceInformation")
	if err != nil {
		return VmsDevice{}, fmt.Errorf("datex2: device %s: %w", raw.ID, err)
	}
	dev := VmsDevice{
		ID:              raw.ID,
		Version:         raw.Version,
		TypeOfDevice:    strings.TrimSpace(raw.TypeOfDevice),
		LastUpdate:      lastUpdate,
		RoadName:        strings.TrimSpace(raw.PointLocation.Supplementary.RoadInfo.RoadName),
		RoadDestination: strings.TrimSpace(raw.PointLocation.Supplementary.RoadInfo.RoadDestination),
		DirectionRoad:   strings.TrimSpace(raw.PointLocation.Tpeg.Ext.Extended.DirectionRoad),
	}
	if p := raw.PointLocation.Tpeg.Point; p != nil {
		if p.Coords != nil {
			dev.Coordinates = &Coordinates{Latitude: p.Coords.Latitude, Longitude: p.Coords.Longitude}
		}
		dev.KilometerPoint = p.Extension.Extended.KilometerPoint
		dev.Province = strings.TrimSpace(p.Extension.Extended.Province)
	}
	return dev, nil
}

func convertVmsController(raw xmlVmsControllerStatus) (VmsController, error) {
	controller := VmsController{ID: raw.Reference.ID}
	for _, outer := range raw.Status.Inner.Messages {
		set, err := parseFeedTime(outer.Detail.TimeLastSet, "vms:timeLastSet")
		if err != nil {
			return VmsController{}, fmt.Errorf("datex2: controller %s: %w", raw.Reference.ID, err)
		}
		msg := VmsMessage{
			TimeLastSet:    set,
			TextLines:      []string{},
			Pictograms:     []string{},
			PictogramCodes: []string{},
		}
		for _, area := range sortDisplayAreas(outer.Detail.DisplayAreas) {
			if url := strings.TrimSpace(area.Content.PictogramURL); url != "" {
				msg.Pictograms = append(msg.Pictograms, url)
			}
			if code := strings.TrimSpace(area.Content.Pictogram.Code); code != "" {
				msg.PictogramCodes = append(msg.PictogramCodes, code)
			}
			for _, mid := range area.Content.TextLineMids {
				msg.TextLines = append(msg.TextLines, textLineText(mid))
			}
		}
		controller.Messages = append(controller.Messages, msg)
	}
	return controller, nil
}

// sortDisplayAreas orders the display areas of a message by their
// displayAreaIndex: the payload publishes them out of order (1, 3, 2), and
// the index is the physical order of the display areas on the panel. The
// sort is stable, so equal indices keep their feed order.
func sortDisplayAreas(areas []xmlVmsDisplayArea) []xmlVmsDisplayArea {
	sorted := make([]xmlVmsDisplayArea, len(areas))
	copy(sorted, areas)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Index < sorted[j].Index })
	return sorted
}

// textLineText extracts the rendered text of one text line: the innermost
// element's text when the DGT nests three levels, falling back to the
// middle and outer elements for shallower nestings. Whitespace is collapsed
// to single spaces and trimmed, so the value is what the panel shows and a
// blank line is "".
func textLineText(mid xmlVmsTextLineMid) string {
	if text := collapseSpaces(mid.Inner.Inner.Text); text != "" {
		return text
	}
	if text := collapseSpaces(mid.Inner.Text); text != "" {
		return text
	}
	return collapseSpaces(mid.Text)
}

// collapseSpaces trims the text and collapses every whitespace run (the
// line-wrap newlines of the feed) to a single space.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
