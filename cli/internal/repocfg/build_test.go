package repocfg

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

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
// provisioned to every repo regardless of class.
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
	var hasMeta, hasAppendOnlyOverride bool
	for _, l := range cfg.Ensure {
		if l.Name == "meta" {
			hasMeta = true
		}
		if l.Name == "append-only-override" {
			hasAppendOnlyOverride = true
		}
	}
	if !hasMeta {
		t.Error("labels op ensure set missing `meta`")
	}
	if !hasAppendOnlyOverride {
		t.Error("labels op ensure set missing `append-only-override`")
	}
	if !slices.Contains(cfg.Retire, "triage") {
		t.Errorf("labels op retire set missing `triage`; got %v", cfg.Retire)
	}
}

// TestBuildPlanProvisionsPagesByDefault checks that published repository
// classes enable Pages through a workflow-backed site.
func TestBuildPlanProvisionsPagesByDefault(t *testing.T) {
	t.Parallel()

	for _, class := range []Class{ClassLibrary, ClassPlatform} {
		t.Run(string(class), func(t *testing.T) {
			t.Parallel()

			preset, err := LoadClass(class)
			if err != nil {
				t.Fatalf("LoadClass(%s): %v", class, err)
			}
			plan, err := BuildPlan(&RepoTarget{
				Org:   "a-novel-kit",
				Repo:  "example",
				Class: preset,
				OrgProfile: &OrgProfile{Org: "a-novel-kit", Bots: map[string]int64{
					"agent": 1, "dependencies": 2, "publish": 3,
				}},
				Discovered: &Discovered{},
			})
			if err != nil {
				t.Fatalf("BuildPlan(%s): %v", class, err)
			}

			var pagesOps []Op
			for _, op := range plan.Ops {
				if op.Method == http.MethodPost && strings.HasSuffix(op.Path, "/pages") {
					pagesOps = append(pagesOps, op)
				}
			}
			if len(pagesOps) != 1 {
				t.Fatalf("BuildPlan(%s) emitted %d Pages ops, want 1; ops = %+v", class, len(pagesOps), plan.Ops)
			}
			body, ok := pagesOps[0].Body.(map[string]any)
			if !ok {
				t.Fatalf("Pages body = %T, want map[string]any", pagesOps[0].Body)
			}
			if got := body["build_type"]; got != "workflow" {
				t.Errorf("Pages build_type = %v, want workflow", got)
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
		Class:         &ClassPreset{Rulesets: ClassRulesets{Master: true, Tags: true}},
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

// TestBuildPlanPrunesUnknownRulesets pins the invariant that makes repo config
// derived: the plan names the complete set of rulesets a repo may carry, and
// apply deletes everything else. A ruleset is removed by dropping it from the
// class preset, and one added by hand in the UI is gone at the next reconcile.
func TestBuildPlanPrunesUnknownRulesets(t *testing.T) {
	t.Parallel()

	build := func(t *testing.T, rs ClassRulesets) *Plan {
		t.Helper()
		plan, err := BuildPlan(&RepoTarget{
			Org:        "a-novel-kit",
			Repo:       "example",
			Class:      &ClassPreset{Rulesets: rs},
			OrgProfile: &OrgProfile{Org: "a-novel-kit", Bots: map[string]int64{"agent": 1, "publish": 2, "dependencies": 3}},
			Checks:     &ChecksConfig{},
			Discovered: &Discovered{},
		})
		if err != nil {
			t.Fatalf("BuildPlan: %v", err)
		}
		return plan
	}

	prunes := func(t *testing.T, plan *Plan) Op {
		t.Helper()
		var found []Op
		for _, op := range plan.Ops {
			if op.PruneRulesets {
				found = append(found, op)
			}
		}
		if len(found) != 1 {
			t.Fatalf("want exactly 1 prune op, got %d", len(found))
		}
		return found[0]
	}

	t.Run("keeps exactly what the plan applies", func(t *testing.T) {
		t.Parallel()
		plan := build(t, ClassRulesets{Master: true, RequireApproval: true, Tags: true})
		var applied []string
		for _, op := range plan.Ops {
			if op.RulesetName != "" {
				applied = append(applied, op.RulesetName)
			}
		}
		keep := prunes(t, plan).KeepRulesets
		slices.Sort(applied)
		slices.Sort(keep)
		if !slices.Equal(applied, keep) {
			t.Errorf("keep set %v != applied rulesets %v — a ruleset would be written then immediately deleted", keep, applied)
		}
	})

	t.Run("prunes last, so the repo is never briefly ungoverned", func(t *testing.T) {
		t.Parallel()
		plan := build(t, ClassRulesets{Master: true, RequireApproval: true, Tags: true})
		lastApply := -1
		pruneAt := -1
		for i, op := range plan.Ops {
			if op.RulesetName != "" {
				lastApply = i
			}
			if op.PruneRulesets {
				pruneAt = i
			}
		}
		if pruneAt < lastApply {
			t.Errorf("prune op at %d precedes the last ruleset apply at %d", pruneAt, lastApply)
		}
	})

	t.Run("a class with no rulesets keeps none", func(t *testing.T) {
		t.Parallel()
		plan := build(t, ClassRulesets{})
		if keep := prunes(t, plan).KeepRulesets; len(keep) != 0 {
			t.Errorf("keep = %v, want empty — an ungoverned class must not retain rulesets", keep)
		}
	})

	t.Run("coverage is never kept", func(t *testing.T) {
		t.Parallel()
		keep := prunes(t, build(t, ClassRulesets{Master: true, RequireApproval: true, Tags: true})).KeepRulesets
		if slices.Contains(keep, "codecov") {
			t.Errorf("codecov is still in the keep set %v — it would survive the prune", keep)
		}
	})

	t.Run("the prune op reads as a deletion", func(t *testing.T) {
		t.Parallel()
		title := prunes(t, build(t, ClassRulesets{Master: true})).Title()
		if !strings.Contains(title, "PRUNE") || !strings.Contains(title, "master") {
			t.Errorf("prune title = %q, want it to name the operation and what survives", title)
		}
	})
}
