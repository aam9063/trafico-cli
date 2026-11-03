package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/errs"
)

// The text renderers of the feed lifecycle (FORMAT-02): `trafico feeds
// list` and `trafico feeds status`, which share the feed table, and
// `trafico doctor`, which repeats that table under its diagnostics.

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
	table := newTextTable(false, false, false, false, true)
	for _, status := range response.Feeds {
		note := status.URL
		if status.Error != nil {
			note = *status.Error
		}
		table.addRow(status.Name, feedStateWord(status.Status),
			formatAgeSeconds(status.AgeSeconds), formatSizeBytes(status.SizeBytes), note)
	}
	truncated := table.writeTo(&b, "  ", nil)
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
	table := newTextTable(false, false, false, false)
	for _, status := range report.Feeds {
		table.addRow(status.Name, feedStateWord(status.Status),
			formatAgeSeconds(status.AgeSeconds), formatSizeBytes(status.SizeBytes))
	}
	table.writeTo(&b, "    ", func(i int) string {
		if status := report.Feeds[i]; status.Error != nil {
			return "  " + truncateText(*status.Error, textColumnWidth)
		}
		return ""
	})

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
