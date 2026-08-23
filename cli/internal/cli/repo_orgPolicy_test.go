package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

func managedConfigurationPages(t *testing.T, codeScanning string) string {
	t.Helper()
	body := desiredOrgSecurityConfigurationBody()
	body["id"] = int64(42)
	body["target_type"] = "organization"
	body["code_scanning_default_setup"] = codeScanning
	raw, err := json.Marshal([][]map[string]any{{body}})
	if err != nil {
		t.Fatalf("marshal managed configuration: %v", err)
	}
	return string(raw)
}

func stubOrgPolicyGH(
	t *testing.T,
	configs, defaults, attachments, repositories string,
	failConfigurations bool,
) *[]string {
	t.Helper()
	original := ghStdin
	var calls []string
	ghStdin = func(stdin string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		if stdin != "" {
			joined += " " + stdin
		}
		calls = append(calls, joined)
		switch {
		case strings.Contains(joined, "configurations/defaults"):
			return defaults, nil
		case strings.Contains(joined, "configurations/42/repositories"):
			return attachments, nil
		case strings.Contains(joined, "orgs/o/repos?"):
			return repositories, nil
		case strings.Contains(joined, "code-security/configurations"):
			if failConfigurations {
				return "", errors.New("forbidden")
			}
			return configs, nil
		default:
			return "", nil
		}
	}
	t.Cleanup(func() { ghStdin = original })
	return &calls
}

func TestPlanOrgPolicy(t *testing.T) {
	// Not parallel: sub-tests swap the package-level ghStdin seam.
	attached := `[[{"status":"attached","repository":{"value":{"id":1}}}]]`
	repositories := `[[{"id":1}]]`
	defaults := `[{"default_for_new_repos":"all","configuration":{"id":42}}]`

	t.Run("updates policy drift", func(t *testing.T) {
		calls := stubOrgPolicyGH(
			t,
			managedConfigurationPages(t, orgPolicySettingEnabled),
			defaults,
			attached,
			repositories,
			false,
		)
		plan, err := planOrgPolicy("o")
		if err != nil {
			t.Fatalf("planOrgPolicy: %v", err)
		}
		if len(plan.plan.Ops) != 1 {
			t.Fatalf("len(plan.Ops) = %d, want 1", len(plan.plan.Ops))
		}
		op := plan.plan.Ops[0]
		if op.Method != http.MethodPatch || op.Path != "orgs/o/code-security/configurations/42" {
			t.Errorf("op = %+v, want PATCH of managed configuration", op)
		}
		body, ok := op.Body.(map[string]any)
		if !ok || body["code_scanning_default_setup"] != codeScanningDefaultSetupDisabled {
			t.Errorf("body = %#v, want CodeQL default setup disabled", op.Body)
		}
		for _, call := range *calls {
			if strings.Contains(call, " -X ") {
				t.Errorf("planning must be read-only, got call %q", call)
			}
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		stubOrgPolicyGH(
			t,
			managedConfigurationPages(t, codeScanningDefaultSetupDisabled),
			defaults,
			attached,
			repositories,
			false,
		)
		plan, err := planOrgPolicy("o")
		if err != nil {
			t.Fatalf("planOrgPolicy: %v", err)
		}
		if len(plan.plan.Ops) != 0 {
			t.Fatalf("len(plan.Ops) = %d, want 0", len(plan.plan.Ops))
		}
	})

	t.Run("creates and applies missing policy", func(t *testing.T) {
		calls := stubOrgPolicyGH(
			t,
			`[[{"id":17,"target_type":"global","name":"GitHub recommended"}]]`,
			"",
			"",
			"",
			false,
		)
		plan, err := planOrgPolicy("o")
		if err != nil {
			t.Fatalf("planOrgPolicy: %v", err)
		}
		if len(plan.plan.Ops) != 3 {
			t.Fatalf("len(plan.Ops) = %d, want 3", len(plan.plan.Ops))
		}
		want := []struct {
			method string
			path   string
		}{
			{http.MethodPost, "orgs/o/code-security/configurations"},
			{http.MethodPut, "orgs/o/code-security/configurations/{configuration_id}/defaults"},
			{http.MethodPost, "orgs/o/code-security/configurations/{configuration_id}/attach"},
		}
		for i, expected := range want {
			op := plan.plan.Ops[i]
			if op.Method != expected.method || op.Path != expected.path {
				t.Errorf("op[%d] = %+v, want %s %s", i, op, expected.method, expected.path)
			}
		}
		body := plan.plan.Ops[0].Body.(map[string]any)
		for key, value := range map[string]any{
			"advanced_security":               orgPolicySettingEnabled,
			"code_scanning_default_setup":     "disabled",
			"secret_scanning":                 orgPolicySettingEnabled,
			"secret_scanning_push_protection": orgPolicySettingEnabled,
		} {
			if body[key] != value {
				t.Errorf("create body[%q] = %#v, want %#v", key, body[key], value)
			}
		}
		if len(*calls) != 1 {
			t.Fatalf("calls = %v, want one configuration read", *calls)
		}
	})

	t.Run("repairs default drift", func(t *testing.T) {
		stubOrgPolicyGH(
			t,
			managedConfigurationPages(t, codeScanningDefaultSetupDisabled),
			`[]`,
			attached,
			repositories,
			false,
		)
		plan, err := planOrgPolicy("o")
		if err != nil {
			t.Fatalf("planOrgPolicy: %v", err)
		}
		if len(plan.plan.Ops) != 1 || plan.plan.Ops[0].Method != http.MethodPut ||
			plan.plan.Ops[0].Path != "orgs/o/code-security/configurations/42/defaults" {
			t.Fatalf("ops = %+v, want one defaults PUT", plan.plan.Ops)
		}
	})

	t.Run("repairs attachment drift", func(t *testing.T) {
		stubOrgPolicyGH(
			t,
			managedConfigurationPages(t, codeScanningDefaultSetupDisabled),
			defaults,
			`[[]]`,
			repositories,
			false,
		)
		plan, err := planOrgPolicy("o")
		if err != nil {
			t.Fatalf("planOrgPolicy: %v", err)
		}
		if len(plan.plan.Ops) != 1 || plan.plan.Ops[0].Method != http.MethodPost ||
			plan.plan.Ops[0].Path != "orgs/o/code-security/configurations/42/attach" {
			t.Fatalf("ops = %+v, want one attach POST", plan.plan.Ops)
		}
	})

	t.Run("accepts terminal failed association", func(t *testing.T) {
		stubOrgPolicyGH(
			t,
			managedConfigurationPages(t, codeScanningDefaultSetupDisabled),
			defaults,
			`[[{"status":"failed","repository":{"id":1}}]]`,
			repositories,
			false,
		)
		plan, err := planOrgPolicy("o")
		if err != nil {
			t.Fatalf("planOrgPolicy: %v", err)
		}
		if len(plan.plan.Ops) != 0 {
			t.Fatalf("ops = %+v, want no retry for terminal failed association", plan.plan.Ops)
		}
	})

	t.Run("reports API failure", func(t *testing.T) {
		stubOrgPolicyGH(t, "", "", "", "", true)
		_, err := planOrgPolicy("o")
		if err == nil || !strings.Contains(err.Error(), "read o security configurations: forbidden") {
			t.Fatalf("planOrgPolicy error = %v, want configuration read failure", err)
		}
	})
}

func TestPlanOrgPolicies(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	original := ghStdin
	var calls []string
	ghStdin = func(stdin string, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return `[[{"id":17,"target_type":"global","name":"GitHub recommended"}]]`, nil
	}
	t.Cleanup(func() { ghStdin = original })
	items := []plannedUpdate{
		{cand: updateCandidate{org: orgAnovel, repo: "service-authentication"}},
		{cand: updateCandidate{org: orgAnovel, repo: "service-json-keys"}},
		{cand: updateCandidate{org: orgAnovelKit, repo: "golib"}},
	}

	plans, err := planOrgPolicies(items)
	if err != nil {
		t.Fatalf("planOrgPolicies: %v", err)
	}
	if len(plans) != 2 || plans[0].org != orgAnovel || plans[1].org != orgAnovelKit {
		t.Fatalf("plans = %+v, want one sorted plan per organization", plans)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %v, want one organization read per organization", calls)
	}
	for _, call := range calls {
		if strings.Contains(call, " -X ") {
			t.Errorf("planning must be read-only, got call %q", call)
		}
	}

	var progress bytes.Buffer
	var out bytes.Buffer
	if err := renderAllDryRun(&progress, &out, plans, nil); err != nil {
		t.Fatalf("renderAllDryRun: %v", err)
	}
	for _, org := range []string{orgAnovel, orgAnovelKit} {
		if got := strings.Count(progress.String(), "# dry-run "+org+" organization policy"); got != 1 {
			t.Errorf("%s dry-run headers = %d, want 1\n%s", org, got, progress.String())
		}
		if got := strings.Count(out.String(), "### POST orgs/"+org+"/code-security/configurations\n"); got != 1 {
			t.Errorf("%s creation operations = %d, want 1\n%s", org, got, out.String())
		}
	}
}

func TestApplyOrgPolicy(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	original := ghStdin
	var calls []string
	ghStdin = func(stdin string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		if stdin != "" {
			joined += " " + stdin
		}
		calls = append(calls, joined)
		if joined == "api -X POST orgs/o/code-security/configurations --input - "+string(mustJSON(t, desiredOrgSecurityConfigurationBody())) {
			return `{"id":42}`, nil
		}
		return "", nil
	}
	t.Cleanup(func() { ghStdin = original })
	plan := plannedOrgPolicy{
		org: "o",
		plan: &repocfg.Plan{Ops: []repocfg.Op{
			{
				Method: http.MethodPost,
				Path:   "orgs/o/code-security/configurations",
				Body:   desiredOrgSecurityConfigurationBody(),
			},
			{
				Method: http.MethodPut,
				Path:   "orgs/o/code-security/configurations/{configuration_id}/defaults",
				Body:   map[string]any{"default_for_new_repos": orgPolicyScopeAll},
			},
			{
				Method: http.MethodPost,
				Path:   "orgs/o/code-security/configurations/{configuration_id}/attach",
				Body:   map[string]any{"scope": orgPolicyScopeAll},
			},
		}},
	}

	if err := applyOrgPolicy(plan); err != nil {
		t.Fatalf("applyOrgPolicy: %v", err)
	}
	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"api -X POST orgs/o/code-security/configurations --input -",
		"api -X PUT orgs/o/code-security/configurations/42/defaults --input -",
		`"default_for_new_repos":"all"`,
		"api -X POST orgs/o/code-security/configurations/42/attach --input -",
		`"scope":"all"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("calls missing %q\n%s", want, joined)
		}
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return raw
}

func TestRepoUpdateSingleDoesNotPlanOrgPolicy(t *testing.T) {
	// Not parallel: changes the working directory and swaps ghStdin.
	root := t.TempDir()
	mustGit(t, root, "init", "--quiet")
	mustGit(t, root, "remote", "add", "origin", "git@github.com:a-novel-kit/stack.git")
	if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755); err != nil {
		t.Fatalf("create workflow directory: %v", err)
	}
	writeFixture(t, root, ".github/workflows/main.yaml", strings.Join([]string{
		"name: main",
		"on: [push]",
		"jobs:",
		"  lint-go:",
		"    runs-on: ubuntu-latest",
		"    steps:",
		"      - run: 'true'",
		"",
	}, "\n"))

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDir) })
	calls := fakeGH(t, nil)

	cmd := newRepoUpdateCmd()
	cmd.SetArgs([]string{"--dry-run"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("repo update --dry-run: %v", err)
	}
	for _, call := range *calls {
		if strings.Contains(call, "orgs/") {
			t.Errorf("single-repository update made organization call %q", call)
		}
	}
}
