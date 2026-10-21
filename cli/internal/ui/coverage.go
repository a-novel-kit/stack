package ui

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/a-novel-kit/stack/cli/internal/detect"
	"github.com/a-novel-kit/stack/cli/internal/jobs"
)

// coverRe matches the package line `go test -cover` prints, such as
// "ok  pkg  0.12s  coverage: 78.3% of statements". Its group captures the
// percentage.
var coverRe = regexp.MustCompile(`coverage:\s+([0-9.]+)%\s+of statements`)

// covExclude matches the package paths the coverage report leaves out:
// generated mocks, test-support trees and generated protobuf code. Their tests
// still run. Their coverage is noise, so they stay out of the list and the
// mean.
var covExclude = regexp.MustCompile(`/(mocks|test|protogen)(/|$)`)

// vitestHeader matches the header row of vitest's v8 text coverage table. Its
// "% Stmts" and "% Lines" pair is unique to that table, so it reliably anchors
// the node coverage block.
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

// covEntry is one Go package's statement coverage, in percent.
type covEntry struct {
	pkg string
	pct float64
}

// goCoverage extracts per-package coverage from Go test results only.
func goCoverage(results []jobs.Result) []covEntry {
	var es []covEntry
	for _, r := range results {
		if r.Group != string(detect.KindGo) {
			continue
		}
		for _, line := range strings.Split(r.Output, "\n") {
			if e, ok := parseCoverLine(line, r.Name); ok {
				es = append(es, e)
			}
		}
	}
	return es
}

// parseCoverLine reads one `go test -cover` package line. The package is the
// first field holding a slash, or fallback when none does. It reports false
// for any other line and for a package covExclude matches.
func parseCoverLine(line, fallback string) (covEntry, bool) {
	m := coverRe.FindStringSubmatch(line)
	if m == nil {
		return covEntry{}, false
	}
	pct, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return covEntry{}, false
	}
	pkg := fallback
	for _, f := range strings.Fields(line) {
		if strings.Contains(f, "/") {
			pkg = f
			break
		}
	}
	if covExclude.MatchString(pkg) {
		return covEntry{}, false
	}
	return covEntry{pkg: pkg, pct: pct}, true
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
// color-banded percentage list with a mean, and each pnpm target's native
// vitest table verbatim beneath it. CoverageView returns "" when nothing was
// measured, so a caller can append it unconditionally.
func CoverageView(results []jobs.Result, width int) string {
	goEntries := goCoverage(results)

	type nodeCov struct{ name, block string }
	var node []nodeCov
	for _, r := range results {
		if r.Group != string(detect.KindPnpm) {
			continue
		}
		if blk := nodeCoverageBlock(r.Output); blk != "" {
			node = append(node, nodeCov{r.Name, blk})
		}
	}

	if len(goEntries) == 0 && len(node) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(section("coverage", colGold, width) + "\n")

	if len(goEntries) > 0 {
		slices.SortFunc(goEntries, func(x, y covEntry) int { return strings.Compare(x.pkg, y.pkg) })
		var sum float64
		for _, e := range goEntries {
			sum += e.pct
		}
		b.WriteString("\n" + covHeading("Go") + "\n")
		for _, e := range goEntries {
			fmt.Fprintf(&b, "  %s  %s\n", covPct(e.pct), Muted.Render(e.pkg))
		}
		fmt.Fprintf(&b, "  %s\n", Muted.Render(fmt.Sprintf(
			"mean %.1f%% across %d package(s)", sum/float64(len(goEntries)), len(goEntries))))
	}

	for _, n := range node {
		b.WriteString("\n" + covHeading("pnpm · "+n.name) + "\n")
		for _, line := range strings.Split(n.block, "\n") {
			b.WriteString("  " + Muted.Render(line) + "\n")
		}
	}
	return b.String()
}

// covHeading is a sub-section title within the COVERAGE block, in the "▌ title"
// idiom of panel titles, so the Go and pnpm parts each get a heading.
func covHeading(s string) string {
	return Gold.Render("▌ " + s)
}

// covPct colors a coverage figure by the usual green/amber/red bands so a
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
