package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"

	"github.com/a-novel-kit/stack/cli/internal/detect"
	"github.com/a-novel-kit/stack/cli/internal/version"
)

// Palette.
//
// Semantic colors are the exact A-Novel values and stay fixed across terminal
// themes, so brand and status signalling never drift; only the dimming colors
// adapt. Everything is foreground-only for readability on any background.
//
// Purple and blue are the identity. Gold is a structural accent for headings and
// counts, and colCrit is reserved for what demands the eye at once, such as a
// failed run.
var (
	colBrand  = lipgloss.Color("#DC24FF")                                                               // A-Novel purple
	colAccent = lipgloss.Color("#00A9B2")                                                               // A-Novel blue
	colGold   = lipgloss.Color("#C38500")                                                               // structural accent
	colOK     = lipgloss.Color("#00AF84")                                                               // success
	colErr    = lipgloss.Color("#ED6200")                                                               // error
	colCrit   = lipgloss.Color("#FF00AB")                                                               // immediate attention
	colMuted  = compat.AdaptiveColor{Light: lipgloss.Color("#868E96"), Dark: lipgloss.Color("#6C757D")} // dim detail
	colWarn   = compat.AdaptiveColor{Light: lipgloss.Color("#E8590C"), Dark: lipgloss.Color("#FFA94D")} // aborted / caution
)

// Styles shared by every screen of the CLI.
var (
	Brand  = lipgloss.NewStyle().Foreground(colBrand).Bold(true)
	Accent = lipgloss.NewStyle().Foreground(colAccent)
	Gold   = lipgloss.NewStyle().Foreground(colGold).Bold(true)
	Muted  = lipgloss.NewStyle().Foreground(colMuted)
	OK     = lipgloss.NewStyle().Foreground(colOK).Bold(true)
	Warn   = lipgloss.NewStyle().Foreground(colWarn).Bold(true)
	Err    = lipgloss.NewStyle().Foreground(colErr).Bold(true)
	Crit   = lipgloss.NewStyle().Foreground(colCrit).Bold(true)
)

var styleVersion = lipgloss.NewStyle().
	Foreground(colBrand).
	Border(lipgloss.RoundedBorder()).
	BorderForeground(colBrand).
	Padding(0, 1)

// wordmark is the quill logo. Each row's first glyph, its contour, renders in
// purple and the rest in blue, painting a shadow down the diagonal.
var wordmark = []struct{ contour, body string }{
	{"▄", "    "},
	{"█", "▙   "},
	{"▜", "█▙  "},
	{"▝█", "█▌ "},
	{" ▝█", "▛ "},
	{"   ▚ ", ""},
	{"   ▝▖", ""},
}

// Banner renders the branded header with the resolved CLI version.
func Banner() string {
	rows := make([]string, len(wordmark))
	for i, row := range wordmark {
		rows[i] = Brand.Render(row.contour) + Accent.Bold(true).Render(row.body)
	}
	text := lipgloss.JoinVertical(
		lipgloss.Left,
		"",
		Brand.Render("A-NOVEL"),
		Accent.Render("the storyverse build tool"),
		"",
		styleVersion.Render("a-novel "+version.String()),
	)
	return lipgloss.NewStyle().
		Padding(1, 2, 0, 2).
		Render(lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.JoinVertical(lipgloss.Left, rows...), "  ", text))
}

// groupColor gives each build kind its own color, so a tag alone tells
// go/pnpm/podman apart.
func groupColor(group string) color.Color {
	switch detect.Kind(group) {
	case detect.KindGo:
		return colAccent
	case detect.KindPnpm:
		return colGold
	case detect.KindPodman:
		return colBrand
	default:
		return colMuted
	}
}

// kindLabel is the heading of a target group in the picker.
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

// Status glyphs.
const (
	glyphChecked   = "◉"
	glyphPartial   = "◐" // group: some-but-not-all members selected
	glyphUnchecked = "◯"
	glyphCursor    = "▸"
	glyphOK        = "✓"
	glyphFail      = "✗"
)
