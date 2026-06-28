package cli

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

// Brand-aligned styles for the repo command's interactive output (the shared
// ui package keeps its palette unexported, so mirror the few colours used).
var (
	repoHead  = lipgloss.NewStyle().Foreground(lipgloss.Color("#DC24FF")).Bold(true) // A-Novel purple
	repoLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#C38500")).Bold(true) // structural gold
	repoOn    = lipgloss.NewStyle().Foreground(lipgloss.Color("#00AF84"))            // on / success
	repoOff   = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C757D"))            // off / muted
)

// selectClass prompts for a class when one was not supplied via --class or a
// repos/<org>_<repo>.yaml override. Numeric pick over the known classes.
func selectClass(cmd *cobra.Command) (repocfg.Class, error) {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintln(out, repoHead.Render("Pick a class:"))
	for i, c := range repocfg.AllClasses {
		_, _ = fmt.Fprintf(out, "  %s %s\n", repoLabel.Render(strconv.Itoa(i+1)+")"), c)
	}
	_, _ = fmt.Fprint(out, "> ")
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	choice := strings.TrimSpace(line)
	// Accept either the number or the class name.
	if n, err := strconv.Atoi(choice); err == nil && n >= 1 && n <= len(repocfg.AllClasses) {
		return repocfg.AllClasses[n-1], nil
	}
	for _, c := range repocfg.AllClasses {
		if strings.EqualFold(choice, string(c)) {
			return c, nil
		}
	}
	return "", fmt.Errorf("not a known class: %q", choice)
}

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
	if c.CodeQL.Enabled && len(t.Discovered.CodeQLLangs) > 0 {
		line("CodeQL", strings.Join(t.Discovered.CodeQLLangs, ", ")+" ("+c.CodeQL.QuerySuite+")")
	} else {
		line("CodeQL", repoOff.Render("off"))
	}
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
	if codecovWanted(c, t) {
		rs = append(rs, "codecov")
	}
	line("Rulesets", strings.Join(rs, ", "))
	if checks := masterChecksFor(t); len(checks) > 0 {
		line("Checks", fmt.Sprintf("%s (%d)", strings.Join(checks, ", "), len(checks)))
	}
}

// masterChecksFor returns the required-check contexts the master ruleset will
// carry. It calls the same resolver the plan uses, so the summary the human
// confirms is exactly what gets applied (reconciled, or reset under --full).
func masterChecksFor(t *repocfg.RepoTarget) []string {
	src := t.ResolveMasterChecks()
	out := make([]string, len(src))
	for i, c := range src {
		out[i] = c.Context
	}
	return out
}

// codecovWanted mirrors the engine's codecov gate: enabled outright, or auto
// when Codecov reports on the repo.
func codecovWanted(c *repocfg.ClassPreset, t *repocfg.RepoTarget) bool {
	switch c.Codecov {
	case repocfg.CodecovEnabled:
		return true
	case repocfg.CodecovAuto:
		return t.CodecovReports
	default:
		return false
	}
}

func pad(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}
