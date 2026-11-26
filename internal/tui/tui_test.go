package tui

import (
	"fmt"
	"image/png"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
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

// withTrueColor stands in for a colour terminal: the tests run with no TTY,
// where lipgloss detects Ascii and the TUI would fall back to the block
// letters. The banner tests need the picture path, so they swap the probe;
// the cleanup restores the real probe for every other test.
func withTrueColor(t *testing.T) {
	t.Helper()
	old := bannerColorProfile
	bannerColorProfile = func() termenv.Profile { return termenv.TrueColor }
	t.Cleanup(func() { bannerColorProfile = old })
}

// frameTable is the base model the width test draws: two loaded rows, one
// long free-text cell, everything the packing arithmetic has to survive.
func frameTable(m Model) Model {
	m.states[viewIncidents] = ready
	m.tables[viewIncidents] = table{
		cols: []string{"CARRETERA", "PK", "TIPO", "GRAVEDAD", "DESCRIPCIÓN"},
		rows: [][]string{
			{"A-1", "12.5-13", "obras", "alta", "obras de mantenimiento en el arcón derecho"},
			{"M-30", "3.0", "accidente", "media", "accidente con dos vehículos"},
		},
	}
	return m
}

// ready makes a view pretend to be loaded so its key paths are reachable.
func loaded(m Model, v view) Model {
	m.states[v] = ready
	m.tables[v].cols = []string{"CARRETERA"}
	m.tables[v].rows = [][]string{{"A-1"}}
	return m
}

// chosenView picks a view from the opening menu, the way a user does: one
// number key closes the menu and opens the province search, and the search
// is cancelled so the test controls when it opens.
func chosenView(m Model, v view) Model {
	nm, _ := m.Update(key(fmt.Sprintf("%d", int(v)+1)))
	m = nm.(Model)
	if m.menuOpen() || !m.typing {
		panic("chosenView: the menu did not hand over to the search")
	}
	m.typing, m.typed = false, ""
	return m
}

// S8 / TUI-03: `/` opens the search and enter applies the province; esc
// from the rows goes back to the menu, which forgets the filter. The opening
// menu is modal: the tests pick a view first, exactly as a user must.
func TestProvinceSearchLifecycle(t *testing.T) {
	m := press(loaded(chosenView(newModel(), viewIncidents), viewIncidents), "/", "t", "o", "l")
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
	if m.province != "" || m.typing || !m.menuOpen() {
		t.Fatalf("esc did not go back to the menu: province=%q searching=%v menu=%v", m.province, m.typing, m.menuOpen())
	}
}

// While typing, keys belong to the input: a digit must not switch views and
// even q must not quit the program.
func TestTypingGoesToInput(t *testing.T) {
	m := press(loaded(chosenView(newModel(), viewIncidents), viewIncidents), "/", "4", "q")
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
	m := press(loaded(chosenView(newModel(), viewIncidents), viewIncidents), "/", "a", "l", "backspace", "a")
	if m.typed != "aa" {
		t.Fatalf("typed %q, want aa", m.typed)
	}

	cancelled := press(loaded(chosenView(newModel(), viewIncidents), viewIncidents), "/", "to", "esc")
	if cancelled.typing || cancelled.typed != "" || cancelled.province != "" {
		t.Fatalf("esc while typing: searching=%v typed=%q province=%q",
			cancelled.typing, cancelled.typed, cancelled.province)
	}

	empty := press(loaded(chosenView(newModel(), viewIncidents), viewIncidents), "/", "enter")
	if empty.typing || empty.province != "" {
		t.Fatalf("empty enter changed state: searching=%v province=%q", empty.typing, empty.province)
	}
}

// The filter reaches the app criteria of every view, never a filter of the
// TUI's own (S5); esc (back to the menu) clears it from the criteria too.
func TestFilterPropagatesToCriteria(t *testing.T) {
	m := press(loaded(chosenView(newModel(), viewIncidents), viewIncidents), "/", "alicante", "enter")
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
// does not refetch. esc goes back to the menu, never quits.
func TestViewReloadsOnFilterChange(t *testing.T) {
	m := loaded(chosenView(newModel(), viewIncidents), viewIncidents)
	m.states[viewSigns] = ready
	m.tables[viewSigns].cols = []string{"CARRETERA"}

	var cmd tea.Cmd
	m, cmd = send(m, "/")
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

	// esc is "back", never "quit": from the rows it returns to the menu.
	m, cmd = send(m, "esc")
	if cmd != nil || !m.menuOpen() || m.province != "" {
		t.Fatalf("esc from a view: cmd=%v menu=%v province=%q, want back to the menu", cmd != nil, m.menuOpen(), m.province)
	}
}

// esc goes back one step and q quits, everywhere but inside the search box
// (where q is a letter). The steps: menu -> province search -> rows. From
// the rows esc returns to the menu; from the search the menu opened, esc
// returns to the menu too; from a search opened with /, esc returns to the
// rows. The menu is the top: esc there quits, as before.
func TestEscGoesBackAndQQuits(t *testing.T) {
	withTrueColor(t)

	// Menu -> search -> esc: back on the menu, nothing applied.
	m := press(newModel(), "3")
	if !m.typing || m.menuOpen() {
		t.Fatalf("3 on the menu: typing=%v menu=%v", m.typing, m.menuOpen())
	}
	m, cmd := send(m, "esc")
	if cmd != nil || !m.menuOpen() || m.typing {
		t.Fatalf("esc in the menu's search: cmd=%v menu=%v typing=%v, want the menu", cmd != nil, m.menuOpen(), m.typing)
	}

	// Menu -> search -> enter with nothing typed: every province, loaded.
	m = press(m, "3")
	m, cmd = send(m, "enter")
	if cmd == nil || m.states[viewRadars] != loading || m.province != "" {
		t.Fatalf("empty enter from the menu: cmd=%v state=%v province=%q, want all provinces loading",
			cmd != nil, m.states[viewRadars], m.province)
	}
	m = loaded(m, viewRadars)

	// Rows -> / -> esc: back on the rows, still on the view.
	m = press(m, "/", "al", "esc")
	if m.typing || m.menuOpen() || m.current != viewRadars {
		t.Fatalf("esc in a / search: typing=%v menu=%v view=%d, want the rows", m.typing, m.menuOpen(), m.current)
	}

	// q on the rows quits; esc on the rows goes back to the menu.
	if _, cmd := send(m, "q"); cmd == nil {
		t.Fatal("q on the rows does not quit")
	}
	m, cmd = send(m, "esc")
	if cmd != nil || !m.menuOpen() {
		t.Fatalf("esc on the rows: cmd=%v menu=%v, want back to the menu", cmd != nil, m.menuOpen())
	}
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if v := ansi.Strip(sized.(Model).View()); !strings.Contains(v, "¿Qué quieres saber?") {
		t.Fatalf("back on the menu, the question is not drawn:\n%s", v)
	}

	// Picking again works: another view, its search, its rows.
	m = press(m, "2", "enter")
	if m.current != viewSigns || m.menuOpen() || m.states[viewSigns] != loading {
		t.Fatalf("picking after going back: view=%d menu=%v state=%v", m.current, m.menuOpen(), m.states[viewSigns])
	}

	// The footer names the way back where there is one.
	nm, _ := loaded(m, viewSigns).Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	if f := nm.(Model).footer(); !strings.Contains(f, "esc atrás") || !strings.Contains(f, "q salir") {
		t.Fatalf("footer on the rows = %q, want esc atrás and q salir", f)
	}
}

// The frame must close at every terminal width: the bug this pins is the
// border breaking open when the footer or the banner was wider than the
// window. Every drawn line has exactly the width the terminal reported.
func TestFrameIsExactlyTerminalWidth(t *testing.T) {
	for _, w := range []int{40, 60, 76, 79, 80, 96, 100, 140} {
		m := frameTable(New(nil, nil))
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

	// The animating picture obeys the same discipline: every frame of the
	// opening animation lands exactly on the terminal width, banner rows
	// included, whether the picture fits or the terminal is under its 80
	// columns and nothing is drawn at all.
	withTrueColor(t)
	maxFrames := len(artBanners[len(artBanners)-1].lines)
	for _, w := range []int{40, 76, 79, 80, 96, 100, 140, 210} {
		for _, h := range []int{24, 32, 45} {
			for f := 0; f < maxFrames; f++ {
				mm := frameTable(New(nil, nil))
				mm.frame = f
				nm, _ := mm.Update(tea.WindowSizeMsg{Width: w, Height: h})
				mm = nm.(Model)
				for i, line := range splitLines(mm.View()) {
					if !strings.HasPrefix(line, "╭") && !strings.HasPrefix(line, "│") && !strings.HasPrefix(line, "╰") {
						continue
					}
					if got := ansi.StringWidth(line); got != w {
						t.Fatalf("%dx%d frame %d line %d has %d cells: %q", w, h, f, i, got, line)
					}
				}
			}
		}
	}
}

// The animated frames differ from each other -- the reveal really advances --
// and the animation stops: after the last tick the model reports it over and a
// further tick neither advances nor schedules anything.
func TestBannerAnimationMovesAndStops(t *testing.T) {
	withTrueColor(t)
	m := newModel()
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)
	if !m.bannerAnimating() {
		t.Fatal("a fresh model on a wide terminal is not animating")
	}

	bannerFrames := m.bannerFrames()
	frames := make([]string, bannerFrames)
	for f := 0; f < bannerFrames; f++ {
		mm := m
		mm.frame = f
		frames[f] = mm.View()
	}
	for f := 0; f+1 < bannerFrames; f++ {
		if frames[f] == frames[f+1] {
			t.Fatalf("animation frame %d equals frame %d: the cars are not moving", f, f+1)
		}
	}

	// bannerFrames-1 ticks reveal the picture a row at a time; the last one
	// lands it whole and stops the ticking.
	for i := 0; i < bannerFrames-1; i++ {
		nm, cmd := m.Update(bannerTickMsg{})
		m = nm.(Model)
		if i < bannerFrames-2 && cmd == nil {
			t.Fatalf("tick %d did not schedule the next frame", i)
		}
	}
	if m.frame != bannerFrames-1 || m.bannerAnimating() {
		t.Fatalf("after the ticks the animation is still running: frame=%d", m.frame)
	}
	nm, cmd := m.Update(bannerTickMsg{})
	if cmd != nil || nm.(Model).frame != bannerFrames-1 {
		t.Fatal("a tick after the last frame advanced or scheduled again")
	}
}

// A terminal too small for the picture gets no banner of any kind, no
// animation, and the table keeps every row the frame can hold.
func TestTooSmallTerminalHasNoBanner(t *testing.T) {
	// Colour terminal, but the picture would not fit vertically: nothing is
	// drawn at the top and the table gets the full frame.
	withTrueColor(t)
	small := artBannerCrop[0] // what a data screen draws
	short := dataChromeRows + len(small.lines) - 1
	m := chosenView(newModel(), viewIncidents)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: short})
	m = nm.(Model)
	if m.pictureShowing() || m.artShowing() {
		t.Fatal("a colour terminal too short for the picture must drop the banner entirely")
	}
	if got := m.visibleRows(); got != short-8 {
		t.Fatalf("visible rows = %d, want the full %d", got, short-8)
	}
	if v := m.View(); strings.Contains(v, "▀") || strings.Contains(v, "____") {
		t.Fatal("a small terminal drew a banner:\n" + v)
	}
	nm, cmd := m.Update(bannerTickMsg{})
	if cmd != nil || nm.(Model).frame != 0 || nm.(Model).bannerAnimating() {
		t.Fatal("a terminal with no picture must not animate")
	}

	// The opening screen has more room to give, but not unlimited: one row
	// short of the smallest picture plus its furniture, it draws none.
	nm, _ = newModel().Update(tea.WindowSizeMsg{Width: 100, Height: menuChromeRows + len(artBanners[0].lines) - 1})
	if menu := nm.(Model); menu.pictureShowing() || strings.Contains(menu.View(), "▀") {
		t.Fatal("an opening screen too short for the smallest picture drew one")
	}

	// And the classic small case: too narrow for the name at all, whatever
	// the profile could paint.
	nm, _ = m.Update(tea.WindowSizeMsg{Width: 79, Height: 30})
	m = nm.(Model)
	if m.banner() {
		t.Fatal("a 79-column terminal must drop the banner")
	}
	if got := m.visibleRows(); got != 30-8 {
		t.Fatalf("narrow terminal visible rows = %d, want %d", got, 30-8)
	}
}

// The art only ever paints colours the source PNG itself carries. Averaging a
// block of pixels invents in-between colours -- mud blending a car into the
// road -- so the generator reduces the source to a palette of its own
// colours and every half-cell takes one of them, verbatim.
func TestBannerArtUsesSourcePalette(t *testing.T) {
	f, err := os.Open("../../assets/banner/banner.png")
	if err != nil {
		t.Fatalf("open the source picture: %v", err)
	}
	defer func() { _ = f.Close() }()
	src, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode the source picture: %v", err)
	}
	source := map[[3]uint8]bool{}
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := src.At(x, y).RGBA()
			source[[3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8)}] = true
		}
	}
	for _, a := range everyBannerArt() {
		for _, c := range drawnColours(a) {
			if !source[c] {
				t.Fatalf("the %d-column picture paints %v, a colour the source never had", a.cols, c)
			}
		}
	}
}

// ansiColorRe matches the 24-bit SGR parameters the generator emits, both the
// foreground form 38;2;r;g;b and the background form 48;2;r;g;b.
var ansiColorRe = regexp.MustCompile(`(?:38|48);2;(\d+);(\d+);(\d+)`)

// everyBannerArt is every generated picture: the widths of the whole one
// and the data screens' cut-out. They share one palette.
func everyBannerArt() []bannerArt {
	return append(append([]bannerArt{}, artBanners...), artBannerCrop...)
}

// drawnColours lists every 24-bit colour a picture paints, repeats included.
func drawnColours(a bannerArt) [][3]uint8 {
	var out [][3]uint8
	for _, line := range a.lines {
		for _, m := range ansiColorRe.FindAllStringSubmatch(line, -1) {
			var c [3]uint8
			for i := 0; i < 3; i++ {
				n, _ := strconv.Atoi(m[1+i])
				c[i] = uint8(n)
			}
			out = append(out, c)
		}
	}
	return out
}

// The source is an antialiased illustration with tens of thousands of
// near-identical colours; painting every half-cell with its own slightly
// different blue is what reads as blur. Every width shares one small palette
// (the generator's -colors 48), so flat areas stay flat and edges stay hard.
func TestBannerArtIsQuantized(t *testing.T) {
	drawn := map[[3]uint8]bool{}
	for _, a := range everyBannerArt() {
		for _, c := range drawnColours(a) {
			drawn[c] = true
		}
	}
	if len(drawn) > 48 {
		t.Fatalf("the pictures paint %d distinct colours, more than the 48 of the palette", len(drawn))
	}
	if len(drawn) < 16 {
		t.Fatalf("the pictures paint only %d colours: the palette collapsed", len(drawn))
	}
}

// Every generated picture is exactly what the frame will draw: every row as
// wide as its declared width, every row painted, the widths ascending so the
// largest that fits is the last one that fits. A row one cell over would
// break the frame open; an unpainted row would make two frames of the reveal
// identical and the animation would look stuck.
func TestBannerPictureGeometry(t *testing.T) {
	withTrueColor(t)
	if len(artBanners) < 2 {
		t.Fatalf("%d picture sizes: the banner cannot adapt to the terminal", len(artBanners))
	}
	for i := 1; i < len(artBanners); i++ {
		a, prev := artBanners[i], artBanners[i-1]
		if a.cols <= prev.cols || len(a.lines) < len(prev.lines) {
			t.Fatalf("picture %d (%dx%d) is not larger than the one before it", i, a.cols, len(a.lines))
		}
	}
	if len(artBannerCrop) != 1 {
		t.Fatalf("%d cut-outs: the data screens draw exactly one", len(artBannerCrop))
	}
	for _, a := range everyBannerArt() {
		if len(a.lines) < 2 {
			t.Fatalf("the %d-column picture has %d rows: there is nothing to reveal", a.cols, len(a.lines))
		}
		for r, line := range a.lines {
			if got := ansi.StringWidth(line); got != a.cols {
				t.Fatalf("the %d-column picture's row %d is %d cells", a.cols, r, got)
			}
			if !ansiColorRe.MatchString(line) {
				t.Fatalf("the %d-column picture's row %d paints nothing", a.cols, r)
			}
			if !strings.HasSuffix(line, "\x1b[0m") {
				t.Fatalf("the %d-column picture's row %d does not reset: its colour bleeds into the frame", a.cols, r)
			}
		}
	}
	small := artBannerCrop[0] // the data screens' picture
	if small.cols > 80-3 || dataChromeRows+len(small.lines) > 24 {
		t.Fatalf("the data screens' picture (%dx%d) does not fit an 80x24 data screen", small.cols, len(small.lines))
	}

	// The reveal adds one row per frame, keeps the block's height constant so
	// the menu never jumps, and ends with the picture whole.
	m := newModel()
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)
	art, _, _ := m.bannerPlan()
	rows := m.bannerRows()
	for f := 0; f < m.bannerFrames(); f++ {
		mm := m
		mm.frame = f
		want := min(f+1, rows)
		if got := mm.bannerRevealRows(); got != want {
			t.Fatalf("frame %d reveals %d rows, want %d", f, got, want)
		}
		scene := mm.sceneLines()
		if len(scene) != rows {
			t.Fatalf("frame %d draws %d rows: the block must keep its height", f, len(scene))
		}
		for i, line := range scene {
			if i >= want {
				if line != "" {
					t.Fatalf("frame %d draws row %d before the reveal reaches it", f, i)
				}
				continue
			}
			// Centred: the left pad plus the picture's own width.
			if got, want := ansi.StringWidth(line), (100-3-art.cols)/2+art.cols; got != want {
				t.Fatalf("frame %d row %d is %d cells, want %d", f, i, got, want)
			}
		}
	}

	// The canonical 80x24 terminal still gets the picture, on the opening
	// screen and on a data screen, and the tagline sits under it: those
	// words are the only place the banner promises anything.
	for _, screen := range []Model{newModel(), chosenView(newModel(), viewIncidents)} {
		nm, _ = screen.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		s := nm.(Model)
		if !s.pictureShowing() {
			t.Fatalf("80x24 (menu=%v) must show the picture", s.menuOpen())
		}
		s.frame = s.bannerFrames() - 1
		if v := s.View(); !strings.Contains(v, bannerTagline) {
			t.Fatalf("the picture is up but the tagline is missing:\n%s", v)
		}
	}
}

// The opening screen has nothing else to show, so it draws the largest
// picture the terminal holds -- the name above it whenever both fit -- and
// spends any rows left over as air, so the frame always meets the bottom
// edge and never runs past it. A data screen keeps the smallest picture: there
// the table is the point.
func TestOpeningScreenDrawsTheLargestPictureThatFits(t *testing.T) {
	withTrueColor(t)
	for _, w := range []int{80, 100, 120, 150, 207} {
		for h := 20; h <= 60; h++ {
			nm, _ := newModel().Update(tea.WindowSizeMsg{Width: w, Height: h})
			m := nm.(Model)
			if got := len(splitLines(m.View())); got != h {
				t.Fatalf("%dx%d: the opening screen is %d rows, want the terminal's %d", w, h, got, h)
			}
			if !m.pictureShowing() {
				continue
			}
			art, letters, _ := m.bannerPlan()
			room := h - menuChromeRows
			if letters {
				room -= artRows
			}
			for _, bigger := range artBanners {
				if bigger.cols > art.cols && bigger.cols <= w-3 && len(bigger.lines) <= room {
					t.Fatalf("%dx%d draws the %d-column picture, but the %d-column one fits", w, h, art.cols, bigger.cols)
				}
			}
		}
	}

	// The reporter's terminal, 207x32: the name and a picture nearly twice
	// the eight rows the banner used to have.
	nm, _ := newModel().Update(tea.WindowSizeMsg{Width: 207, Height: 32})
	m := nm.(Model)
	if !m.lettersShowing() || m.bannerRows() < 15 {
		t.Fatalf("207x32: letters=%v picture rows=%d, want the name and at least 15 rows", m.lettersShowing(), m.bannerRows())
	}

	// Once a view is chosen the table needs the rows: a small picture, and
	// not the whole one shrunk to a smudge but the cut-out of the road and
	// its cars, drawn at the opening's sharpness.
	nm, _ = chosenView(newModel(), viewIncidents).Update(tea.WindowSizeMsg{Width: 207, Height: 60})
	data := nm.(Model)
	data.frame = 99
	if art, _, _ := data.bannerPlan(); art.cols != artBannerCrop[0].cols || art.lines[0] != artBannerCrop[0].lines[0] {
		t.Fatalf("a data screen draws the %d-column picture, want the %d-column cut-out", art.cols, artBannerCrop[0].cols)
	}
	if v := data.View(); !strings.Contains(v, artBannerCrop[0].lines[len(artBannerCrop[0].lines)-1]) {
		t.Fatal("the data screen does not draw the cut-out")
	}

	// A big terminal does not get a bigger picture than a header: 88x18 at
	// most, and the menu sits right under it -- the question is the line
	// after the tagline and its blank, the spare rows go below everything.
	nm, _ = newModel().Update(tea.WindowSizeMsg{Width: 207, Height: 60})
	m = nm.(Model)
	if art, _, _ := m.bannerPlan(); art.cols > 88 || len(art.lines) > 18 {
		t.Fatalf("207x60 draws a %dx%d picture, want at most 88x18", art.cols, len(art.lines))
	}
	rows := splitLines(ansi.Strip(m.View()))
	tagline, question := -1, -1
	for i, r := range rows {
		if strings.Contains(r, bannerTagline) {
			tagline = i
		}
		if strings.Contains(r, "¿Qué quieres saber?") {
			question = i
		}
	}
	if tagline < 0 || question != tagline+2 {
		t.Fatalf("207x60: tagline on row %d, question on row %d; want the question two rows under", tagline, question)
	}
}

// Each answer on the opening menu says what is behind it, and the
// descriptions start in one column.
func TestMenuDescribesEachView(t *testing.T) {
	withTrueColor(t)
	nm, _ := newModel().Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	v := ansi.Strip(nm.(Model).View())
	col := -1
	for i, name := range viewNames {
		line := ""
		for _, r := range splitLines(v) {
			if strings.Contains(r, fmt.Sprintf("%d.%s", i+1, name)) {
				line = r
			}
		}
		at := strings.Index(line, viewBlurbs[i])
		if at < 0 {
			t.Fatalf("the menu line for %s has no description:\n%s", name, v)
		}
		if col >= 0 && at != col {
			t.Fatalf("the description of %s starts at byte %d, the others at %d", name, at, col)
		}
		col = at
	}
}

// The opening screen asks a question; it does not claim to be loading
// anything, because nothing is: no request goes out before a view is chosen.
func TestOpeningScreenDoesNotClaimToLoad(t *testing.T) {
	withTrueColor(t)
	nm, _ := newModel().Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if v := ansi.Strip(nm.(Model).View()); strings.Contains(v, viewLoading[viewIncidents]) {
		t.Fatalf("the opening screen says it is loading:\n%s", v)
	}
}

// The opening flow: after the banner, the app asks ¿Qué quieres saber? with
// the four views; choosing one opens the province search, and typing a
// province and pressing enter shows the data. Enter with no province shows
// everything, because "todas las provincias" is also an answer.
func TestOpeningMenuFlow(t *testing.T) {
	withTrueColor(t)
	m := newModel()
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)
	if !m.menuOpen() {
		t.Fatal("the menu is not open before the banner finishes")
	}
	v := m.View()
	for _, want := range []string{"¿Qué quieres saber?", "1.Incidencias", "2.Paneles", "3.Radares", "4.Tramos"} {
		if !strings.Contains(ansi.Strip(v), want) {
			t.Fatalf("the menu is missing %q:\n%s", want, v)
		}
	}

	// Choosing a view opens the province search instead of showing raw rows.
	m = press(m, "2")
	if m.current != viewSigns {
		t.Fatalf("chose 2, view is %d", m.current)
	}
	if !m.typing {
		t.Fatal("choosing a view should open the province search")
	}
	if v := ansi.Strip(m.View()); strings.Contains(v, "CARRETERA") {
		t.Fatalf("rows shown before a province was entered:\n%s", v)
	}
	// yet the menu is closed once a view was chosen.
	if m.menuOpen() {
		t.Fatal("the menu stayed open after choosing")
	}

	// Typing a province and pressing enter shows the data.
	m = press(m, "a", "l", "i", "enter")
	if m.typing || m.province != "ali" {
		t.Fatalf("the province did not apply: searching=%v province=%q", m.typing, m.province)
	}
	if m.states[viewSigns] != loading {
		t.Fatalf("the view did not reload under the province: state %v", m.states[viewSigns])
	}
	// The choice is consultable later: the menu does not come back after the
	// first data screen.
	m = press(m, "3")
	if m.menuOpen() || m.typing {
		t.Fatalf("keys resumed the menu: menu=%v typing=%v", m.menuOpen(), m.typing)
	}
	if m.current != viewRadars {
		t.Fatalf("3 switched to view %d, want radares", m.current)
	}
}

// Like the reference UI: the name sits above the picture on a colour
// terminal too, spelled by the block letters, not baked into the pixels. A
// terminal too short for both keeps the picture alone: half a name is no
// name.
func TestBannerLettersAbovePicture(t *testing.T) {
	withTrueColor(t)
	m := newModel()
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 33})
	m = nm.(Model)
	if !m.pictureShowing() || !m.lettersShowing() {
		t.Fatalf("100x33: picture=%v letters=%v, want both", m.pictureShowing(), m.lettersShowing())
	}
	v := ansi.Strip(m.View())
	letters := strings.Index(v, "|_   _|")
	tagline := strings.Index(v, bannerTagline)
	if letters < 0 {
		t.Fatalf("the name is missing above the picture:\n%s", v)
	}
	if tagline < 0 || tagline < letters {
		t.Fatalf("the name is not above the picture: letters@%d, tagline@%d", letters, tagline)
	}

	// A terminal tall enough for the picture but not for it plus the name
	// shows the picture alone rather than squeezing the pair.
	nm, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 25})
	m = nm.(Model)
	if !m.pictureShowing() || m.lettersShowing() {
		t.Fatalf("100x25: picture=%v letters=%v, want picture only", m.pictureShowing(), m.lettersShowing())
	}

	// Everything else is unchanged: the province field still rides inside
	// the frame on both screens.
	nm, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 33})
	m = nm.(Model)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Provincia") {
		t.Fatalf("the province field left the frame:\n%s", v)
	}
}

// The menu is modal for view keys, but never a trap: quit keys must work
// on the menu screen too, or the only way out is closing the terminal.
func TestMenuStillQuits(t *testing.T) {
	withTrueColor(t)
	m := newModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil || !m.menuOpen() {
		t.Fatalf("q on the menu: cmd=%v menuOpen=%v", cmd != nil, m.menuOpen())
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c on the menu does not quit")
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc on the menu does not quit")
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

// The block letters spell T-R-A-F-I-C-O - C-L-I, glyph by glyph, in FIGlet's
// standard font. A C in the fourth slot once shipped as "TRACICO-CLI": every
// glyph is pinned here so a misspelt name cannot pass for block letters.
func TestBannerSpellsTheName(t *testing.T) {
	glyphs := map[rune][5]string{
		'T': {" _____ ", "|_   _|", "  | |  ", "  | |  ", "  |_|  "},
		'R': {" ____  ", "|  _ \\ ", "| |_) |", "|  _ < ", "|_| \\_\\"},
		'A': {"    _    ", "   / \\   ", "  / _ \\  ", " / ___ \\ ", "/_/   \\_\\"},
		'F': {" _____ ", "|  ___|", "| |_   ", "|  _|  ", "|_|    "},
		'I': {" ___ ", "|_ _|", " | | ", " | | ", "|___|"},
		'C': {"  ____ ", " / ___|", "| |    ", "| |___ ", " \\____|"},
		'O': {"  ___  ", " / _ \\ ", "| | | |", "| |_| |", " \\___/ "},
		'-': {"       ", " _____ ", "|_____|", "       ", "       "},
		'L': {" _     ", "| |    ", "| |    ", "| |___ ", "|_____|"},
	}
	// The glyphs sit side by side, no kerning.
	var want [5]string
	for _, r := range "TRAFICO-CLI" {
		for row := range want {
			want[row] += glyphs[r][row]
		}
	}
	got := splitLines(art)
	for row := range want {
		if strings.TrimRight(got[row], " ") != strings.TrimRight(want[row], " ") {
			t.Fatalf("row %d of the name:\n got %q\nwant %q", row, got[row], want[row])
		}
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
	m := loaded(chosenView(newModel(), viewIncidents), viewIncidents)
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
