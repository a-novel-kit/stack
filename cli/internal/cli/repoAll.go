// `a-novel repo update --all` — reconcile every pulled workspace repo in one
// pass. The candidates are the workspace checkouts `core sync` manages, so the
// operator never lists repos by hand.
//
// The sweep leaves ongoing work alone. Config is discovered from each working
// tree, so a repo off its default branch or with uncommitted changes is skipped
// untouched and only committed config reaches GitHub. Planning runs for every
// repo at once; one confirm gates the batch, then --jobs repos apply at a time.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/jobs"
	"github.com/a-novel-kit/stack/cli/internal/repocfg"
	"github.com/a-novel-kit/stack/cli/internal/ui"
)

// plannedUpdate is a checkout ready to reconcile, with its computed plan.
type plannedUpdate struct {
	checkout

	target *repocfg.RepoTarget
	plan   *repocfg.Plan
	prune  string // the dry-run's prune impact, resolved while planning
}

func runRepoUpdateAll(cmd *cobra.Command, rootDir, class string, exclude []string, dryRun, jsonOut bool, limit int) error {
	root, err := workspaceRoot(rootDir)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	// Previews keep stdout for the plans, so a --json document stays pipeable.
	progress := out
	if jsonOut || dryRun {
		progress = cmd.ErrOrStderr()
	}

	checkouts, err := workspaceCheckouts(root)
	if err != nil {
		return err
	}
	if len(checkouts) == 0 {
		_, _ = fmt.Fprintf(progress, "no pulled workspace repos found at %s — run `a-novel core sync` first.\n", root)
		if jsonOut {
			return renderAllJSON(out, nil)
		}
		return nil
	}

	_, _ = fmt.Fprintf(progress, "▸ Planning %d workspace repo(s) at %s\n", len(checkouts), root)
	planned, notes := planAll(checkouts, class, normaliseFilter(exclude), dryRun)
	for _, note := range notes {
		if note != "" {
			_, _ = fmt.Fprintln(progress, "  "+note)
		}
	}

	switch {
	case jsonOut:
		return renderAllJSON(out, planned)
	case len(planned) == 0:
		_, _ = fmt.Fprintln(progress, "\nnothing to reconcile — every repo was skipped or excluded.")
		return nil
	case dryRun:
		for _, p := range planned {
			_, _ = fmt.Fprintf(progress, "\n# dry-run %s — class %s\n%s\n", p.FullName(), p.target.Class.Class, p.prune)
			if err := p.plan.Render(out); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(out)
		}
		return nil
	}

	_, _ = fmt.Fprintf(out, "\nReconcile %d repo(s):\n", len(planned))
	for _, p := range planned {
		renderCompactSummary(out, p.target)
	}
	if !stdinIsTTY() {
		return errors.New("repo update --all is interactive (human-only); run it in a terminal, or use --dry-run")
	}
	if !confirm(cmd, fmt.Sprintf("\nApply configuration to these %d repo(s)?", len(planned))) {
		_, _ = fmt.Fprintln(out, "aborted.")
		return nil
	}

	list := make([]jobs.Job, len(planned))
	for i, p := range planned {
		list[i] = jobs.Job{
			Name: p.FullName(),
			Run: func(_ context.Context, out io.Writer) (string, error) {
				return "reconciled", applyPlan(out, p.target, p.plan)
			},
		}
	}
	_, _ = fmt.Fprintln(out)
	batch := ui.RunJobs(cmd.Context(), list, limit, stdoutIsTTY())
	// Every repo's per-operation log is the audit trail of the writes.
	_, _ = fmt.Fprint(out, ui.Report("REPO UPDATE", batch, func(jobs.Result) bool { return true }))
	return batchError(batch)
}

// planAll plans every checkout at once and returns the eligible plans in
// checkout order, with one note per checkout left out (or "").
func planAll(checkouts []checkout, class string, excluded map[string]bool, preview bool) ([]plannedUpdate, []string) {
	plans := make([]*plannedUpdate, len(checkouts))
	notes := make([]string, len(checkouts))
	var wg sync.WaitGroup
	for i, c := range checkouts {
		wg.Go(func() {
			if c.listed(excluded) {
				notes[i] = ui.Muted.Render("○") + " " + c.FullName() + " — excluded"
				return
			}
			if reason := ongoingWork(c.dir); reason != "" {
				notes[i] = ui.Muted.Render("⏸") + " " + c.FullName() + " — " + reason + ", skipped"
				return
			}
			target, plan, err := planRepo(c.repoEntry, c.dir, class)
			if err != nil {
				notes[i] = ui.Muted.Render("✗") + " " + c.FullName() + " — " + firstLine(err.Error())
				return
			}
			p := &plannedUpdate{checkout: c, target: target, plan: plan}
			if preview {
				p.prune = pruneImpact(target, plan)
			}
			plans[i] = p
		})
	}
	wg.Wait()

	var planned []plannedUpdate
	for _, p := range plans {
		if p != nil {
			planned = append(planned, *p)
		}
	}
	return planned, notes
}

// renderCompactSummary prints the batch confirm's two-line overview of a repo:
// its class plus what varies between repos, the rulesets and required checks.
func renderCompactSummary(w io.Writer, t *repocfg.RepoTarget) {
	header := fmt.Sprintf("  %s %s — class %s", ui.OK.Render("▸"), ui.Brand.Render(t.Org+"/"+t.Repo), t.Class.Class)
	if rs := rulesetNames(t.Class); len(rs) > 0 {
		header += " · " + strings.Join(rs, ", ")
	}
	_, _ = fmt.Fprintln(w, header)
	if checks := checkContexts(t); len(checks) > 0 {
		_, _ = fmt.Fprintf(w, "      %s %s (%d)\n", ui.Gold.Render("checks"), strings.Join(checks, ", "), len(checks))
	}
}

// renderAllJSON writes the previewed plans as a JSON array of {repo, ops}, so
// an operator can inspect or diff exactly what a bulk run would apply. An empty
// batch renders as `[]`.
func renderAllJSON(w io.Writer, planned []plannedUpdate) error {
	type repoOps struct {
		Repo string       `json:"repo"`
		Ops  []repocfg.Op `json:"ops"`
	}
	arr := make([]repoOps, len(planned))
	for i, p := range planned {
		arr[i] = repoOps{Repo: p.FullName(), Ops: p.plan.Ops}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(arr)
}
