package ui

import (
	"image/color"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/a-novel-kit/stack/cli/internal/build"
	"github.com/a-novel-kit/stack/cli/internal/detect"
)

// Reusable lipgloss presentation primitives shared by the dry-run view and the
// build report: a width helper, per-kind color coding, stat "pills", titled
// bordered panels, and the two data tables. Keeping them here means the views
// describe what to show; this file owns how it looks.

// termWidth is the content width budget for static (non-TUI) output. The TUI
// gets its width from tea.WindowSizeMsg; piped/CI output has no terminal to
// ask, so honor $COLUMNS and otherwise fall back to a readable default,
// clamped so tables never get absurdly narrow or wide.
func termWidth() int {
	if c := os.Getenv("COLUMNS"); c != "" {
		if n, err := strconv.Atoi(c); err == nil {
			return clamp(n, 80, 140)
		}
	}
	return 100
}

// Verb parameterizes the screens shared by `build` and `test` so the same
// model/report/dry-run render the right wording for each.
type Verb struct {
	Base  string // "build" / "test"
	Ing   string // "building" / "testing"
	Upper string // "BUILD" / "TEST"
	Looks string // what discovery scans for — used in the "nothing found" error
}

var (
	VerbBuild = Verb{
		Base: "build", Ing: "building", Upper: "BUILD",
		Looks: "a go.mod (Go), a package.json with build* scripts (pnpm), or builds/*.Dockerfile (Podman)",
	}
	VerbTest = Verb{
		Base: "test", Ing: "testing", Upper: "TEST",
		Looks: "a go.mod (Go tests) or a package.json with test* scripts (pnpm)",
	}
	VerbRun = Verb{
		Base: "run", Ing: "running", Upper: "RUN",
		Looks: "a Go `package main` (cmd/…) or a package.json with run* scripts",
	}
)

// relLabel renders a target's directory for display: the scan root ("." from
// filepath.Rel) reads as "(root)" everywhere it is shown — selection list,
// dry-run table, and failure-panel titles — so a root failure is never the
// cryptic "[.]".
func relLabel(rel string) string {
	if rel == "." {
		return "(root)"
	}
	return rel
}

// gutter is the left margin (in spaces) every screen block sits under in the
// TUI. Centralized so indentBlock and the width math (cw = w - gutter) agree.
const gutter = 2

// indentBlock left-pads every non-empty line of a multi-line block by the
// gutter, so a flush-left table or panel sits under the same margin as the
// surrounding TUI text without distorting its internal alignment.
func indentBlock(s string) string {
	pad := strings.Repeat(" ", gutter)
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if ln != "" {
			lines[i] = pad + ln
		}
	}
	return strings.Join(lines, "\n")
}

func clamp(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	default:
		return v
	}
}

// rule is a full-width horizontal divider in the dim color — the quiet
// separator between major blocks.
func rule(width int) string {
	if width <= 0 {
		width = termWidth()
	}
	return styleMuted.Render(strings.Repeat("─", width))
}

// section is a titled divider: a colored, bold, upper-cased label followed by
// a dim rule that fills the rest of the width. It headlines a block of output
// so the eye can find "RESULTS" / "FAILURES" without counting blank lines.
func section(title string, c color.Color, width int) string {
	if width <= 0 {
		width = termWidth()
	}
	head := lipgloss.NewStyle().Foreground(c).Bold(true).
		Render("┄ " + strings.ToUpper(title) + " ")
	fill := width - lipgloss.Width(head)
	if fill < 0 {
		fill = 0
	}
	return head + styleMuted.Render(strings.Repeat("─", fill))
}

// para word-wraps an explanatory sentence to width in the dim color. Used
// sparingly to tell the reader what a block is before showing it.
func para(text string, width int) string {
	if width <= 0 {
		width = termWidth()
	}
	// .Width() word-wraps but right-pads every line to the full width; strip
	// that trailing whitespace so the prose leaves no ragged blank gutter.
	wrapped := lipgloss.NewStyle().Foreground(colMuted).Width(width).Render(text)
	lines := strings.Split(wrapped, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " ")
	}
	return strings.Join(lines, "\n")
}

// kindColor gives each build kind its own palette color so a glance at the
// KIND column (or a badge) is enough to tell go/pnpm/podman apart.
func kindColor(k detect.Kind) color.Color {
	switch k {
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

// kindTag is a fixed-width, color-coded, upper-cased kind label (GO / PNPM /
// PODMAN). Padded to the longest name so a column of these stays aligned in
// the live build list where there is no table to do it for us. The padding is
// applied before coloring so it counts toward visible width.
func kindTag(k detect.Kind) string {
	return lipgloss.NewStyle().Foreground(kindColor(k)).Bold(true).
		Render(pad(strings.ToUpper(string(k)), 6))
}

// targetName colors a build target's name by outcome: green on success,
// error-orange on failure. Deliberately NOT a kind color and NOT the default
// foreground — once a result exists, the name itself should carry the verdict
// (reinforcing the ✓/✗ glyph) without being mistaken for the kind palette.
// Before a result exists (dry run, selection, in-flight) callers use the plain
// default color instead.
func targetName(name string, ok bool) string {
	c := colOK
	if !ok {
		c = colErr
	}
	return lipgloss.NewStyle().Foreground(c).Render(name)
}

// pill renders a rounded chip — "label value" — bordered and colored by c.
// Used in a row to give scannable headline stats (counts, pass/fail).
func pill(label, value string, c color.Color) string {
	inner := lipgloss.NewStyle().Foreground(colMuted).Render(label) + " " +
		lipgloss.NewStyle().Foreground(c).Bold(true).Render(value)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c).
		Padding(0, 1).
		Render(inner)
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

// panel wraps body in a rounded muted border under a colored title bar — a
// titled card, bounded to width. lipgloss has no native bordered-title, so the
// title is a separate accent line above the box (a common, clean compromise).
//
// width is the OUTER width budget: the box's content is hard-set to
// width-4 (1 border + 1 padding each side) and lipgloss wraps long lines into
// it, so an over-long build-log line stays bounded by the border instead of
// stretching it off the terminal.
func panel(title string, c color.Color, body string, width int) string {
	if width <= 0 {
		width = termWidth()
	}
	inner := width - 4
	if inner < 20 {
		inner = 20
	}
	bar := lipgloss.NewStyle().Foreground(c).Bold(true).MaxWidth(width).
		Render("▌ " + title)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colMuted).
		Padding(0, 1).
		Width(inner).
		Render(body)
	return bar + "\n" + box
}

// baseTable builds a table with the house style: rounded muted border, a gold
// bold header, and one-space cell padding. Callers add headers/rows and may
// layer an extra StyleFunc concern on top via the returned table.
func baseTable() *table.Table {
	return table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(colMuted)).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return lipgloss.NewStyle().Foreground(colGold).Bold(true).Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})
}

// targetsTable is the dry-run overview: every detected target as one row, the
// KIND cell colored, the COMMAND cell wrapped within the width budget so the
// exact invocation is always fully visible (the whole point of a dry run).
func targetsTable(targets []detect.Target) string {
	t := baseTable().
		Width(termWidth()).
		Wrap(true).
		Headers("KIND", "TARGET", "COMMAND")

	for _, tg := range targets {
		loc := relLabel(tg.RelDir)
		kind := lipgloss.NewStyle().Foreground(kindColor(tg.Kind)).Bold(true).
			Render(strings.ToUpper(string(tg.Kind)))
		// TARGET cell is two lines: the name (default terminal color — a dry
		// run has no outcome, so no green/red and no kind color), then its
		// location dimmed beneath it.
		target := tg.Name + "\n" + styleMuted.Render("↳ "+loc)
		cmd := lipgloss.NewStyle().Foreground(colMuted).
			Render(tg.Cmd + " " + strings.Join(tg.Args, " "))
		// When the target needs a podman-compose env, show it under the
		// command — for `test` this is the decisive extra fact.
		if tg.Env != nil {
			cmd += "\n" + lipgloss.NewStyle().Foreground(colAccent).
				Render("↳ env "+tg.Env.ID)
		}
		t.Row(kind, target, cmd)
	}
	return t.Render()
}

// resultsTable is the per-target build outcome grid: a colored status badge,
// the target, its kind, and how long it took.
func resultsTable(results []build.Result) string {
	// KIND before TARGET: the kind is the strongest disambiguator, so it
	// reads first (matches the live build list ordering).
	t := baseTable().Headers("", "KIND", "TARGET", "TIME")

	// In a multi-service run (global mode), the TARGET column would otherwise
	// be ambiguous — `rest` appears for every service. Prepend the service
	// name so the row reads `service-json-keys/rest`. Single-service runs
	// keep the bare name (the header already carries the service).
	multi := distinctServices(results) > 1

	for _, r := range results {
		badge := styleOK.Render(glyphOK)
		if !r.Success {
			badge = styleErr.Render(glyphFail)
		}
		kind := lipgloss.NewStyle().Foreground(kindColor(r.Target.Kind)).Bold(true).
			Render(strings.ToUpper(string(r.Target.Kind)))
		label := r.Target.Name
		if multi && r.Target.Service != "" {
			label = r.Target.Service + "/" + r.Target.Name
		}
		name := targetName(label, r.Success)
		dur := lipgloss.NewStyle().Foreground(colMuted).
			Render(r.Duration.Round(1e7).String())
		t.Row(badge, kind, name, dur)
	}
	return t.Render()
}

// distinctServices counts the unique non-empty Service values across results
// — the signal that the run spans more than one repo (global mode).
func distinctServices(results []build.Result) int {
	seen := map[string]bool{}
	for _, r := range results {
		if r.Target.Service != "" {
			seen[r.Target.Service] = true
		}
	}
	return len(seen)
}

// cleanOutput normalizes captured subprocess output for display in a fixed
// box. Build tools (podman especially) emit CRLF and bare carriage returns for
// in-place progress; rendered verbatim into a static panel those overprint and
// shred the layout. Per line we keep only what survives the last '\r' (what a
// terminal would actually show), drop trailing whitespace, and trim leading/
// trailing blank lines so the panel starts and ends on real content.
func cleanOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if j := strings.LastIndexByte(ln, '\r'); j >= 0 {
			ln = ln[j+1:]
		}
		lines[i] = strings.TrimRight(ln, " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// failurePanel renders one failed build as a critical-bordered card, bounded
// to width: the command that ran, the process error, then its captured output
// (normalized + wrapped inside the border). tail > 0 keeps only the last N
// output lines (the in-TUI preview); tail <= 0 keeps the full log (the
// authoritative scrollback report).
func failurePanel(r build.Result, tail, width int) string {
	var b strings.Builder
	b.WriteString(styleMuted.Render("$ "+r.Target.Cmd+" "+strings.Join(r.Target.Args, " ")) + "\n")
	if r.ExitErr != nil {
		b.WriteString(styleErr.Render("error: "+r.ExitErr.Error()) + "\n")
	}
	b.WriteString("\n")

	out := cleanOutput(r.Output)
	switch {
	case out == "":
		b.WriteString(styleMuted.Render("(no output captured)"))
	case tail > 0:
		lines := strings.Split(out, "\n")
		if len(lines) > tail {
			b.WriteString(styleMuted.Render("… "+strconv.Itoa(len(lines)-tail)+" earlier lines hidden") + "\n")
			lines = lines[len(lines)-tail:]
		}
		b.WriteString(strings.Join(lines, "\n"))
	default:
		b.WriteString(out)
	}

	title := glyphFail + " " + r.Target.Name + "  [" + relLabel(r.Target.RelDir) + "]"
	return panel(title, colErr, b.String(), width)
}

// EnvConflictView renders the leftover-environment warning shown by the
// preflight: a critical titled section, a short explanation, and the stale
// envs with their containers nested beneath — styled to match the report
// rather than plain stderr text.
func EnvConflictView(verb Verb, conflicts []build.Conflict) string {
	w := termWidth()
	var b strings.Builder
	b.WriteString(section("environment conflict", colCrit, w) + "\n\n")
	b.WriteString(para(
		"An existing "+verb.Base+" environment was found — almost certainly the "+
			"leftover of a previously aborted run. Running on top of it would be "+
			"unreliable, so it must be cleared first.", w) + "\n\n")
	for _, c := range conflicts {
		b.WriteString("  " + styleErr.Render(glyphFail+" "+c.Env.ID) + "\n")
		for _, name := range c.Containers {
			b.WriteString("    " + styleMuted.Render("↳ "+name) + "\n")
		}
	}
	return b.String()
}

// EnvStep is a dim, indented progress line for the cleanup actions.
func EnvStep(s string) string { return styleMuted.Render("  " + s) }

// EnvNote is a one-line caution notice (non-interactive abort message).
func EnvNote(s string) string { return styleWarn.Render(s) }

// EnvPrompt is the styled inline clean/abort question (no trailing newline).
func EnvPrompt(s string) string { return styleGold.Render(s) }
