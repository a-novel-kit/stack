package repocfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// adminRoleID is the built-in "admin" repository role.
const adminRoleID int64 = 5

// dependabotAppID is GitHub's Dependabot App.
const dependabotAppID int64 = 29110

// Ruleset bypass modes.
const (
	modeAlways = "always"
	modeExempt = "exempt"
)

// rulesetMaster is the name of the default-branch ruleset.
const rulesetMaster = "master"

// rulesetTags is the name of the release-tag ruleset.
const rulesetTags = "tags"

// rulesetReleaseLines is the name of the release/vX.Y branch ruleset.
const rulesetReleaseLines = "release-lines"

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

// Op is one API operation the plan performs. Most ops carry a Method, a Path
// and a Body; the other fields mark the ops apply reconciles in a way of their
// own.
type Op struct {
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
	Body   any    `json:"body,omitempty"`
	// Content is the text of a managed file the op writes.
	Content string `json:"content,omitempty"`
	// RulesetName marks a ruleset op, reconciled by name: apply POSTs a
	// ruleset the repo lacks and PUTs .../{id} over one it has.
	RulesetName string `json:"ruleset,omitempty"`
	// PruneRulesets marks the op that reconciles the whole ruleset set: apply
	// deletes every live ruleset KeepRulesets does not name.
	PruneRulesets bool `json:"prune_rulesets,omitempty"`
	// KeepRulesets names the rulesets the prune keeps.
	KeepRulesets []string `json:"keep_rulesets,omitempty"`
	// Secret names the scoped org secret the op grants (PUT) or revokes
	// (DELETE) the repo's access to. Apply appends the repo's numeric ID to
	// Path.
	Secret string `json:"secret,omitempty"`
	// Teams is the complete set of tier-team grants the repo holds. Apply
	// creates a missing team, grants each one, and removes the grant of any
	// other tier team.
	Teams []TeamGrant `json:"teams,omitempty"`
	// Governance names the privileged secrets the repo's governance
	// environment holds, and the Bitwarden project apply copies them from.
	Governance *Governance `json:"governance,omitempty"`
	// Reviewers lists the slugs of the teams whose approval an environment's
	// jobs wait for. Apply resolves them to the team IDs GitHub expects.
	Reviewers []string `json:"reviewers,omitempty"`
}

// TeamGrant is one team's role on a repository. Team is an area team,
// `<tier>-<class>`, nested under Parent, its tier team. A grant without Parent
// goes to the tier team itself.
type TeamGrant struct {
	Team       string `json:"team"`
	Parent     string `json:"parent"`
	Permission string `json:"permission"`
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
	if o.Secret != "" {
		return o.Method + " " + o.Path + "/{repository_id}"
	}
	if o.Governance != nil {
		return fmt.Sprintf("RECONCILE environment secrets (%s) — hold only: %s", o.Path, strings.Join(o.Governance.Secrets, ", "))
	}
	if len(o.Teams) > 0 {
		grants := make([]string, 0, len(o.Teams))
		for _, g := range o.Teams {
			grants = append(grants, g.Team+"="+g.Permission)
		}
		return fmt.Sprintf("RECONCILE tier teams (%s) — grant only: %s", o.Path, strings.Join(grants, ", "))
	}
	return o.Method + " " + o.Path
}

// DeploymentPolicy is one branch or tag pattern an environment admits.
type DeploymentPolicy struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// releases reports whether the repo's workflows release through the release
// environment.
func releases(t *RepoTarget) bool {
	return slices.Contains(t.Discovered.Environments, "release")
}

// Plan is the ordered set of operations to bring a repo to desired state.
type Plan struct {
	Ops []Op
}

// BuildPlan assembles the desired-state operations for a repo. Managed files
// come before the rulesets that require their checks, and the prune comes last.
func BuildPlan(t *RepoTarget) (*Plan, error) {
	repoPath := fmt.Sprintf("repos/%s/%s", t.Org, t.Repo)
	uniform, err := uniformOps(t, repoPath)
	if err != nil {
		return nil, err
	}
	workflows, err := workflowOps(t.Class, repoPath)
	if err != nil {
		return nil, err
	}
	rulesets, err := rulesetOps(t, repoPath)
	if err != nil {
		return nil, err
	}
	return &Plan{Ops: slices.Concat(
		settingsOps(t.Class, repoPath),
		uniform,
		securityOps(t.Class, repoPath),
		environmentOps(t, repoPath),
		scopedSecretOps(t),
		workflows,
		[]Op{pagesOp(t.Class, repoPath)},
		rulesets,
	)}, nil
}

// settingsOps sets the repository settings and, when the class names it, the
// Dependabot alerts toggle.
func settingsOps(c *ClassPreset, repoPath string) []Op {
	ops := []Op{{Method: http.MethodPatch, Path: repoPath, Body: SettingsBody(c)}}
	// Dependabot alerts have an endpoint of their own, outside the repository's
	// security_and_analysis block. A preset that omits the field leaves the
	// live setting as it is.
	if c.Security.DependabotAlerts != nil {
		method := http.MethodDelete
		if *c.Security.DependabotAlerts {
			method = http.MethodPut
		}
		ops = append(ops, Op{Method: method, Path: repoPath + "/vulnerability-alerts"})
	}
	return ops
}

// BotDispatchRepos maps each org to the repo hosting its bot-comment dispatcher
// workflow. Each org has its own Agent App, whose key is a secret of the
// dispatcher's governance environment, so each org has its own dispatcher.
var BotDispatchRepos = map[string]string{
	"a-novel-kit": "a-novel-kit/stack",
	"a-novel":     "a-novel/.github",
}

// uniformOps provisions what every repo carries whatever its class:
// CODEOWNERS, the label vocabulary, and the tier teams' grants.
func uniformOps(t *RepoTarget, repoPath string) ([]Op, error) {
	// CODEOWNERS routes review requests automatically. It is written to
	// .github/, which GitHub honors over a root copy, and apply removes any
	// stray root file so a repo never carries two.
	codeowners, err := CODEOWNERS()
	if err != nil {
		return nil, err
	}
	// Labels are reconciled at apply time: ensured labels upserted, retired
	// ones deleted.
	labels, err := LoadLabels()
	if err != nil {
		return nil, err
	}
	// Each contributor tier holds its role here through the area team of this
	// repo's class.
	teams, err := LoadTeams()
	if err != nil {
		return nil, err
	}
	grants := make([]TeamGrant, 0, len(teams.Tiers))
	for _, tier := range teams.Tiers {
		grants = append(grants, TeamGrant{Team: tier.Name + "-" + string(t.Class.Class), Parent: tier.Name, Permission: tier.Permission})
	}
	// Every member may post bot comments. A comment dispatches from this repo
	// with the member's own token, which needs Write here, and a tier team's
	// grant reaches all of its area teams.
	if BotDispatchRepos[t.Org] == t.Org+"/"+t.Repo {
		for _, tier := range teams.Tiers {
			grants = append(grants, TeamGrant{Team: tier.Name, Permission: "push"})
		}
	}
	return []Op{
		{Method: http.MethodPut, Path: repoPath + "/contents/.github/CODEOWNERS", Content: codeowners},
		{Method: http.MethodPut, Path: repoPath + "/labels", Body: labels},
		{Method: http.MethodPut, Path: repoPath + "/teams", Teams: grants},
	}, nil
}

// securityOps asserts the code analysis products and the workflow token's
// limits.
func securityOps(c *ClassPreset, repoPath string) []Op {
	// GitHub Code Quality bills per active committer and per AI credit on every
	// default-branch push, so the product follows the same class flag as the
	// master ruleset's code_quality rule.
	codeQuality := "not-configured"
	if c.CodeQuality {
		codeQuality = "configured"
	}
	workflowPermissions := map[string]any{"default_workflow_permissions": "read", "can_approve_pull_request_reviews": false}
	forkApproval := map[string]any{"approval_policy": "all_external_contributors"}
	return []Op{
		// Code scanning stays off, asserted on every update. The lint-semgrep
		// and scan-secrets jobs of main.yaml do the static analysis, as
		// required checks. The assertion undoes an org-level "enable for all
		// repositories" toggle.
		{Method: http.MethodPatch, Path: repoPath + "/code-scanning/default-setup", Body: map[string]any{"state": "not-configured"}},
		{Method: http.MethodPatch, Path: repoPath + "/code-quality/setup", Body: map[string]any{"state": codeQuality}},
		// A workflow token is read-only unless its job asks for more, and it
		// never approves a pull request. An outside contributor's fork runs
		// nothing until a maintainer approves it.
		{Method: http.MethodPut, Path: repoPath + "/actions/permissions/workflow", Body: workflowPermissions},
		{Method: http.MethodPut, Path: repoPath + "/actions/permissions/fork-pr-contributor-approval", Body: forkApproval},
		// A published release keeps its tag and assets: no key that bypasses
		// the tags ruleset can move a released tag to other code.
		{Method: http.MethodPut, Path: repoPath + "/immutable-releases"},
	}
}

// environmentOps writes the release, governance and sourcemaps environments the
// repo's workflows use. They open only on refs a ruleset guards: the default
// branch, and release lines and version tags where the tags rulesets limit who
// creates them. A feature branch's workflow never unlocks them.
func environmentOps(t *RepoTarget, repoPath string) []Op {
	policies := []DeploymentPolicy{{Name: t.DefaultBranch, Type: "branch"}}
	if t.Class.Rulesets.Tags {
		policies = append(policies, DeploymentPolicy{Name: "release/v*", Type: "branch"}, DeploymentPolicy{Name: "v*", Type: "tag"})
	}

	var ops []Op
	// The release App's key is a secret of the release environment, and every
	// cut waits there for core or the repo's owners, who may approve a run they
	// dispatched. No bot can approve, so no bot can release.
	if releases(t) {
		ops = append(ops, environmentWrites(repoPath, "release", policies, "core", "owners-"+string(t.Class.Class))...)
	}
	// The privileged keys are secrets of the governance environment alone, so
	// a branch's copy of a workflow cannot read them. It holds the privileged
	// secrets the repo's workflows name.
	governance := t.OrgProfile.Governance
	privileged := func(name string) bool { return slices.Contains(t.OrgProfile.Governance.Secrets, name) }
	governance.Secrets = slices.DeleteFunc(slices.Clone(t.Discovered.Secrets), func(name string) bool { return !privileged(name) })
	if len(governance.Secrets) > 0 {
		ops = append(ops, environmentWrites(repoPath, "governance", policies)...)
		ops = append(ops, Op{Path: repoPath + "/environments/governance/secrets", Governance: &governance})
	}
	// A platform's tag build reads its source-map upload key, set by hand, from
	// the sourcemaps environment. No reviewer gates it, so a release still takes
	// one approval: the cut's.
	if slices.Contains(t.Discovered.Environments, "sourcemaps") {
		ops = append(ops, environmentWrites(repoPath, "sourcemaps", policies)...)
	}
	return ops
}

// environmentWrites writes the environment name, reviewed by the reviewers
// teams and open only on the refs policies admit.
func environmentWrites(repoPath, name string, policies []DeploymentPolicy, reviewers ...string) []Op {
	body := map[string]any{"deployment_branch_policy": map[string]any{"protected_branches": false, "custom_branch_policies": true}}
	return []Op{
		{Method: http.MethodPut, Path: repoPath + "/environments/" + name, Reviewers: reviewers, Body: body},
		{Method: http.MethodPut, Path: repoPath + "/environments/" + name + "/deployment-branch-policies", Body: policies},
	}
}

// scopedSecretOps grants each scoped org secret to the repo exactly when its
// workflows name it, and revokes it otherwise.
func scopedSecretOps(t *RepoTarget) []Op {
	ops := make([]Op, 0, len(t.OrgProfile.ScopedSecrets))
	for _, name := range t.OrgProfile.ScopedSecrets {
		method := http.MethodDelete
		if slices.Contains(t.Discovered.Secrets, name) {
			method = http.MethodPut
		}
		ops = append(ops, Op{Method: method, Path: fmt.Sprintf("orgs/%s/actions/secrets/%s/repositories", t.Org, name), Secret: name})
	}
	return ops
}

// workflowFiles collects the governance workflow writes and deletions of a
// plan, in order.
type workflowFiles struct {
	dir string
	ops []Op
	err error
}

// put writes each named workflow from its template.
func (w *workflowFiles) put(names ...string) {
	for _, name := range names {
		content, err := ReadTemplate("governance/" + name)
		w.err = errors.Join(w.err, err)
		w.ops = append(w.ops, Op{Method: http.MethodPut, Path: w.dir + name, Content: string(content)})
	}
}

// drop deletes each named workflow, one an earlier reconcile left that the
// class no longer ships.
func (w *workflowFiles) drop(names ...string) {
	for _, name := range names {
		w.ops = append(w.ops, Op{Method: http.MethodDelete, Path: w.dir + name})
	}
}

// workflowOps writes the governance workflows the class ships and deletes the
// ones it does not.
func workflowOps(c *ClassPreset, repoPath string) ([]Op, error) {
	w := &workflowFiles{dir: repoPath + "/contents/.github/workflows/"}
	// The master ruleset gates merges, and these workflows run that gate.
	if c.Rulesets.Master {
		w.put(
			"merge-gate.yaml",
			"epic-freeze.yaml",
			"approve-pr.yaml",
			"derive-status.yaml",
			"epic-rollback.yaml",
			"recover-prs.yaml",        // re-arms pull requests the merge queue drops on a timeout
			"check-dependencies.yaml", // posts a check the master ruleset requires
			"feature-env.yaml",        // removes the with-feature-env label a push made stale
			"review-gate.yaml",        // posts a check the master ruleset requires
			"pr-relay.yaml",           // carries review and merge-queue events to the gates on the default branch
		)
	}
	// A class without the tags ruleset deletes the release callers, so a class
	// change leaves no release mechanics behind.
	if c.Rulesets.Master && c.Rulesets.Tags {
		w.put("release-train.yaml", "hotfix.yaml")
	} else {
		w.drop("release-train.yaml", "hotfix.yaml")
	}
	// Every repo locks a conversation once its issue or pull request closes.
	// The plan deletes lock-pr.yaml, which locked pull requests as they opened
	// and so blocked the approval bot.
	w.put("lock-closed.yaml")
	w.drop("lock-pr.yaml")
	// Every repo reports its default-branch failures and its releases to Discord.
	w.put("notify.yaml")
	// Reviews run a pull request's own workflow copy, so they post from pr-relay's runs.
	w.put("notify-reviews.yaml")
	// Only classes whose images no deployment pins by digest delete the images nothing names.
	if c.CleanImages {
		w.put("clean-images.yaml")
	} else {
		w.drop("clean-images.yaml")
	}
	// Every repo assigns a person's pull request to its author.
	w.put("auto-assign-author.yaml")
	// The trusted dependency bots' PRs are auto-approved wherever
	// require-approval would otherwise hold their version bumps for a human.
	if c.Rulesets.RequireApproval {
		w.put("auto-approve-dependabot.yaml")
	}
	return w.ops, w.err
}

// pagesOp enables the Pages site, built by a workflow, or deletes it.
func pagesOp(c *ClassPreset, repoPath string) Op {
	if c.Pages {
		return Op{Method: http.MethodPost, Path: repoPath + "/pages", Body: map[string]any{"build_type": "workflow"}}
	}
	return Op{Method: http.MethodDelete, Path: repoPath + "/pages"}
}

// rulesetOps writes the rulesets the class turns on, reconciled by name, then
// prunes every other one.
//
// The master ruleset gates exactly the discovered checks, set wholesale.
// commit-messages governs the same branch, so it ships alongside master.
// Release lines take master's checks, and exist wherever releases are tagged.
func rulesetOps(t *RepoTarget, repoPath string) ([]Op, error) {
	c := t.Class
	wanted := []struct {
		name   string
		on     bool
		checks []CheckRef
	}{
		{rulesetMaster, c.Rulesets.Master, t.Discovered.Checks},
		{"commit-messages", c.Rulesets.Master, nil},
		{"require-approval", c.Rulesets.RequireApproval, nil},
		{rulesetTags, c.Rulesets.Tags, nil},
		{rulesetReleaseLines, c.Rulesets.Tags, t.Discovered.Checks},
	}
	var ops []Op
	keep := make([]string, 0, len(wanted))
	for _, w := range wanted {
		if !w.on {
			continue
		}
		op, err := rulesetOp(w.name, t, w.checks)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
		keep = append(keep, w.name)
	}

	// A repo's ruleset set is derived from the class preset, the org profile
	// and discovery. Any ruleset this plan does not name is drift, dropped from
	// the templates or added by hand in the UI, and apply deletes it.
	//
	// Pruning runs last, once every desired ruleset has been written, so a repo
	// is never momentarily left ungoverned.
	return append(ops, Op{PruneRulesets: true, KeepRulesets: keep, Path: repoPath + "/rulesets"}), nil
}

// rulesetOp writes the named ruleset from its template, with the bypass actors
// the repo's class and workflows allow and checks as its required checks.
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
	// GitHub accepts Dependabot as a bypass actor only on repos it runs in,
	// which are the classes that enable its security updates.
	if !t.Class.Security.Dependabot {
		spec.Bypass = slices.DeleteFunc(spec.Bypass, func(entry string) bool { return entry == "dependabot" })
	}
	// The release App acts only in a repo whose workflows release.
	if !releases(t) {
		spec.Bypass = slices.DeleteFunc(spec.Bypass, func(entry string) bool { return entry == "release" })
	}
	if t.Class.Rulesets.AdminsOnly {
		spec.Bypass = slices.DeleteFunc(spec.Bypass, func(entry string) bool { return entry != "admins" })
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
		// An empty list, never null: a ruleset may have no bypass actors.
		BypassActors: []APIBypassActor{},
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
		params := statusCheckParams(r.RequiredStatusChecks, checks)
		rs.Rules = append(rs.Rules, APIRule{Type: "required_status_checks", Parameters: params})
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
	if r.CommitMessagePattern != nil {
		rs.Rules = append(rs.Rules, APIRule{Type: "commit_message_pattern", Parameters: r.CommitMessagePattern})
	}
	return rs, nil
}

// statusCheckParams is the parameters of a required_status_checks rule that
// requires checks, each bound to the App that posts it when it names one.
func statusCheckParams(params *RSCParams, checks []CheckRef) map[string]any {
	required := make([]map[string]any, 0, len(checks))
	for _, c := range checks {
		check := map[string]any{"context": c.Context}
		if c.IntegrationID != 0 {
			check["integration_id"] = c.IntegrationID
		}
		required = append(required, check)
	}
	return map[string]any{
		"strict_required_status_checks_policy": params.Strict,
		"do_not_enforce_on_create":             params.DoNotEnforceOnCreate,
		"required_status_checks":               required,
	}
}

// resolveBypass maps one generic bypass entry to concrete actors. Admins
// bypass in mode "always". Bots bypass in mode "always" on master, tags and
// release lines, where they write directly, and in mode "exempt" elsewhere.
// Dependabot is GitHub's own App, so its ID is the same in every org. An entry
// that resolves to no actor is an error, which catches a typo in a template.
func resolveBypass(entry, rulesetName string, org *OrgProfile) ([]APIBypassActor, error) {
	botMode := modeExempt
	if slices.Contains([]string{rulesetMaster, rulesetTags, rulesetReleaseLines}, rulesetName) {
		botMode = modeAlways
	}
	switch entry {
	case "admins":
		role := adminRoleID
		return []APIBypassActor{
			{ActorID: nil, ActorType: "OrganizationAdmin", BypassMode: modeAlways},
			{ActorID: &role, ActorType: "RepositoryRole", BypassMode: modeAlways},
		}, nil
	case "dependabot":
		id := dependabotAppID
		return []APIBypassActor{{ActorID: &id, ActorType: "Integration", BypassMode: botMode}}, nil
	}
	if id, ok := org.Bots[entry]; ok {
		return []APIBypassActor{{ActorID: &id, ActorType: "Integration", BypassMode: botMode}}, nil
	}
	return nil, fmt.Errorf("bypass entry %q in ruleset %q resolves to no actor "+
		"(expected admins, dependabot or an org bot key from orgs/<org>.yaml)", entry, rulesetName)
}

// SettingsBody is the PATCH /repos body for general + merge + security.
func SettingsBody(c *ClassPreset) map[string]any {
	return map[string]any{
		"has_issues":                   c.Features.Issues,
		"has_wiki":                     c.Features.Wiki,
		"has_projects":                 c.Features.Projects,
		"has_discussions":              c.Features.Discussions,
		"pull_request_creation_policy": c.Features.PullRequests,
		"allow_squash_merge":           c.Merge.Squash,
		"allow_merge_commit":           c.Merge.MergeCommit,
		"allow_rebase_merge":           c.Merge.Rebase,
		"allow_auto_merge":             c.Merge.AutoMerge,
		"delete_branch_on_merge":       c.Merge.DeleteBranchOnMerge,
		"allow_update_branch":          c.Merge.AllowUpdateBranch,
		"web_commit_signoff_required":  c.Merge.SignoffRequired,
		// A squash merge lands with the PR title as its subject, so the
		// commit-messages ruleset judges the title reviewers saw. The body sends
		// no merge-commit title settings: GitHub rejects the whole PATCH with
		// them while merge commits are disabled.
		"squash_merge_commit_title":   "PR_TITLE",
		"squash_merge_commit_message": "COMMIT_MESSAGES",
		"security_and_analysis":       SecurityBlock(c),
	}
}

// SecurityBlock is the security_and_analysis sub-object.
func SecurityBlock(c *ClassPreset) map[string]any {
	return map[string]any{
		"secret_scanning":                 securityStatus(c.Security.SecretScanning),
		"secret_scanning_push_protection": securityStatus(c.Security.PushProtection),
		"dependabot_security_updates":     securityStatus(c.Security.Dependabot),
	}
}

// securityStatus is the {"status": ...} object a security_and_analysis feature
// takes.
func securityStatus(enabled bool) map[string]string {
	if enabled {
		return map[string]string{"status": "enabled"}
	}
	return map[string]string{"status": "disabled"}
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
		// An op without a request body, such as a deletion or the prune, prints
		// its title alone.
		if op.PruneRulesets || op.Body == nil {
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

// RenderJSON writes the plan as a JSON array of ops, so the exact operations
// can be inspected or applied verbatim.
func (p *Plan) RenderJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p.Ops)
}
