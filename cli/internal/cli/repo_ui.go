package cli

import (
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

// Brand-aligned styles for the repo command's interactive output (the shared
// ui package keeps its palette unexported, so mirror the few colors used).
var (
	repoHead  = lipgloss.NewStyle().Foreground(lipgloss.Color("#DC24FF")).Bold(true) // A-Novel purple
	repoLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#C38500")).Bold(true) // structural gold
	repoOn    = lipgloss.NewStyle().Foreground(lipgloss.Color("#00AF84"))            // on / success
	repoOff   = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C757D"))            // off / muted
)

// renderSummary prints a readable, grouped overview of the desired config —
// the human-facing alternative to the raw API ops dump, shown before the
// apply confirm.
func renderSummary(w io.Writer, t *repocfg.RepoTarget) {
	c := t.Class
	line := func(label, val string) {
		_, _ = fmt.Fprintf(w, "  %s  %s\n", repoLabel.Render(pad(label, 10)), val)
	}
	onOff := func(label string, on bool) string {
		if on {
			return repoOn.Render(label)
		}
		return repoOff.Render(label + "✗")
	}

	_, _ = fmt.Fprintf(w, "%s — class %s\n", repoHead.Render(t.Org+"/"+t.Repo), c.Class)
	line("Features", strings.Join([]string{
		onOff("issues", c.Features.Issues), onOff("projects", c.Features.Projects),
		onOff("discussions", c.Features.Discussions), onOff("wiki", c.Features.Wiki),
	}, " "))
	merge := []string{}
	if c.Merge.Squash {
		merge = append(merge, "squash")
	}
	if c.Merge.MergeCommit {
		merge = append(merge, "merge")
	}
	if c.Merge.Rebase {
		merge = append(merge, "rebase")
	}
	mergeStr := strings.Join(merge, "+")
	if c.Merge.AutoMerge {
		mergeStr += ", auto-merge"
	}
	if c.Merge.SignoffRequired {
		mergeStr += ", signoff"
	}
	line("Merge", mergeStr)
	line("Security", strings.Join([]string{
		onOff("secret-scanning", c.Security.SecretScanning),
		onOff("push-protection", c.Security.PushProtection),
		onOff("dependabot", c.Security.Dependabot),
	}, " "))
	if c.Pages {
		line("Pages", repoOn.Render("enabled"))
	}
	rs := []string{}
	if c.Rulesets.Master {
		rs = append(rs, "master")
	}
	if c.Rulesets.RequireApproval {
		rs = append(rs, "require-approval")
	}
	line("Rulesets", strings.Join(rs, ", "))
	if checks := masterChecksFor(t); len(checks) > 0 {
		line("Checks", fmt.Sprintf("%s (%d)", strings.Join(checks, ", "), len(checks)))
	}
}

// masterChecksFor returns the required-check contexts the master ruleset will
// carry — the discovered set (always + the repo's main.yaml jobs, minus
// exclusions), exactly what the plan applies.
func masterChecksFor(t *repocfg.RepoTarget) []string {
	out := make([]string, len(t.Discovered.Checks))
	for i, c := range t.Discovered.Checks {
		out[i] = c.Context
	}
	return out
}

func pad(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}
