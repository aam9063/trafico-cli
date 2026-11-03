package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/model"
)

// The text renderers of `trafico incidents` (FORMAT-02): the list and the
// detail share the response type; the show invocation carries no query
// echo, and that is the shape the detail render gives it.

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
	table := newTextTable(false, false, false, false, true)
	for _, incident := range response.Incidents {
		table.addRow(incident.Road, formatPK(incident.PK), incident.Type,
			incident.Severity, incidentText(incident))
	}
	truncated := table.writeTo(&b, "  ", nil)
	b.WriteString(publishedNote(response.Meta))
	b.WriteString(truncationNote(truncated))
	b.WriteString("\n" + attributionClosingLine)
	_, err := io.WriteString(w, b.String())
	return err
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
