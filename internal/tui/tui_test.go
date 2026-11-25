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

// S8 / TUI-03: `/` opens the search, enter applies the province and esc
// clears the filter once applied. The opening menu is modal: the tests pick
// a view first, exactly as a user must.
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
	if m.province != "" || m.typing {
		t.Fatalf("esc did not clear the filter: province=%q searching=%v", m.province, m.typing)
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
// TUI's own (S5); esc clears it from the criteria too.
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
// does not refetch. With no filter, esc quits instead.
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
	for _, w := range []int{40, 76, 79, 80, 96, 100, 140} {
		for f := 0; f < bannerFrames; f++ {
			mm := frameTable(New(nil, nil))
			mm.frame = f
			nm, _ := mm.Update(tea.WindowSizeMsg{Width: w, Height: 24})
			mm = nm.(Model)
			for i, line := range splitLines(mm.View()) {
				if !strings.HasPrefix(line, "╭") && !strings.HasPrefix(line, "│") && !strings.HasPrefix(line, "╰") {
					continue
				}
				if got := ansi.StringWidth(line); got != w {
					t.Fatalf("width %d frame %d line %d has %d cells: %q", w, f, i, got, line)
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
	m := newModel()
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 21})
	m = nm.(Model)
	if m.pictureShowing() || m.artShowing() {
		t.Fatal("a colour terminal too short for the picture must drop the banner entirely")
	}
	if got := m.visibleRows(); got != 21-8 {
		t.Fatalf("visible rows = %d, want the full %d", got, 21-8)
	}
	if v := m.View(); strings.Contains(v, "▀") || strings.Contains(v, "____") {
		t.Fatal("a small terminal drew a banner:\n" + v)
	}
	nm, cmd := m.Update(bannerTickMsg{})
	if cmd != nil || nm.(Model).frame != 0 || nm.(Model).bannerAnimating() {
		t.Fatal("a terminal with no picture must not animate")
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

// The generated picture is exactly what the frame will draw: every row as
// wide as the tool was asked for, every row carrying something, and narrow
// enough for the smallest terminal the gate lets through. A row one cell over
// would break the frame open; a blank row would make two frames of the reveal
// identical and the animation would look stuck.
// The source is flat-colour pixel art: every colour the picture shows exists
// in the PNG. Averaging a 10x10-pixel block invents intermediate colours (mud
// blending a car into the road), so the generated art must only paint with
// colours the source itself carries, up to a small tolerance for the
// antialiased edges of the original image.
func TestBannerArtUsesSourcePalette(t *testing.T) {
	withTrueColor(t)
	f, err := os.Open("../../assets/banner/banner.png")
	if err != nil {
		t.Fatalf("open the source picture: %v", err)
	}
	defer func() { _ = f.Close() }()
	src, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode the source picture: %v", err)
	}

	// The palette the source carries: every distinct opaque colour, counted
	// so a stray antialiasing pixel can be told from a real flat fill.
	counts := map[[3]uint8]int{}
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := src.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			counts[[3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8)}]++
		}
	}
	palette := map[[3]uint8]bool{}
	for c, n := range counts {
		if n >= 16 { // a flat fill repeats; an edge pixel does not
			palette[c] = true
		}
	}
	if len(palette) < 4 {
		t.Fatalf("the source palette collapsed to %d colours: the fixture is wrong", len(palette))
	}

	// Every drawn colour in the art must be one the source carries (within
	// a small tolerance for rounding), not an invented average.
	near := func(a, b [3]uint8) bool {
		d := 0
		for i := range a {
			delta := int(a[i]) - int(b[i])
			if delta < 0 {
				delta = -delta
			}
			if delta > d {
				d = delta
			}
			if delta > 12 {
				return false
			}
		}
		return d <= 12
	}
	offenders := 0
	for _, line := range bannerArtLines {
		for _, m := range ansiColorRe.FindAllStringSubmatch(line, -1) {
			var c [3]uint8
			for i := 0; i < 3; i++ {
				n, _ := strconv.Atoi(m[1+i])
				c[i] = uint8(n)
			}
			decided := false
			if near(c, [3]uint8{40, 44, 52}) {
				decided = true
			} else {
				for p := range palette {
					if near(c, p) {
						decided = true
						break
					}
				}
			}
			if !decided {
				offenders++
			}
		}
	}
	if offenders > 0 {
		t.Fatalf("%d drawn colours are averages the source never had: regenerate with palette snapping", offenders)
	}
}

// ansiColorRe matches the 24-bit SGR parameters the generator emits, both the
// foreground form 38;2;r;g;b and the background form 48;2;r;g;b.
var ansiColorRe = regexp.MustCompile(`(?:38|48);2;(\d+);(\d+);(\d+)`)

func TestBannerPictureGeometry(t *testing.T) {
	withTrueColor(t)
	if bannerArtRows < 2 {
		t.Fatalf("the picture has %d rows: there is nothing to reveal", bannerArtRows)
	}
	for i, line := range bannerArtLines {
		if got := ansi.StringWidth(line); got > bannerArtCols {
			t.Fatalf("row %d is %d cells, wider than the picture's %d", i, got, bannerArtCols)
		}
		if strings.TrimSpace(ansi.Strip(line)) == "" {
			t.Fatalf("row %d of the picture is blank", i)
		}
	}
	if bannerArtCols > 80-3 {
		t.Fatalf("the picture is %d cells wide and does not fit the 80-column frame", bannerArtCols)
	}

	// The reveal adds one row per frame, keeps the block's height constant so
	// the table never jumps, and ends with the picture whole.
	m := newModel()
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)
	for f := 0; f < bannerFrames; f++ {
		mm := m
		mm.frame = f
		want := f + 1
		if want > bannerArtRows {
			want = bannerArtRows
		}
		if got := mm.bannerRevealRows(); got != want {
			t.Fatalf("frame %d reveals %d rows, want %d", f, got, want)
		}
		if got := len(mm.sceneLines()); got != bannerArtRows {
			t.Fatalf("frame %d draws %d rows: the block must keep its height", f, got)
		}
		// The rows the reveal has reached are padded back out to the
		// picture's own width, so the block is square at its edges instead
		// of ragged where the generator trimmed trailing spaces.
		width := -1
		for i, line := range mm.sceneLines() {
			if i >= want {
				if line != "" {
					t.Fatalf("frame %d draws row %d before the reveal reaches it", f, i)
				}
				continue
			}
			got := ansi.StringWidth(line)
			if width < 0 {
				width = got
			}
			if got != width || got < bannerArtCols {
				t.Fatalf("frame %d row %d is %d cells, want the block's %d", f, i, got, width)
			}
		}
	}

	// The canonical 80x24 terminal still gets the picture, and the tagline
	// sits under it: those words are the only place the banner promises
	// anything, so they have to be on the screen with the art.
	nm, _ = New(nil, nil).Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	small := nm.(Model)
	if !small.pictureShowing() {
		t.Fatal("80x24 must show the picture")
	}
	small.frame = bannerFrames - 1
	if v := small.View(); !strings.Contains(v, bannerTagline) {
		t.Fatalf("the picture is up but the tagline is missing:\n%s", v)
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
