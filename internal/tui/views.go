package tui

// The four table views. Each one calls the same internal/app function its
// command calls and flattens the result into text cells; the drawing code is
// shared, so a view is a column list and a row mapper, nothing more.

import (
	"context"
	"fmt"
	"strings"

	"github.com/aam9063/trafico-cli/internal/app"
	"github.com/aam9063/trafico-cli/internal/filter"
	"github.com/aam9063/trafico-cli/internal/model"
)

// view identifies a screen. The order is the order of the number keys.
type view int

const (
	viewIncidents view = iota
	viewSigns
	viewRadars
	viewSections
	viewCount
)

// viewNames is the Spanish name of each view, shown in the title bar.
var viewNames = [viewCount]string{"Incidencias", "Paneles", "Radares", "Tramos"}

// viewLoading is what the footer says while a view loads.
var viewLoading = [viewCount]string{
	"Cargando incidencias…",
	"Cargando paneles…",
	"Cargando radares…",
	"Cargando tramos…",
}

// table is what a view produces once its feed is in: the column headers and
// the rows, already flattened to text. Keeping it text means the drawing code
// never learns about model types, and a new view is a function here.
type table struct {
	cols []string
	rows [][]string
}

// loadView is the tea.Cmd body of one view: fetch off the render path.
func (m Model) loadView(v view) tableMsg {
	ctx := context.Background()
	// TUI-03: one province filter for every view, sent through the same app
	// criteria the commands use, never a filter of the TUI's own (S5).
	// ProvinceEqual folds case, accents and the bilingual pairs, so
	// "alicante" finds "Alacant/Alicante".
	provinces := m.provinces()
	switch v {
	case viewIncidents:
		resp, err := app.Incidents(ctx, m.cfg, m.fetcher, app.IncidentCriteria{Criteria: filter.Criteria{Provinces: provinces}})
		if err != nil {
			return tableMsg{view: v, err: err}
		}
		rows := make([][]string, 0, len(resp.Incidents))
		for _, inc := range resp.Incidents {
			rows = append(rows, []string{
				inc.Road, pk(inc.PK), es(typeES, inc.Type), es(severityES, inc.Severity), describe(inc),
			})
		}
		return tableMsg{view: v, t: table{
			cols: []string{"CARRETERA", "PK", "TIPO", "GRAVEDAD", "DESCRIPCIÓN"},
			rows: rows,
		}}

	case viewSigns:
		resp, err := app.Signs(ctx, m.cfg, m.fetcher, app.SignCriteria{Provinces: provinces})
		if err != nil {
			return tableMsg{view: v, err: err}
		}
		rows := make([][]string, 0, len(resp.Signs))
		for _, s := range resp.Signs {
			rows = append(rows, []string{
				s.Road, pointPK(s.PK), dash(s.Province), signText(s),
			})
		}
		return tableMsg{view: v, t: table{
			cols: []string{"CARRETERA", "PK", "PROVINCIA", "MENSAJE"},
			rows: rows,
		}}

	case viewRadars:
		resp, err := app.Radars(ctx, m.cfg, m.fetcher, app.RadarCriteria{Provinces: provinces})
		if err != nil {
			return tableMsg{view: v, err: err}
		}
		rows := make([][]string, 0, len(resp.Radars))
		for _, r := range resp.Radars {
			rows = append(rows, []string{
				r.Road, pk(r.PK), dashPtr(r.Province), dashPtr(r.Singularity),
			})
		}
		return tableMsg{view: v, t: table{
			cols: []string{"CARRETERA", "PK", "PROVINCIA", "TIPO"},
			rows: rows,
		}}

	default: // viewSections
		resp, err := app.Sections(ctx, m.cfg, m.fetcher, app.SectionCriteria{Kind: model.SectionKindInvive, Provinces: provinces})
		if err != nil {
			return tableMsg{view: v, err: err}
		}
		rows := make([][]string, 0, len(resp.Sections))
		for _, s := range resp.Sections {
			rows = append(rows, []string{
				s.Road, pk(s.PK), dashPtr(s.Province), directionCell(s.Direction),
			})
		}
		return tableMsg{view: v, t: table{
			cols: []string{"CARRETERA", "PK", "PROVINCIA", "DIRECCIÓN"},
			rows: rows,
		}}
	}
}

// provinces is the province filter as the app criteria expect it: nil when
// nothing is filtered, so "see everything" stays the default.
func (m Model) provinces() []string {
	if m.province == "" {
		return nil
	}
	return []string{m.province}
}

// The criteria accessors below exist for the tests (S8): they prove the
// province filter reaches the same app criteria the commands use, and nothing
// else does the filtering.
func (m Model) incidentCriteria() app.IncidentCriteria {
	return app.IncidentCriteria{Criteria: filter.Criteria{Provinces: m.provinces()}}
}

func (m Model) signCriteria() app.SignCriteria {
	return app.SignCriteria{Provinces: m.provinces()}
}

func (m Model) radarCriteria() app.RadarCriteria {
	return app.RadarCriteria{Provinces: m.provinces()}
}

func (m Model) sectionCriteria() app.SectionCriteria {
	return app.SectionCriteria{Kind: model.SectionKindInvive, Provinces: m.provinces()}
}

// title is the title bar text for the current view, filter included.
func (m Model) title() string {
	return titleOf(m.current, len(m.tables[m.current].rows), m.states[m.current], m.province)
}

// pointPK renders a panel's single kilometre point.
func pointPK(p *float64) string {
	if p == nil {
		return "-"
	}
	return trim(*p)
}

// dash is the empty cell. A province the feed does not publish is "-", never
// an invented name (P-04).
func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// directionCell renders the section direction for a person. The contract keeps
// the feed's own English value ("both", "positive", "negative"); this is the
// only place that translates it, and an unknown value falls through to the
// original rather than becoming a guess.
func directionCell(s *string) string {
	if s == nil || *s == "" {
		return "-"
	}
	return es(directionES, *s)
}

func dashPtr(s *string) string {
	if s == nil || *s == "" {
		return "-"
	}
	return *s
}

// signText is what the panel says, joined; a panel with no messages is a panel
// that is off, and says so rather than showing an empty cell.
func signText(s model.Sign) string {
	if len(s.Messages) == 0 {
		return "(apagado)"
	}
	out := ""
	for i, msg := range s.Messages {
		if i > 0 {
			out += " · "
		}
		out += strings.Join(msg.TextLines, " ")
	}
	return out
}

// widthOf returns the widest cell of a column, capped so one long feed value
// cannot push the other columns out of the frame.
func widthOf(t table, c int) int {
	w := len(t.cols[c])
	for _, r := range t.rows {
		if c < len(r) && len(r[c]) > w {
			w = len(r[c])
		}
	}
	if w > 26 {
		w = 26
	}
	return w
}

// render draws the table with the columns that fit. The last column is the
// free-text one: it takes the remaining width and is truncated, so the frame
// closes instead of being pushed open by a long description.
func (m Model) render(t table, cursor, scroll, visible, width int) string {
	if len(t.cols) == 0 {
		return ""
	}
	narrow := width < 96

	// Which columns survive a narrow terminal: the last one (free text) always
	// goes when the width runs out, the others in a fixed order.
	last := len(t.cols) - 1
	keep := len(t.cols)
	if narrow {
		keep = len(t.cols) - 1
	}

	widths := make([]int, keep)
	used := 0
	for c := 0; c < keep; c++ {
		widths[c] = widthOf(t, c)
		used += widths[c] + 2
	}
	free := width - used - 4 // frame border and padding
	if free < 8 {
		free = 8
	}
	if !narrow {
		widths[last] = free
	}

	var b []string
	b = append(b, styleHeader.Render(m.line(t.cols, widths, keep)))
	for i := scroll; i < scroll+visible && i < len(t.rows); i++ {
		line := m.line(t.rows[i], widths, keep)
		switch {
		case i == cursor:
			b = append(b, styleSel.Render(line))
		case len(t.rows[i]) > 3 && t.rows[i][3] == "high":
			b = append(b, styleWarm.Render(line))
		default:
			b = append(b, line)
		}
	}
	return joinLines(b)
}

// line pads every cell to its width. The last kept column is not padded, so
// the selected row's background does not trail to the frame edge.
func (m Model) line(cells []string, widths []int, keep int) string {
	out := ""
	for c := 0; c < keep && c < len(cells); c++ {
		cell := cells[c]
		if c == keep-1 && keep < len(widths)+1 && len(widths) > c {
			cell = truncate(cell, widths[c])
			out += cell
			continue
		}
		if len(widths) > c {
			cell = truncate(cell, widths[c])
			out += pad(cell, widths[c]) + "  "
		}
	}
	return out
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out + "\n"
}

// keyOf maps a key to the view it opens, or viewCount when it opens none.
func keyOf(s string) view {
	if len(s) == 1 && s[0] >= '1' && s[0] < '1'+byte(viewCount) {
		return view(s[0] - '1')
	}
	return viewCount
}

// titleOf is the title bar text for a view.
func titleOf(v view, n int, st state, province string) string {
	title := viewNames[v]
	// A filtered screen must never look like the whole world.
	if province != "" {
		title += " · provincia " + province
	}
	if st == ready {
		title += fmt.Sprintf(" · %d", n)
	}
	return title
}

// Spanish labels for the contract values. The JSON keeps the feed's own text
// (P-04: never invent or translate a contract value); this is presentation, and
// a person reading a table in Spanish should not be handed "roadworks".
//
// An unknown value falls through to the original, which is the honest failure:
// a new feed value shows up in English instead of disappearing behind a guess.
var (
	typeES = map[string]string{
		"roadworks":                         "obras",
		"obstruction":                       "obstrucción",
		"restriction":                       "restricción",
		"event":                             "evento",
		"weather":                           "meteorología",
		"accident":                          "accidente",
		"congestion":                        "congestión",
		"abnormalTraffic":                   "tráfico anómalo",
		"roadOrCarriagewayOrLaneManagement": "gestión de vía",
		"maintenanceWorks":                  "mantenimiento",
		"generalObstruction":                "obstrucción",
		"infrastructureDamage":              "daño en la vía",
		"environmentalObstruction":          "obstáculo",
		"publicEvent":                       "evento público",
	}
	severityES = map[string]string{
		"unknown": "desconocida",
		"low":     "baja",
		"medium":  "media",
		"high":    "alta",
	}
	directionES = map[string]string{
		"both":     "ambos",
		"positive": "creciente",
		"negative": "decreciente",
	}
	subtypeES = map[string]string{
		"roadworks":                  "obras",
		"maintenanceWorks":           "mantenimiento",
		"damagedRoadSurface":         "firme dañado",
		"damagedRoadsign":            "señal dañada",
		"rockfalls":                  "desprendimientos",
		"avalanches":                 "aludes",
		"flooding":                   "inundación",
		"snow":                       "nieve",
		"ice":                        "hielo",
		"fog":                        "niebla",
		"rain":                       "lluvia",
		"wind":                       "viento",
		"objectOnTheRoad":            "objeto en la calzada",
		"brokenDownVehicle":          "vehículo averiado",
		"vehicleOnFire":              "vehículo incendiado",
		"obstruction":                "obstrucción",
		"other":                      "otra",
		"demonstration":              "manifestación",
		"roadClosed":                 "vía cortada",
		"laneClosures":               "carriles cerrados",
		"singleAlternateLineTraffic": "paso alternativo",
	}
)

// es translates a contract value for display, falling back to the original.
func es(m map[string]string, v string) string {
	if t, ok := m[v]; ok {
		return t
	}
	return v
}
