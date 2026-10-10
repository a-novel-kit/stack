package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

// TestRenderCompactSummary asserts the batch view shows the per-repo dimensions
// that vary — class, rulesets, codecov, and the discovered required checks.
func TestRenderCompactSummary(t *testing.T) {
	t.Parallel()
	target := &repocfg.RepoTarget{
		Org:  orgAnovel,
		Repo: "service-auth",
		Class: &repocfg.ClassPreset{
			Class:    repocfg.ClassService,
			Rulesets: repocfg.ClassRulesets{Master: true, RequireApproval: true},
		},
		Discovered: &repocfg.Discovered{Checks: []repocfg.CheckRef{{Context: "lint-go"}, {Context: "test-go"}}},
	}
	var buf bytes.Buffer
	renderCompactSummary(&buf, target)
	got := buf.String()
	for _, want := range []string{
		"a-novel/service-auth", "class service",
		"master", "require-approval",
		"lint-go, test-go (2)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("compact summary missing %q\n--- got ---\n%s", want, got)
		}
	}
	// A single repo's overview must stay compact — two lines, not the full grouped view.
	if lines := strings.Count(strings.TrimSpace(got), "\n"); lines > 1 {
		t.Errorf("compact summary should be at most 2 lines, got %d:\n%s", lines+1, got)
	}
}

// TestRenderAllJSON checks the --all --json document shape: an array of
// {repo, ops} objects, and a clean "[]" when nothing is eligible.
func TestRenderAllJSON(t *testing.T) {
	t.Parallel()

	t.Run("with plans", func(t *testing.T) {
		t.Parallel()
		items := []plannedUpdate{{
			checkout: checkout{repoEntry: repoEntry{Org: orgAnovel, Name: "service-json-keys"}},
			plan:     &repocfg.Plan{Ops: []repocfg.Op{{Method: "PATCH", Path: "repos/a-novel/service-json-keys"}}},
		}}
		var buf bytes.Buffer
		if err := renderAllJSON(&buf, items); err != nil {
			t.Fatalf("renderAllJSON: %v", err)
		}
		for _, want := range []string{`"repo": "a-novel/service-json-keys"`, `"method": "PATCH"`, `"ops"`} {
			if !strings.Contains(buf.String(), want) {
				t.Errorf("json missing %q\n%s", want, buf.String())
			}
		}
	})

	t.Run("empty renders as an array", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		if err := renderAllJSON(&buf, nil); err != nil {
			t.Fatalf("renderAllJSON(nil): %v", err)
		}
		if got := strings.TrimSpace(buf.String()); got != "[]" {
			t.Errorf("empty json = %q, want %q", got, "[]")
		}
	})
}
