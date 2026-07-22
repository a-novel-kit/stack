package repocfg

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRenderCodeQL(t *testing.T) {
	t.Parallel()

	t.Run("named suite emits queries + filter in config", func(t *testing.T) {
		t.Parallel()
		out, err := RenderCodeQL([]string{"go", "actions"}, "security-and-quality", "master")
		if err != nil {
			t.Fatalf("RenderCodeQL: %v", err)
		}
		for _, want := range []string{
			`language: ["go", "actions"]`,
			`branches: ["master"]`,
			"config: |",
			"queries:",
			"- uses: security-and-quality",
			"query-filters:",
			"- exclude:",
			"id: actions/unpinned-tag",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in:\n%s", want, out)
			}
		}
		// The bare placeholder must be fully substituted.
		if strings.Contains(out, "__") {
			t.Errorf("unsubstituted placeholder remains:\n%s", out)
		}
	})

	t.Run("default suite omits queries but keeps the filter", func(t *testing.T) {
		t.Parallel()
		out, err := RenderCodeQL([]string{"go"}, "", "main")
		if err != nil {
			t.Fatalf("RenderCodeQL: %v", err)
		}
		if strings.Contains(out, "queries:") {
			t.Errorf("default suite must omit queries:\n%s", out)
		}
		for _, want := range []string{"config: |", "query-filters:", "id: actions/unpinned-tag"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in:\n%s", want, out)
			}
		}
	})
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

// TestBuildPlanProvisionsCODEOWNERS checks that the CODEOWNERS file is committed
// to .github/ for every repo regardless of class — a minimal preset (everything
// off) still emits the op.
func TestBuildPlanProvisionsCODEOWNERS(t *testing.T) {
	t.Parallel()
	plan, err := BuildPlan(&RepoTarget{
		Org:        "a-novel-kit",
		Repo:       "example",
		Class:      &ClassPreset{},
		Discovered: &Discovered{},
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	var found bool
	for _, op := range plan.Ops {
		if op.Method == http.MethodPut && strings.HasSuffix(op.Path, "/contents/.github/CODEOWNERS") {
			found = true
			if !strings.Contains(op.Content, "* @kushuh") {
				t.Errorf("CODEOWNERS op content = %q, want it to contain %q", op.Content, "* @kushuh")
			}
		}
	}
	if !found {
		t.Errorf("BuildPlan emitted no .github/CODEOWNERS op; ops = %+v", plan.Ops)
	}
}

// TestBuildPlanProvisionsLabels checks that the canonical label set is
// provisioned to every repo regardless of class — like CODEOWNERS — and carries
// the new `meta` label plus the `triage` retirement.
func TestBuildPlanProvisionsLabels(t *testing.T) {
	t.Parallel()
	plan, err := BuildPlan(&RepoTarget{
		Org:        "a-novel-kit",
		Repo:       "example",
		Class:      &ClassPreset{},
		Discovered: &Discovered{},
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	var cfg *LabelsConfig
	for _, op := range plan.Ops {
		if op.Method == http.MethodPut && strings.HasSuffix(op.Path, "/labels") {
			c, ok := op.Body.(*LabelsConfig)
			if !ok {
				t.Fatalf("labels op body = %T, want *LabelsConfig", op.Body)
			}
			cfg = c
		}
	}
	if cfg == nil {
		t.Fatalf("BuildPlan emitted no /labels op; ops = %+v", plan.Ops)
	}
	var hasMeta bool
	for _, l := range cfg.Ensure {
		if l.Name == "meta" {
			hasMeta = true
		}
	}
	if !hasMeta {
		t.Error("labels op ensure set missing `meta`")
	}
	if !slices.Contains(cfg.Retire, "triage") {
		t.Errorf("labels op retire set missing `triage`; got %v", cfg.Retire)
	}
}

// TestLabelsSatisfyGitHubConstraints guards the constraints GitHub enforces only
// at apply time — the label reconcile runs against live GitHub, so CI never
// exercises them otherwise. A `description` must be <= 100 characters and a color
// a bare 6-hex string. A 312-char description once 422'd the label reconcile on
// every repo (fixed in cli/v1.4.5); this keeps that class of failure at author
// time instead of surfacing during `a-novel repo update`.
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

// TestBuildPlanProvisionsMergeGateWorkflows checks that the merge-enforcement
// workflows ride along wherever the master ruleset gates merges — the merge-gate
// runner and the admin approve-pr override — pinned to the workflows action, and
// that a class WITHOUT the master ruleset gets neither.
func TestBuildPlanProvisionsMergeGateWorkflows(t *testing.T) {
	t.Parallel()

	plan, err := BuildPlan(&RepoTarget{
		Org:           "a-novel-kit",
		Repo:          "example",
		DefaultBranch: "master",
		Class:         &ClassPreset{Rulesets: ClassRulesets{Master: true}},
		OrgProfile: &OrgProfile{Org: "a-novel-kit", Bots: map[string]int64{
			"dependencies": 1734926, "publish": 1734949, "agent": 3549379,
		}},
		Discovered: &Discovered{},
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	want := map[string]string{
		// Factorized: the thin caller references the reusable *-run.yaml engine, not the action.
		"/contents/.github/workflows/merge-gate.yaml":    "merge-gate-run.yaml@",
		"/contents/.github/workflows/release-train.yaml": "release-train-run.yaml@",
		"/contents/.github/workflows/hotfix.yaml":        "hotfix-run.yaml@",
		"/contents/.github/workflows/epic-rollback.yaml": "epic-rollback-run.yaml@",
		// Not factorized (already thin): still call the action directly.
		"/contents/.github/workflows/approve-pr.yaml":    "generic-actions/approve-pr@",
		"/contents/.github/workflows/derive-status.yaml": "generic-actions/derive-status@",
	}
	for suffix, ref := range want {
		var op *Op
		for i := range plan.Ops {
			if plan.Ops[i].Method == http.MethodPut && strings.HasSuffix(plan.Ops[i].Path, suffix) {
				op = &plan.Ops[i]
			}
		}
		if op == nil {
			t.Errorf("BuildPlan emitted no op for %s", suffix)
			continue
		}
		if !strings.Contains(op.Content, ref) {
			t.Errorf("%s content missing action ref %q", suffix, ref)
		}
	}

	// A class without the master ruleset gets neither workflow.
	bare, err := BuildPlan(&RepoTarget{
		Org: "a-novel-kit", Repo: "example",
		Class: &ClassPreset{}, Discovered: &Discovered{},
	})
	if err != nil {
		t.Fatalf("BuildPlan(bare): %v", err)
	}
	for _, op := range bare.Ops {
		if strings.Contains(op.Path, "merge-gate.yaml") || strings.Contains(op.Path, "approve-pr.yaml") ||
			strings.Contains(op.Path, "derive-status.yaml") {
			t.Errorf("master-less class must not get governance workflows; got %s", op.Path)
		}
	}
}

// TestBuildPlanProvisionsAutoApprove checks that the dependency-bot auto-approval
// workflow is pushed wherever require-approval is active, and nowhere else.
func TestBuildPlanProvisionsAutoApprove(t *testing.T) {
	t.Parallel()

	plan, err := BuildPlan(&RepoTarget{
		Org:           "a-novel-kit",
		Repo:          "example",
		DefaultBranch: "master",
		Class:         &ClassPreset{Rulesets: ClassRulesets{RequireApproval: true}},
		OrgProfile: &OrgProfile{Org: "a-novel-kit", Bots: map[string]int64{
			"dependencies": 1734926, "publish": 1734949, "agent": 3549379,
		}},
		Discovered: &Discovered{},
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	var op *Op
	for i := range plan.Ops {
		if plan.Ops[i].Method == http.MethodPut &&
			strings.HasSuffix(plan.Ops[i].Path, "/contents/.github/workflows/auto-approve-dependabot.yaml") {
			op = &plan.Ops[i]
		}
	}
	if op == nil {
		t.Fatal("BuildPlan emitted no auto-approve workflow op under require-approval")
	}
	if !strings.Contains(op.Content, "auto-approve-dependabot-run.yaml@") {
		t.Error("auto-approve content missing the reusable-workflow ref")
	}

	// Without require-approval, the auto-approve workflow is not pushed.
	bare, err := BuildPlan(&RepoTarget{
		Org: "a-novel-kit", Repo: "example",
		Class: &ClassPreset{}, Discovered: &Discovered{},
	})
	if err != nil {
		t.Fatalf("BuildPlan(bare): %v", err)
	}
	for _, op := range bare.Ops {
		if strings.Contains(op.Path, "auto-approve") {
			t.Errorf("auto-approve pushed without require-approval: %s", op.Path)
		}
	}
}

// contextsOf extracts the context names from a CheckRef slice, preserving order.
func contextsOf(checks []CheckRef) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Context
	}
	return out
}

// TestBuildPlanRetiresRulesets pins the half of a removal that is easy to forget.
// Apply reconciles rulesets BY NAME — PUT when one exists, POST when it does not —
// so deleting a template only stops repocfg from MANAGING that ruleset. Whatever is
// already live keeps enforcing, unreviewed, on every repo that carries it. The plan
// must therefore carry an explicit delete for each retired name.
func TestBuildPlanRetiresRulesets(t *testing.T) {
	t.Parallel()
	plan, err := BuildPlan(&RepoTarget{
		Org:        "a-novel-kit",
		Repo:       "example",
		Class:      &ClassPreset{},
		Discovered: &Discovered{},
	})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	retired, err := LoadRetired()
	if err != nil {
		t.Fatalf("LoadRetired: %v", err)
	}
	if len(retired.Rulesets) == 0 {
		t.Fatal("retired.rulesets is empty — every assertion below would pass vacuously")
	}
	for _, name := range retired.Rulesets {
		var op *Op
		for i := range plan.Ops {
			if plan.Ops[i].RulesetName == name && plan.Ops[i].Retire {
				op = &plan.Ops[i]
			}
		}
		if op == nil {
			t.Errorf("no retire op for ruleset %q — the live one would survive every reconcile", name)
			continue
		}
		if op.Path != "repos/a-novel-kit/example/rulesets" {
			t.Errorf("retire op path = %q, want the repo's rulesets collection", op.Path)
		}
		if op.Body != nil {
			t.Errorf("retire op for %q carries a body; a delete must not send one", name)
		}
		if !strings.HasPrefix(op.Title(), "DELETE ") {
			t.Errorf("retire op title = %q, want it to read as a deletion", op.Title())
		}
	}
	// A name must never be both applied and retired in one plan: the two ops would
	// race, and whichever ran last would decide whether the gate survived.
	for _, op := range plan.Ops {
		if !op.Retire && op.RulesetName != "" && slices.Contains(retired.Rulesets, op.RulesetName) {
			t.Errorf("ruleset %q is both applied and retired in the same plan", op.RulesetName)
		}
	}
}
