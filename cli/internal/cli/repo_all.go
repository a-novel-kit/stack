// `a-novel repo update --all` — reconcile every pulled workspace repo in one
// interactive pass. The candidate set is auto-discovered (the stack repo plus
// each whitelisted checkout present under app/ or kit/, the same list
// `core sync` manages), so the operator never enumerates repos by hand.
//
// The sweep is conservative about ongoing work. Config is discovered from each
// working tree (repocfg.Discover reads .github/workflows/main.yaml), and a repo
// that is off its default branch or has uncommitted changes is skipped
// untouched. Only committed config reaches the live repo. A single confirm
// gates the organization and repository changes, and --dry-run / --json
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

// runRepoUpdateAll reconciles organization policy and every pulled workspace
// repo in one pass. It scans the candidate set, skips any repo carrying ongoing
// work (off its default branch or with a dirty working tree) or named in
// --exclude, builds the organization and repository plans, then applies them
// after a single batch confirm. --dry-run prints the plans and --json emits them
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
			return renderAllJSON(out, nil, nil)
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

	if len(eligible) == 0 {
		if jsonOut {
			return renderAllJSON(out, nil, nil)
		}
		_, _ = fmt.Fprintln(progress, "\nnothing to reconcile — every repo was skipped or excluded.")
		return nil
	}
	orgPolicies, err := planOrgPolicies(eligible)
	if err != nil {
		return err
	}
	if jsonOut {
		return renderAllJSON(out, orgPolicies, eligible)
	}
	if dryRun {
		return renderAllDryRun(progress, out, orgPolicies, eligible)
	}

	// Interactive: compact organization and repository summaries, then a single
	// confirm for the batch.
	_, _ = fmt.Fprintf(out, "\nReconcile %d organization policy change(s) and %d repo(s):\n",
		len(orgPolicies), len(eligible))
	for _, policy := range orgPolicies {
		renderOrgPolicySummary(out, policy)
	}
	for _, p := range eligible {
		renderCompactSummary(out, p.target)
	}
	if !stdinIsTTY() {
		return errors.New("repo update --all is interactive (human-only); run it in a terminal, or use --dry-run")
	}
	if !confirm(cmd, fmt.Sprintf(
		"\nApply %d organization policy change(s) and configuration to %d repo(s)?",
		len(orgPolicies),
		len(eligible),
	)) {
		_, _ = fmt.Fprintln(out, "aborted.")
		return nil
	}

	// Organization policy applies first because inherited default setup can
	// override the repository-level desired state. A failure stops the repo
	// phase, keeping that unsafe mismatch visible for the next run.
	var orgFailed int
	for _, policy := range orgPolicies {
		_, _ = fmt.Fprintf(out, "\n▸ %s organization policy\n", repoHead.Render(policy.org))
		if applyErr := applyOrgPolicy(policy); applyErr != nil {
			_, _ = fmt.Fprintf(out, "  %s %s\n", repoOff.Render("✗"), firstLine(applyErr.Error()))
			orgFailed++
			continue
		}
		_, _ = fmt.Fprintln(out, "  ✓ managed security policy applied to every repository")
	}
	if orgFailed > 0 {
		return fmt.Errorf(
			"%d organization policy change(s) failed; repository configuration was not applied",
			orgFailed,
		)
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
	if len(orgPolicies) > 0 {
		_, _ = fmt.Fprintf(out, "  ✓ organization policies %d\n", len(orgPolicies))
	}
	_, _ = fmt.Fprintf(out, "  ✓ reconciled %d\n", reconciled)
	_, _ = fmt.Fprintf(out, "  ✗ failed     %d\n", failed)
	if failed > 0 {
		return fmt.Errorf("%d repo(s) failed to reconcile", failed)
	}
	return nil
}

// renderAllDryRun writes organization policy operations once per organization,
// followed by each eligible repository plan. It performs no writes.
func renderAllDryRun(
	progress io.Writer,
	out io.Writer,
	orgPolicies []plannedOrgPolicy,
	items []plannedUpdate,
) error {
	for _, policy := range orgPolicies {
		_, _ = fmt.Fprintf(progress, "\n# dry-run %s organization policy\n", policy.org)
		if err := policy.plan.Render(out); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(out)
	}
	for _, item := range items {
		_, _ = fmt.Fprintf(progress, "\n# dry-run %s — class %s\n",
			item.cand.fullName(), item.target.Class.Class)
		renderPruneImpact(progress, item.target.Org, item.target.Repo, item.plan)
		if err := item.plan.Render(out); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(out)
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
	if t.Class.Rulesets.Tags {
		rs = append(rs, "tags")
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

// renderOrgPolicySummary describes the organization-wide effect before the
// fleet confirmation.
func renderOrgPolicySummary(w io.Writer, policy plannedOrgPolicy) {
	_, _ = fmt.Fprintf(w, "  %s %s — apply managed security policy with CodeQL default setup disabled\n",
		repoOn.Render("▸"), repoHead.Render(policy.org))
}

// renderAllJSON writes the previewed plans as a JSON array of organization or
// repository targets and their operations. An empty plan renders as `[]`.
func renderAllJSON(w io.Writer, orgPolicies []plannedOrgPolicy, items []plannedUpdate) error {
	type targetOps struct {
		Org  string       `json:"org,omitempty"`
		Repo string       `json:"repo,omitempty"`
		Ops  []repocfg.Op `json:"ops"`
	}
	arr := make([]targetOps, 0, len(orgPolicies)+len(items))
	for _, policy := range orgPolicies {
		arr = append(arr, targetOps{Org: policy.org, Ops: policy.plan.Ops})
	}
	for _, item := range items {
		arr = append(arr, targetOps{Repo: item.cand.fullName(), Ops: item.plan.Ops})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(arr)
}
