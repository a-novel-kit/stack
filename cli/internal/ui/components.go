package ui

import (
	"image/color"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"

	"github.com/a-novel-kit/stack/cli/internal/build"
	"github.com/a-novel-kit/stack/cli/internal/detect"
)

// termWidth is the width budget for printed output. A live view gets its width
// from Bubble Tea; printed output honors $COLUMNS, clamped to a readable range.
func termWidth() int {
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil {
		return min(max(n, 80), 140)
	}
	return 100
}

// Truncate cuts s to width terminal cells, ending a cut line with an ellipsis.
// It is width- and escape-aware, so it never splits a multi-byte rune.
func Truncate(s string, width int) string {
	return ansi.Truncate(s, max(width, 0), "…")
}

// relLabel renders a directory for display; the scan root reads as "(root)".
func relLabel(rel string) string {
	if rel == "." {
		return "(root)"
	}
	return rel
}

// rule is a full-width dim divider between major blocks.
func rule(width int) string {
	return Muted.Render(strings.Repeat("─", width))
}

// section is a titled divider: a bold upper-cased label in c, then a dim rule
// filling the rest of the width.
func section(title string, c color.Color, width int) string {
	head := lipgloss.NewStyle().Foreground(c).Bold(true).Render("┄ " + strings.ToUpper(title) + " ")
	return head + Muted.Render(strings.Repeat("─", max(width-lipgloss.Width(head), 0)))
}

// para word-wraps an explanatory sentence to width in the dim color.
func para(text string, width int) string {
	// Width() right-pads every wrapped line; trim it so prose leaves no ragged
	// blank gutter.
	lines := strings.Split(lipgloss.NewStyle().Foreground(colMuted).Width(width).Render(text), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

// groupTag is a fixed-width, colored, upper-cased group label, padded before
// coloring so a column of tags stays aligned.
func groupTag(group string) string {
	return lipgloss.NewStyle().Foreground(groupColor(group)).Bold(true).
		Render(pad(strings.ToUpper(group), 6))
}

// pad right-pads s with spaces to w bytes.
func pad(s string, w int) string {
	return s + strings.Repeat(" ", max(w-len(s), 0))
}

// pill renders a rounded "label value" chip bordered in c.
func pill(label, value string, c color.Color) string {
	inner := Muted.Render(label) + " " + lipgloss.NewStyle().Foreground(c).Bold(true).Render(value)
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c).Padding(0, 1).Render(inner)
}

// pillRow lays chips out horizontally with breathing room between them.
func pillRow(pills ...string) string {
	spaced := make([]string, 0, len(pills)*2)
	for i, p := range pills {
		if i > 0 {
			spaced = append(spaced, "  ")
		}
		spaced = append(spaced, p)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, spaced...)
}

// panel wraps body in a rounded muted border under a title bar colored c. The
// box's content is set to width-4 (border and padding each side), so lipgloss
// wraps long log lines inside the border.
func panel(title string, c color.Color, body string, width int) string {
	bar := lipgloss.NewStyle().Foreground(c).Bold(true).MaxWidth(width).Render("▌ " + title)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colMuted).
		Padding(0, 1).
		Width(max(width-4, 20)).
		Render(body)
	return bar + "\n" + box
}

// targetsTable is the dry-run overview: one row per target, its kind colored
// and its exact command wrapped within the width so it is always fully visible.
func targetsTable(targets []detect.Target, width int) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(Muted).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return Gold.Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		}).
		Width(width).
		Wrap(true).
		Headers("KIND", "TARGET", "COMMAND")
	for _, tg := range targets {
		kind := lipgloss.NewStyle().Foreground(groupColor(string(tg.Kind))).Bold(true).
			Render(strings.ToUpper(string(tg.Kind)))
		target := tg.Name + "\n" + Muted.Render("↳ "+relLabel(tg.RelDir))
		cmd := Muted.Render(tg.Cmd + " " + strings.Join(tg.Args, " "))
		if tg.Env != nil {
			cmd += "\n" + Accent.Render("↳ env "+tg.Env.ID)
		}
		t.Row(kind, target, cmd)
	}
	return t.Render()
}

// cleanOutput normalizes captured output for a fixed box. Tools such as podman
// redraw progress with carriage returns, which shred a static panel, so each
// line keeps only what follows its last '\r', as a terminal would show.
func cleanOutput(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if j := strings.LastIndexByte(line, '\r'); j >= 0 {
			line = line[j+1:]
		}
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// EnvConflictView is the warning shown when test environments from an aborted
// run are still up: an explanation, then the stale envs and their containers.
func EnvConflictView(verb string, conflicts []build.Conflict) string {
	w := termWidth()
	var b strings.Builder
	b.WriteString(section("environment conflict", colCrit, w) + "\n\n")
	b.WriteString(para("An existing "+verb+" environment was found — almost certainly the "+
		"leftover of a previously aborted run. Running on top of it would be "+
		"unreliable, so it must be cleared first.", w) + "\n\n")
	for _, c := range conflicts {
		b.WriteString("  " + Err.Render(glyphFail+" "+c.Env.ID) + "\n")
		for _, name := range c.Containers {
			b.WriteString("    " + Muted.Render("↳ "+name) + "\n")
		}
	}
	return b.String()
}
