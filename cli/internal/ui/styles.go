package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/a-novel-kit/stack/cli/internal/detect"
)

// Palette.
//
// All semantic colours are the exact A-Novel values and so are fixed
// lipgloss.Color (not adaptive) — brand and status signalling must not drift
// per terminal theme. Only colMuted/colWarn stay adaptive (pure dimming, no
// brand meaning). Everything is foreground-only, the same readability
// discipline as the shared scripts/lib/style.sh: no backgrounds, no pure
// black/white.
//
// The two brand colours (purple/blue) are the identity; colGold is a third,
// non-brand accent used for structural chrome (section headings, counts) so
// the UI does not read as an endless purple/blue stripe. colCrit is reserved
// for the few things that demand the eye immediately (a failed build) — used
// sparingly so it keeps its alarm value.
var (
	colBrand  = lipgloss.Color("#DC24FF")                                 // A-Novel purple   — rgb(220,36,255)
	colAccent = lipgloss.Color("#00A9B2")                                 // A-Novel blue     — rgb(0,169,178)
	colGold   = lipgloss.Color("#C38500")                                 // structural accent — rgb(195,133,0)
	colOK     = lipgloss.Color("#00AF84")                                 // success          — rgb(0,175,132)
	colErr    = lipgloss.Color("#ED6200")                                 // error            — rgb(237,98,0)
	colCrit   = lipgloss.Color("#FF00AB")                                 // immediate attention — rgb(255,0,171)
	colMuted  = lipgloss.AdaptiveColor{Light: "#868E96", Dark: "#6C757D"} // dim detail
	colWarn   = lipgloss.AdaptiveColor{Light: "#E8590C", Dark: "#FFA94D"} // aborted / caution
)

var (
	styleBrand   = lipgloss.NewStyle().Foreground(colBrand).Bold(true)
	styleAccent  = lipgloss.NewStyle().Foreground(colAccent)
	styleGold    = lipgloss.NewStyle().Foreground(colGold).Bold(true)
	styleMuted   = lipgloss.NewStyle().Foreground(colMuted)
	styleOK      = lipgloss.NewStyle().Foreground(colOK).Bold(true)
	styleWarn    = lipgloss.NewStyle().Foreground(colWarn).Bold(true)
	styleErr     = lipgloss.NewStyle().Foreground(colErr).Bold(true)
	styleCrit    = lipgloss.NewStyle().Foreground(colCrit).Bold(true)
	styleNib     = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleSel     = lipgloss.NewStyle().Foreground(colBrand).Bold(true)
	styleHelp    = lipgloss.NewStyle().Foreground(colMuted)
	styleGroup   = lipgloss.NewStyle().Foreground(colGold).Bold(true)
	styleVersion = lipgloss.NewStyle().
			Foreground(colBrand).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colBrand).
			Padding(0, 1)
)

var wordmarkContour = []string{
	"▄",
	"█",
	"▜",
	"▝█",
	" ▝█",
	"   ▚ ",
	"   ▝▖",
}

var wordmark = []string{
	"▄    ",
	"█▙   ",
	"▜█▙  ",
	"▝██▌ ",
	" ▝█▛ ",
	"   ▚ ",
	"   ▝▖",
}

// renderQuillLine two-tones a single wordmark row: the first visible glyph in
// purple, everything after it in blue. Down the diagonal this paints a purple
// leading edge with a blue body — a shadow/relief effect using only the two
// trademark colours. Leading spaces (which carry the diagonal) stay uncoloured.
func renderQuillLine() []string {
	out := make([]string, len(wordmark))

	for i, line := range wordmark {
		lead := wordmarkContour[i]
		rest := line[len(lead):]
		out[i] = styleBrand.Render(lead) + styleNib.Render(rest)
	}

	return out
}

// Banner renders the branded header shown at the top of every screen. version
// is the resolved CLI version (see internal/version).
func Banner(version string) string {
	rows := renderQuillLine()
	mark := lipgloss.JoinVertical(lipgloss.Left, rows...)

	text := lipgloss.JoinVertical(
		lipgloss.Left,
		"",
		styleBrand.Render("A-NOVEL"),
		styleAccent.Render("the storyverse build tool"),
		"",
		styleVersion.Render("a-novel "+version),
	)

	return lipgloss.NewStyle().
		Padding(1, 2, 0, 2).
		Render(lipgloss.JoinHorizontal(lipgloss.Top, mark, "  ", text))
}

// kindLabel is the human heading for a target group.
func kindLabel(k detect.Kind) string {
	switch k {
	case detect.KindGo:
		return "Go modules"
	case detect.KindPnpm:
		return "pnpm scripts"
	case detect.KindPodman:
		return "Podman images"
	default:
		return string(k)
	}
}

// glyphs — Unicode for richness; mirrors style.sh's symbol choices so CLI and
// shell tooling read consistently.
const (
	glyphChecked   = "◉"
	glyphPartial   = "◐" // group: some-but-not-all members selected
	glyphUnchecked = "◯"
	glyphCursor    = "▸"
	glyphOK        = "✓"
	glyphFail      = "✗"
	glyphRun       = "•"
)
