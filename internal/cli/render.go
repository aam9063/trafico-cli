package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/aam9063/trafico-cli/internal/model"
	"github.com/aam9063/trafico-cli/internal/output"
)

// The shared vocabulary of the human-readable renderers (FORMAT-02): every
// render_<area> file turns its §5.3 envelope into something a person reads
// — a heading with the count, aligned rows or labelled Spanish fields (the
// way `trafico skills` does it), closed by the attribution line (ARRANQUE
// section 2.1). No ANSI (ARRANQUE section 6.1). The --json contract is
// untouched: these run only when the user asked for the readable default.
// This file holds only what several renderers share: text truncation and
// the counting notes, kilometre points, ages and sizes, column measuring,
// the detail-field printer, and the aligned textTable. The per-command
// renderers live beside their commands: render_incidents.go,
// render_routes.go, render_feeds.go, render_static.go and render_watch.go
// (the FORMAT-01 renderers live in schema.go and skills.go).

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
//
// Both bounds are values, never pointers: routeCheckPK stores float64 and so
// does the watch road target. A segment therefore cannot lack a kilometre
// point, and this call site cannot panic on nil -- a native review refuted it
// once as a nil dereference and it was not. Turning either type into a pointer
// would make that real, and the test below pins the shape so the compiler is
// not the only thing that would notice.
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

// detailLabelWidth aligns the labels of every detail render. It covers the
// longest label in use ("Severidad mínima:" for watches).
const detailLabelWidth = 18

// writeDetailField prints one labelled Spanish field of a detail render.
func writeDetailField(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "  %-*s %s\n", detailLabelWidth, label+":", value)
}

// emptyToDash prints the mandatory display value of an absent field: a
// dash, never an invented value (P-04).
func emptyToDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// optionalToDash prints an optional string as "-" when the feed carries none.
func optionalToDash(s *string) string {
	if s == nil || *s == "" {
		return "-"
	}
	return *s
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

// textTable is the aligned table of the list renderers (FORMAT-02). Rows
// are collected first with addRow — one cell per column, the free text
// last — then writeTo measures every column once over the full cell values
// and prints the rows: all columns but the last padded to their width and
// separated by two spaces, the free-text columns truncated at
// textColumnWidth and counted for the truncation note.
type textTable struct {
	free []bool
	rows [][]string
}

// newTextTable returns a table with one column per free flag; a true flag
// marks that column as free text (truncated and counted when printed).
func newTextTable(free ...bool) *textTable {
	return &textTable{free: free}
}

// addRow appends one row; it carries exactly one cell per column.
func (t *textTable) addRow(cells ...string) {
	t.rows = append(t.rows, cells)
}

// writeTo prints the collected rows, each starting with indent (two spaces
// in the list renderers, four in doctor). suffix, when not nil, returns the
// trailing text of a row (doctor's per-feed error, truncated but not
// counted); because the row continues past the last column, every column
// is then padded, the way doctor has always done. Without a suffix the
// last column ends the line and is never padded. It returns how many
// free-text cells were truncated, the count the truncation note needs.
func (t *textTable) writeTo(b *strings.Builder, indent string, suffix func(row int) string) int {
	cols := 0
	for _, row := range t.rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	widths := make([]int, cols)
	for col := range widths {
		cells := make([]string, len(t.rows))
		for i, row := range t.rows {
			cells[i] = row[col]
		}
		widths[col] = columnWidth(cells)
	}
	truncated := 0
	for i, row := range t.rows {
		b.WriteString(indent)
		last := len(row) - 1
		for col, cell := range row {
			if col > 0 {
				b.WriteString("  ")
			}
			// A column is free text only when the table flagged it; a table
			// built without flags (sections) truncates nothing.
			if col < len(t.free) && t.free[col] {
				cell = truncateCounted(cell, &truncated)
			}
			if col == last && suffix == nil {
				// Without a trailing suffix the last column ends the line.
				b.WriteString(cell)
			} else {
				fmt.Fprintf(b, "%-*s", widths[col], cell)
			}
		}
		if suffix != nil {
			// The suffix carries its own leading separator when present
			// (doctor's "  " before the feed error).
			b.WriteString(suffix(i))
		}
		b.WriteString("\n")
	}
	return truncated
}
