package repocfg

import (
	"bytes"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestInfraClassContract(t *testing.T) {
	t.Parallel()

	preset, err := LoadClass(ClassInfra)
	if err != nil {
		t.Fatalf("LoadClass(infra): %v", err)
	}
	if got := DetectClass("infra"); got != ClassInfra {
		t.Fatalf("DetectClass(infra) = %q, want %q", got, ClassInfra)
	}
	if _, err := LoadClass(Class("infrastructure")); err == nil {
		t.Fatal("an unknown class must fail instead of falling back to library")
	}

	if preset.Class != ClassInfra {
		t.Errorf("class = %q, want %q", preset.Class, ClassInfra)
	}
	if want := (Features{Issues: true, Projects: true}); preset.Features != want {
		t.Errorf("features = %+v, want %+v", preset.Features, want)
	}
	if want := (Merge{
		Squash: true, AutoMerge: true, DeleteBranchOnMerge: true,
		AllowUpdateBranch: true, SignoffRequired: true,
	}); preset.Merge != want {
		t.Errorf("merge = %+v, want %+v", preset.Merge, want)
	}
	if !preset.Security.SecretScanning || !preset.Security.PushProtection || preset.Security.Dependabot {
		t.Errorf("security = %+v, want scanning and push protection on with Dependabot updates off", preset.Security)
	}
	if preset.Security.DependabotAlerts == nil || !*preset.Security.DependabotAlerts {
		t.Errorf("Dependabot alerts = %v, want explicitly enabled", preset.Security.DependabotAlerts)
	}
	if preset.Pages || preset.CodeQuality {
		t.Errorf("pages/code_quality = %v/%v, want both off", preset.Pages, preset.CodeQuality)
	}
	if want := (ClassRulesets{Master: true, RequireApproval: true}); preset.Rulesets != want {
		t.Errorf("rulesets = %+v, want %+v", preset.Rulesets, want)
	}
}

func TestInfraPlanIsExactAndDeploymentOnly(t *testing.T) {
	t.Parallel()

	plan := buildInfraPlan(t)
	base := "repos/a-novel/infra"
	signatures := make([]string, 0, len(plan.Ops))
	var settings map[string]any
	var master *APIRuleset
	var hasCODEOWNERS, hasLabels bool

	for _, op := range plan.Ops {
		switch {
		case op.PruneRulesets:
			signatures = append(signatures, "PRUNE "+strings.Join(op.KeepRulesets, ","))
		case op.RulesetName != "":
			signatures = append(signatures, "RULESET "+op.RulesetName)
			if op.RulesetName == rulesetMaster {
				master, _ = op.Body.(*APIRuleset)
			}
		default:
			signatures = append(signatures, op.Method+" "+strings.TrimPrefix(op.Path, base))
		}

		if op.Path == base {
			settings, _ = op.Body.(map[string]any)
		}
		if op.Path == base+"/contents/.github/CODEOWNERS" && strings.TrimSpace(op.Content) == "* @kushuh" {
			hasCODEOWNERS = true
		}
		if op.Path == base+"/labels" {
			_, hasLabels = op.Body.(*LabelsConfig)
		}
		if op.Method == http.MethodPut &&
			(strings.HasSuffix(op.Path, "/release-train.yaml") || strings.HasSuffix(op.Path, "/hotfix.yaml")) {
			t.Errorf("infra must not provision a release caller: %s", op.Path)
		}
	}

	wantSignatures := []string{
		"PATCH ",
		"PUT /vulnerability-alerts",
		"PUT /contents/.github/CODEOWNERS",
		"PUT /labels",
		"PATCH /code-scanning/default-setup",
		"PUT /contents/.github/workflows/merge-gate.yaml",
		"PUT /contents/.github/workflows/epic-freeze.yaml",
		"PUT /contents/.github/workflows/approve-pr.yaml",
		"PUT /contents/.github/workflows/derive-status.yaml",
		"PUT /contents/.github/workflows/epic-rollback.yaml",
		"DELETE /contents/.github/workflows/release-train.yaml",
		"DELETE /contents/.github/workflows/hotfix.yaml",
		"PUT /contents/.github/workflows/auto-approve-dependabot.yaml",
		"DELETE /pages",
		"RULESET master",
		"RULESET require-approval",
		"PRUNE master,require-approval",
	}
	if !slices.Equal(signatures, wantSignatures) {
		t.Fatalf("operation sequence =\n  %s\nwant =\n  %s",
			strings.Join(signatures, "\n  "), strings.Join(wantSignatures, "\n  "))
	}
	if !hasCODEOWNERS || !hasLabels {
		t.Errorf("CODEOWNERS/labels present = %v/%v, want both", hasCODEOWNERS, hasLabels)
	}

	wantFeatures := map[string]any{
		"has_issues": true, "has_wiki": false,
		"has_projects": true, "has_discussions": false,
	}
	for key, want := range wantFeatures {
		if got := settings[key]; got != want {
			t.Errorf("settings[%q] = %v, want %v", key, got, want)
		}
	}
	security, ok := settings["security_and_analysis"].(map[string]any)
	if !ok {
		t.Fatalf("security_and_analysis = %T, want map", settings["security_and_analysis"])
	}
	wantSecurity := map[string]any{
		"secret_scanning":                 map[string]string{"status": "enabled"},
		"secret_scanning_push_protection": map[string]string{"status": "enabled"},
		"dependabot_security_updates":     map[string]string{"status": "disabled"},
	}
	if !reflect.DeepEqual(security, wantSecurity) {
		t.Errorf("security_and_analysis = %#v, want %#v", security, wantSecurity)
	}

	if master == nil {
		t.Fatal("master ruleset body missing")
	}
	var requiredChecks []map[string]any
	for _, rule := range master.Rules {
		if rule.Type == "code_quality" {
			t.Error("infra master ruleset must not carry the GitHub code-quality rule")
		}
		if rule.Type == "required_status_checks" {
			requiredChecks, _ = rule.Parameters["required_status_checks"].([]map[string]any)
		}
	}
	contexts := make([]string, 0, len(requiredChecks))
	for _, check := range requiredChecks {
		contexts = append(contexts, check["context"].(string))
	}
	if want := []string{"epic-freeze", "merge-gate"}; !slices.Equal(contexts, want) {
		t.Errorf("bootstrap required checks = %v, want %v", contexts, want)
	}

	var rendered bytes.Buffer
	if err := plan.Render(&rendered); err != nil {
		t.Fatalf("Render: %v", err)
	}
	dryRun := rendered.String()
	for _, want := range []string{
		"### PUT " + base + "/vulnerability-alerts",
		"### DELETE " + base + "/pages",
		"### DELETE " + base + "/contents/.github/workflows/release-train.yaml",
		"### PRUNE rulesets",
	} {
		if !strings.Contains(dryRun, want) {
			t.Errorf("dry-run missing %q\n%s", want, dryRun)
		}
	}
	if strings.Contains(dryRun, "\nnull\n") || strings.Contains(dryRun, `ruleset "tags"`) {
		t.Errorf("dry-run contains a null body or tag ruleset:\n%s", dryRun)
	}
}

func buildInfraPlan(t *testing.T) *Plan {
	t.Helper()

	preset, err := LoadClass(ClassInfra)
	if err != nil {
		t.Fatalf("LoadClass(infra): %v", err)
	}
	org, err := LoadOrg("a-novel")
	if err != nil {
		t.Fatalf("LoadOrg: %v", err)
	}
	checks, err := LoadChecks()
	if err != nil {
		t.Fatalf("LoadChecks: %v", err)
	}
	checks.ResolveBotIntegrations(org)
	discovered, err := Discover(t.TempDir(), checks)
	if err != nil {
		t.Fatalf("Discover(empty repo): %v", err)
	}
	plan, err := BuildPlan(&RepoTarget{
		Org:           "a-novel",
		Repo:          "infra",
		DefaultBranch: "master",
		Class:         preset,
		OrgProfile:    org,
		Checks:        checks,
		Discovered:    discovered,
	})
	if err != nil {
		t.Fatalf("BuildPlan(infra): %v", err)
	}
	return plan
}
