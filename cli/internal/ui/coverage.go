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

// vitestHeader matches the header row of vitest's v8 text coverage table; its
// "% Stmts" + "% Lines" pair is unique to that table — env-spin / go output
// never has it, so it reliably anchors the node coverage block.
var vitestHeader = regexp.MustCompile(`%\s*Stmts.*%\s*Lines`)

// tableRow reports whether a line is part of a vitest pipe table (a data/
// header row or a dashed rule).
func tableRow(s string) bool {
	if strings.Contains(s, "|") {
		return true
	}
	t := strings.TrimSpace(s)
	return t != "" && strings.Trim(t, "-| ") == ""
}

type covEntry struct {
	pkg string
	pct float64
}

// goCoverage extracts per-package coverage from Go test results only.
func goCoverage(results []build.Result) []covEntry {
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

// nodeCoverageBlock returns the vitest v8 coverage table verbatim from a pnpm
// result's output, or "" if none. It anchors on the unique header row and
// expands to the surrounding pipe/rule lines, so it survives vitest version
// differences and never captures the surrounding test/log noise.
func nodeCoverageBlock(out string) string {
	lines := strings.Split(out, "\n")
	hdr := -1
	for i, l := range lines {
		if vitestHeader.MatchString(l) {
			hdr = i
			break
		}
	}
	if hdr < 0 {
		return ""
	}
	start, end := hdr, hdr
	for start > 0 && tableRow(lines[start-1]) {
		start--
	}
	for end+1 < len(lines) && tableRow(lines[end+1]) {
		end++
	}
	return strings.TrimRight(strings.Join(lines[start:end+1], "\n"), "\n")
}

// CoverageView renders the COVERAGE report section: Go packages as a
// colour-banded percentage list with a mean, and each pnpm target's native
// vitest table verbatim beneath it. Returns "" when nothing was measured
// (flag off, or no coverage produced) so callers can drop it in
// unconditionally.
func CoverageView(results []build.Result, width int) string {
	goEntries := goCoverage(results)

	type nodeCov struct{ name, block string }
	var node []nodeCov
	for _, r := range results {
		if r.Target.Kind != detect.KindPnpm {
			continue
		}
		if blk := nodeCoverageBlock(r.Output); blk != "" {
			node = append(node, nodeCov{r.Target.Name, blk})
		}
	}

	if len(goEntries) == 0 && len(node) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(section("coverage", colGold, width) + "\n")

	if len(goEntries) > 0 {
		sort.Slice(goEntries, func(i, j int) bool { return goEntries[i].pkg < goEntries[j].pkg })
		var sum float64
		for _, e := range goEntries {
			sum += e.pct
		}
		b.WriteString("\n" + covHeading("Go") + "\n")
		for _, e := range goEntries {
			fmt.Fprintf(&b, "  %s  %s\n", covPct(e.pct), styleMuted.Render(e.pkg))
		}
		fmt.Fprintf(&b, "  %s\n", styleMuted.Render(fmt.Sprintf(
			"mean %.1f%% across %d package(s)", sum/float64(len(goEntries)), len(goEntries))))
	}

	for _, n := range node {
		b.WriteString("\n" + covHeading("pnpm · "+n.name) + "\n")
		for _, line := range strings.Split(n.block, "\n") {
			b.WriteString("  " + styleMuted.Render(line) + "\n")
		}
	}
	return b.String()
}

// covHeading is a sub-section title within the COVERAGE block — the same
// "▌ title" idiom as panel titles, so Go and pnpm are each clearly announced.
func covHeading(s string) string {
	return lipgloss.NewStyle().Foreground(colGold).Bold(true).Render("▌ " + s)
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
