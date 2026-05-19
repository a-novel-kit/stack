package ui

import (
	"strconv"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/detect"
)

// DryRunView answers "what would `a-novel build` do here?" without running
// anything: a short explanation, a per-kind count strip, then a table of every
// detected target with its exact command. Sections are separated by titled
// rules so the structure is obvious at a glance.
func DryRunView(version string, targets []detect.Target) string {
	w := termWidth()

	var b strings.Builder
	b.WriteString(Banner(version))
	b.WriteString("\n\n")

	b.WriteString(section("dry run", colGold, w) + "\n\n")
	b.WriteString(para(
		"Nothing is built. The working tree was scanned for every build the CLI "+
			"can run here — Go modules, pnpm build scripts, and Podman images — "+
			"and each is listed below with the exact command it would execute.", w) + "\n\n")

	// Count per kind for the headline pills (only kinds actually present).
	counts := map[detect.Kind]int{}
	for _, t := range targets {
		counts[t.Kind]++
	}
	var pills []string
	for _, k := range []detect.Kind{detect.KindGo, detect.KindPnpm, detect.KindPodman} {
		if n := counts[k]; n > 0 {
			pills = append(pills, pill(kindLabel(k), strconv.Itoa(n), kindColor(k)))
		}
	}
	pills = append(pills, pill("total", strconv.Itoa(len(targets)), colGold))
	b.WriteString(pillRow(pills...))
	b.WriteString("\n\n")

	b.WriteString(section("detected targets", colGold, w) + "\n\n")
	b.WriteString(targetsTable(targets))
	b.WriteString("\n\n")

	b.WriteString(rule(w) + "\n")
	b.WriteString(para(
		"Run `a-novel build` to pick interactively, or `a-novel build -y` to "+
			"build everything. Filter with `--type go,pnpm,podman`.", w) + "\n")
	return b.String()
}
