// Package tui is the terminal interface of trafico-cli.
//
// It is a third presentation of the same data the JSON contract serves and the
// text renderers print: every view calls the same internal/app function the
// command calls, and nothing here parses a feed, filters a record or invents a
// field. If the TUI ever needs a query of its own, that query belongs in app.
//
// It only runs when both stdin and stdout are terminals. In a pipe, in CI, or
// under an agent, `trafico` prints its help as it always has: a UI that draws
// escape codes into a log file is worse than no UI.
package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/aam9063/trafico-cli/internal/config"
	"github.com/aam9063/trafico-cli/internal/feeds"
	"github.com/aam9063/trafico-cli/internal/model"
)

// Palette. One place, so changing the look never means hunting through views.
var (
	colFrame  = lipgloss.Color("62")  // window border and title
	colAccent = lipgloss.Color("212") // the selected row
	colDim    = lipgloss.Color("243") // secondary text
	colWarm   = lipgloss.Color("214") // severity, the one warm tone
)

var (
	// styleBorder paints the frame lines the layout assembles by hand. It must
	// not carry a Border(): a bordered style wraps every fragment in its own
	// box, which is exactly what broke the frame before.
	styleBorder = lipgloss.NewStyle().Foreground(colFrame)
	styleTitle  = lipgloss.NewStyle().Foreground(colFrame).Bold(true)
	styleDim    = lipgloss.NewStyle().Foreground(colDim)
	styleWarm   = lipgloss.NewStyle().Foreground(colWarm)
	styleSel    = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(colAccent)
	styleArt    = lipgloss.NewStyle().Foreground(colFrame)
	styleHeader = lipgloss.NewStyle().Foreground(colDim).Bold(true)
)

// art is drawn once, at the top, and spells the whole name including the
// hyphen. Five rows of block letters rather than a huge ASCII logo: in an
// 80x24 terminal a big picture is the whole screen, and the point of a
// traffic tool is the traffic.
const art = ` _____  ____      _      ____  ___   ____   ___           ____  _      ___
|_   _||  _ \    / \    / ___||_ _| / ___| / _ \  _____  / ___|| |    |_ _|
  | |  | |_) |  / _ \  | |     | | | |    | | | ||_____|| |    | |     | |
  | |  |  _ <  / ___ \ | |___  | | | |___ | |_| |       | |___ | |___  | |
  |_|  |_| \_\/_/   \_\ \____||___| \____| \___/         \____||_____||___|`

// state is what a view is doing.
type state int

const (
	loading state = iota
	ready
	failed
)

// Model is the whole UI. Bubbletea's Update returns a new Model, so the state
// is testable without a terminal: hand it a KeyMsg and read the result.
type Model struct {
	cfg     *config.Config
	fetcher *feeds.Fetcher

	current view
	states  [viewCount]state
	errs    [viewCount]error
	tables  [viewCount]table
	cursors [viewCount]int
	scrolls [viewCount]int

	// Province search (TUI-03): the query being typed, the province that is
	// filtering every view, and the province each view was loaded with, so a
	// switch knows whether the table on screen still answers the question.
	typing     bool
	typed      string
	province   string
	loadedProv [viewCount]string

	width  int
	height int
}

// tableMsg carries a loaded view back into the UI, so the network never blocks
// the render loop.
type tableMsg struct {
	view view
	t    table
	err  error
}

// New builds the model. The first view starts loading immediately.
func New(cfg *config.Config, fetcher *feeds.Fetcher) Model {
	var m Model
	m.cfg, m.fetcher = cfg, fetcher
	m.states[viewIncidents] = loading
	return m
}

// Init starts the first load.
func (m Model) Init() tea.Cmd { return m.load(viewIncidents) }

// load returns the command that fetches one view.
func (m Model) load(v view) tea.Cmd {
	return func() tea.Msg { return m.loadView(v) }
}

// Update handles keys and load results.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tableMsg:
		if msg.err != nil {
			m.states[msg.view], m.errs[msg.view] = failed, msg.err
			return m, nil
		}
		m.states[msg.view], m.tables[msg.view] = ready, msg.t
		m.loadedProv[msg.view] = m.province
		m.cursors[msg.view], m.scrolls[msg.view] = 0, 0
		return m, nil

	case tea.KeyMsg:
		s := msg.String()
		// While the search input is open, every printable key belongs to it;
		// typing "4" must not switch views and "q" must not quit.
		if m.typing {
			switch s {
			case "enter":
				// An empty query just closes: there is nothing to apply.
				m.typing = false
				if m.typed == "" {
					return m, nil
				}
				m.province = m.typed
				m.typed = ""
				// Rows on screen were not filtered: reload the current view.
				m.states[m.current] = loading
				return m, m.load(m.current)
			case "esc":
				m.typing, m.typed = false, ""
				return m, nil
			case "backspace":
				if r := []rune(m.typed); len(r) > 0 {
					m.typed = string(r[:len(r)-1])
				}
				return m, nil
			}
			if len([]rune(s)) == 1 {
				m.typed += s
				return m, nil
			}
			return m, nil
		}
		if s == "/" {
			m.typing, m.typed = true, ""
			return m, nil
		}
		if s == "esc" {
			// With a filter applied, esc clears it and shows everything again;
			// without one, esc quits.
			if m.province != "" {
				m.province = ""
				m.states[m.current] = loading
				return m, m.load(m.current)
			}
			return m, tea.Quit
		}
		if s == "q" || s == "ctrl+c" {
			return m, tea.Quit
		}
		// A number switches view. The first switch starts the load; coming
		// back to a loaded view does not refetch, so flipping through screens
		// costs no requests. A view loaded under another province is stale:
		// switching to it reloads with the current one.
		if v := keyOf(s); v != viewCount {
			m.current = v
			if m.states[v] != ready || m.loadedProv[v] != m.province {
				m.states[v] = loading
				return m, m.load(v)
			}
			return m, nil
		}
		switch s {
		case "j", "down":
			m.move(1)
		case "k", "up":
			m.move(-1)
		case "g", "home":
			m.cursors[m.current] = 0
		case "G", "end":
			m.cursors[m.current] = len(m.tables[m.current].rows) - 1
			if m.cursors[m.current] < 0 {
				m.cursors[m.current] = 0
			}
		case "r":
			m.states[m.current] = loading
			return m, m.load(m.current)
		}
	}
	return m, nil
}

// move clamps the cursor instead of wrapping: at the top, k does nothing, and
// a list that jumps from the end to the beginning is disorienting.
func (m *Model) move(delta int) {
	n := len(m.tables[m.current].rows)
	if m.states[m.current] != ready || n == 0 {
		return
	}
	c := m.cursors[m.current] + delta
	if c < 0 {
		c = 0
	}
	if c >= n {
		c = n - 1
	}
	m.cursors[m.current] = c
}

// visibleRows is how many table rows fit: the frame, the art, the column
// header and the shortcut bar take the rest; the search line takes one more
// while it is open.
func (m Model) visibleRows() int {
	rows := m.height - 8
	if m.banner() {
		rows -= artRows + 1
	}
	if m.typing {
		rows--
	}
	if rows < 3 {
		rows = 3
	}
	return rows
}

// artRows counts how many lines the banner spends, plus the blank line
// under it. Kept next to the layout instead of counted on the fly, so the
// arithmetic below reads as one unit.
const artRows = 5 // plus one blank line, counted in the -8

// banner fits when the terminal can hold the block letters plus the frame.
// Truncating ASCII art would leave half a word, so a narrow window drops it
// and gives the rows back to the table.
func (m Model) banner() bool { return m.width >= 80 && m.height >= 20 }

// cells returns the display width of a possibly styled string: escape
// sequences are not cells.
func cells(s string) int { return ansi.StringWidth(s) }

// View draws the window. Every border line is assembled to an exact width
// from plain text measured in cells, so the frame closes at any terminal
// size instead of breaking open when the footer is longer than the frame.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	v := m.current

	// The search line rides above the frame, like a prompt would. It only
	// exists while typing: a filter already applied is told by the title.
	above := ""
	if m.typing {
		cursor := ""
		if m.typed == "" {
			cursor = "▌" // left half block, the terminal cursor stand-in
		}
		prompt := styleDim.Render("Provincia: ") + styleTitle.Render(m.typed+cursor)
		help := styleDim.Render("   enter aplica · esc cancela")
		// The hint is the first thing to go in a narrow terminal; the prompt
		// and what is being typed are the point.
		if cells(prompt)+cells(help) <= m.width {
			above = prompt + help
		} else {
			above = prompt
		}
		if cells(above) > m.width {
			above = ansi.Truncate(above, m.width, "…")
		}
	}

	// Body lines, one per row. Every block is split: the banner and the
	// table are multi-line strings, and a multi-line block handed to the
	// frame as one element would only get a border on its first line.
	body := []string{}
	if m.banner() {
		body = append(lines(styleArt.Render(art)), "")
	}
	switch m.states[v] {
	case loading:
		body = append(body, lines(styleDim.Render(viewLoading[v]))...)
	case failed:
		body = append(body, lines(styleWarm.Render("No se pudo cargar: "+m.errs[v].Error()))...)
	default:
		t := m.tables[v]
		if len(t.rows) == 0 {
			body = append(body, lines(styleDim.Render("(sin resultados)"))...)
			break
		}
		cursor := m.cursors[v]
		visible := m.visibleRows()
		if cursor < m.scrolls[v] {
			m.scrolls[v] = cursor
		}
		if cursor >= m.scrolls[v]+visible {
			m.scrolls[v] = cursor - visible + 1
		}
		body = append(body, lines(m.render(t, cursor, m.scrolls[v], visible, m.width-4))...)
	}
	body = append(body, "") // breathe between the table and the bottom border

	// Frame assembly. fill keeps every line exactly m.width cells; a text
	// that does not fit is truncated instead of pushing the border open.
	title := titleOf(v, len(m.tables[v].rows), m.states[v], m.province)
	tw := cells(title)
	if tw > m.width-5 {
		tw = m.width - 5
	}
	top := frameCell("╭─") + styleTitle.Render(truncate(title, m.width-6)) +
		frameCell(strings.Repeat("─", m.width-3-tw)+"╮")

	out := make([]string, 0, len(body)+2)
	if above != "" {
		out = append(out, above)
	}
	out = append(out, top)
	// Inner width: the two border columns and the space after the left one.
	inner := max(1, m.width-3)
	for _, line := range body {
		// Truncate by cells, not bytes, so a styled or accented line cannot
		// push the right border out of the window.
		if cells(line) > inner {
			line = ansi.Truncate(line, inner, "…")
		}
		out = append(out, frameCell("│")+" "+line+strings.Repeat(" ", inner-cells(line))+frameCell("│"))
	}
	footer := m.footer()
	fw := cells(footer)
	if fw > m.width-6 {
		fw = m.width - 6
	}
	last := "╰─ " + styleDim.Render(truncate(footer, m.width-6)) +
		frameCell(" "+strings.Repeat("─", max(0, m.width-5-fw))+"╯")
	out = append(out, last)
	return strings.Join(out, "\n")
}

// lines splits a rendered block into its rows, dropping the trailing empty
// row a block renderer leaves behind.
func lines(block string) []string {
	return strings.Split(strings.TrimRight(block, "\n"), "\n")
}

// frameCell paints one border fragment in the frame colour.
func frameCell(s string) string { return styleBorder.Render(s) }

// footer names the keys that work in the current state: hinting "r
// recargar" while loading would be a lie. The hints are dropped in order
// when the terminal is narrow, "q salir" always survives, so a small
// terminal loses detail but never the way out.
func (m Model) footer() string {
	// The views are named, not numbered: "1-4" tells a first-time reader
	// nothing about what is behind each key. The current one is bracketed so
	// the bar doubles as a position indicator.
	views, onlyCurrent := "", "["+fmt.Sprintf("%d %s", int(m.current)+1, viewNames[m.current])+"]"
	for i := 0; i < int(viewCount); i++ {
		v := view(i)
		label := fmt.Sprintf("%d %s", i+1, viewNames[v])
		if v == m.current {
			label = "[" + label + "]"
		}
		if i > 0 {
			views += " "
		}
		views += label
	}

	// Hints in display order, each with how early it should be dropped when
	// the bar does not fit. A higher drop means it goes sooner; the search
	// hint is the newest capability and survives longer than the movement
	// hints an experienced reader already knows.
	type hint struct {
		text string
		drop int
	}
	var hints []hint
	switch {
	case m.typing:
		// The input owns the keys: movement hints would be a lie.
		hints = []hint{{"escribiendo…", 9}, {"enter aplica", 9}, {"esc cancela", 9}}
	case m.states[m.current] == loading:
		hints = []hint{{"cargando…", 9}, {"q salir", 0}}
	case m.states[m.current] == failed:
		hints = []hint{{"r reintentar", 9}, {"q salir", 0}}
	default:
		hints = append(hints, hint{"/ provincia", 2})
		hints = append(hints,
			hint{"j/k mover", 5},
			hint{"g/G extremos", 7},
			hint{"r recargar", 7},
			hint{"q salir", 0},
		)
	}

	sep := "  ·  "
	join := func(bar string, hs []hint) string {
		parts := make([]string, 0, len(hs)+1)
		parts = append(parts, bar)
		for _, h := range hs {
			parts = append(parts, h.text)
		}
		return strings.Join(parts, sep)
	}

	// First drop the hints a reader can live without, then fall back to
	// naming only the current view: the bracket is the position indicator,
	// and a bar that fits tells more than four names cut in half.
	for _, bar := range []string{views, onlyCurrent} {
		for len(hints) > 0 && cells(join(bar, hints)) > m.width-6 {
			worst := 0
			for i, h := range hints {
				if h.drop > hints[worst].drop {
					worst = i
				}
			}
			if hints[worst].drop == 0 {
				break
			}
			hints = append(hints[:worst], hints[worst+1:]...)
		}
		if cells(join(bar, hints)) <= m.width-6 {
			return join(bar, hints)
		}
	}
	// Still too wide (an absurdly narrow window): keep the short bar and the
	// last hint, and let the frame truncate it rather than break open.
	return join(onlyCurrent, hints)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func truncate(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 1 {
		return string(r[:w])
	}
	return string(r[:w-1]) + "…"
}

func pad(s string, w int) string {
	n := w - len([]rune(s))
	if n <= 0 {
		return s
	}
	return s + strings.Repeat(" ", n)
}

// pk renders a kilometre interval, a single point, or a dash. It never invents
// a bound the feed did not publish.
func pk(p *model.PK) string {
	switch {
	case p == nil || (p.From == nil && p.To == nil):
		return "-"
	case p.To == nil:
		return trim(*p.From)
	case p.From == nil:
		return "-" + trim(*p.To)
	default:
		return trim(*p.From) + "-" + trim(*p.To)
	}
}

// trim formats a kilometre point without trailing zeros.
func trim(v float64) string {
	s := fmt.Sprintf("%.3f", v)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// describe is the feed's own words, never a translation.
func describe(inc model.Incident) string {
	if inc.RawDescription != nil && *inc.RawDescription != "" {
		return *inc.RawDescription
	}
	if inc.SubtypeRaw != "" && inc.SubtypeRaw != inc.Type {
		return es(subtypeES, inc.SubtypeRaw)
	}
	return es(typeES, inc.Type)
}

// Run starts the interface. It blocks until the user quits.
func Run(cfg *config.Config, fetcher *feeds.Fetcher) error {
	p := tea.NewProgram(New(cfg, fetcher))
	_, err := p.Run()
	return err
}

// isTerminal reports whether the file is a character device, which is what a
// terminal is. It needs no dependency and behaves the same on the three
// release targets.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// WantsTTY reports whether the process was started with no arguments and a
// terminal on both ends: the only case where the interface opens.
func WantsTTY(args []string) bool {
	return len(args) == 0 && isTerminal(os.Stdin) && isTerminal(os.Stdout)
}
