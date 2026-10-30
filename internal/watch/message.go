package watch

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/store"
)

// payload is the JSON body the watch engine stores in store.Event.Payload and
// the notification text is rendered from. It carries ONLY the fields the
// notification is allowed to show (OLA-3 spec S7, ARRANQUE P-07): road,
// kilometre point, incident type and severity. The watch name, route names,
// place names and coordinates have no field here, so they cannot leak into a
// message by construction. IncidentID identifies the event inside the local
// event log; it is public DGT data, never rendered as a location.
type payload struct {
	Kind       string   `json:"kind"`
	IncidentID string   `json:"incident_id"`
	Road       string   `json:"road,omitempty"`
	PKFrom     *float64 `json:"pk_from,omitempty"`
	PKTo       *float64 `json:"pk_to,omitempty"`
	Type       string   `json:"type,omitempty"`
	Severity   string   `json:"severity,omitempty"`
}

// marshalPayload serialises a payload; it cannot realistically fail for this
// fixed struct, but the error keeps the contract honest at every call site.
func marshalPayload(p payload) ([]byte, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("no se pudo serializar el payload del evento: %w", err)
	}
	return data, nil
}

// payloadMarshalErrorKind is the marker kind the watch engine stores when a
// payload cannot be serialised at all (see watch.go): RenderMessage reports
// it as an explicit internal error instead of rendering a message, and
// DrainPending treats it as undeliverable data instead of retrying forever.
const payloadMarshalErrorKind = "internal.unserialisable-payload"

// unserialisablePayloadJSON is the fixed marker body, a constant so the
// fallback itself can never fail to serialise.
const unserialisablePayloadJSON = `{"kind":"` + payloadMarshalErrorKind + `"}`

// attributionLine is the mandatory CC BY attribution of every notification
// (ARRANQUE §2.1, P-05), in the same wording the pretty output uses.
const attributionLine = "Fuente: DGT (CC BY)"

// typeLabels translates the normalised incident types of the contract
// (ARRANQUE §4.1) into the Spanish wording of the notifications. Anything the
// model may add later falls back to its raw value: never invented, never lost.
var typeLabels = map[string]string{
	"accident":    "accidente",
	"congestion":  "retención",
	"roadworks":   "obras",
	"closure":     "corte",
	"obstruction": "obstáculo",
	"weather":     "meteorología",
	"restriction": "restricción",
	"event":       "evento",
	"other":       "incidencia",
}

// severityLabels translates the contract severities; "unknown" is absent on
// purpose: a notification omits a severity the DGT did not publish instead of
// inventing one (P-04).
var severityLabels = map[string]string{
	"low":    "baja",
	"medium": "media",
	"high":   "alta",
}

// typeLabel renders the incident type in Spanish, falling back to the raw
// value for a type this package does not know yet.
func typeLabel(t string) string {
	if label, ok := typeLabels[t]; ok {
		return label
	}
	if t == "" {
		return "incidencia"
	}
	return t
}

// severityLabel renders the severity in Spanish; ok is false when the feed
// carried none ("unknown" or empty) and the clause must be omitted.
func severityLabel(s string) (label string, ok bool) {
	label, ok = severityLabels[s]
	return label, ok
}

// RenderMessage renders the user-facing text of one watch event. This is the
// whole notification: a user receiving it must be able to know what happened
// on which road and nothing about their own configuration — the watch name, a
// route name, a place name or a coordinate can never appear here because the
// payload has no field for them (P-07; spec S7).
//
// The text is Spanish (user-facing), with the attribution line of P-05.
func RenderMessage(payloadJSON string) (string, error) {
	var p payload
	if err := json.Unmarshal([]byte(payloadJSON), &p); err != nil {
		return "", errs.NewInternal("el payload del evento no es JSON válido: " + err.Error())
	}
	switch p.Kind {
	case store.EventIncidentNew:
		return renderIncidentMessage("Nueva incidencia: ", p), nil
	case store.EventIncidentUpdated:
		return renderIncidentMessage("Incidencia actualizada: ", p), nil
	case payloadMarshalErrorKind:
		// The watch engine could not serialise the payload when it stored
		// the event: report the failure explicitly instead of rendering a
		// message, so the bug surfaces instead of turning into an empty
		// notification or a silent queue stall.
		return "", errs.NewInternal("el payload del evento no se pudo serializar cuando se guardó: el aviso no se puede construir")
	case store.EventIncidentCleared:
		// The incident is gone from the feed, so the message shows what the
		// stored state still remembers: road, PK, type and severity. Each
		// unknown field is omitted, never printed as an empty placeholder
		// (P-04); road and PK come from the state, so they are normally
		// there.
		var b strings.Builder
		b.WriteString("Incidencia finalizada")
		if p.Type != "" || p.Road != "" || p.PKFrom != nil || p.PKTo != nil {
			b.WriteString(":")
		}
		if p.Type != "" {
			b.WriteString(" " + typeLabel(p.Type))
		}
		if location := locationPhrase(p); location != "" {
			b.WriteString(" " + location)
		}
		if label, ok := severityLabel(p.Severity); ok {
			b.WriteString(", severidad " + label)
		}
		b.WriteString(". " + attributionLine)
		return b.String(), nil
	default:
		return "", errs.NewInternal(fmt.Sprintf("el evento tiene una clase desconocida %q", p.Kind))
	}
}

// renderIncidentMessage renders the shared body of incident.new and
// incident.updated: "Nueva incidencia: obras en A-7 PK 102.3-108, severidad
// media. Fuente: DGT (CC BY)".
func renderIncidentMessage(prefix string, p payload) string {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(typeLabel(p.Type))
	if location := locationPhrase(p); location != "" {
		b.WriteString(" " + location)
	}
	if label, ok := severityLabel(p.Severity); ok {
		b.WriteString(", severidad " + label)
	}
	b.WriteString(". " + attributionLine)
	return b.String()
}

// locationPhrase renders the road and kilometre point ("en A-7 PK 102.3-108"),
// omitting whatever the feed did not carry (P-04).
func locationPhrase(p payload) string {
	var b strings.Builder
	if p.Road != "" {
		b.WriteString("en " + p.Road)
	}
	if pk := pkPhrase(p.PKFrom, p.PKTo); pk != "" {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString(pk)
	}
	return b.String()
}

// pkPhrase renders the kilometre-point interval. A missing bound is
// open-ended and prints as a single value; no bounds at all prints nothing.
func pkPhrase(from, to *float64) string {
	switch {
	case from != nil && to != nil:
		return "PK " + formatKM(*from) + "-" + formatKM(*to)
	case from != nil:
		return "PK " + formatKM(*from)
	case to != nil:
		return "PK " + formatKM(*to)
	default:
		return ""
	}
}

// formatKM prints a kilometre point compactly: 102.3 stays "102.3" and 108.0
// loses the trailing ".0".
func formatKM(km float64) string {
	return strconv.FormatFloat(km, 'f', -1, 64)
}
