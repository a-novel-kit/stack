package repocfg

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// adminRoleID is the built-in "admin" repository role.
const adminRoleID int64 = 5

// Ruleset bypass modes.
const (
	modeAlways = "always"
	modeExempt = "exempt"
)

// rulesetMaster is the name of the default-branch ruleset; bots bypass it with
// mode "always" (they push to the default branch directly).
const rulesetMaster = "master"

// rulesetTags is the name of the release-tag ruleset. The release bot creates
// the tag, so like master it bypasses with mode "always".
const rulesetTags = "tags"

// APIBypassActor is one bypass-actor entry in the GitHub rulesets API request body.
type APIBypassActor struct {
	ActorID    *int64 `json:"actor_id"`
	ActorType  string `json:"actor_type"`
	BypassMode string `json:"bypass_mode"`
}

// APIRule is one rule entry in the GitHub rulesets API request body.
type APIRule struct {
	Type       string         `json:"type"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// APIRuleset is the GitHub rulesets API request body.
type APIRuleset struct {
	Name         string           `json:"name"`
	Target       string           `json:"target"`
	Enforcement  string           `json:"enforcement"`
	Conditions   map[string]any   `json:"conditions"`
	BypassActors []APIBypassActor `json:"bypass_actors"`
	Rules        []APIRule        `json:"rules"`
}

// RepoTarget is everything needed to compute a repo's desired config.
type RepoTarget struct {
	Org, Repo, DefaultBranch string
	Class                    *ClassPreset
	OrgProfile               *OrgProfile
	Checks                   *ChecksConfig
	Discovered               *Discovered
}

// Op is one API operation the plan performs, a small tagged union. Most ops
// carry a Method, Path, and Body (or Content, for a file write). A ruleset op
// instead sets RulesetName and is reconciled by name at apply time: POST when
// the ruleset is absent, PUT .../{id} when it already exists.
//
// PruneRulesets marks the one op that reconciles the whole ruleset set:
// KeepRulesets is the complete desired list, and apply deletes every live
// ruleset outside it.
type Op struct {
	Method        string   `json:"method,omitempty"`
	Path          string   `json:"path,omitempty"`
	Body          any      `json:"body,omitempty"`
	Content       string   `json:"content,omitempty"`
	RulesetName   string   `json:"ruleset,omitempty"`
	PruneRulesets bool     `json:"prune_rulesets,omitempty"`
	KeepRulesets  []string `json:"keep_rulesets,omitempty"`
}

// Title is a one-line human label for the op.
func (o Op) Title() string {
	if o.PruneRulesets {
		keep := "nothing"
		if len(o.KeepRulesets) > 0 {
			keep = strings.Join(o.KeepRulesets, ", ")
		}
		return fmt.Sprintf("PRUNE rulesets (%s) — keep only: %s", o.Path, keep)
	}
	if o.RulesetName != "" {
		return fmt.Sprintf("PUT|POST ruleset %q (%s)", o.RulesetName, o.Path)
	}
	return o.Method + " " + o.Path
}

// Plan is the ordered set of operations to bring a repo to desired state.
type Plan struct {
	Ops []Op
}

// BuildPlan assembles the desired-state operations for a repo.
func BuildPlan(t *RepoTarget) (*Plan, error) {
	p := &Plan{}
	c := t.Class

	repoPath := fmt.Sprintf("repos/%s/%s", t.Org, t.Repo)

	p.Ops = append(p.Ops, Op{Method: http.MethodPatch, Path: repoPath, Body: SettingsBody(c)})

	// CODEOWNERS is provisioned to every repo regardless of class, so review
	// requests route automatically. Written to .github/ (GitHub honours that
	// over a root copy); apply removes any stray root file so a repo never
	// carries two.
	codeowners, err := CODEOWNERS()
	if err != nil {
		return nil, err
	}
	p.Ops = append(p.Ops, Op{
		Method:  http.MethodPut,
		Path:    repoPath + "/contents/.github/CODEOWNERS",
		Content: codeowners,
	})

	// Labels: the uniform label vocabulary, provisioned to every repo like
	// CODEOWNERS. Reconciled (ensure upserted, retire deleted) at apply time.
	labels, err := LoadLabels()
	if err != nil {
		return nil, err
	}
	p.Ops = append(p.Ops, Op{
		Method: http.MethodPut,
		Path:   repoPath + "/labels",
		Body:   labels,
	})

	// Code scanning stays off, asserted on every update rather than assumed.
	// Static analysis is the lint-semgrep and scan-secrets jobs in main.yaml,
	// which report as ordinary required checks. Default setup used to be held
	// off as a side effect of committing the advanced-setup workflow; with that
	// workflow gone, an org-level "enable for all repositories" toggle would
	// otherwise switch scanning back on here unnoticed. The call is idempotent.
	p.Ops = append(p.Ops, Op{
		Method: http.MethodPatch,
		Path:   repoPath + "/code-scanning/default-setup",
		Body:   map[string]any{"state": "not-configured"},
	})

	// Governance workflows, pushed wherever the master ruleset gates merges —
	// the same repos whose PRs feed the board. They ship as static files: the
	// [Agent] credentials and the per-org board id are GitHub-expression refs
	// the workflow resolves at run time.
	if c.Rulesets.Master {
		for _, wf := range []string{"merge-gate.yaml", "epic-freeze.yaml", "approve-pr.yaml", "derive-status.yaml", "epic-rollback.yaml", "release-train.yaml", "hotfix.yaml"} {
			content, err := ReadTemplate("governance/" + wf)
			if err != nil {
				return nil, err
			}
			p.Ops = append(p.Ops, Op{
				Method:  http.MethodPut,
				Path:    repoPath + "/contents/.github/workflows/" + wf,
				Content: string(content),
			})
		}
	}

	// Auto-approve the trusted dependency bots' PRs so their version bumps don't
	// wait on a human. The workflow ships wherever require-approval holds them.
	if c.Rulesets.RequireApproval {
		content, err := ReadTemplate("governance/auto-approve-dependabot.yaml")
		if err != nil {
			return nil, err
		}
		p.Ops = append(p.Ops, Op{
			Method:  http.MethodPut,
			Path:    repoPath + "/contents/.github/workflows/auto-approve-dependabot.yaml",
			Content: string(content),
		})
	}

	if c.Pages {
		p.Ops = append(p.Ops, Op{Method: http.MethodPost, Path: repoPath + "/pages", Body: map[string]any{"build_type": "workflow"}})
	}

	// Rulesets, reconciled by name (POST when absent, PUT .../{id} when present).
	// The master ruleset gates exactly the discovered checks (always + the
	// repo's main.yaml jobs, minus exclusions) — set wholesale, no reconcile.
	wanted := []struct {
		name   string
		on     bool
		checks []CheckRef
	}{
		{rulesetMaster, c.Rulesets.Master, t.Discovered.Checks},
		{"require-approval", c.Rulesets.RequireApproval, nil},
		{rulesetTags, c.Rulesets.Tags, nil},
	}
	keep := make([]string, 0, len(wanted))
	for _, w := range wanted {
		if !w.on {
			continue
		}
		op, err := rulesetOp(w.name, t, w.checks)
		if err != nil {
			return nil, err
		}
		p.Ops = append(p.Ops, op)
		keep = append(keep, w.name)
	}

	// A repo's ruleset set is derived. Everything above follows from the class
	// preset, the org profile and code-driven discovery, so any ruleset this
	// plan does not name is drift — dropped from the templates, or added by
	// hand in the UI — and apply deletes it.
	//
	// Pruning runs last, once every desired ruleset has been written, so a repo
	// is never momentarily left ungoverned.
	p.Ops = append(p.Ops, Op{
		PruneRulesets: true,
		KeepRulesets:  keep,
		Path:          repoPath + "/rulesets",
	})

	return p, nil
}

func rulesetOp(name string, t *RepoTarget, checks []CheckRef) (Op, error) {
	spec, err := LoadRuleset(name)
	if err != nil {
		return Op{}, err
	}
	// The code_quality rule belongs only on classes that opt into it; a
	// docs or workflows repo has no code to gate.
	if !t.Class.CodeQuality {
		spec.Rules.CodeQuality = nil
	}
	body, err := BuildRuleset(spec, t.OrgProfile, checks)
	if err != nil {
		return Op{}, err
	}
	return Op{
		RulesetName: name,
		Path:        fmt.Sprintf("repos/%s/%s/rulesets", t.Org, t.Repo),
		Body:        body,
	}, nil
}

// BuildRuleset turns a ruleset template + org + discovered checks into the
// API body, resolving generic bypass entries and injecting the checks.
func BuildRuleset(spec *RulesetSpec, org *OrgProfile, checks []CheckRef) (*APIRuleset, error) {
	rs := &APIRuleset{
		Name:        spec.Name,
		Target:      spec.Target,
		Enforcement: spec.Enforcement,
		Conditions: map[string]any{"ref_name": map[string]any{
			"include": spec.Conditions.RefName.Include,
			"exclude": spec.Conditions.RefName.Exclude,
		}},
	}

	for _, entry := range spec.Bypass {
		actors, err := resolveBypass(entry, spec.Name, org)
		if err != nil {
			return nil, err
		}
		rs.BypassActors = append(rs.BypassActors, actors...)
	}

	r := spec.Rules
	if r.Creation {
		rs.Rules = append(rs.Rules, APIRule{Type: "creation"})
	}
	if r.Update {
		rs.Rules = append(rs.Rules, APIRule{Type: "update"})
	}
	if r.Deletion {
		rs.Rules = append(rs.Rules, APIRule{Type: "deletion"})
	}
	if r.NonFastForward {
		rs.Rules = append(rs.Rules, APIRule{Type: "non_fast_forward"})
	}
	if r.RequiredSignatures {
		rs.Rules = append(rs.Rules, APIRule{Type: "required_signatures"})
	}
	if r.RequiredStatusChecks != nil {
		list := make([]map[string]any, 0, len(checks))
		for _, c := range checks {
			m := map[string]any{"context": c.Context}
			if c.IntegrationID != 0 {
				m["integration_id"] = c.IntegrationID
			}
			list = append(list, m)
		}
		rs.Rules = append(rs.Rules, APIRule{Type: "required_status_checks", Parameters: map[string]any{
			"strict_required_status_checks_policy": r.RequiredStatusChecks.Strict,
			"do_not_enforce_on_create":             r.RequiredStatusChecks.DoNotEnforceOnCreate,
			"required_status_checks":               list,
		}})
	}
	if r.MergeQueue != nil {
		rs.Rules = append(rs.Rules, APIRule{Type: "merge_queue", Parameters: r.MergeQueue})
	}
	if r.PullRequest != nil {
		rs.Rules = append(rs.Rules, APIRule{Type: "pull_request", Parameters: r.PullRequest})
	}
	if r.CodeQuality != nil {
		rs.Rules = append(rs.Rules, APIRule{Type: "code_quality", Parameters: map[string]any{"severity": r.CodeQuality.Severity}})
	}
	return rs, nil
}

// resolveBypass maps one generic bypass entry to concrete actors. Admins
// always bypass with mode "always"; bots bypass with "always" on master and
// tags, where they write directly (the bump commit and the release tag), and
// "exempt" on PR rulesets. An entry that resolves to nothing is an error, so a
// typo in a ruleset template is caught before the bypass list ships.
func resolveBypass(entry, rulesetName string, org *OrgProfile) ([]APIBypassActor, error) {
	botMode := modeExempt
	if rulesetName == rulesetMaster || rulesetName == rulesetTags {
		botMode = modeAlways
	}
	switch entry {
	case "admins":
		role := adminRoleID
		return []APIBypassActor{
			{ActorID: nil, ActorType: "OrganizationAdmin", BypassMode: modeAlways},
			{ActorID: &role, ActorType: "RepositoryRole", BypassMode: modeAlways},
		}, nil
	default:
		if id, ok := org.Bots[entry]; ok {
			id := id
			return []APIBypassActor{{ActorID: &id, ActorType: "Integration", BypassMode: botMode}}, nil
		}
	}
	return nil, fmt.Errorf("bypass entry %q in ruleset %q resolves to no actor "+
		"(expected admins or an org bot key from orgs/<org>.yaml)", entry, rulesetName)
}

// SettingsBody is the PATCH /repos body for general + merge + security.
func SettingsBody(c *ClassPreset) map[string]any {
	return map[string]any{
		"has_issues":                  c.Features.Issues,
		"has_wiki":                    c.Features.Wiki,
		"has_projects":                c.Features.Projects,
		"has_discussions":             c.Features.Discussions,
		"allow_squash_merge":          c.Merge.Squash,
		"allow_merge_commit":          c.Merge.MergeCommit,
		"allow_rebase_merge":          c.Merge.Rebase,
		"allow_auto_merge":            c.Merge.AutoMerge,
		"delete_branch_on_merge":      c.Merge.DeleteBranchOnMerge,
		"allow_update_branch":         c.Merge.AllowUpdateBranch,
		"web_commit_signoff_required": c.Merge.SignoffRequired,
		"squash_merge_commit_title":   "COMMIT_OR_PR_TITLE",
		"squash_merge_commit_message": "COMMIT_MESSAGES",
		"security_and_analysis":       SecurityBlock(c),
	}
}

// SecurityBlock is the security_and_analysis sub-object.
func SecurityBlock(c *ClassPreset) map[string]any {
	st := func(b bool) map[string]string {
		if b {
			return map[string]string{"status": "enabled"}
		}
		return map[string]string{"status": "disabled"}
	}
	return map[string]any{
		"secret_scanning":                 st(c.Security.SecretScanning),
		"secret_scanning_push_protection": st(c.Security.PushProtection),
		"dependabot_security_updates":     st(c.Security.Dependabot),
	}
}

// CODEOWNERS returns the uniform CODEOWNERS file provisioned to every repo. It
// is a static single-owner file, so there is nothing to render.
func CODEOWNERS() (string, error) {
	raw, err := ReadTemplate("governance/CODEOWNERS")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// Render writes the plan as labeled raw JSON, with verbatim file content for
// content writes, so the preview stays close to what GitHub receives.
func (p *Plan) Render(w io.Writer) error {
	for i, op := range p.Ops {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
		}
		_, _ = fmt.Fprintf(w, "### %s\n", op.Title())
		if op.Content != "" {
			_, _ = fmt.Fprint(w, op.Content)
			continue
		}
		// A prune carries no request body, so its title stands alone.
		if op.PruneRulesets {
			continue
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(op.Body); err != nil {
			return err
		}
	}
	return nil
}

// RenderJSON writes the plan as a JSON array of ops — machine-readable, so
// the exact operations can be inspected or applied verbatim.
func (p *Plan) RenderJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p.Ops)
}
