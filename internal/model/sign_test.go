package model

import (
	"testing"
	"time"

	"github.com/aam9063/trafico-cli/internal/datex2"
)

func floatPtrForSign(v float64) *float64 { return &v }

// TestSignFromDeviceWithActiveMessage pins the wire shape of a sign with a
// real message: the dgt: id of the incident contract, the normalised road,
// the source province text (this feed is uppercase, docs/feeds.md §5.1), the
// destination label, the UTC timestamp and the rendered message.
func TestSignFromDeviceWithActiveMessage(t *testing.T) {
	dev := datex2.VmsDevice{
		ID:              "61441",
		Version:         "4",
		TypeOfDevice:    "vms",
		RoadName:        "m 607",
		RoadDestination: "MADRID",
		KilometerPoint:  floatPtrForSign(18.532),
		Province:        "MADRID",
		DirectionRoad:   "negative",
		Coordinates:     &datex2.Coordinates{Latitude: 40.56056, Longitude: -3.713396},
	}
	msg := datex2.VmsMessage{
		TimeLastSet:    time.Date(2026, 10, 28, 11, 55, 31, 0, time.UTC),
		TextLines:      []string{"VELOCIDAD CONTROLADA POR RADAR"},
		Pictograms:     []string{"https://etraffic.dgt.es/estaticosEtraffic/Iconografia/pictogramas/R301100I.png"},
		PictogramCodes: []string{"R301100I"},
	}
	sign := SignFromDevice(dev, []datex2.VmsMessage{msg})

	if sign.SchemaVersion != SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", sign.SchemaVersion, SchemaVersion)
	}
	if sign.ID != "dgt:61441" {
		t.Errorf("ID = %q, want dgt:61441", sign.ID)
	}
	if sign.Source != Source || sign.SourceVersion != SourceVersion {
		t.Errorf("Source = %q, SourceVersion = %q, want %q/%q", sign.Source, sign.SourceVersion, Source, SourceVersion)
	}
	if sign.Road != "M-607" {
		t.Errorf("Road = %q, want the normalised M-607", sign.Road)
	}
	if sign.PK == nil || *sign.PK != 18.532 {
		t.Errorf("PK = %v, want 18.532", sign.PK)
	}
	if sign.Direction == nil || *sign.Direction != "negative" {
		t.Errorf("Direction = %v, want negative", sign.Direction)
	}
	if sign.DirectionLabel == nil || *sign.DirectionLabel != "MADRID" {
		t.Errorf("DirectionLabel = %v, want MADRID", sign.DirectionLabel)
	}
	if sign.Province != "MADRID" {
		t.Errorf("Province = %q, want the source text MADRID", sign.Province)
	}
	if sign.Location == nil || sign.Location.Latitude != 40.56056 || sign.Location.Longitude != -3.713396 {
		t.Errorf("Location = %+v, want 40.56056/-3.713396", sign.Location)
	}
	if len(sign.Messages) != 1 {
		t.Fatalf("Messages = %+v, want one message", sign.Messages)
	}
	if got := sign.Messages[0].TextLines; len(got) != 1 || got[0] != "VELOCIDAD CONTROLADA POR RADAR" {
		t.Errorf("Messages[0].TextLines = %q, want the rendered text", got)
	}
	if got := sign.Messages[0].Pictograms; len(got) != 1 || got[0] != "https://etraffic.dgt.es/estaticosEtraffic/Iconografia/pictogramas/R301100I.png" {
		t.Errorf("Messages[0].Pictograms = %q, want the pictogram URL", got)
	}
	if sign.MessageUpdatedAt == nil || *sign.MessageUpdatedAt != "2026-10-28T11:55:31Z" {
		t.Errorf("MessageUpdatedAt = %v, want 2026-10-28T11:55:31Z", sign.MessageUpdatedAt)
	}
}

// TestSignFromDeviceDropsOffMessages pins the off-panel rule: a message with
// only blank text lines and the blank pictogram (code "0") is an off panel
// and is not emitted as an empty message; an active message on the same
// panel survives, and message_updated_at follows the kept messages only.
func TestSignFromDeviceDropsOffMessages(t *testing.T) {
	off := datex2.VmsMessage{
		TimeLastSet:    time.Date(2026, 10, 27, 18, 22, 51, 0, time.UTC),
		TextLines:      []string{"", "   "},
		Pictograms:     []string{"https://etraffic.dgt.es/estaticosEtraffic/Iconografia/pictogramas/0.png"},
		PictogramCodes: []string{"0"},
	}
	active := datex2.VmsMessage{
		TimeLastSet:    time.Date(2026, 10, 28, 11, 55, 31, 0, time.UTC),
		TextLines:      []string{"OBRAS"},
		Pictograms:     []string{"https://etraffic.dgt.es/estaticosEtraffic/Iconografia/pictogramas/E17.png"},
		PictogramCodes: []string{"E17"},
	}
	sign := SignFromDevice(datex2.VmsDevice{ID: "61441", RoadName: "M-607"}, []datex2.VmsMessage{off, active})

	if len(sign.Messages) != 1 {
		t.Fatalf("Messages = %+v, want only the active message", sign.Messages)
	}
	if len(sign.Messages[0].TextLines) != 1 || sign.Messages[0].TextLines[0] != "OBRAS" {
		t.Errorf("Messages[0].TextLines = %q, want [OBRAS]", sign.Messages[0].TextLines)
	}
	if sign.MessageUpdatedAt == nil || *sign.MessageUpdatedAt != "2026-10-28T11:55:31Z" {
		t.Errorf("MessageUpdatedAt = %v, want the active message time", sign.MessageUpdatedAt)
	}
}

// TestSignFromDeviceOffOnlyMeansNoMessages pins the documented
// representation of a panel that is off: messages serialises as [] (never
// null, never an empty string) and message_updated_at as null, because an
// empty text line would read as content and null messages would be
// ambiguous with "the model has no field".
func TestSignFromDeviceOffOnlyMeansNoMessages(t *testing.T) {
	off := datex2.VmsMessage{
		TimeLastSet:    time.Date(2026, 10, 27, 18, 22, 51, 0, time.UTC),
		TextLines:      []string{"   "},
		PictogramCodes: []string{"0"},
	}
	sign := SignFromDevice(datex2.VmsDevice{ID: "167938", RoadName: "A-54"}, []datex2.VmsMessage{off})

	if sign.Messages == nil {
		t.Fatal("Messages = nil, want an empty (non-nil) array so JSON renders []")
	}
	if len(sign.Messages) != 0 {
		t.Fatalf("Messages = %+v, want none", sign.Messages)
	}
	if sign.MessageUpdatedAt != nil {
		t.Errorf("MessageUpdatedAt = %v, want nil for an off panel", sign.MessageUpdatedAt)
	}
}

// TestSignFromDeviceNullsWhenFeedOmitsFields pins P-04 for the sign model:
// absent roadDestination, kilometer point, direction or coordinates
// serialise as null, and the road name is normalised.
func TestSignFromDeviceNullsWhenFeedOmitsFields(t *testing.T) {
	dev := datex2.VmsDevice{ID: "42", TypeOfDevice: "vms", RoadName: "ma-10", Province: "Mallorca", DirectionRoad: "unknown"}
	sign := SignFromDevice(dev, nil)

	if sign.Road != "MA-10" {
		t.Errorf("Road = %q, want the normalised MA-10", sign.Road)
	}
	if sign.PK != nil {
		t.Errorf("PK = %v, want nil", sign.PK)
	}
	if sign.Direction != nil {
		t.Errorf("Direction = %v, want nil for a value outside positive|negative|both", sign.Direction)
	}
	if sign.DirectionLabel != nil {
		t.Errorf("DirectionLabel = %v, want nil", sign.DirectionLabel)
	}
	if sign.Location != nil {
		t.Errorf("Location = %+v, want nil", sign.Location)
	}
	if sign.Messages == nil || len(sign.Messages) != 0 {
		t.Errorf("Messages = %+v, want an empty non-nil array", sign.Messages)
	}
	if sign.MessageUpdatedAt != nil {
		t.Errorf("MessageUpdatedAt = %v, want nil", sign.MessageUpdatedAt)
	}
}

// TestSignFromDeviceKeepsLatestMessageTime pins message_updated_at: with
// several kept messages it is the most recent timeLastSet, never the first.
func TestSignFromDeviceKeepsLatestMessageTime(t *testing.T) {
	older := datex2.VmsMessage{
		TimeLastSet: time.Date(2026, 10, 27, 18, 22, 51, 0, time.UTC),
		TextLines:   []string{"ANTERIOR"},
	}
	newer := datex2.VmsMessage{
		TimeLastSet:    time.Date(2026, 10, 28, 11, 55, 31, 0, time.UTC),
		Pictograms:     []string{"https://etraffic.dgt.es/estaticosEtraffic/Iconografia/pictogramas/E17.png"},
		PictogramCodes: []string{"E17"},
	}
	sign := SignFromDevice(datex2.VmsDevice{ID: "1"}, []datex2.VmsMessage{older, newer})

	if sign.MessageUpdatedAt == nil || *sign.MessageUpdatedAt != "2026-10-28T11:55:31Z" {
		t.Errorf("MessageUpdatedAt = %v, want the latest timeLastSet", sign.MessageUpdatedAt)
	}
}

// TestSignFromDeviceBlankLabelIsNull pins the trimming of the destination
// label: whitespace-only is absent (null), never an empty string.
func TestSignFromDeviceBlankLabelIsNull(t *testing.T) {
	sign := SignFromDevice(datex2.VmsDevice{ID: "1", RoadDestination: "   "}, nil)
	if sign.DirectionLabel != nil {
		t.Errorf("DirectionLabel = %v, want nil for a blank label", sign.DirectionLabel)
	}
}

// TestSignFromDeviceKeepsPictogramWithoutCode pins the off-panel edge of the
// review (FEED-03): a message carrying a real pictogramDisplayUrl but no
// customPictogramCode is content, never an off panel. In the measured feed
// the code is always present (2032/2032), so the rule must not depend on it.
func TestSignFromDeviceKeepsPictogramWithoutCode(t *testing.T) {
	sign := SignFromDevice(datex2.VmsDevice{ID: "1", RoadName: "A-1"},
		[]datex2.VmsMessage{{TimeLastSet: time.Date(2026, 10, 28, 11, 0, 0, 0, time.UTC),
			Pictograms: []string{"https://etraffic.dgt.es/estaticosEtraffic/Iconografia/pictogramas/X.png"}}})
	if len(sign.Messages) != 1 {
		t.Fatalf("Messages = %+v, want the pictogram-only message kept", sign.Messages)
	}
}
