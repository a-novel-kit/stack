package repocfg

import (
	"net/http"
	"strings"
	"testing"
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

// contextsOf extracts the context names from a CheckRef slice, preserving order.
func contextsOf(checks []CheckRef) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Context
	}
	return out
}
