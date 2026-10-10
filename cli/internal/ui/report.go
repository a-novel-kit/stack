package ui

import (
	"cmp"
	"image/color"
	"strconv"
	"strings"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/jobs"
)

// Report renders a finished batch under title ("TEST", "SYNC"): a headline,
// outcome counts, coverage when the output carries it, then the full output of
// every job show selects, so nothing is lost to a truncated view.
func Report(title string, b Batch, show func(jobs.Result) bool) string {
	w := termWidth()
	var failed int
	counts := map[string]int{}
	var outcomes []string
	for _, r := range b.Results {
		if r.Err != nil {
			failed++
			continue
		}
		status := cmp.Or(r.Status, "passed")
		if counts[status] == 0 {
			outcomes = append(outcomes, status)
		}
		counts[status]++
	}

	headline, lead := OK.Render(glyphOK+" "+title+" PASSED"), "Every job succeeded."
	switch {
	case b.Aborted:
		headline, lead = Warn.Render("! "+title+" ABORTED"), "Interrupted before every job finished — results below are partial."
	case failed > 0:
		headline, lead = Crit.Render(glyphFail+" "+title+" FAILED"), "One or more jobs failed; their full output follows."
	}

	var s strings.Builder
	s.WriteString("\n" + headline + "\n" + para(lead, w) + "\n\n")

	var failColor color.Color = colMuted
	if failed > 0 {
		failColor = colCrit
	}
	if len(outcomes) == 0 {
		outcomes = []string{"passed"}
	}
	pills := make([]string, 0, len(outcomes)+3)
	for _, o := range outcomes {
		pills = append(pills, pill(o, strconv.Itoa(counts[o]), colOK))
	}
	pills = append(pills,
		pill("failed", strconv.Itoa(failed), failColor),
		pill("total", strconv.Itoa(len(b.Results)), colGold),
		pill("took", b.Elapsed.Round(10*time.Millisecond).String(), colAccent),
	)
	s.WriteString(pillRow(pills...) + "\n")

	if cv := CoverageView(b.Results, w); cv != "" {
		s.WriteString("\n" + cv)
	}

	var shown []jobs.Result
	for _, r := range b.Results {
		if show(r) {
			shown = append(shown, r)
		}
	}
	if len(shown) > 0 {
		heading := "failures"
		for _, r := range shown {
			if r.Err == nil {
				heading = "output"
			}
		}
		s.WriteString("\n" + section(heading, colGold, w) + "\n")
		for _, r := range shown {
			s.WriteString("\n" + outputPanel(r, w) + "\n")
		}
	}
	return s.String()
}

// Failed selects the failed jobs, the usual [Report] filter.
func Failed(r jobs.Result) bool { return r.Err != nil }

// outputPanel renders one job's full output, its error first, in a card titled
// with its outcome.
func outputPanel(r jobs.Result, width int) string {
	var body strings.Builder
	if r.Err != nil {
		body.WriteString(Err.Render("error: "+r.Err.Error()) + "\n\n")
	}
	if out := cleanOutput(r.Output); out != "" {
		body.WriteString(out)
	} else {
		body.WriteString(Muted.Render("(no output captured)"))
	}
	title, c := glyphOK+" "+r.Name, colOK
	if r.Err != nil {
		title, c = glyphFail+" "+r.Name, colErr
	}
	if r.Detail != "" {
		title += "  [" + relLabel(r.Detail) + "]"
	}
	return panel(title, c, body.String(), width)
}
