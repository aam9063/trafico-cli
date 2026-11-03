package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aam9063/trafico-cli/internal/errs"
	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/store"
)

// The text renderers of `trafico watch` (FORMAT-02 and the FORMAT-03
// reports): the saved-watch list (which the add/remove confirmations
// reuse), the detail, and the three per-command reports — watch check
// (what a check found), watch run (the unattended cycle, one log line per
// watch) and watch service (the scheduler unit). They reuse the shared
// vocabulary of render.go and end with the attribution line like every
// other render.

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
	table := newTextTable(false, false, true, true, false, false, false)
	for _, info := range response.Watches {
		sink := "-"
		if len(info.Sinks) > 0 {
			sink = strings.Join(info.Sinks, ", ")
		}
		table.addRow(info.Name, info.Kind, watchTargetText(info),
			strings.Join(info.Windows, ", "), emptyToDash(info.MinSeverity),
			info.Interval, sink)
	}
	truncated := table.writeTo(&b, "  ", nil)
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
//
// The tail is a footer, not an entry: from the blank line after the last
// per-watch entry to the attribution closing line, no line carries the
// RFC3339 stamp. A log consumer parses only the "<stamp> <watch>: ..."
// entry lines and knows every line after the blank line is footer (QA-01).
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
	b.WriteString("\n")
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
		return fmt.Sprintf("%d %s: %s", entry.Events,
			countNoun(entry.Events, "evento", "eventos"), strings.Join(words, ", "))
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
	case "status":
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
