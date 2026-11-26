package tui

// The opening banner: a road into a city at night, cars heading for the
// horizon, drawn as half-blocks and revealed a row at a time.
//
// The picture is generated source (banner_gen.go, made from
// assets/banner/banner.png by tools/img2blocks.go), so the binary carries no
// image decoder and what lands in the repository is plain source anyone can
// read. One terminal cell is two pixels: the top drawn with ▀ in the upper
// pixel's colour over the lower pixel's colour.
//
// The picture comes in several widths. Detail is bounded by the cells it is
// given, so one fixed size is wrong everywhere: on 80 columns it must be
// small, on a wide monitor the same small picture is a smudge. The opening
// screen draws the largest width that fits, up to 88x18: past that the
// picture stops being a header and buries the menu under it.
//
// The data screens keep a small picture, because there the table is the
// point -- but not the whole picture shrunk: at 44 columns every cell would
// average twice the pixels the opening's cells do, and the cars turn to
// smudges. They get a cut-out of the road and its cars instead, drawn at
// the opening's sharpness and with the same palette.
//
// The name and the tagline are deliberately NOT in the picture: a baked
// pixel font turns to mush, and the terminal spells both better than an
// image can -- the name in the block letters above, the tagline as ordinary
// text in the app's own colour.
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

// bannerArt is the picture at one width: every line exactly cols cells.
//
// Regenerate every width with `go generate ./internal/tui` after changing
// the picture; the directive is the whole pipeline, so the flags that matter
// (the widths, the crop and the palette) are recorded next to what they
// produce. artBannerCrop is the data screens' cut-out: one width only.
//
//go:generate go run ../../tools/img2blocks.go -in ../../assets/banner/banner.png -out banner_gen.go -var artBanners -cols 44,63,73,88 -colors 48 -cover 0.5
//go:generate go run ../../tools/img2blocks.go -in ../../assets/banner/banner.png -out banner_crop_gen.go -var artBannerCrop -cols 44 -crop 177,60,749,294 -colors 48 -cover 0.5
type bannerArt struct {
	cols  int
	lines []string
}

// bannerTagline says what the tool does, in the app's own words. It is text
// rather than pixels on purpose: see the note at the top of this file.
const bannerTagline = "Consulta incidencias, radares y paneles de la DGT, en tu terminal"

// bannerTick is the gap between two frames of the reveal, one row each: the
// common opening picture lands in about a second, the largest in 1.3 s.
var bannerTick = 70 * time.Millisecond

const (
	// menuChromeRows is everything on the opening screen besides the name
	// and the picture: the two borders, the tagline and the blank under it,
	// the question and its four answers, a blank, the province field and
	// the blank under it. Whatever the picture leaves over goes under all
	// of it, so the menu sits right below the picture.
	menuChromeRows = 12
	// dataChromeRows is what a data screen spends besides the name and the
	// picture: the eight rows the frame and its furniture take, the tagline
	// and its blank, and five rows the table keeps for itself. Under this
	// the banner is dropped entire and the table gets every row back -- a
	// squeezed picture is not a picture -- but 80x24 still shows it.
	dataChromeRows = 8 + 2 + 5
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

// bannerPlan decides what the banner draws on this screen: which picture,
// and whether the name rides above it. ok is false when no picture fits.
//
// The opening screen prefers the name with a smaller picture to a bigger
// picture without it: the letters are how the tool introduces itself.
func (m Model) bannerPlan() (art bannerArt, letters, ok bool) {
	if !m.banner() || !bannerColorSupport() {
		return bannerArt{}, false, false
	}
	inner := m.width - 3
	if !m.menuOpen() {
		small := artBannerCrop[0]
		need := dataChromeRows + len(small.lines)
		if small.cols > inner || m.height < need {
			return bannerArt{}, false, false
		}
		return small, m.height >= need+artRows+1, true
	}
	if a, ok := largestArt(inner, m.height-menuChromeRows-artRows); ok {
		return a, true, true
	}
	if a, ok := largestArt(inner, m.height-menuChromeRows); ok {
		return a, false, true
	}
	return bannerArt{}, false, false
}

// largestArt is the widest picture no wider than cols and no taller than
// rows.
func largestArt(cols, rows int) (bannerArt, bool) {
	for i := len(artBanners) - 1; i >= 0; i-- {
		if a := artBanners[i]; a.cols <= cols && len(a.lines) <= rows {
			return a, true
		}
	}
	return bannerArt{}, false
}

// pictureShowing reports whether this terminal gets the picture: the room for
// it, the colour for it, and the frame gate the banner always had.
func (m Model) pictureShowing() bool {
	_, _, ok := m.bannerPlan()
	return ok
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
	_, letters, ok := m.bannerPlan()
	return ok && letters
}

// bannerRows is how many text rows the picture on this screen spends; zero
// when none is showing.
func (m Model) bannerRows() int {
	art, _, _ := m.bannerPlan()
	return len(art.lines)
}

// bannerFrames is how many steps the reveal takes: one new row per frame,
// the last frame showing the picture whole.
func (m Model) bannerFrames() int { return m.bannerRows() }

// bannerAnimating reports whether the reveal still has rows to draw: pure
// model state, readable from Update without a terminal.
func (m Model) bannerAnimating() bool {
	return m.pictureShowing() && m.frame < m.bannerFrames()-1
}

// bannerRevealRows is how many of the picture's rows this frame draws. The
// last frame draws all of them, and the reveal stops there: the settled
// picture is the static banner.
func (m Model) bannerRevealRows() int {
	rows := m.bannerRows()
	if m.frame >= rows {
		return rows
	}
	return m.frame + 1
}

// sceneLines draws the picture into the frame's inner width: the revealed
// rows centred in whatever room the terminal has beyond it, and the rows the
// reveal has not reached yet left blank so the block keeps its height from
// the first frame.
func (m Model) sceneLines() []string {
	art, _, ok := m.bannerPlan()
	if !ok {
		return nil
	}
	pad := strings.Repeat(" ", maxInt(0, (m.width-3-art.cols)/2))
	shown := m.bannerRevealRows()
	out := make([]string, len(art.lines))
	for i := 0; i < shown; i++ {
		out[i] = pad + art.lines[i]
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
// the same number keys the views have always answered to. Each answer says
// what is behind it, dimmed so the names stay what the eye reads first; the
// names are padded to one column so the descriptions line up.
func (m Model) menuLines() []string {
	out := []string{styleWarm.Render("¿Qué quieres saber?")}
	width := 0
	for _, name := range viewNames {
		width = max(width, len([]rune(name)))
	}
	for i, name := range viewNames {
		label := fmt.Sprintf("%d.%-*s", i+1, width, name)
		out = append(out, label+"   "+styleDim.Render(viewBlurbs[i]))
	}
	return out
}
