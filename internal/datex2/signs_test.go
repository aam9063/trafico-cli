package datex2

import (
	"os"
	"strings"
	"testing"
	"time"
)

// vmsLocationsFixture loads the real trimmed DGT DevicePublication snapshot
// (public CC BY data) so the tests exercise the actual v3 dialect of
// docs/feeds.md §5.1, including its namespaces and Spanish extensions.
func vmsLocationsFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/feeds/vms-locations/snap-1.xml")
	if err != nil {
		t.Fatalf("reading the vms-locations snapshot: %v", err)
	}
	return data
}

// vmsMessagesFixture loads the real trimmed DGT VmsPublication snapshot
// (docs/feeds.md §5.2).
func vmsMessagesFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/feeds/vms-messages/snap-1.xml")
	if err != nil {
		t.Fatalf("reading the vms-messages snapshot: %v", err)
	}
	return data
}

// TestParseVmsDevicesFixture pins the observed shape of the 5 trimmed
// devices: id, type, road, destination label, kilometre point, province in
// UPPERCASE (unlike the incident feed, docs/feeds.md §5.1), travel direction
// and coordinates, all in feed order.
func TestParseVmsDevicesFixture(t *testing.T) {
	devices, err := ParseVmsDevices(strings.NewReader(string(vmsLocationsFixture(t))))
	if err != nil {
		t.Fatalf("ParseVmsDevices failed: %v", err)
	}
	if len(devices) != 5 {
		t.Fatalf("ParseVmsDevices returned %d devices, want 5", len(devices))
	}

	first := devices[0]
	if first.ID != "167938" {
		t.Errorf("device[0].ID = %q, want 167938", first.ID)
	}
	if first.Version != "3" {
		t.Errorf("device[0].Version = %q, want 3", first.Version)
	}
	if first.TypeOfDevice != "vms" {
		t.Errorf("device[0].TypeOfDevice = %q, want vms", first.TypeOfDevice)
	}
	if first.RoadName != "A-54" {
		t.Errorf("device[0].RoadName = %q, want A-54", first.RoadName)
	}
	if first.RoadDestination != "LUGO" {
		t.Errorf("device[0].RoadDestination = %q, want LUGO", first.RoadDestination)
	}
	if first.Province != "LUGO" {
		t.Errorf("device[0].Province = %q, want the source text LUGO (this feed is uppercase)", first.Province)
	}
	if first.DirectionRoad != "negative" {
		t.Errorf("device[0].DirectionRoad = %q, want negative", first.DirectionRoad)
	}
	if first.KilometerPoint == nil || *first.KilometerPoint != 0.65 {
		t.Errorf("device[0].KilometerPoint = %v, want 0.65", first.KilometerPoint)
	}
	if first.Coordinates == nil || first.Coordinates.Latitude != 42.96447 || first.Coordinates.Longitude != -7.5005 {
		t.Errorf("device[0].Coordinates = %+v, want 42.96447/-7.5005", first.Coordinates)
	}
	wantLastUpdate := time.Date(2026, 6, 8, 7, 11, 25, 0, time.UTC)
	if !first.LastUpdate.Equal(wantLastUpdate) {
		t.Errorf("device[0].LastUpdate = %v, want %v (UTC)", first.LastUpdate, wantLastUpdate)
	}

	second := devices[1]
	if second.ID != "61441" || second.RoadName != "M-607" || second.RoadDestination != "MADRID" {
		t.Errorf("device[1] = %+v, want id 61441 on M-607 towards MADRID", second)
	}
	if second.KilometerPoint == nil || *second.KilometerPoint != 18.532 {
		t.Errorf("device[1].KilometerPoint = %v, want 18.532", second.KilometerPoint)
	}

	last := devices[4]
	if last.ID != "167937" || last.Version != "1" || last.RoadName != "A-6" || last.RoadDestination != "MADRID" {
		t.Errorf("device[4] = %+v, want id 167937 version 1 on A-6 towards MADRID", last)
	}
	if last.KilometerPoint == nil || *last.KilometerPoint != 486.9 {
		t.Errorf("device[4].KilometerPoint = %v, want 486.9", last.KilometerPoint)
	}
}

// minimalVmsLocationsBody is a synthetic DevicePublication with one device
// that carries none of the optional fields, so the P-04 rule (absent stays
// absent) is pinned against the real dialect.
const minimalVmsLocationsBody = `<?xml version="1.0" encoding="UTF-8"?>
<d2:payload xsi:type="ns2:DevicePublication" xmlns:com="http://levelC/schema/3/common" xmlns:ns2="http://levelC/schema/3/faultAndStatus" xmlns:loc="http://levelC/schema/3/locationReferencing" xmlns:lse="http://levelC/schema/3/locationReferencingSpanishExtension" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <com:publicationTime>2026-10-28T12:05:01.219+01:00</com:publicationTime>
  <ns2:device id="42" version="1">
    <ns2:typeOfDevice>vms</ns2:typeOfDevice>
    <ns2:pointLocation>
      <loc:supplementaryPositionalDescription>
        <loc:roadInformation>
          <loc:roadName>ma-10</loc:roadName>
        </loc:roadInformation>
      </loc:supplementaryPositionalDescription>
      <loc:tpegPointLocation xsi:type="loc:TpegSimplePoint">
        <loc:point xsi:type="loc:TpegNonJunctionPoint">
          <loc:_tpegNonJunctionPointExtension>
            <loc:extendedTpegNonJunctionPoint>
              <lse:province>Mallorca</lse:province>
            </loc:extendedTpegNonJunctionPoint>
          </loc:_tpegNonJunctionPointExtension>
        </loc:point>
      </loc:tpegPointLocation>
    </ns2:pointLocation>
  </ns2:device>
</d2:payload>`

// TestParseVmsDevicesKeepsAbsentFieldsEmpty pins P-04 for the device feed:
// no roadDestination, kilometre point, coordinates or direction means empty
// or nil, never an invented value, and the province keeps its source casing.
func TestParseVmsDevicesKeepsAbsentFieldsEmpty(t *testing.T) {
	devices, err := ParseVmsDevices(strings.NewReader(minimalVmsLocationsBody))
	if err != nil {
		t.Fatalf("ParseVmsDevices failed: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("ParseVmsDevices returned %d devices, want 1", len(devices))
	}
	dev := devices[0]
	if dev.ID != "42" {
		t.Errorf("ID = %q, want 42", dev.ID)
	}
	if dev.RoadName != "ma-10" {
		t.Errorf("RoadName = %q, want the source text ma-10 (normalisation is the model's job)", dev.RoadName)
	}
	if dev.RoadDestination != "" {
		t.Errorf("RoadDestination = %q, want empty", dev.RoadDestination)
	}
	if dev.KilometerPoint != nil {
		t.Errorf("KilometerPoint = %v, want nil", dev.KilometerPoint)
	}
	if dev.Coordinates != nil {
		t.Errorf("Coordinates = %+v, want nil", dev.Coordinates)
	}
	if dev.DirectionRoad != "" {
		t.Errorf("DirectionRoad = %q, want empty", dev.DirectionRoad)
	}
	if dev.Province != "Mallorca" {
		t.Errorf("Province = %q, want the source text Mallorca", dev.Province)
	}
	if !dev.LastUpdate.IsZero() {
		t.Errorf("LastUpdate = %v, want zero (the element is absent)", dev.LastUpdate)
	}
}

func TestParseVmsDevicesTruncatedBodyFails(t *testing.T) {
	body := vmsLocationsFixture(t)
	if _, err := ParseVmsDevices(strings.NewReader(string(body[:len(body)/2]))); err == nil {
		t.Fatal("ParseVmsDevices accepted a truncated body, want an error")
	}
}

func TestParseVmsDevicesEmptyBodyFails(t *testing.T) {
	if _, err := ParseVmsDevices(strings.NewReader("")); err == nil {
		t.Fatal("ParseVmsDevices accepted an empty body, want an error")
	}
}

// TestParseVmsMessagesFixture pins the observed shape of the 3 trimmed
// controllers: the reference id that joins to ns2:device/@id, the messages
// with their timeLastSet in UTC, the text lines and the pictograms in
// display-area index order (the fixture publishes areas 1, 3, 2).
func TestParseVmsMessagesFixture(t *testing.T) {
	controllers, err := ParseVmsMessages(strings.NewReader(string(vmsMessagesFixture(t))))
	if err != nil {
		t.Fatalf("ParseVmsMessages failed: %v", err)
	}
	if len(controllers) != 3 {
		t.Fatalf("ParseVmsMessages returned %d controllers, want 3", len(controllers))
	}
	wantIDs := []string{"167938", "167939", "61441"}
	for i, want := range wantIDs {
		if controllers[i].ID != want {
			t.Errorf("controllers[%d].ID = %q, want %q", i, controllers[i].ID, want)
		}
	}

	off := controllers[0]
	if len(off.Messages) != 2 {
		t.Fatalf("controller 167938 has %d messages, want 2", len(off.Messages))
	}
	offMsg := off.Messages[0]
	wantSet := time.Date(2026, 10, 27, 18, 22, 51, 0, time.UTC)
	if !offMsg.TimeLastSet.Equal(wantSet) {
		t.Errorf("message timeLastSet = %v, want %v (UTC)", offMsg.TimeLastSet, wantSet)
	}
	if len(offMsg.TextLines) != 0 {
		t.Errorf("off message text lines = %q, want none", offMsg.TextLines)
	}
	if len(offMsg.Pictograms) != 1 || !strings.HasSuffix(offMsg.Pictograms[0], "/0.png") {
		t.Errorf("off message pictograms = %q, want the blank pictogram 0.png", offMsg.Pictograms)
	}
	if len(offMsg.PictogramCodes) != 1 || offMsg.PictogramCodes[0] != "0" {
		t.Errorf("off message pictogram codes = %q, want [0]", offMsg.PictogramCodes)
	}

	active := controllers[2]
	if len(active.Messages) != 1 {
		t.Fatalf("controller 61441 has %d messages, want 1", len(active.Messages))
	}
	activeMsg := active.Messages[0]
	wantActiveSet := time.Date(2026, 10, 28, 11, 55, 31, 0, time.UTC)
	if !activeMsg.TimeLastSet.Equal(wantActiveSet) {
		t.Errorf("active message timeLastSet = %v, want %v (UTC)", activeMsg.TimeLastSet, wantActiveSet)
	}
	if len(activeMsg.TextLines) != 1 || activeMsg.TextLines[0] != "VELOCIDAD CONTROLADA POR RADAR" {
		t.Errorf("active message text lines = %q, want [VELOCIDAD CONTROLADA POR RADAR]", activeMsg.TextLines)
	}
	wantPictos := []string{
		"https://etraffic.dgt.es/estaticosEtraffic/Iconografia/pictogramas/R301100I.png",
		"https://etraffic.dgt.es/estaticosEtraffic/Iconografia/pictogramas/E17.png",
	}
	if len(activeMsg.Pictograms) != 2 || activeMsg.Pictograms[0] != wantPictos[0] || activeMsg.Pictograms[1] != wantPictos[1] {
		t.Errorf("active message pictograms = %q, want %q in display-area order", activeMsg.Pictograms, wantPictos)
	}
	if len(activeMsg.PictogramCodes) != 2 || activeMsg.PictogramCodes[0] != "R301100I" || activeMsg.PictogramCodes[1] != "E17" {
		t.Errorf("active message pictogram codes = %q, want [R301100I E17]", activeMsg.PictogramCodes)
	}
}

func TestParseVmsMessagesTruncatedBodyFails(t *testing.T) {
	body := vmsMessagesFixture(t)
	if _, err := ParseVmsMessages(strings.NewReader(string(body[:len(body)/2]))); err == nil {
		t.Fatal("ParseVmsMessages accepted a truncated body, want an error")
	}
}

func TestParseVmsMessagesEmptyBodyFails(t *testing.T) {
	if _, err := ParseVmsMessages(strings.NewReader("")); err == nil {
		t.Fatal("ParseVmsMessages accepted an empty body, want an error")
	}
}

// TestParseVmsPublicationTimeFromFixtures pins the freshness source of both
// sign feeds (docs/feeds.md §9): both carry com:publicationTime and it is
// normalised to UTC. A body without one reads as zero, not as an invented
// instant.
func TestParseVmsPublicationTimeFromFixtures(t *testing.T) {
	devices, err := ParseVmsPublicationTime(strings.NewReader(string(vmsLocationsFixture(t))))
	if err != nil {
		t.Fatalf("ParseVmsPublicationTime failed: %v", err)
	}
	wantDevices := time.Date(2026, 10, 28, 11, 5, 1, 219000000, time.UTC)
	if !devices.Equal(wantDevices) {
		t.Errorf("locations publicationTime = %v, want %v", devices, wantDevices)
	}

	messages, err := ParseVmsPublicationTime(strings.NewReader(string(vmsMessagesFixture(t))))
	if err != nil {
		t.Fatalf("ParseVmsPublicationTime failed: %v", err)
	}
	wantMessages := time.Date(2026, 10, 28, 12, 4, 37, 0, time.UTC)
	if !messages.Equal(wantMessages) {
		t.Errorf("messages publicationTime = %v, want %v", messages, wantMessages)
	}

	none, err := ParseVmsPublicationTime(strings.NewReader(minimalVmsLocationsBody[:0]))
	if err != nil {
		t.Fatalf("ParseVmsPublicationTime on an empty body failed: %v", err)
	}
	if !none.IsZero() {
		t.Errorf("publicationTime of an empty body = %v, want zero", none)
	}
}

// TestParseVmsDevicesRejectsForeignDialect pins the payload identity of the
// device parser (review FEED-03): the v3.7 situations payload, a v1.0 radar
// body and a v3 tefiva body carry no ns2:device, so without an identity
// check they parsed as zero devices and no error, and `trafico signs`
// exited 0 with signs: [] instead of feed_changed (exit 7).
func TestParseVmsDevicesRejectsForeignDialect(t *testing.T) {
	for _, rel := range []string{
		"../../testdata/feeds/situations/snap-1.xml",
		"../../testdata/feeds/radars/snap-1.xml",
		"../../testdata/feeds/tefiva/snap-1.xml",
	} {
		body, err := os.ReadFile(rel)
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
		if devs, err := ParseVmsDevices(strings.NewReader(string(body))); err == nil || len(devs) != 0 {
			t.Errorf("%s: got %d devices, err %v; want a feed-changed error", rel, len(devs), err)
		}
	}
}

// TestParseVmsMessagesRejectsForeignDialect pins the same payload identity
// for the message parser (review FEED-03): a body from another DGT dialect
// must be a feed-changed error, never a silent zero-controller result.
func TestParseVmsMessagesRejectsForeignDialect(t *testing.T) {
	for _, rel := range []string{
		"../../testdata/feeds/situations/snap-1.xml",
		"../../testdata/feeds/radars/snap-1.xml",
		"../../testdata/feeds/tefiva/snap-1.xml",
	} {
		body, err := os.ReadFile(rel)
		if err != nil {
			t.Fatalf("reading %s: %v", rel, err)
		}
		if ctrls, err := ParseVmsMessages(strings.NewReader(string(body))); err == nil || len(ctrls) != 0 {
			t.Errorf("%s: got %d controllers, err %v; want a feed-changed error", rel, len(ctrls), err)
		}
	}
}

// minimalVmsMessagesBody is a synthetic VmsPublication with no controllers,
// so the identity check is pinned against a genuine empty publication of
// the right dialect: zero controllers and no error.
const minimalVmsMessagesBody = `<?xml version="1.0" encoding="UTF-8"?>
<d2:payload xmlns:com="http://levelC/schema/3/common" xmlns:vms="http://levelC/schema/3/vms" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="vms:VmsPublication">
  <com:publicationTime>2026-10-28T12:04:37.000+01:00</com:publicationTime>
</d2:payload>`

// TestParseVmsMessagesAcceptsEmptyPublication pins that the identity check
// demands the dialect, not content: a genuine VmsPublication without
// controllers parses as zero controllers and no error.
func TestParseVmsMessagesAcceptsEmptyPublication(t *testing.T) {
	controllers, err := ParseVmsMessages(strings.NewReader(minimalVmsMessagesBody))
	if err != nil {
		t.Fatalf("ParseVmsMessages failed: %v", err)
	}
	if len(controllers) != 0 {
		t.Errorf("controllers = %+v, want none", controllers)
	}
}

// TestParseVmsDevicesWithoutPublicationTimeFails and
// TestParseVmsMessagesWithoutPublicationTimeFails pin the freshness marker
// of docs/feeds.md §9: both sign feeds carry com:publicationTime, the same
// guard ParseSituations applies (parse.go), and a same-dialect body without
// one is a changed feed, not a silent empty result.
func TestParseVmsDevicesWithoutPublicationTimeFails(t *testing.T) {
	body := strings.Replace(minimalVmsLocationsBody,
		"<com:publicationTime>2026-10-28T12:05:01.219+01:00</com:publicationTime>", "", 1)
	if _, err := ParseVmsDevices(strings.NewReader(body)); err == nil {
		t.Fatal("ParseVmsDevices accepted a body without com:publicationTime, want an error")
	}
}

func TestParseVmsMessagesWithoutPublicationTimeFails(t *testing.T) {
	body := strings.Replace(minimalVmsMessagesBody,
		"<com:publicationTime>2026-10-28T12:04:37.000+01:00</com:publicationTime>", "", 1)
	if _, err := ParseVmsMessages(strings.NewReader(body)); err == nil {
		t.Fatal("ParseVmsMessages accepted a body without com:publicationTime, want an error")
	}
}
