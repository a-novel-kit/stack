package ui

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/a-novel-kit/stack/cli/internal/build"
	"github.com/a-novel-kit/stack/cli/internal/detect"
)

// coverRe matches the per-package line `go test -cover` prints, e.g.
// "ok  pkg  0.12s  coverage: 78.3% of statements". That exact phrase is
// emitted ONLY by go test -cover — podman/compose/setup-env output and
// pnpm/vitest output never contain it.
var coverRe = regexp.MustCompile(`coverage:\s+([0-9.]+)%\s+of statements`)

type covEntry struct {
	pkg string
	pct float64
}

// coverageEntries extracts per-package coverage from Go test results only.
// Doubly reliable: non-Go results are skipped outright (so node/pnpm output
// never enters), and within Go output only the exact go-test coverage phrase
// is matched (so env-spin lines never enter).
func coverageEntries(results []build.Result) []covEntry {
	var es []covEntry
	for _, r := range results {
		if r.Target.Kind != detect.KindGo {
			continue
		}
		for _, line := range strings.Split(r.Output, "\n") {
			m := coverRe.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			pct, err := strconv.ParseFloat(m[1], 64)
			if err != nil {
				continue
			}
			pkg := r.Target.Name
			for _, f := range strings.Fields(line) {
				if strings.Contains(f, "/") {
					pkg = f
					break
				}
			}
			es = append(es, covEntry{pkg: pkg, pct: pct})
		}
	}
	return es
}

// CoverageView renders the COVERAGE report section, or "" when nothing was
// measured (no -cover, or no Go packages with statements) — so callers can
// drop it in unconditionally; it self-gates.
func CoverageView(results []build.Result, width int) string {
	es := coverageEntries(results)
	if len(es) == 0 {
		return ""
	}
	sort.Slice(es, func(i, j int) bool { return es[i].pkg < es[j].pkg })

	var sum float64
	for _, e := range es {
		sum += e.pct
	}
	mean := sum / float64(len(es))

	var b strings.Builder
	b.WriteString(section("coverage", colGold, width) + "\n\n")
	for _, e := range es {
		fmt.Fprintf(&b, "  %s  %s\n", covPct(e.pct), styleMuted.Render(e.pkg))
	}
	fmt.Fprintf(&b, "\n  %s\n", styleGold.Render(
		fmt.Sprintf("mean %.1f%% across %d package(s)", mean, len(es))))
	return b.String()
}

// covPct colours a coverage figure by the usual green/amber/red bands so a
// thin package stands out at a glance.
func covPct(p float64) string {
	c := colErr
	switch {
	case p >= 80:
		c = colOK
	case p >= 50:
		c = colGold
	}
	return lipgloss.NewStyle().Foreground(c).Bold(true).Render(fmt.Sprintf("%5.1f%%", p))
}
