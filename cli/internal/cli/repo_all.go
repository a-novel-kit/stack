// `a-novel repo update --all` — reconcile every pulled workspace repo in one
// interactive pass. The candidate set is auto-discovered (the stack repo plus
// each whitelisted checkout present under app/ or kit/, the same list
// `core sync` manages), so the operator never enumerates repos by hand.
//
// The sweep is conservative about ongoing work. Config is discovered from each
// working tree (repocfg.Discover reads .github/workflows/main.yaml), so a repo
// that is off its default branch or has uncommitted changes is skipped
// untouched; reconciling from an in-progress checkout would push unmerged
// config live. A single confirm gates the whole batch, and --dry-run / --json
// preview without applying.

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

// updateCandidate is one pulled checkout the --all sweep may reconcile: the
// repo it resolves to (from its origin remote) and the directory it lives in.
type updateCandidate struct {
	org  string
	repo string
	dir  string
}

// fullName returns "<org>/<repo>" — the form used in output and --exclude.
func (c updateCandidate) fullName() string { return c.org + "/" + c.repo }

// plannedUpdate pairs an eligible candidate with the target and plan computed
// for it, so the confirm/apply phase does not recompute.
type plannedUpdate struct {
	cand   updateCandidate
	target *repocfg.RepoTarget
	plan   *repocfg.Plan
}

// runRepoUpdateAll reconciles every pulled workspace repo in one pass. It scans
// the candidate set, skips any repo carrying ongoing work (off its default
// branch or with a dirty working tree) or named in --exclude, builds a plan for
// each eligible repo, then — after a single batch confirm — applies them,
// streaming per-repo results. --dry-run prints the plans and --json emits them
// as machine-readable output; neither applies anything and neither needs a TTY.
func runRepoUpdateAll(cmd *cobra.Command, rootDir, class string, exclude []string, dryRun, jsonOut bool) error {
	root, err := resolveSyncRoot(rootDir)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	// In preview modes stdout carries only the plans, so scan/skip commentary
	// goes to stderr — keeping --json output a clean, pipeable document.
	progress := out
	if jsonOut || dryRun {
		progress = cmd.ErrOrStderr()
	}

	cands, err := discoverUpdateCandidates(root)
	if err != nil {
		return err
	}
	if len(cands) == 0 {
		_, _ = fmt.Fprintf(progress,
			"no pulled workspace repos found at %s — run `a-novel core sync` first.\n", root)
		if jsonOut {
			return renderAllJSON(out, nil)
		}
		return nil
	}

	excludeSet := normaliseFilter(exclude)
	_, _ = fmt.Fprintf(progress, "▸ Scanning %d workspace repo(s) at %s\n", len(cands), root)
	var eligible []plannedUpdate
	for _, c := range cands {
		if excludedRepo(excludeSet, c) {
			_, _ = fmt.Fprintf(progress, "  %s %s — excluded\n", repoOff.Render("○"), c.fullName())
			continue
		}
		// Skip repos with ongoing work: their working tree (which discovery
		// reads) may not match the canonical default-branch state.
		if reason := ongoingWork(c.dir); reason != "" {
			_, _ = fmt.Fprintf(progress, "  %s %s — %s, skipped\n", repoOff.Render("⏸"), c.fullName(), reason)
			continue
		}
		target, plan, buildErr := buildRepoTarget(c.dir, class)
		if buildErr != nil {
			_, _ = fmt.Fprintf(progress, "  %s %s — %s\n", repoOff.Render("✗"), c.fullName(), firstLine(buildErr.Error()))
			continue
		}
		eligible = append(eligible, plannedUpdate{cand: c, target: target, plan: plan})
	}

	if jsonOut {
		return renderAllJSON(out, eligible)
	}
	if len(eligible) == 0 {
		_, _ = fmt.Fprintln(progress, "\nnothing to reconcile — every repo was skipped or excluded.")
		return nil
	}
	if dryRun {
		for _, p := range eligible {
			_, _ = fmt.Fprintf(progress, "\n# dry-run %s — class %s\n", p.cand.fullName(), p.target.Class.Class)
			renderPruneImpact(progress, p.target.Org, p.target.Repo, p.plan)
			if renderErr := p.plan.Render(out); renderErr != nil {
				return renderErr
			}
			_, _ = fmt.Fprintln(out)
		}
		return nil
	}

	// Interactive: compact per-repo summary, then a single confirm for the batch.
	_, _ = fmt.Fprintf(out, "\nReconcile %d repo(s):\n", len(eligible))
	for _, p := range eligible {
		renderCompactSummary(out, p.target)
	}
	if !stdinIsTTY() {
		return errors.New("repo update --all is interactive (human-only); run it in a terminal, or use --dry-run")
	}
	if !confirm(cmd, fmt.Sprintf("\nApply configuration to these %d repo(s)?", len(eligible))) {
		_, _ = fmt.Fprintln(out, "aborted.")
		return nil
	}

	// Apply each in turn; keep going after a failure so one fiddly repo does
	// not strand the rest, and report the tally at the end.
	var reconciled, failed int
	for _, p := range eligible {
		_, _ = fmt.Fprintf(out, "\n▸ %s\n", repoHead.Render(p.cand.fullName()))
		if applyErr := applyPlan(out, p.target.Org, p.target.Repo, p.target.DefaultBranch, p.plan); applyErr != nil {
			_, _ = fmt.Fprintf(out, "  %s %s\n", repoOff.Render("✗"), firstLine(applyErr.Error()))
			failed++
			continue
		}
		reconciled++
	}
	_, _ = fmt.Fprintln(out, "\nSummary")
	_, _ = fmt.Fprintf(out, "  ✓ reconciled %d\n", reconciled)
	_, _ = fmt.Fprintf(out, "  ✗ failed     %d\n", failed)
	if failed > 0 {
		return fmt.Errorf("%d repo(s) failed to reconcile", failed)
	}
	return nil
}

// discoverUpdateCandidates finds every pulled workspace checkout: the stack repo
// at root (resolved from its own origin), plus each whitelisted repo actually
// present on disk under app/ or kit/. A repo listed in workspace-repos.yaml but
// not yet cloned is simply absent — there is nothing local to reconcile from.
// The stack repo is deduplicated in case the whitelist ever lists it.
func discoverUpdateCandidates(root string) ([]updateCandidate, error) {
	var cands []updateCandidate
	seen := map[string]struct{}{}
	if org, repo, err := repoFromGitRemote(root); err == nil {
		cands = append(cands, updateCandidate{org: org, repo: repo, dir: root})
		seen[org+"/"+repo] = struct{}{}
	}
	entries, err := loadRepoWhitelist(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if _, dup := seen[e.FullName()]; dup {
			continue
		}
		dir := filepath.Join(root, e.TargetSubdir(), e.Name)
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			continue
		}
		cands = append(cands, updateCandidate{org: e.Org, repo: e.Name, dir: dir})
		seen[e.FullName()] = struct{}{}
	}
	return cands, nil
}

// ongoingWork reports a human-readable reason to leave dir untouched — it is off
// its default branch or its working tree is dirty — or "" when the checkout is a
// clean default-branch tree safe to reconcile from. The default branch is read
// from origin/HEAD (no network), matching `core sync`.
func ongoingWork(dir string) string {
	def := resolveDefaultBranch(dir)
	branch := ""
	if out, err := runGit(dir, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		branch = strings.TrimSpace(out)
	}
	switch {
	case branch == "":
		return "detached HEAD"
	case branch != def:
		return "on " + branch
	}
	if out, err := runGit(dir, "status", "--porcelain"); err == nil && strings.TrimSpace(out) != "" {
		return "uncommitted changes"
	}
	return ""
}

// excludedRepo reports whether c is filtered out by the --exclude set, matched
// on either its full <org>/<name> or its bare <name>. Repo names are unique
// org-wide, so a bare name is an unambiguous convenience form.
func excludedRepo(set map[string]struct{}, c updateCandidate) bool {
	if _, ok := set[c.fullName()]; ok {
		return true
	}
	_, ok := set[c.repo]
	return ok
}

// renderCompactSummary prints the two-line overview the batch confirm shows per
// eligible repo: the class plus the dimensions that vary between repos, such as
// rulesets and the discovered required checks.
func renderCompactSummary(w io.Writer, t *repocfg.RepoTarget) {
	rs := []string{}
	if t.Class.Rulesets.Master {
		rs = append(rs, "master")
	}
	if t.Class.Rulesets.RequireApproval {
		rs = append(rs, "require-approval")
	}
	header := fmt.Sprintf("  %s %s — class %s",
		repoOn.Render("▸"), repoHead.Render(t.Org+"/"+t.Repo), t.Class.Class)
	if len(rs) > 0 {
		header += " · " + strings.Join(rs, ", ")
	}
	_, _ = fmt.Fprintln(w, header)
	if checks := masterChecksFor(t); len(checks) > 0 {
		_, _ = fmt.Fprintf(w, "      %s %s (%d)\n",
			repoLabel.Render("checks"), strings.Join(checks, ", "), len(checks))
	}
}

// renderAllJSON writes the previewed plans as a JSON array of {repo, ops}
// objects, so an operator can inspect or diff the exact operations a bulk run
// would apply. An empty slice renders as `[]`.
func renderAllJSON(w io.Writer, items []plannedUpdate) error {
	type repoOps struct {
		Repo string       `json:"repo"`
		Ops  []repocfg.Op `json:"ops"`
	}
	arr := make([]repoOps, len(items))
	for i, p := range items {
		arr[i] = repoOps{Repo: p.cand.fullName(), Ops: p.plan.Ops}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(arr)
}
