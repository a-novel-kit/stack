package repocfg

import (
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

// planFor builds a-novel-kit/example's plan for class, with every bot a
// ruleset may name and checks as the discovered required checks.
func planFor(t *testing.T, class *ClassPreset, checks ...CheckRef) *Plan {
	t.Helper()
	plan, err := BuildPlan(&RepoTarget{
		Org: "a-novel-kit", Repo: "example", DefaultBranch: "master", Class: class,
		OrgProfile: &OrgProfile{Org: "a-novel-kit", Bots: map[string]int64{
			"agent": 3549379, "publish": 1734949, "dependencies": 1734926,
		}},
		Discovered: &Discovered{Checks: checks},
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	return plan
}

// rulesetBody returns the body of the plan's named ruleset, or nil.
func rulesetBody(plan *Plan, name string) *APIRuleset {
	for _, op := range plan.Ops {
		if op.RulesetName == name {
			return op.Body.(*APIRuleset)
		}
	}
	return nil
}

func TestCODEOWNERS(t *testing.T) {
	t.Parallel()
	out, err := CODEOWNERS()
	if err != nil {
		t.Fatalf("CODEOWNERS: %v", err)
	}
	if strings.TrimSpace(out) != "* @kushuh" {
		t.Errorf("CODEOWNERS = %q, want %q", out, "* @kushuh")
	}
}

// TestBuildPlanProvisions pins the single op a plan carries for each managed
// path. CODEOWNERS, labels and the conversation lock reach every repo, even a
// minimal preset, whatever its pull request policy, and the retired lock-pr
// copy is deleted. Merge enforcement rides the master ruleset and dependency
// auto-approval rides require-approval, so a bare class gets neither.
func TestBuildPlanProvisions(t *testing.T) {
	t.Parallel()

	labels, err := LoadLabels()
	if err != nil {
		t.Fatalf("LoadLabels: %v", err)
	}
	loadPlan := func(class Class) *Plan {
		preset, err := LoadClass(class)
		if err != nil {
			t.Fatalf("LoadClass(%s): %v", class, err)
		}
		return planFor(t, preset)
	}
	bare := planFor(t, &ClassPreset{})
	collaborators := planFor(t, &ClassPreset{Features: Features{PullRequests: "collaborators_only"}})
	everyone := planFor(t, &ClassPreset{Features: Features{PullRequests: "all"}})
	master := planFor(t, &ClassPreset{Rulesets: ClassRulesets{Master: true, Tags: true}})
	approval := planFor(t, &ClassPreset{Rulesets: ClassRulesets{RequireApproval: true}})
	workflow := map[string]any{"build_type": "workflow"}

	testCases := []struct {
		name         string
		plan         *Plan
		method, path string
		content      string // a substring of the committed file
		body         any
		gated        bool // absent from the bare plan
	}{
		{name: "CODEOWNERS", plan: bare, method: http.MethodPut, path: "/contents/.github/CODEOWNERS", content: "* @kushuh"},
		{name: "Labels", plan: bare, method: http.MethodPut, path: "/labels", body: labels},
		{name: "LockClosed/CollaboratorsOnly", plan: collaborators, method: http.MethodPut, path: "/lock-closed.yaml", content: "/lock"},
		{name: "LockClosed/All", plan: everyone, method: http.MethodPut, path: "/lock-closed.yaml", content: "/lock"},
		{name: "LockPR", plan: bare, method: http.MethodDelete, path: "/lock-pr.yaml"},
		// Factorized callers reference the reusable *-run.yaml engine, not the action.
		{name: "MergeGate", plan: master, method: http.MethodPut, path: "/merge-gate.yaml", content: "merge-gate-run.yaml@", gated: true},
		{name: "ReleaseTrain", plan: master, method: http.MethodPut, path: "/release-train.yaml", content: "release-train-run.yaml@"},
		{name: "Hotfix", plan: master, method: http.MethodPut, path: "/hotfix.yaml", content: "backport-run.yaml@"},
		{name: "EpicRollback", plan: master, method: http.MethodPut, path: "/epic-rollback.yaml", content: "epic-rollback-run.yaml@", gated: true},
		// Already-thin callers still call the action directly.
		{name: "ApprovePR", plan: master, method: http.MethodPut, path: "/approve-pr.yaml", content: "generic-actions/approve-pr@", gated: true},
		{name: "DeriveStatus", plan: master, method: http.MethodPut, path: "/derive-status.yaml", content: "generic-actions/derive-status@", gated: true},
		{name: "RecoverPRs", plan: master, method: http.MethodPut, path: "/recover-prs.yaml", content: "generic-actions/enable-auto-merge@", gated: true},
		{name: "AutoApprove", plan: approval, method: http.MethodPut, path: "/auto-approve-dependabot.yaml", content: "auto-approve-dependabot-run.yaml@", gated: true},
		// GitHub Code Quality bills per active committer and per AI credit, so it follows the class flag.
		{name: "CodeQuality/Off", plan: bare, method: http.MethodPatch, path: "/code-quality/setup", body: map[string]any{"state": "not-configured"}},
		{name: "CodeQuality/On", plan: planFor(t, &ClassPreset{CodeQuality: true}), method: http.MethodPatch, path: "/code-quality/setup", body: map[string]any{"state": "configured"}},
		// Published classes enable Pages through a workflow-backed site.
		{name: "Pages/Library", plan: loadPlan(ClassLibrary), method: http.MethodPost, path: "/pages", body: workflow},
		{name: "Pages/Platform", plan: loadPlan(ClassPlatform), method: http.MethodPost, path: "/pages", body: workflow},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			onPath := func(op Op) bool { return strings.HasSuffix(op.Path, testCase.path) }
			ops := slices.DeleteFunc(slices.Clone(testCase.plan.Ops), func(op Op) bool { return !onPath(op) })
			if len(ops) != 1 || ops[0].Method != testCase.method ||
				!strings.Contains(ops[0].Content, testCase.content) ||
				(testCase.body != nil && !reflect.DeepEqual(ops[0].Body, testCase.body)) {
				t.Fatalf("ops on %s = %+v, want one %s carrying %q / %+v",
					testCase.path, ops, testCase.method, testCase.content, testCase.body)
			}
			if testCase.gated && slices.ContainsFunc(bare.Ops, onPath) {
				t.Errorf("a bare class's plan touches %s", testCase.path)
			}
		})
	}
}

// TestLabelsSatisfyGitHubConstraints guards the constraints GitHub enforces only
// at apply time — the label reconcile runs against live GitHub, so CI never
// exercises them otherwise. A `description` must be <= 100 characters and a color
// a bare 6-hex string. Either one 422s the reconcile for every repo in the run,
// so both are checked here at author time.
func TestLabelsSatisfyGitHubConstraints(t *testing.T) {
	t.Parallel()
	labels, err := LoadLabels()
	if err != nil {
		t.Fatalf("LoadLabels: %v", err)
	}
	hexColor := regexp.MustCompile(`^[0-9a-fA-F]{6}$`)
	for _, l := range labels.Ensure {
		// GitHub counts characters (runes), not bytes — an em-dash is one char.
		if n := utf8.RuneCountInString(l.Description); n > 100 {
			t.Errorf("label %q: description is %d characters, GitHub's limit is 100 — shorten it and move the rationale to a YAML comment", l.Name, n)
		}
		if l.Color != "" && !hexColor.MatchString(l.Color) {
			t.Errorf("label %q: color %q must be a bare 6-hex string (no leading #)", l.Name, l.Color)
		}
	}
}

// TestBuildPlanGovernsReleaseLines pins the release/vX.Y protection: wherever
// releases are tagged, a backport lands like a default-branch change, behind
// master's checks, an approval and a squash, while the agent bot alone writes
// to the line directly, to create it and to push a patch's bump commit.
func TestBuildPlanGovernsReleaseLines(t *testing.T) {
	t.Parallel()

	checks := []CheckRef{{Context: "merge-gate", IntegrationID: 3549379}, {Context: "test", IntegrationID: 15368}}
	if rulesetBody(planFor(t, &ClassPreset{Rulesets: ClassRulesets{Master: true}}, checks...), rulesetReleaseLines) != nil {
		t.Error("an untagged class carries the release-lines ruleset")
	}
	ruleset := rulesetBody(planFor(t, &ClassPreset{Rulesets: ClassRulesets{Master: true, Tags: true}}, checks...), rulesetReleaseLines)
	if ruleset == nil {
		t.Fatal("a tagged class lacks the release-lines ruleset")
	}
	refs := ruleset.Conditions["ref_name"].(map[string]any)
	if ruleset.Target != "branch" || !slices.Equal(refs["include"].([]string), []string{"refs/heads/release/**"}) {
		t.Fatalf("target/ref_name = %s/%v, want branch release/**", ruleset.Target, refs)
	}
	agent := slices.IndexFunc(ruleset.BypassActors, func(a APIBypassActor) bool {
		return a.ActorID != nil && *a.ActorID == 3549379
	})
	if agent < 0 || ruleset.BypassActors[agent].BypassMode != modeAlways {
		t.Fatalf("bypass actors = %+v, want the agent bot in always mode", ruleset.BypassActors)
	}
	rules := map[string]map[string]any{}
	for _, r := range ruleset.Rules {
		rules[r.Type] = r.Parameters
	}
	for _, rule := range []string{"creation", "deletion", "non_fast_forward", "required_signatures"} {
		if _, ok := rules[rule]; !ok {
			t.Errorf("release-lines lacks the %s rule", rule)
		}
	}
	if got := rules["pull_request"]["allowed_merge_methods"]; !slices.Equal(got.([]any), []any{"squash"}) {
		t.Errorf("allowed_merge_methods = %v, want squash only", got)
	}
	if got := rules["pull_request"]["required_approving_review_count"]; got != 1 {
		t.Errorf("required_approving_review_count = %v, want 1", got)
	}
	required := rules["required_status_checks"]["required_status_checks"].([]map[string]any)
	if len(required) != len(checks) || required[0]["context"] != "merge-gate" || required[1]["context"] != "test" {
		t.Errorf("required checks = %v, want master's discovered checks", required)
	}
}

// TestBuildPlanPrunesUnknownRulesets pins the invariant that makes repo config
// derived: the plan names the complete set of rulesets a repo may carry, and
// apply deletes everything else. A ruleset is removed by dropping it from the
// class preset, and one added by hand in the UI is gone at the next reconcile.
func TestBuildPlanPrunesUnknownRulesets(t *testing.T) {
	t.Parallel()

	pruneOf := func(plan *Plan) (Op, int) {
		var at []int
		for i, op := range plan.Ops {
			if op.PruneRulesets {
				at = append(at, i)
			}
		}
		if len(at) != 1 {
			t.Fatalf("want exactly 1 prune op, got %d", len(at))
		}
		return plan.Ops[at[0]], at[0]
	}

	plan := planFor(t, &ClassPreset{Rulesets: ClassRulesets{Master: true, RequireApproval: true, Tags: true}})
	prune, pruneAt := pruneOf(plan)
	var applied []string
	for i, op := range plan.Ops {
		if op.RulesetName == "" {
			continue
		}
		applied = append(applied, op.RulesetName)
		// Pruning runs last, so the repo is never briefly ungoverned.
		if i > pruneAt {
			t.Errorf("ruleset apply at %d follows the prune op at %d", i, pruneAt)
		}
	}
	keep := slices.Sorted(slices.Values(prune.KeepRulesets))
	slices.Sort(applied)
	if !slices.Equal(applied, keep) {
		t.Errorf("keep set %v != applied rulesets %v — a ruleset would be written then immediately deleted", keep, applied)
	}
	if slices.Contains(keep, "codecov") {
		t.Errorf("codecov is still in the keep set %v — it would survive the prune", keep)
	}
	if title := prune.Title(); !strings.Contains(title, "PRUNE") || !strings.Contains(title, "master") {
		t.Errorf("prune title = %q, want it to name the operation and what survives", title)
	}
	if bare, _ := pruneOf(planFor(t, &ClassPreset{})); len(bare.KeepRulesets) != 0 {
		t.Errorf("keep = %v, want empty — an ungoverned class must not retain rulesets", bare.KeepRulesets)
	}
}

// TestSettingsBody pins the settings contract. The pull request creation policy
// follows the class, and a squash merge carries the PR title as its subject,
// which the commit-messages ruleset relies on. The merge-commit title settings
// stay out, since GitHub rejects the whole PATCH with them while merge commits
// are off.
func TestSettingsBody(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{"collaborators_only", "all"} {
		body := SettingsBody(&ClassPreset{Features: Features{PullRequests: policy}})
		if body["pull_request_creation_policy"] != policy || body["squash_merge_commit_title"] != "PR_TITLE" {
			t.Errorf("pull_request_creation_policy/squash_merge_commit_title = %v/%v, want %s/PR_TITLE",
				body["pull_request_creation_policy"], body["squash_merge_commit_title"], policy)
		}
		for _, key := range []string{"merge_commit_title", "merge_commit_message"} {
			if _, ok := body[key]; ok {
				t.Errorf("%s is set; GitHub rejects it while merge commits are disabled", key)
			}
		}
	}
}

// TestBuildPlanDependabotBypassFollowsSecurityUpdates pins where Dependabot may
// bypass commit-messages. GitHub rejects a Dependabot bypass actor on a repo it
// does not run in (HTTP 422, "must be part of the ruleset source"), so the
// entry ships only with classes that enable Dependabot security updates.
func TestBuildPlanDependabotBypassFollowsSecurityUpdates(t *testing.T) {
	t.Parallel()
	for _, securityUpdates := range []bool{true, false} {
		ruleset := rulesetBody(planFor(t, &ClassPreset{
			Rulesets: ClassRulesets{Master: true},
			Security: SecurityToggles{Dependabot: securityUpdates},
		}), "commit-messages")
		if ruleset == nil {
			t.Fatal("plan carries no commit-messages ruleset")
		}
		hasDependabot := slices.ContainsFunc(ruleset.BypassActors, func(a APIBypassActor) bool {
			return a.ActorID != nil && *a.ActorID == dependabotAppID
		})
		if hasDependabot != securityUpdates {
			t.Errorf("security updates %v: Dependabot bypass = %v", securityUpdates, hasDependabot)
		}
		if !securityUpdates && (ruleset.BypassActors == nil || len(ruleset.BypassActors) != 0) {
			t.Errorf("bypass actors = %#v, want an empty list", ruleset.BypassActors)
		}
	}
}
