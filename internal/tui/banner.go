package tui

// The opening banner: a road at the edge of a city at night, drawn as
// half-blocks and revealed a row at a time while the first view loads.
//
// The picture is a generated constant (banner_gen.go, made from
// assets/banner/banner.png by tools/img2blocks.go), so the binary carries no
// image decoder and what lands in the repository is plain source anyone can
// read. One terminal cell is two pixels: the top drawn with ▀ in the upper
// pixel's colour over the lower pixel's colour.
//
// The name and the tagline are deliberately NOT in the picture. At 76 columns
// a baked pixel font turns to mush -- the subtitle of the reference image was
// the first thing to go -- and the terminal spells both better than an image
// can: the name in the frame's title and in the block letters below, the
// tagline as ordinary text in the app's own words, in the app's own colour.
//
// The picture is 24-bit by construction, so the gate asks for a 24-bit
// terminal. One that paints only 256 colours is not left without a banner: it
// gets the block letters, which need no colour at all.

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// bannerArtLines is the generated picture split once at load: the constant is
// one string and the frame wants one line per row.
//
// Regenerate it with `go generate ./internal/tui` after changing the picture;
// the directive is the whole pipeline, so the flags that matter (the width
// and the dark threshold) are recorded next to what they produce.
//
//go:generate go run ../../tools/img2blocks.go -in ../../assets/banner/banner.png -out banner_gen.go -const artBanner -cols 76 -bg 40
var bannerArtLines = strings.Split(artBanner, "\n")

// bannerArtRows is how many text rows the picture spends. The width comes from
// the generated file: every row has its trailing spaces trimmed, so the
// picture's width cannot be read off the art itself.
var (
	bannerArtRows = len(bannerArtLines)
	bannerArtCols = artBannerCols
)

// bannerTagline says what the tool does, in the app's own words. It is text
// rather than pixels on purpose: see the note at the top of this file.
const bannerTagline = "Consulta incidencias, radares y paneles de la DGT, en tu terminal"

// Animation pacing and picture size.
var (
	// bannerFrames is how many steps the reveal takes: one new row per frame,
	// the last frame showing the picture whole. Deriving it from the picture
	// keeps every frame visibly different from the one before.
	bannerFrames = bannerArtRows
	// bannerTick is the gap between two frames: the whole reveal is roughly a
	// second, the same feeling as the startup animation the reference had.
	bannerTick = 110 * time.Millisecond
	// bannerBlockRows is what the picture costs the table: its own rows, the
	// tagline under them and the blank line that separates the block from the
	// province field. Reserved from the first frame, so nothing jumps when
	// the reveal finishes.
	bannerBlockRows = bannerArtRows + 2
	// bannerMinHeight is how tall a colour terminal must be for the picture:
	// the eight rows the frame and its furniture take, the block above, and
	// five rows the table keeps for itself. Under this the banner is dropped
	// entire and the table gets every row back -- a squeezed picture is not a
	// picture -- but a canonical 80x24 terminal still shows it.
	bannerMinHeight = 8 + bannerBlockRows + 5
	// bannerNameExtraHeight is what the name above the picture adds: the
	// letters and a blank line. Under the pair's gate the terminal keeps
	// the picture alone -- half a name is no name.
	bannerNameExtraHeight = artRows + 1
	// bannerMinHeightWithName is the gate for name + picture together.
	bannerMinHeightWithName = bannerMinHeight + bannerNameExtraHeight
)

// bannerTickMsg advances the reveal one row.
type bannerTickMsg struct{}

// tickBanner schedules the next frame. tea.Tick drives it, so the render loop
// never blocks and no goroutine of our own is needed.
func tickBanner() tea.Cmd {
	return tea.Tick(bannerTick, func(time.Time) tea.Msg { return bannerTickMsg{} })
}

// bannerColorProfile is the terminal's colour profile, as a variable so the
// tests can stand in for a terminal. In a real run it is lipgloss's probe,
// the same detection the TUI's own styles use.
var bannerColorProfile = lipgloss.ColorProfile

// bannerColorSupport reports whether the terminal can paint the picture: the
// art is 24-bit by construction, so anything coarser gets the block letters
// instead of a dithered approximation of a photograph.
func bannerColorSupport() bool { return bannerColorProfile() == termenv.TrueColor }

// pictureShowing reports whether this terminal gets the picture: the room for
// it, the colour for it, and the frame gate the banner always had.
func (m Model) pictureShowing() bool {
	return m.banner() && bannerColorSupport() && m.height >= bannerMinHeight
}

// artShowing reports whether the block-letter fallback carries the name: on a
// terminal that cannot paint 24-bit colour. One that is simply too short for
// the picture has no banner at all and its table keeps every row.
func (m Model) artShowing() bool {
	return m.banner() && !bannerColorSupport()
}

// lettersShowing reports whether the name rides above the picture: a colour
// terminal tall enough for the letters and the picture both, like the
// reference UI. Too short a terminal keeps the picture alone.
func (m Model) lettersShowing() bool {
	return m.pictureShowing() && m.height >= bannerMinHeightWithName
}

// bannerAnimating reports whether the reveal still has rows to draw: pure
// model state, readable from Update without a terminal.
func (m Model) bannerAnimating() bool {
	return m.pictureShowing() && m.frame < bannerFrames-1
}

// bannerRevealRows is how many of the picture's rows this frame draws. The
// last frame draws all of them, and the reveal stops there: the settled
// picture is the static banner.
func (m Model) bannerRevealRows() int {
	if !m.pictureShowing() {
		return 0
	}
	if m.frame >= bannerArtRows {
		return bannerArtRows
	}
	return m.frame + 1
}

// sceneLines draws the picture into the frame's inner width: every revealed
// row padded back out to the picture's own width, the whole block centred in
// whatever room the terminal has beyond it, and the rows the reveal has not
// reached yet left blank so the block keeps its height from the first frame.
func (m Model) sceneLines() []string {
	inner := m.width - 3
	pad := strings.Repeat(" ", maxInt(0, (inner-bannerArtCols)/2))
	shown := m.bannerRevealRows()
	out := make([]string, bannerArtRows)
	for i := 0; i < bannerArtRows; i++ {
		if i >= shown {
			continue
		}
		tail := strings.Repeat(" ", maxInt(0, bannerArtCols-cells(bannerArtLines[i])))
		out[i] = pad + bannerArtLines[i] + tail
	}
	return out
}

// taglineLine is the tagline centred under the picture, in the secondary
// colour: the picture is what the eye lands on, the words say what it is for.
func (m Model) taglineLine() string {
	inner := m.width - 3
	text := truncate(bannerTagline, inner)
	pad := strings.Repeat(" ", maxInt(0, (inner-cells(text))/2))
	return pad + styleDim.Render(text)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// menuLines draws the opening question and its four answers, in the shape
// the user asked for: one plain line per view, numbered to be picked with
// the same number keys the views have always answered to.
func (m Model) menuLines() []string {
	out := []string{styleWarm.Render("¿Qué quieres saber?")}
	for i, name := range viewNames {
		out = append(out, fmt.Sprintf("%d.%s", i+1, name))
	}
	return out
}

// ProbeView exists only for the tmp probe (deleted after the debug).
func ProbeView(w, h int) string {
	old := bannerColorProfile
	bannerColorProfile = func() termenv.Profile { return termenv.TrueColor }
	defer func() { bannerColorProfile = old }()
	m := New(nil, nil)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return nm.(Model).View()
}
