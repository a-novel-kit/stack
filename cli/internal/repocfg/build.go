package repocfg

import (
	"encoding/json"
	"fmt"
	"io"
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

// rulesetTags is the name of the release-tag ruleset. Like master, the release
// bot writes to it directly (it creates the tag), so its bypass mode is "always"
// rather than the PR-only "exempt".
const rulesetTags = "tags"

// APIBypassActor / APIRule / APIRuleset mirror the GitHub rulesets API
// request body.
type APIBypassActor struct {
	ActorID    *int64 `json:"actor_id"`
	ActorType  string `json:"actor_type"`
	BypassMode string `json:"bypass_mode"`
}

type APIRule struct {
	Type       string         `json:"type"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

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

	// MasterChecks, when non-nil, is the authoritative required-check set
	// for the master ruleset — used by `update` to PRESERVE the live
	// ruleset's checks rather than overwrite them with discovery (which can
	// only approximate names). Nil (e.g. on `create`) falls back to the
	// discovered set.
	MasterChecks []CheckRef
	// CodecovReports is whether Codecov posts a status on this repo. It
	// gates `codecov: auto` — a coverage ruleset is only enforced where
	// Codecov actually reports, so it never blocks PRs on a repo that has
	// no coverage upload.
	CodecovReports bool
}

// Op is one API operation the plan would perform.
//
//   - settings/pages: Method + Path + Body.
//   - contents (codeql/dependabot): Method PUT + Path + Content (file text).
//   - ruleset: RulesetName set; reconciled by name at apply time (POST when
//     absent, PUT .../{id} when present), Body is the ruleset payload.
type Op struct {
	Method      string `json:"method,omitempty"`
	Path        string `json:"path,omitempty"`
	Body        any    `json:"body,omitempty"`
	Content     string `json:"content,omitempty"`
	RulesetName string `json:"ruleset,omitempty"`
}

// Title is a one-line human label for the op.
func (o Op) Title() string {
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

	p.Ops = append(p.Ops, Op{Method: "PATCH", Path: repoPath, Body: SettingsBody(c)})

	if c.CodeQL.Enabled && len(t.Discovered.CodeQLLangs) > 0 {
		content, err := RenderCodeQL(t.Discovered.CodeQLLangs, c.CodeQL.QuerySuite, t.DefaultBranch)
		if err != nil {
			return nil, err
		}
		p.Ops = append(p.Ops, Op{
			Method:  "PUT",
			Path:    repoPath + "/contents/.github/workflows/codeql.yml",
			Content: content,
		})
	}

	if c.Pages {
		p.Ops = append(p.Ops, Op{Method: "POST", Path: repoPath + "/pages", Body: map[string]any{"build_type": "workflow"}})
	}

	// Rulesets, reconciled by name (POST when absent, PUT .../{id} when present).
	if c.Rulesets.Master {
		masterChecks := t.Discovered.Checks
		if t.MasterChecks != nil {
			masterChecks = t.MasterChecks // preserve live (lossless on update)
		}
		if op, err := rulesetOp(rulesetMaster, t, masterChecks); err != nil {
			return nil, err
		} else {
			p.Ops = append(p.Ops, op)
		}
	}
	if c.Rulesets.RequireApproval {
		if op, err := rulesetOp("require-approval", t, nil); err != nil {
			return nil, err
		} else {
			p.Ops = append(p.Ops, op)
		}
	}
	if c.Rulesets.Tags {
		if op, err := rulesetOp(rulesetTags, t, nil); err != nil {
			return nil, err
		} else {
			p.Ops = append(p.Ops, op)
		}
	}
	if codecovEnabled(c, t) {
		checks := resolveCheckDefs(t.Checks.Codecov.Checks, t.Checks)
		if op, err := rulesetOp("codecov", t, checks); err != nil {
			return nil, err
		} else {
			p.Ops = append(p.Ops, op)
		}
	}

	return p, nil
}

func rulesetOp(name string, t *RepoTarget, checks []CheckRef) (Op, error) {
	spec, err := LoadRuleset(name)
	if err != nil {
		return Op{}, err
	}
	// The code_quality rule only belongs on classes that opt into it; drop
	// it everywhere else (docs/workflows repos have no code to gate).
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

func codecovEnabled(c *ClassPreset, t *RepoTarget) bool {
	switch c.Codecov {
	case CodecovEnabled:
		return true
	case CodecovAuto:
		// Only enforce coverage where Codecov actually reports, so the gate
		// never blocks PRs on a repo that uploads no coverage.
		return t.CodecovReports
	default:
		return false
	}
}

func resolveCheckDefs(defs []CheckDef, cc *ChecksConfig) []CheckRef {
	out := make([]CheckRef, 0, len(defs))
	for _, cd := range defs {
		out = append(out, CheckRef{Context: cd.Context, IntegrationID: cc.Integrations[cd.Integration]})
	}
	return out
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
	if r.CopilotCodeReview != nil {
		rs.Rules = append(rs.Rules, APIRule{Type: "copilot_code_review", Parameters: r.CopilotCodeReview})
	}
	if r.CodeQuality != nil {
		rs.Rules = append(rs.Rules, APIRule{Type: "code_quality", Parameters: map[string]any{"severity": r.CodeQuality.Severity}})
	}
	return rs, nil
}

// resolveBypass maps one generic bypass entry to concrete actors. Admins
// always bypass with mode "always"; bots bypass with "always" on master and
// tags (direct writes — the bump commit and the release tag) and "exempt" on
// PR rulesets. An entry that resolves to
// nothing is an error, not a silent drop — a typo in a ruleset template would
// otherwise quietly strip a bypass actor and break the bot's automation.
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

// RenderCodeQL fills the CodeQL advanced-setup workflow template.
func RenderCodeQL(langs []string, querySuite, defaultBranch string) (string, error) {
	raw, err := ReadTemplate("security/codeql.yml.tmpl")
	if err != nil {
		return "", err
	}
	// Build the array prettier-style (`["a", "b"]`, space after comma) so the
	// committed workflow passes the org's `prettier --check` lint-node gate.
	quoted := make([]string, len(langs))
	for i, l := range langs {
		quoted[i] = `"` + l + `"`
	}
	out := string(raw)
	out = strings.ReplaceAll(out, "__LANGUAGES__", "["+strings.Join(quoted, ", ")+"]")
	out = strings.ReplaceAll(out, "__DEFAULT_BRANCH__", defaultBranch)
	out = strings.ReplaceAll(out, "__CODEQL_CONFIG__", codeqlConfigBlock(querySuite))
	return out, nil
}

// codeqlConfigBlock builds the init step's inline `config:` input: the query
// suite plus a filter that drops actions/unpinned-tag.
//
// Both the suite and the filter live inside `config:` (rather than the
// separate `queries:` input) so query-filters apply cleanly — the docs note
// the queries input/config interaction needs a `+` prefix and "instead of"
// semantics that are easy to get wrong.
//
// CodeQL's implicit default suite is selected by OMITTING the queries block
// (passing the literal "default" makes init look for a pack named "default"
// and fail), so emit queries only for an explicit named suite.
//
// actions/unpinned-tag is excluded because the org pins GitHub Actions via
// floating major tags managed by Renovate; the query would flag every
// `uses: action@vN` as unpinned, which is pure noise under that policy.
func codeqlConfigBlock(querySuite string) string {
	var b strings.Builder
	b.WriteString("          config: |\n")
	if querySuite != "" && querySuite != "default" {
		b.WriteString("            queries:\n")
		b.WriteString("              - uses: " + querySuite + "\n")
	}
	b.WriteString("            query-filters:\n")
	b.WriteString("              - exclude:\n")
	b.WriteString("                  id: actions/unpinned-tag")
	return b.String()
}

// Render writes the plan as labelled raw JSON (and verbatim file content for
// content writes) — close to what GitHub receives, no extra formatting.
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
