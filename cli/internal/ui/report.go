package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/a-novel-kit/stack/cli/internal/build"
)

// RenderTextReport produces the authoritative, scrollback-safe build report.
//
// It is printed two ways:
//   - after the interactive TUI tears down, so the FULL failing logs survive
//     (the in-TUI report only shows a tail);
//   - as the entire output of non-interactive mode (`a-novel build -y`).
//
// Unlike the TUI views it never truncates failure output — this is the copy a
// user pastes into an issue or scrolls back through.
// elapsed is the real wall-clock run time tracked by the runner; it is shown
// as "took". Summary.CumulativeDuration is deliberately NOT used here — under
// parallelism it overstates how long the user actually waited.
func RenderTextReport(results []build.Result, aborted bool, elapsed time.Duration, verb Verb) string {
	s := build.Summarize(results)
	w := termWidth()

	var b strings.Builder
	b.WriteString("\n")

	headline := styleOK.Render("✓ " + verb.Upper + " PASSED")
	lead := "Every selected target passed."
	switch {
	case aborted:
		headline = styleWarn.Render("! " + verb.Upper + " ABORTED")
		lead = "Interrupted before every target finished — results below are partial."
	case s.Failed > 0:
		headline = styleCrit.Render("✗ " + verb.Upper + " FAILED")
		lead = "One or more targets failed. The summary is below; full output for each " +
			"failure follows so nothing is lost to scrollback."
	}
	b.WriteString(headline + "\n")
	b.WriteString(para(lead, w) + "\n\n")

	// Headline stats as pills: failed turns critical only when non-zero so a
	// clean run is calm, a broken one shouts.
	var failColor lipgloss.TerminalColor = colMuted
	if s.Failed > 0 {
		failColor = colCrit
	}
	b.WriteString(pillRow(
		pill("passed", strconv.Itoa(s.Passed), colOK),
		pill("failed", strconv.Itoa(s.Failed), failColor),
		pill("total", strconv.Itoa(s.Total), colGold),
		pill("took", elapsed.Round(1e7).String(), colAccent),
	))
	b.WriteString("\n\n")

	b.WriteString(section("results", colGold, w) + "\n\n")
	b.WriteString(resultsTable(results))
	b.WriteString("\n")

	if s.Failed > 0 {
		b.WriteString("\n" + section("failures", colCrit, w) + "\n\n")
		b.WriteString(para(
			"Full, untruncated output for each failed target — this is the copy to "+
				"read or paste into an issue.", w) + "\n")
		for _, r := range results {
			if r.Success {
				continue
			}
			// tail <= 0 → full log; this report is the source of truth.
			b.WriteString("\n" + failurePanel(r, 0, w) + "\n")
		}
	}

	return b.String()
}
