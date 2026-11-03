package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/errs"
)

// The text renderers of `trafico route` (FORMAT-02): the saved-route list
// (which the add/remove confirmations reuse), the detail and the per-route
// check report.

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
	table := newTextTable(false, false, true)
	for _, route := range response.Routes {
		parts := make([]string, 0, len(route.Segments))
		for _, segment := range route.Segments {
			parts = append(parts, segment.Road+" "+formatPKInterval(segment.From, segment.To))
		}
		table.addRow(route.Name, emptyToDash(route.Direction), strings.Join(parts, ", "))
	}
	truncated := table.writeTo(&b, "  ", nil)
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
