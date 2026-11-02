package model

import (
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/datex2"
	"github.com/aam9063/trafico-cli/internal/road"
)

// Sign is the normalized variable message sign ("panel de mensaje variable")
// of the JSON contract. It joins the two VMS feeds of docs/feeds.md §5 by
// id (S6: nothing is invented — every absent field serialises as null):
//
//   - road is the normalised road name (docs/feeds.md §4.7 rules);
//   - direction_label is loc:roadDestination, the real destination label
//     that the incident feed does not carry (§4.3, §5.1); null for the
//     ~38 % of panels that publish none;
//   - province keeps the source text, which is UPPERCASE in this feed
//     (§5.1) unlike the incident feed; filters fold it with ProvinceEqual;
//   - messages carries the rendered text lines and pictogram URLs of the
//     panel's current message.
//
// Off panels ("apagado": blank text lines, pictogram 0.png, §5.2) are
// honest: a message whose every text line is blank and whose every
// pictogram is the blank glyph (code "0") is not emitted at all, so such a
// panel serialises messages as [] (never null, never an empty string) and
// message_updated_at as null. Rationale: an empty text line would read as
// displayed content and null messages would be ambiguous with "the model
// has no field"; an empty array states plainly "no visible message", which
// also covers a device with no controller entry in the messages feed.
type Sign struct {
	SchemaVersion    int           `json:"schema_version"`
	ID               string        `json:"id"`
	Source           string        `json:"source"`
	SourceVersion    string        `json:"source_version"`
	Road             string        `json:"road"`
	PK               *float64      `json:"pk"`
	Direction        *string       `json:"direction"`
	DirectionLabel   *string       `json:"direction_label"`
	Province         string        `json:"province"`
	Location         *Coordinates  `json:"location"`
	Messages         []SignMessage `json:"messages"`
	MessageUpdatedAt *string       `json:"message_updated_at"`
}

// SignMessage is one rendered message of a sign: the text lines and the
// pictogram URLs, in display-area index order. TextLines carries only real
// text; a blank line never serialises as an empty string.
type SignMessage struct {
	TextLines  []string `json:"text_lines"`
	Pictograms []string `json:"pictograms"`
}

// SignFromDevice converts a parsed device and the messages of the
// controller that shares its id into the contract sign. msgs is the
// controller's message list (empty when the messages feed has no entry for
// the device, or when the panel is off).
func SignFromDevice(dev datex2.VmsDevice, msgs []datex2.VmsMessage) Sign {
	sign := Sign{
		SchemaVersion:  SchemaVersion,
		ID:             "dgt:" + dev.ID,
		Source:         Source,
		SourceVersion:  SourceVersion,
		Road:           road.Normalize(dev.RoadName),
		PK:             dev.KilometerPoint,
		Direction:      direction(dev.DirectionRoad),
		DirectionLabel: optionalString(strings.TrimSpace(dev.RoadDestination)),
		Province:       dev.Province,
		Location:       coordinates(dev.Coordinates),
		Messages:       []SignMessage{},
	}
	kept := make([]SignMessage, 0, len(msgs))
	var latest time.Time
	for _, msg := range msgs {
		if isOffMessage(msg) {
			continue
		}
		kept = append(kept, SignMessage{
			TextLines:  nonBlankLines(msg.TextLines),
			Pictograms: msg.Pictograms,
		})
		if msg.TimeLastSet.After(latest) {
			latest = msg.TimeLastSet
		}
	}
	sign.Messages = kept
	if !latest.IsZero() {
		stamp := latest.UTC().Format(time.RFC3339)
		sign.MessageUpdatedAt = &stamp
	}
	return sign
}

// isOffMessage reports whether a parsed message is an off panel: every text
// line blank and every pictogram the blank glyph (code "0", docs/feeds.md
// §5.2). A message with any real content is never off — including a message
// that carries a real pictogramDisplayUrl whose customPictogramCode the feed
// omitted (review FEED-03): the URL is content on its own, and only the
// documented blank glyph (pictogramas/0.png) may hide a panel that publishes
// it without a code.
func isOffMessage(msg datex2.VmsMessage) bool {
	for _, line := range msg.TextLines {
		if strings.TrimSpace(line) != "" {
			return false
		}
	}
	for i, url := range msg.Pictograms {
		code := ""
		if i < len(msg.PictogramCodes) {
			code = msg.PictogramCodes[i]
		}
		if code == "0" {
			continue // the blank glyph, with its code
		}
		if code == "" && strings.HasSuffix(url, "/0.png") {
			continue // the blank glyph's URL with its code omitted
		}
		return false
	}
	// Pictogram codes beyond the URL list (an area that published only the
	// code) keep the original rule: anything but the blank glyph "0" is
	// content.
	if len(msg.PictogramCodes) > len(msg.Pictograms) {
		for _, code := range msg.PictogramCodes[len(msg.Pictograms):] {
			if code != "0" {
				return false
			}
		}
	}
	return true
}

// nonBlankLines keeps the real text lines of a kept message; blank lines
// are display-area emptiness, not content.
func nonBlankLines(lines []string) []string {
	out := []string{}
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

// optionalString maps an empty string to nil so absent fields serialise as
// null, never as an empty string.
func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
