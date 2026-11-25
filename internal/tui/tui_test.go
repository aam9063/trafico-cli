package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// key builds the KeyMsg a real key press produces, without a terminal.
func key(s string) tea.KeyMsg {
	runes := []rune(s)
	if len(runes) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: runes}
	}
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: runes}
}

// send feeds one key to the model and keeps the returned command too.
func send(m Model, s string) (Model, tea.Cmd) {
	nm, cmd := m.Update(key(s))
	return nm.(Model), cmd
}

// press types a sequence of keys, dropping the commands. A multi-rune
// word is pressed one letter at a time, the way a human types it.
func press(m Model, keys ...string) Model {
	for _, k := range keys {
		runes := []rune(k)
		if len(runes) > 1 && k != "enter" && k != "esc" && k != "backspace" {
			for _, r := range runes {
				nm, _ := send(m, string(r))
				m = nm
			}
			continue
		}
		m, _ = send(m, k)
	}
	return m
}

func newModel() Model {
	return New(nil, nil)
}

// ready makes a view pretend to be loaded so its key paths are reachable.
func loaded(m Model, v view) Model {
	m.states[v] = ready
	m.tables[v].cols = []string{"CARRETERA"}
	m.tables[v].rows = [][]string{{"A-1"}}
	return m
}

// S8 / TUI-03: `/` opens the search, enter applies the province and esc
// clears the filter once applied.
func TestProvinceSearchLifecycle(t *testing.T) {
	m := press(loaded(newModel(), viewIncidents), "/", "t", "o", "l")
	if !m.typing {
		t.Fatal("/ did not open the search")
	}
	if m.typed != "tol" {
		t.Fatalf("typed %q, want tol", m.typed)
	}
	if m.province != "" {
		t.Fatalf("the filter applied before enter: %q", m.province)
	}

	m = press(m, "enter")
	if m.typing || m.province != "tol" {
		t.Fatalf("enter did not apply the filter: searching=%v province=%q", m.typing, m.province)
	}

	m = press(m, "esc")
	if m.province != "" || m.typing {
		t.Fatalf("esc did not clear the filter: province=%q searching=%v", m.province, m.typing)
	}
}

// While typing, keys belong to the input: a digit must not switch views and
// even q must not quit the program.
func TestTypingGoesToInput(t *testing.T) {
	m := press(loaded(newModel(), viewIncidents), "/", "4", "q")
	if m.current != viewIncidents {
		t.Fatalf("typing switched the view to %d", m.current)
	}
	if m.typed != "4q" {
		t.Fatalf("typed %q, want 4q", m.typed)
	}
}

// Backspace edits the typed text; esc while typing cancels without applying;
// enter with an empty query just closes the search.
func TestSearchEditing(t *testing.T) {
	m := press(loaded(newModel(), viewIncidents), "/", "a", "l", "backspace", "a")
	if m.typed != "aa" {
		t.Fatalf("typed %q, want aa", m.typed)
	}

	cancelled := press(loaded(newModel(), viewIncidents), "/", "to", "esc")
	if cancelled.typing || cancelled.typed != "" || cancelled.province != "" {
		t.Fatalf("esc while typing: searching=%v typed=%q province=%q",
			cancelled.typing, cancelled.typed, cancelled.province)
	}

	empty := press(loaded(newModel(), viewIncidents), "/", "enter")
	if empty.typing || empty.province != "" {
		t.Fatalf("empty enter changed state: searching=%v province=%q", empty.typing, empty.province)
	}
}

// The filter reaches the app criteria of every view, never a filter of the
// TUI's own (S5); esc clears it from the criteria too.
func TestFilterPropagatesToCriteria(t *testing.T) {
	m := press(loaded(newModel(), viewIncidents), "/", "alicante", "enter")
	m = loaded(m, viewIncidents) // the reload landed; one result row

	if inc := m.incidentCriteria(); len(inc.Criteria.Provinces) != 1 || inc.Criteria.Provinces[0] != "alicante" {
		t.Fatalf("incident criteria provinces = %v", inc.Criteria.Provinces)
	}
	if s := m.signCriteria(); len(s.Provinces) != 1 {
		t.Fatalf("sign criteria provinces = %v", s.Provinces)
	}
	if r := m.radarCriteria(); len(r.Provinces) != 1 {
		t.Fatalf("radar criteria provinces = %v", r.Provinces)
	}
	if sec := m.sectionCriteria(); len(sec.Provinces) != 1 {
		t.Fatalf("section criteria provinces = %v", sec.Provinces)
	}

	if m.title() != "Incidencias · provincia alicante · 1" {
		t.Fatalf("title with filter = %q", m.title())
	}

	m = press(m, "esc")
	if len(m.incidentCriteria().Criteria.Provinces) != 0 {
		t.Fatalf("criteria keep provinces after esc: %v", m.incidentCriteria().Criteria.Provinces)
	}
}

// Applying the filter reloads the current view; a view loaded under another
// province reloads on the switch; one already loaded with the same province
// does not refetch. With no filter, esc quits instead.
func TestViewReloadsOnFilterChange(t *testing.T) {
	m := loaded(newModel(), viewIncidents)
	m.states[viewSigns] = ready
	m.tables[viewSigns].cols = []string{"CARRETERA"}

	m, cmd := send(m, "/")
	_ = cmd
	m = press(m, "t", "o")
	m, cmd = send(m, "enter")
	if cmd == nil || m.states[viewIncidents] != loading {
		t.Fatal("applying the filter did not reload the current view")
	}

	// The reload lands with its province recorded.
	m.states[viewIncidents] = ready
	m.tables[viewIncidents] = table{cols: []string{"CARRETERA"}, rows: [][]string{{"A-1"}}}
	m.loadedProv[viewIncidents] = "to"

	// Signs was loaded without a filter: switching there must reload it.
	_, cmd = send(m, "2")
	if cmd == nil {
		t.Fatal("switching to a view loaded under another province did not reload it")
	}
	m.province = "to"

	// Loaded with the same province: switching back costs no request.
	m.loadedProv[viewSigns] = "to"
	m2, cmd := send(m, "1")
	if cmd != nil {
		t.Fatal("a view already loaded with the current filter was refetched")
	}
	_ = m2

	// Applying a second filter marks the view stale again.
	m.province = "to"
	m, _ = send(m, "/")
	m = press(m, "g", "u")
	m, cmd = send(m, "enter")
	if cmd == nil || m.province != "gu" {
		t.Fatalf("a new filter did not reload the view: province=%q", m.province)
	}

	// With no filter active, esc quits rather than clearing nothing.
	m.province = ""
	m, cmd = send(m, "esc")
	if cmd == nil || m.typing || m.province != "" {
		t.Fatal("esc without a filter must quit")
	}
}

// The frame must close at every terminal width: the bug this pins is the
// border breaking open when the footer or the banner was wider than the
// window. Every drawn line has exactly the width the terminal reported.
func TestFrameIsExactlyTerminalWidth(t *testing.T) {
	for _, w := range []int{40, 60, 76, 79, 80, 96, 100, 140} {
		m := New(nil, nil)
		m.states[viewIncidents] = ready
		m.tables[viewIncidents] = table{
			cols: []string{"CARRETERA", "PK", "TIPO", "GRAVEDAD", "DESCRIPCIÓN"},
			rows: [][]string{
				{"A-1", "12.5-13", "obras", "alta", "obras de mantenimiento en el arcón derecho"},
				{"M-30", "3.0", "accidente", "media", "accidente con dos vehículos"},
			},
		}
		for _, typing := range []bool{false, true} {
			mm := m
			mm.typing, mm.typed = typing, ""
			if typing {
				mm.typed = "toledo"
			}
			nm, _ := mm.Update(tea.WindowSizeMsg{Width: w, Height: 24})
			mm = nm.(Model)
			for i, line := range splitLines(mm.View()) {
				// The search prompt sits above the frame and may be shorter;
				// the frame lines themselves must all be exactly w cells.
				if !strings.HasPrefix(line, "╭") && !strings.HasPrefix(line, "│") && !strings.HasPrefix(line, "╰") {
					continue
				}
				if got := ansi.StringWidth(line); got != w {
					t.Fatalf("width %d typing=%v line %d has %d cells: %q", w, typing, i, got, line)
				}
			}
		}
	}
}

// The banner spells the whole name, hyphen included, and only appears when it
// fits: half a block letter is worse than no banner.
func TestBanner(t *testing.T) {
	if !strings.Contains(art, "____") || len(splitLines(art)) != 5 {
		t.Fatal("the banner is not five rows of block letters")
	}
	for _, want := range []string{"|_   _|", "|_____|"} {
		if !strings.Contains(art, want) {
			t.Fatalf("the banner lost %q", want)
		}
	}
	if widest := ansi.StringWidth(splitLines(art)[0]); widest > 78 {
		t.Fatalf("the banner is %d cells wide and no longer fits 80 columns", widest)
	}

	wide := New(nil, nil)
	nm, _ := wide.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !nm.(Model).banner() {
		t.Fatal("80x24 must show the banner")
	}
	nm, _ = wide.Update(tea.WindowSizeMsg{Width: 70, Height: 24})
	if nm.(Model).banner() {
		t.Fatal("a 70-column terminal must drop the banner")
	}
	nm, _ = wide.Update(tea.WindowSizeMsg{Width: 120, Height: 12})
	if nm.(Model).banner() {
		t.Fatal("a 12-row terminal must drop the banner")
	}
}

func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

// The table speaks Spanish even where the contract keeps the feed's English
// value: a person reading "DIRECCIÓN" should not be handed "both". The JSON
// is untouched (P-04); this is presentation only.
func TestDirectionIsTranslated(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := []struct {
		in   *string
		want string
	}{
		{str("both"), "ambos"},
		{str("positive"), "creciente"},
		{str("negative"), "decreciente"},
		{str(""), "-"},
		{nil, "-"},
		{str("algoNuevo"), "algoNuevo"}, // honest fallback, never a guess
	}
	for _, c := range cases {
		if got := directionCell(c.in); got != c.want {
			t.Errorf("directionCell(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The province search lives INSIDE the window, as a labelled field above the
// table — not floating above the top border where a reader cannot tell where
// it came from. It is always drawn, even with no filter, so it is discoverable
// before anyone presses `/`.
func TestProvinceSearchRidesInsideTheFrame(t *testing.T) {
	m := loaded(newModel(), viewIncidents)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)

	lines := splitLines(m.View())
	top, field := -1, -1
	for i, line := range lines {
		if strings.HasPrefix(line, "╭") {
			top = i
		}
		if strings.Contains(line, "Provincia") && strings.HasPrefix(line, "│") {
			field = i
		}
	}
	if top < 0 {
		t.Fatal("no top border in the view")
	}
	if field < 0 {
		t.Fatalf("the search field is not a framed row:\n%s", m.View())
	}
	if field < top {
		t.Fatalf("the search field sits above the frame (top=%d field=%d):\n%s", top, field, m.View())
	}
	// Nothing may be drawn above the top border: that is the bug.
	for i := 0; i < top; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			t.Fatalf("line %d draws outside the frame: %q", i, lines[i])
		}
	}

	// While typing, the field shows what is being typed, in its own row.
	typing := press(m, "/", "toledo")
	if got := fieldLine(t, typing.View()); !strings.Contains(got, "toledo") {
		t.Fatalf("the field does not show the typed province: %q", got)
	}
	// Applied and unfocused, it still shows the filter so the screen explains
	// itself without pressing anything.
	applied := press(typing, "enter")
	if got := fieldLine(t, applied.View()); !strings.Contains(got, "toledo") {
		t.Fatalf("the field does not keep the applied province: %q", got)
	}
}

// fieldLine returns the framed row that carries the search label.
func fieldLine(t *testing.T, view string) string {
	t.Helper()
	for _, line := range splitLines(view) {
		if strings.Contains(line, "Provincia") && strings.HasPrefix(line, "│") {
			return line
		}
	}
	t.Fatalf("no search field row in:\n%s", view)
	return ""
}
