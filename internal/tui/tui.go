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
	styleFrame  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colFrame)
	styleTitle  = lipgloss.NewStyle().Foreground(colFrame).Bold(true)
	styleDim    = lipgloss.NewStyle().Foreground(colDim)
	styleWarm   = lipgloss.NewStyle().Foreground(colWarm)
	styleSel    = lipgloss.NewStyle().Foreground(lipgloss.Color("230")).Background(colAccent)
	styleArt    = lipgloss.NewStyle().Foreground(colFrame)
	styleHeader = lipgloss.NewStyle().Foreground(colDim).Bold(true)
)

// art is drawn once, at the top. Three rows of block letters rather than a huge
// ASCII logo: in an 80x24 terminal a big picture is the whole screen, and the
// point of a traffic tool is the traffic.
const art = ` _____ ____      _    _____ ___ ___  ___    ___ _     ___
|_   _|  _ \    / \  |  ___|_ _/ _ \/ _ \  / __| |   |_ _|
  | | | |_) |  / _ \ | |_   | | | | | | | | (__| |    | |
  | | |  _ <  / ___ \|  _|  | | |_| | |_|  \___ \ |__ _| |
  |_| |_| \_\/_/   \_\_|   |___\___/ \___|     |_____|___|`

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
		m.cursors[msg.view], m.scrolls[msg.view] = 0, 0
		return m, nil

	case tea.KeyMsg:
		s := msg.String()
		if s == "q" || s == "ctrl+c" || s == "esc" {
			return m, tea.Quit
		}
		// A number switches view. The first switch starts the load; coming
		// back to a loaded view does not refetch, so flipping through screens
		// costs no requests.
		if v := keyOf(s); v != viewCount {
			m.current = v
			if m.states[v] == loading && m.tables[v].cols == nil {
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
// header and the shortcut bar take the rest.
func (m Model) visibleRows() int {
	rows := m.height - len(strings.Split(art, "\n")) - 8
	if rows < 3 {
		rows = 3
	}
	return rows
}

// View draws the window.
func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	v := m.current

	var b strings.Builder
	b.WriteString(styleArt.Render(art))
	b.WriteString("\n\n")

	switch m.states[v] {
	case loading:
		b.WriteString(styleDim.Render(viewLoading[v]))
	case failed:
		b.WriteString(styleWarm.Render("No se pudo cargar: " + m.errs[v].Error()))
	default:
		t := m.tables[v]
		if len(t.rows) == 0 {
			b.WriteString(styleDim.Render("(sin resultados)"))
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
		b.WriteString(m.render(t, cursor, m.scrolls[v], visible, m.width-4))
	}

	body := strings.TrimRight(b.String(), "\n")
	frame := styleFrame.Width(m.width - 2).Render(body)
	title := styleTitle.Render(" " + titleOf(v, len(m.tables[v].rows), m.states[v]))
	bar := styleDim.Render(m.footer())

	// The title rides the top border and the shortcuts the bottom one, so the
	// window itself is the chrome and no rows are spent on decoration.
	lines := strings.Split(frame, "\n")
	if len(lines) >= 2 {
		first := lines[0]
		lines[0] = "╭─" + title + strings.Repeat("─", max(1, m.width-len(show(v))+6)) + "╮"
		last := lines[len(lines)-1]
		lines[len(lines)-1] = styleFrame.Render("╰─ ") + bar +
			styleFrame.Render(strings.Repeat("─", max(1, m.width-len(m.footer())-8)+4)) + styleFrame.Render("╯")
		_ = first
		_ = last
	}
	return strings.Join(lines, "\n")
}

// show is the plain title, used to size the top border.
func show(v view) string { return viewNames[v] }

// footer names only the keys that work in the current state: hinting "r
// recargar" while loading would be a lie.
func (m Model) footer() string {
	switch m.states[m.current] {
	case loading:
		return "cargando…  ·  1-4 vistas  ·  q salir"
	case failed:
		return "r reintentar  ·  1-4 vistas  ·  q salir"
	default:
		return "1-4 vistas  ·  j/k mover  ·  g/G extremos  ·  r recargar  ·  q salir"
	}
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
		return inc.SubtypeRaw
	}
	return inc.Type
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
