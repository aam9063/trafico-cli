package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/model"
)

// The text renderers of the static datasets (FORMAT-02): signs, radars and
// sections — one aligned row per record of a feed that publishes whole
// catalogues rather than live events. `trafico route check` reuses signText
// for the panels it reports.

// signText joins the text lines of every visible message of a panel; an off
// or silent panel reads as "(sin mensaje visible)".
func signText(sign model.Sign) string {
	lines := make([]string, 0, 3)
	for _, message := range sign.Messages {
		lines = append(lines, message.TextLines...)
	}
	if len(lines) == 0 {
		return "(sin mensaje visible)"
	}
	return strings.Join(lines, " / ")
}

// renderSignsText renders `trafico signs` for a human (FORMAT-02): a
// heading with the count and one aligned row per panel with its road, PK,
// destination label, province and the message it displays.
func renderSignsText(w io.Writer, v any) error {
	response, ok := v.(app.SignsResponse)
	if !ok {
		return errs.NewInternal("el render de texto de signs recibió una respuesta de otro tipo")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Paneles de mensaje variable (%d):\n\n", len(response.Signs))
	if len(response.Signs) == 0 {
		b.WriteString("(sin resultados)\n")
	}
	table := newTextTable(false, false, false, false, true)
	for _, sign := range response.Signs {
		pk := "-"
		if sign.PK != nil {
			pk = formatPKValue(*sign.PK)
		}
		table.addRow(sign.Road, pk, optionalToDash(sign.DirectionLabel),
			emptyToDash(sign.Province), signText(sign))
	}
	truncated := table.writeTo(&b, "  ", nil)
	b.WriteString(publishedNote(response.Meta))
	b.WriteString(truncationNote(truncated))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// renderRadarsText renders `trafico radars` for a human (FORMAT-02): a
// heading with the count and one aligned row per radar with its road,
// kilometre interval, province, road kind and destination label.
func renderRadarsText(w io.Writer, v any) error {
	response, ok := v.(app.RadarsResponse)
	if !ok {
		return errs.NewInternal("el render de texto de radars recibió una respuesta de otro tipo")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Radares fijos (%d):\n\n", len(response.Radars))
	if len(response.Radars) == 0 {
		b.WriteString("(sin resultados)\n")
	}
	table := newTextTable(false, false, false, false, true)
	truncated := 0
	for _, radar := range response.Radars {
		// The road kind is counted as truncated but rendered in full: the
		// pre-split renderer truncated a copy and printed the column value
		// (kinds[i]) unchanged. The quirk is preserved byte for byte here;
		// fixing it is a user-visible change for a follow-up task.
		if kind := optionalToDash(radar.Singularity); len([]rune(kind)) > textColumnWidth {
			truncated++
		}
		table.addRow(radar.Road, formatPK(radar.PK), optionalToDash(radar.Province),
			optionalToDash(radar.Singularity), optionalToDash(radar.DirectionLabel))
	}
	// The destination label, the free-text column, truncates and counts.
	truncated += table.writeTo(&b, "  ", nil)
	b.WriteString(publishedNote(response.Meta))
	b.WriteString(truncationNote(truncated))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// renderSectionsText renders `trafico sections` for a human (FORMAT-02): a
// heading naming the dataset with the count, and one aligned row per
// section with its road, kilometre interval, province, length and direction.
func renderSectionsText(w io.Writer, v any) error {
	response, ok := v.(app.SectionsResponse)
	if !ok {
		return errs.NewInternal("el render de texto de sections recibió una respuesta de otro tipo")
	}
	// The kind comes from the response, not from the first match: an empty
	// result has no first section to read it from and used to print
	// "Tramos  (0):" with a double space and no dataset name.
	kind := response.Kind
	var b strings.Builder
	fmt.Fprintf(&b, "Tramos %s (%d):\n\n", kind, len(response.Sections))
	if len(response.Sections) == 0 {
		b.WriteString("(sin resultados)\n")
	}
	table := newTextTable()
	for _, section := range response.Sections {
		length := "-"
		if section.LengthKM != nil {
			length = formatPKValue(*section.LengthKM) + " km"
		}
		table.addRow(section.Road, formatPK(section.PK), optionalToDash(section.Province),
			length, optionalToDash(section.Direction))
	}
	// The dataset kind travels in the heading; nothing else truncates here.
	table.writeTo(&b, "  ", nil)
	b.WriteString(publishedNote(response.Meta))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}
