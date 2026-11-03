package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/output"
	"github.com/aam9063/trafico-cli/internal/store"
)

// The human-readable renderers of FORMAT-02: every data command turns its
// §5.3 envelope into something a person reads — a heading with the count,
// aligned rows or labelled Spanish fields (the way `trafico skills` does
// it), closed by the attribution line (ARRANQUE section 2.1). No ANSI
// (ARRANQUE section 6.1). The --json contract is untouched: these run only
// when the user asked for the readable default.

// textColumnWidth is the maximum width of a free-text column in these
// renderers. It deliberately matches the description column of
// `trafico schema` (schema.go): one truncation rule for the whole CLI.
const textColumnWidth = descriptionColumnWidth

// truncateText cuts a free-text field to the width at a word boundary,
// ending with an ellipsis when it truncates (the schema.go rule). The
// truncation is presentation only: --json keeps the full text.
func truncateText(s string, width int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= width {
		return string(runes)
	}
	cut := width
	for cut > 0 && !unicode.IsSpace(runes[cut-1]) {
		cut--
	}
	if cut == 0 {
		cut = width
	}
	return string(runes[:cut]) + "…"
}

// truncationNote is the closing line that tells the user how many free-text
// fields were cut (FORMAT-02); empty when none were.
func truncationNote(count int) string {
	switch count {
	case 0:
		return ""
	case 1:
		return "Nota: 1 descripción truncada; usa --json para verla completa.\n"
	default:
		return fmt.Sprintf("Nota: %d descripciones truncadas; usa --json para verlas completas.\n", count)
	}
}

// publishedNote renders meta.published_at for the human output (P-08: a
// reader never subtracts dates), plus the cache note when the body came
// from the shared cache; empty when the feed publishes no publication time.
func publishedNote(meta *output.Meta) string {
	if meta == nil || meta.PublishedAt == nil {
		return ""
	}
	line := "Publicado: " + meta.PublishedAt.UTC().Format(time.RFC3339)
	if meta.FromCache {
		line += " (desde la caché)"
	}
	return line + "\n"
}

// formatPKValue prints a kilometre point without trailing zeros
// (26.6 stays 26.6, 37.0 stays 37).
func formatPKValue(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// formatPK prints a model.PK: an interval "40-55", a point "26.6" or "-"
// when the record carries no kilometre point (P-04: nothing invented).
func formatPK(pk *model.PK) string {
	switch {
	case pk == nil || (pk.From == nil && pk.To == nil):
		return "-"
	case pk.To == nil:
		return formatPKValue(*pk.From)
	case pk.From == nil:
		return "-" + formatPKValue(*pk.To)
	default:
		return formatPKValue(*pk.From) + "-" + formatPKValue(*pk.To)
	}
}

// formatPKInterval prints a concrete from-to kilometre interval, the shape
// route segments and watch road targets carry.
func formatPKInterval(from, to float64) string {
	return formatPKValue(from) + "-" + formatPKValue(to)
}

// formatAgeSeconds prints an age as a short human duration ("42s",
// "12min", "5h", "3d"); "-" when there is none.
func formatAgeSeconds(age *int64) string {
	if age == nil {
		return "-"
	}
	switch s := *age; {
	case s < 90:
		return strconv.FormatInt(s, 10) + "s"
	case s < 90*60:
		return strconv.FormatInt(s/60, 10) + "min"
	case s < 48*3600:
		return strconv.FormatInt(s/3600, 10) + "h"
	default:
		return strconv.FormatInt(s/86400, 10) + "d"
	}
}

// formatSizeBytes prints a body size in human units ("7.1 MB", "512 kB",
// "300 B"); "-" when it is unknown.
func formatSizeBytes(size *int64) string {
	if size == nil {
		return "-"
	}
	switch b := *size; {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// columnWidth returns the display width of the widest value in the column.
func columnWidth(values []string) int {
	width := 0
	for _, value := range values {
		if l := len([]rune(value)); l > width {
			width = l
		}
	}
	return width
}

// incidentText is the free text of an incident for a row: the original
// cause value the feed published (P-04), never a synthesised sentence.
func incidentText(incident model.Incident) string {
	return incident.SubtypeRaw
}

// renderIncidentsText renders `trafico incidents` (a list) and
// `trafico incidents show` (a detail) for a human. The two share the
// response type; the show invocation carries no query echo, and that is the
// shape the detail render gives it.
func renderIncidentsText(w io.Writer, v any) error {
	response, ok := v.(app.IncidentsResponse)
	if !ok {
		return errs.NewInternal("el render de texto de incidents recibió una respuesta de otro tipo")
	}
	if response.Query == nil {
		return writeIncidentDetail(w, response)
	}
	return writeIncidentList(w, response)
}

// writeIncidentList renders the incidents list: a heading with the count,
// one aligned row per incident (road, PK, type, severity, description) and
// the freshness of the data.
func writeIncidentList(w io.Writer, response app.IncidentsResponse) error {
	var b strings.Builder
	fmt.Fprintf(&b, "Incidencias (%d):\n\n", len(response.Incidents))
	if len(response.Incidents) == 0 {
		b.WriteString("(sin resultados)\n")
	}
	roads, pks, types, severities := make([]string, 0, len(response.Incidents)),
		make([]string, 0, len(response.Incidents)),
		make([]string, 0, len(response.Incidents)),
		make([]string, 0, len(response.Incidents))
	for _, incident := range response.Incidents {
		roads = append(roads, incident.Road)
		pks = append(pks, formatPK(incident.PK))
		types = append(types, incident.Type)
		severities = append(severities, incident.Severity)
	}
	roadWidth, pkWidth := columnWidth(roads), columnWidth(pks)
	typeWidth, severityWidth := columnWidth(types), columnWidth(severities)

	truncated := 0
	for i, incident := range response.Incidents {
		description := incidentText(incident)
		if len([]rune(description)) > textColumnWidth {
			description = truncateText(description, textColumnWidth)
			truncated++
		}
		fmt.Fprintf(&b, "  %-*s  %-*s  %-*s  %-*s  %s\n",
			roadWidth, roads[i], pkWidth, pks[i], typeWidth, types[i],
			severityWidth, severities[i], description)
	}
	b.WriteString(publishedNote(response.Meta))
	b.WriteString(truncationNote(truncated))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// detailLabelWidth aligns the labels of every detail render. It covers the
// longest label in use ("Severidad mínima:" for watches).
const detailLabelWidth = 18

// writeDetailField prints one labelled Spanish field of a detail render.
func writeDetailField(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "  %-*s %s\n", detailLabelWidth, label+":", value)
}

// writeIncidentDetail renders `trafico incidents show <id>`: labelled
// Spanish fields, the way `trafico skills get` does it.
func writeIncidentDetail(w io.Writer, response app.IncidentsResponse) error {
	if len(response.Incidents) != 1 {
		return errs.NewInternal("el render de texto de incidents show espera exactamente una incidencia")
	}
	incident := response.Incidents[0]
	var b strings.Builder
	fmt.Fprintf(&b, "Incidencia %s\n\n", incident.ID)
	writeDetailField(&b, "Tipo", incident.Type)
	writeDetailField(&b, "Severidad", incident.Severity)
	writeDetailField(&b, "Estado", incident.Status)
	writeDetailField(&b, "Carretera", incident.Road)
	writeDetailField(&b, "PK", formatPK(incident.PK))
	writeDetailField(&b, "Dirección", directionText(incident.Direction, incident.DirectionBearing))
	writeDetailField(&b, "Provincia", emptyToDash(incident.Location.Province))
	writeDetailField(&b, "Municipio", emptyToDash(incident.Location.Municipality))
	writeDetailField(&b, "Inicio", emptyToDash(incident.StartTime))
	if incident.EndTime != nil {
		writeDetailField(&b, "Fin", *incident.EndTime)
	}
	if len(incident.LanesAffected) > 0 {
		writeDetailField(&b, "Carriles", strings.Join(incident.LanesAffected, ", "))
	}
	writeDetailField(&b, "Actualizado", emptyToDash(incident.UpdatedAt))
	if text := incidentText(incident); text != "" {
		writeDetailField(&b, "Descripción", text)
	}
	b.WriteString(publishedNote(response.Meta))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// directionText renders the direction pair: the published travel direction
// plus the compass bearing in parentheses when the feed carries one.
func directionText(direction, bearing *string) string {
	parts := make([]string, 0, 2)
	for _, value := range []*string{direction, bearing} {
		if value != nil && *value != "" {
			parts = append(parts, *value)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " (") + strings.Repeat(")", len(parts)-1)
}

// emptyToDash prints the mandatory display value of an absent field: a
// dash, never an invented value (P-04).
func emptyToDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

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
	roads, pks, labels, provinces, messages := make([]string, 0, len(response.Signs)),
		make([]string, 0, len(response.Signs)), make([]string, 0, len(response.Signs)),
		make([]string, 0, len(response.Signs)), make([]string, 0, len(response.Signs))
	for _, sign := range response.Signs {
		roads = append(roads, sign.Road)
		pk := "-"
		if sign.PK != nil {
			pk = formatPKValue(*sign.PK)
		}
		pks = append(pks, pk)
		labels = append(labels, optionalToDash(sign.DirectionLabel))
		provinces = append(provinces, emptyToDash(sign.Province))
		messages = append(messages, signText(sign))
	}
	roadWidth, pkWidth := columnWidth(roads), columnWidth(pks)
	labelWidth, provinceWidth := columnWidth(labels), columnWidth(provinces)

	truncated := 0
	for i := range response.Signs {
		message := messages[i]
		if len([]rune(message)) > textColumnWidth {
			message = truncateText(message, textColumnWidth)
			truncated++
		}
		fmt.Fprintf(&b, "  %-*s  %-*s  %-*s  %-*s  %s\n",
			roadWidth, roads[i], pkWidth, pks[i], labelWidth, labels[i],
			provinceWidth, provinces[i], message)
	}
	b.WriteString(publishedNote(response.Meta))
	b.WriteString(truncationNote(truncated))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// optionalToDash prints an optional string as "-" when the feed carries none.
func optionalToDash(s *string) string {
	if s == nil || *s == "" {
		return "-"
	}
	return *s
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
	roads, pks, provinces, kinds, labels := make([]string, 0, len(response.Radars)),
		make([]string, 0, len(response.Radars)), make([]string, 0, len(response.Radars)),
		make([]string, 0, len(response.Radars)), make([]string, 0, len(response.Radars))
	for _, radar := range response.Radars {
		roads = append(roads, radar.Road)
		pks = append(pks, formatPK(radar.PK))
		provinces = append(provinces, optionalToDash(radar.Province))
		kinds = append(kinds, optionalToDash(radar.Singularity))
		labels = append(labels, optionalToDash(radar.DirectionLabel))
	}
	roadWidth, pkWidth := columnWidth(roads), columnWidth(pks)
	provinceWidth, kindWidth := columnWidth(provinces), columnWidth(kinds)

	truncated := 0
	for i := range response.Radars {
		kind, label := kinds[i], labels[i]
		for _, field := range []*string{&kind, &label} {
			if len([]rune(*field)) > textColumnWidth {
				*field = truncateText(*field, textColumnWidth)
				truncated++
			}
		}
		fmt.Fprintf(&b, "  %-*s  %-*s  %-*s  %-*s  %s\n",
			roadWidth, roads[i], pkWidth, pks[i], provinceWidth, provinces[i],
			kindWidth, kinds[i], label)
	}
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
	kind := ""
	if len(response.Sections) > 0 {
		kind = response.Sections[0].Kind
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Tramos %s (%d):\n\n", kind, len(response.Sections))
	if len(response.Sections) == 0 {
		b.WriteString("(sin resultados)\n")
	}
	roads, pks, provinces, lengths, directions := make([]string, 0, len(response.Sections)),
		make([]string, 0, len(response.Sections)), make([]string, 0, len(response.Sections)),
		make([]string, 0, len(response.Sections)), make([]string, 0, len(response.Sections))
	for _, section := range response.Sections {
		roads = append(roads, section.Road)
		pks = append(pks, formatPK(section.PK))
		provinces = append(provinces, optionalToDash(section.Province))
		length := "-"
		if section.LengthKM != nil {
			length = formatPKValue(*section.LengthKM) + " km"
		}
		lengths = append(lengths, length)
		directions = append(directions, optionalToDash(section.Direction))
	}
	roadWidth, pkWidth := columnWidth(roads), columnWidth(pks)
	provinceWidth, lengthWidth := columnWidth(provinces), columnWidth(lengths)

	for i := range response.Sections {
		fmt.Fprintf(&b, "  %-*s  %-*s  %-*s  %-*s  %s\n",
			roadWidth, roads[i], pkWidth, pks[i], provinceWidth, provinces[i],
			lengthWidth, lengths[i], directions[i])
	}
	b.WriteString(publishedNote(response.Meta))
	// The dataset kind travels in the heading; nothing else truncates here.
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// renderRoutesText renders `trafico route list` (and the add/remove
// confirmations, which reuse the list response) for a human (FORMAT-02): a
// heading with the count and one aligned row per saved route with its name,
// direction and declared segments.
func renderRoutesText(w io.Writer, v any) error {
	response, ok := v.(routesResponse)
	if !ok {
		return errs.NewInternal("el render de texto de route recibió una respuesta de otro tipo")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Rutas guardadas (%d):\n\n", len(response.Routes))
	if len(response.Routes) == 0 {
		b.WriteString("(sin rutas guardadas)\n")
	}
	names, directions, segments := make([]string, 0, len(response.Routes)),
		make([]string, 0, len(response.Routes)), make([]string, 0, len(response.Routes))
	for _, route := range response.Routes {
		names = append(names, route.Name)
		directions = append(directions, emptyToDash(route.Direction))
		parts := make([]string, 0, len(route.Segments))
		for _, segment := range route.Segments {
			parts = append(parts, segment.Road+" "+formatPKInterval(segment.From, segment.To))
		}
		segments = append(segments, strings.Join(parts, ", "))
	}
	nameWidth, directionWidth := columnWidth(names), columnWidth(directions)

	truncated := 0
	for i := range response.Routes {
		segmentText := segments[i]
		if len([]rune(segmentText)) > textColumnWidth {
			segmentText = truncateText(segmentText, textColumnWidth)
			truncated++
		}
		fmt.Fprintf(&b, "  %-*s  %-*s  %s\n", nameWidth, names[i], directionWidth, directions[i], segmentText)
	}
	b.WriteString(truncationNote(truncated))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// renderRouteDetailText renders `trafico route show <name>` for a human
// (FORMAT-02): labelled Spanish fields with the segments on their own
// lines, the way `trafico skills get` does it.
func renderRouteDetailText(w io.Writer, v any) error {
	response, ok := v.(routeResponse)
	if !ok {
		return errs.NewInternal("el render de texto de route show recibió una respuesta de otro tipo")
	}
	route := response.Route
	var b strings.Builder
	fmt.Fprintf(&b, "Ruta %s\n\n", route.Name)
	writeDetailField(&b, "Dirección", emptyToDash(route.Direction))
	b.WriteString("  Tramos:\n")
	for _, segment := range route.Segments {
		fmt.Fprintf(&b, "    %s %s\n", segment.Road, formatPKInterval(segment.From, segment.To))
	}
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// renderRouteCheckText renders `trafico route check <name>` for a human
// (FORMAT-02): one block per segment in declaration order with its
// incidents underneath, the summary at the end, and the freshness of the
// data — the report a person reads on a phone.
func renderRouteCheckText(w io.Writer, v any) error {
	response, ok := v.(app.RouteCheckResponse)
	if !ok {
		return errs.NewInternal("el render de texto de route check recibió una respuesta de otro tipo")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Ruta %s — %d tramos:\n\n", response.Route, len(response.Segments))
	truncated := 0
	for _, segment := range response.Segments {
		fmt.Fprintf(&b, "%s %s\n", segment.Road, formatPKInterval(segment.PK.From, segment.PK.To))
		if len(segment.Incidents) == 0 {
			b.WriteString("  Sin incidencias.\n")
		}
		for _, incident := range segment.Incidents {
			description := truncateCounted(incidentText(incident), &truncated)
			fmt.Fprintf(&b, "  - %s (%s) PK %s: %s\n",
				incident.Type, incident.Severity, formatPK(incident.PK), description)
		}
		if len(segment.Signs) > 0 {
			b.WriteString("  Paneles:\n")
			for _, sign := range segment.Signs {
				pk := "-"
				if sign.PK != nil {
					pk = formatPKValue(*sign.PK)
				}
				fmt.Fprintf(&b, "    - %s PK %s: %s\n", sign.Road, pk, truncateCounted(signText(sign), &truncated))
			}
		}
		if len(segment.Radars) > 0 {
			b.WriteString("  Radares:\n")
			for _, radar := range segment.Radars {
				fmt.Fprintf(&b, "    - %s PK %s: %s\n", radar.Road, formatPK(radar.PK),
					truncateCounted(optionalToDash(radar.Singularity), &truncated))
			}
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Resumen: %d incidencias, %d de severidad high, %d cortes",
		response.Summary.Incidents, response.Summary.HighSeverity, response.Summary.Closures)
	if response.Summary.Signs > 0 {
		fmt.Fprintf(&b, ", %d paneles", response.Summary.Signs)
	}
	if response.Summary.Radars > 0 {
		fmt.Fprintf(&b, ", %d radares", response.Summary.Radars)
	}
	b.WriteString("\n")
	b.WriteString(publishedNote(response.Meta))
	b.WriteString(truncationNote(truncated))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// truncateCounted truncates s at the column width, incrementing the
// render's truncation counter when it cuts.
func truncateCounted(s string, counter *int) string {
	if len([]rune(s)) <= textColumnWidth {
		return s
	}
	*counter++
	return truncateText(s, textColumnWidth)
}

// feedStateWord is the Spanish state word of a feed row, per lifecycle
// state (CMD-02): a checked feed reads ok or error, a merely configured one
// reads activado or desactivado.
func feedStateWord(status string) string {
	switch status {
	case app.FeedStatusOK:
		return "ok"
	case app.FeedStatusError:
		return "error"
	case app.FeedStatusDisabled:
		return "desactivado"
	default:
		return "activado"
	}
}

// renderFeedsText renders `trafico feeds list` and `trafico feeds status`
// for a human (FORMAT-02): one aligned status row per feed — state word,
// publication age, body size — and the URL for the static view or the error
// message for a checked feed that failed.
func renderFeedsText(w io.Writer, v any) error {
	response, ok := v.(feedsResponse)
	if !ok {
		return errs.NewInternal("el render de texto de feeds recibió una respuesta de otro tipo")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Feeds (%d):\n\n", len(response.Feeds))
	if len(response.Feeds) == 0 {
		b.WriteString("(sin feeds configurados)\n")
	}
	names, states, ages, sizes, notes := make([]string, 0, len(response.Feeds)),
		make([]string, 0, len(response.Feeds)), make([]string, 0, len(response.Feeds)),
		make([]string, 0, len(response.Feeds)), make([]string, 0, len(response.Feeds))
	for _, status := range response.Feeds {
		names = append(names, status.Name)
		states = append(states, feedStateWord(status.Status))
		ages = append(ages, formatAgeSeconds(status.AgeSeconds))
		sizes = append(sizes, formatSizeBytes(status.SizeBytes))
		note := status.URL
		if status.Error != nil {
			note = *status.Error
		}
		notes = append(notes, note)
	}
	nameWidth, stateWidth := columnWidth(names), columnWidth(states)
	ageWidth, sizeWidth := columnWidth(ages), columnWidth(sizes)

	truncated := 0
	for i := range response.Feeds {
		note := notes[i]
		if len([]rune(note)) > textColumnWidth {
			note = truncateText(note, textColumnWidth)
			truncated++
		}
		fmt.Fprintf(&b, "  %-*s  %-*s  %-*s  %-*s  %s\n",
			nameWidth, names[i], stateWidth, states[i], ageWidth, ages[i],
			sizeWidth, sizes[i], note)
	}
	b.WriteString(publishedNote(response.Meta))
	b.WriteString(truncationNote(truncated))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// renderDoctorText renders `trafico doctor` for a human (FORMAT-02): the
// config, feed and cache diagnostics, the not-an-official-source warning
// (ARRANQUE section 13) and the overall state.
func renderDoctorText(w io.Writer, v any) error {
	report, ok := v.(app.DoctorReport)
	if !ok {
		return errs.NewInternal("el render de texto de doctor recibió una respuesta de otro tipo")
	}
	var b strings.Builder
	b.WriteString("Doctor\n\n")
	fmt.Fprintf(&b, "  Configuración: %s\n", report.Config.Path)
	exists := "no existe (se aplican los valores por defecto)"
	if report.Config.Exists {
		exists = "existe"
	}
	loaded := "no cargada"
	if report.Config.Loaded {
		loaded = "cargada"
	}
	permissions := "permisos holgados"
	if report.Config.PermissionsOK {
		permissions = "permisos ok"
	}
	line := fmt.Sprintf("    %s, %s, %s", exists, loaded, permissions)
	if report.Config.Error != nil {
		line += ": " + truncateText(*report.Config.Error, textColumnWidth)
	}
	b.WriteString(line + "\n")

	b.WriteString("  Feeds:\n")
	if len(report.Feeds) == 0 {
		b.WriteString("    (ningún feed configurado)\n")
	}
	names, states, ages, sizes := make([]string, 0, len(report.Feeds)),
		make([]string, 0, len(report.Feeds)), make([]string, 0, len(report.Feeds)),
		make([]string, 0, len(report.Feeds))
	for _, status := range report.Feeds {
		names = append(names, status.Name)
		states = append(states, feedStateWord(status.Status))
		ages = append(ages, formatAgeSeconds(status.AgeSeconds))
		sizes = append(sizes, formatSizeBytes(status.SizeBytes))
	}
	nameWidth, stateWidth := columnWidth(names), columnWidth(states)
	ageWidth, sizeWidth := columnWidth(ages), columnWidth(sizes)
	for i, status := range report.Feeds {
		fmt.Fprintf(&b, "    %-*s  %-*s  %-*s  %-*s", nameWidth, names[i],
			stateWidth, states[i], ageWidth, ages[i], sizeWidth, sizes[i])
		if status.Error != nil {
			fmt.Fprintf(&b, "  %s", truncateText(*status.Error, textColumnWidth))
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "  Caché: %s (%d entradas, %s)\n", report.Cache.Dir,
		report.Cache.Entries, formatSizeBytes(&report.Cache.TotalBytes))
	state := "con problemas"
	if report.OK {
		state = "ok"
	}
	fmt.Fprintf(&b, "  Estado: %s\n", state)
	fmt.Fprintf(&b, "\n  %s\n", report.NotOfficial)
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// watchTargetText renders what a watch looks at, per kind, in Spanish.
func watchTargetText(info watchInfo) string {
	switch info.Kind {
	case "route":
		return "ruta " + info.Target.Route
	case "road":
		target := "carretera " + info.Target.Road
		if info.Target.PK != nil {
			target += " PK " + formatPK(&model.PK{From: info.Target.PK.From, To: info.Target.PK.To})
		}
		return target
	case "near":
		if info.Target.Place != "" {
			target := "cerca de " + info.Target.Place
			if info.Target.RadiusKm > 0 {
				target += fmt.Sprintf(" (%s km)", formatPKValue(info.Target.RadiusKm))
			}
			return target
		}
		if info.Target.Lat != nil && info.Target.Lon != nil {
			return fmt.Sprintf("cerca de %s, %s (%s km)",
				formatPKValue(*info.Target.Lat), formatPKValue(*info.Target.Lon),
				formatPKValue(info.Target.RadiusKm))
		}
		return "-"
	default:
		return "-"
	}
}

// renderWatchesText renders `trafico watch list` (and the add/remove
// confirmations, which reuse the list response) for a human (FORMAT-02): a
// heading with the count and one aligned row per saved watch.
func renderWatchesText(w io.Writer, v any) error {
	response, ok := v.(watchesResponse)
	if !ok {
		return errs.NewInternal("el render de texto de watch recibió una respuesta de otro tipo")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Vigilancias (%d):\n\n", len(response.Watches))
	if len(response.Watches) == 0 {
		b.WriteString("(sin vigilancias guardadas)\n")
	}
	names, kinds, targets, windows, severities, intervals, sinks :=
		make([]string, 0, len(response.Watches)), make([]string, 0, len(response.Watches)),
		make([]string, 0, len(response.Watches)), make([]string, 0, len(response.Watches)),
		make([]string, 0, len(response.Watches)), make([]string, 0, len(response.Watches)),
		make([]string, 0, len(response.Watches))
	for _, info := range response.Watches {
		names = append(names, info.Name)
		kinds = append(kinds, info.Kind)
		targets = append(targets, watchTargetText(info))
		windows = append(windows, strings.Join(info.Windows, ", "))
		severities = append(severities, emptyToDash(info.MinSeverity))
		intervals = append(intervals, info.Interval)
		sink := "-"
		if len(info.Sinks) > 0 {
			sink = strings.Join(info.Sinks, ", ")
		}
		sinks = append(sinks, sink)
	}
	nameWidth, kindWidth := columnWidth(names), columnWidth(kinds)
	targetWidth, windowWidth := columnWidth(targets), columnWidth(windows)
	severityWidth, intervalWidth := columnWidth(severities), columnWidth(intervals)

	truncated := 0
	for i := range response.Watches {
		target, window := targets[i], windows[i]
		if len([]rune(target)) > textColumnWidth {
			target = truncateText(target, textColumnWidth)
			truncated++
		}
		if len([]rune(window)) > textColumnWidth {
			window = truncateText(window, textColumnWidth)
			truncated++
		}
		fmt.Fprintf(&b, "  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %s\n",
			nameWidth, names[i], kindWidth, kinds[i], targetWidth, target,
			windowWidth, window, severityWidth, severities[i], intervalWidth, intervals[i],
			sinks[i])
	}
	b.WriteString(truncationNote(truncated))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// renderWatchDetailText renders `trafico watch show <name>` for a human
// (FORMAT-02): labelled Spanish fields, the way `trafico skills get` does
// it. The target, windows and sinks are storage metadata shown to the user;
// they never reach a notification (P-07).
func renderWatchDetailText(w io.Writer, v any) error {
	response, ok := v.(watchResponse)
	if !ok {
		return errs.NewInternal("el render de texto de watch show recibió una respuesta de otro tipo")
	}
	info := response.Watch
	var b strings.Builder
	fmt.Fprintf(&b, "Vigilancia %s\n\n", info.Name)
	writeDetailField(&b, "Tipo", info.Kind)
	writeDetailField(&b, "Objetivo", watchTargetText(info))
	writeDetailField(&b, "Ventanas", emptyToDash(strings.Join(info.Windows, ", ")))
	writeDetailField(&b, "Severidad mínima", emptyToDash(info.MinSeverity))
	emitInitial := "no"
	if info.EmitInitial {
		emitInitial = "sí"
	}
	writeDetailField(&b, "Emitir inicial", emitInitial)
	writeDetailField(&b, "Sinks", emptyToDash(strings.Join(info.Sinks, ", ")))
	writeDetailField(&b, "Intervalo", info.Interval)
	writeDetailField(&b, "Creado", info.CreatedAt.UTC().Format(time.RFC3339))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// The FORMAT-03 renderers: the three watch commands that are per-command
// reports rather than lists or details — watch check (what a check found),
// watch run (the unattended cycle, one log line per watch) and watch service
// (the scheduler unit). They reuse the helpers of FORMAT-02 and end with the
// attribution line like every other render.

// watchEventWord is the Spanish word of one event kind, the way the
// notification messages read (nueva, actualizada, resuelta); an unknown kind
// prints as itself (P-04: nothing invented).
func watchEventWord(kind string) string {
	switch kind {
	case store.EventIncidentNew:
		return "nueva"
	case store.EventIncidentUpdated:
		return "actualizada"
	case store.EventIncidentCleared:
		return "resuelta"
	default:
		return kind
	}
}

// renderWatchCheckText renders `trafico watch check <name>` for a human
// (FORMAT-03): what the check found, one line per event with the kind and
// the incident it refers to. Nothing changed reads as one line, never an
// empty array; a skipped check says why it checked nothing.
func renderWatchCheckText(w io.Writer, v any) error {
	response, ok := v.(watchCheckResponse)
	if !ok {
		return errs.NewInternal("el render de texto de watch check recibió una respuesta de otro tipo")
	}
	var b strings.Builder
	switch {
	case response.Skipped:
		fmt.Fprintf(&b, "Vigilancia %s: fuera de la ventana horaria; no se comprueba ni se avisa.\n", response.Watch)
	case len(response.eventsDetail) > 0:
		fmt.Fprintf(&b, "Vigilancia %s — novedades (%d):\n\n", response.Watch, len(response.eventsDetail))
		for _, event := range response.eventsDetail {
			fmt.Fprintf(&b, "  - %s: %s\n", watchEventWord(event.Kind), event.IncidentID)
		}
	case response.Baseline:
		fmt.Fprintf(&b, "Vigilancia %s: sin estado previo; la próxima ejecución registraría la línea base sin avisar.\n", response.Watch)
	default:
		fmt.Fprintf(&b, "Vigilancia %s: sin novedades.\n", response.Watch)
	}
	b.WriteString(publishedNote(response.Meta))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// renderWatchRunText renders `trafico watch run` for a human (FORMAT-03):
// one timestamped line per watch — it runs unattended on a timer, so the
// text must be useful in a log — with the cycle summary at the end and no
// headers to repeat per iteration.
func renderWatchRunText(w io.Writer, v any) error {
	response, ok := v.(watchRunResponse)
	if !ok {
		return errs.NewInternal("el render de texto de watch run recibió una respuesta de otro tipo")
	}
	var b strings.Builder
	stamp := time.Now().UTC().Format(time.RFC3339)
	totalEvents := 0
	for _, entry := range response.Runs {
		totalEvents += entry.Events
		fmt.Fprintf(&b, "%s %s: %s\n", stamp, entry.Watch, watchRunEntryText(entry))
	}
	b.WriteString(publishedNote(response.Meta))
	fmt.Fprintf(&b, "Resumen: %d %s, %d %s, %d %s.\n",
		len(response.Runs), countNoun(len(response.Runs), "vigilancia", "vigilancias"),
		totalEvents, countNoun(totalEvents, "evento", "eventos"),
		response.Delivered, countNoun(response.Delivered, "aviso entregado", "avisos entregados"))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}

// countNoun picks the singular or plural Spanish noun of a count; zero and
// any other number above one take the plural.
func countNoun(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

// watchRunEntryText is the status phrase of one watch of a run cycle, in the
// order a real cycle decides it: a per-watch failure first, then the two
// skips (window, interval), then the events, then the baseline.
func watchRunEntryText(entry watchRunEntry) string {
	switch {
	case entry.Error != "":
		return "error: " + entry.Error
	case entry.Skipped:
		return "fuera de ventana"
	case entry.SkippedInterval:
		return "esperando intervalo"
	case entry.Events > 0:
		words := make([]string, 0, len(entry.Kinds))
		for _, kind := range entry.Kinds {
			words = append(words, watchEventWord(kind))
		}
		noun := "eventos"
		if entry.Events == 1 {
			noun = "evento"
		}
		return fmt.Sprintf("%d %s: %s", entry.Events, noun, strings.Join(words, ", "))
	case entry.Baseline:
		return "línea base registrada"
	default:
		return "sin novedades"
	}
}

// renderWatchServiceText renders the `watch service` commands for a human
// (FORMAT-03): a one-line confirmation for install and uninstall, and the
// unit state in plain Spanish for status, with what to do when the service
// is not installed or not active.
func renderWatchServiceText(w io.Writer, v any) error {
	response, ok := v.(serviceResponse)
	if !ok {
		return errs.NewInternal("el render de texto de watch service recibió una respuesta de otro tipo")
	}
	info := response.Service
	var b strings.Builder
	switch response.verb {
	case "install":
		fmt.Fprintf(&b, "Servicio de vigilancia instalado")
		if info.Active {
			b.WriteString(" y activo")
		}
		fmt.Fprintf(&b, " (%s en %s).\n", info.Scheduler, info.Platform)
		for _, file := range info.Files {
			fmt.Fprintf(&b, "  - %s\n", file)
		}
	case "uninstall":
		if len(info.Files) == 0 && !info.Installed {
			b.WriteString("Servicio de vigilancia desinstalado (no había nada instalado).\n")
		} else {
			b.WriteString("Servicio de vigilancia desinstalado.\n")
			for _, file := range info.Files {
				fmt.Fprintf(&b, "  - %s\n", file)
			}
		}
	default:
		switch {
		case !info.Installed:
			b.WriteString("Servicio de vigilancia no instalado. Instálalo con `trafico watch service install`.\n")
		case !info.Active:
			b.WriteString("Servicio de vigilancia instalado pero no activo. Reactívalo con `trafico watch service install`.\n")
			for _, file := range info.Files {
				fmt.Fprintf(&b, "  - %s\n", file)
			}
		default:
			fmt.Fprintf(&b, "Servicio de vigilancia instalado y activo (%s en %s).\n", info.Scheduler, info.Platform)
			for _, file := range info.Files {
				fmt.Fprintf(&b, "  - %s\n", file)
			}
		}
	}
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
}
