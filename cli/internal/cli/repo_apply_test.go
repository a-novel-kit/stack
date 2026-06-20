package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

// fakeGH installs a stub gh runner for the test, returning canned output per
// matcher and recording every invocation. Restores the real runner on cleanup.
func fakeGH(t *testing.T, responses map[string]string) *[]string {
	t.Helper()
	orig := ghStdin
	var calls []string
	ghStdin = func(_ string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		for substr, out := range responses {
			if strings.Contains(joined, substr) {
				return out, nil
			}
		}
		return "", nil
	}
	t.Cleanup(func() { ghStdin = orig })
	return &calls
}

func TestApplyPlan(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	calls := fakeGH(t, map[string]string{
		// no existing master ruleset → POST; codecov ruleset exists → PUT.
		`rulesets --jq .[]|select(.name=="codecov")`: "777",
	})

	plan := &repocfg.Plan{Ops: []repocfg.Op{
		{Method: "PATCH", Path: "repos/o/r", Body: map[string]any{"has_wiki": false}},
		{Method: "PUT", Path: "repos/o/r/contents/.github/workflows/codeql.yml", Content: "name: CodeQL\n"},
		{RulesetName: branchMaster, Path: "repos/o/r/rulesets", Body: &repocfg.APIRuleset{Name: branchMaster}},
		{RulesetName: "codecov", Path: "repos/o/r/rulesets", Body: &repocfg.APIRuleset{Name: "codecov"}},
	}}

	if err := applyPlan(io.Discard, "o", "r", branchMaster, plan); err != nil {
		t.Fatalf("applyPlan: %v", err)
	}

	joined := strings.Join(*calls, "\n")
	wants := []string{
		"api -X PATCH repos/o/r --input -",                           // settings
		"code-scanning/default-setup -f state=not-configured",        // codeql: disable default setup first
		"api -X PUT repos/o/r/contents/.github/workflows/codeql.yml", // codeql: commit the workflow
		"api -X POST repos/o/r/rulesets --input -",                   // master ruleset created (no existing id)
		"api -X PUT repos/o/r/rulesets/777 --input -",                // codecov ruleset updated (existing id 777)
	}
	for _, w := range wants {
		if !strings.Contains(joined, w) {
			t.Errorf("expected a gh call containing %q; calls:\n%s", w, joined)
		}
	}
}

func TestApplySettingsSignoffRetry(t *testing.T) {
	var bodies []string
	orig := ghStdin
	t.Cleanup(func() { ghStdin = orig })
	first := true
	ghStdin = func(stdin string, args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "PATCH repos/o/r") {
			bodies = append(bodies, stdin)
			if first {
				first = false
				return "", signoffError{}
			}
		}
		return "", nil
	}

	op := repocfg.Op{Method: "PATCH", Path: "repos/o/r", Body: map[string]any{
		"has_wiki":                    false,
		"web_commit_signoff_required": true,
	}}
	if err := applySettings(op); err != nil {
		t.Fatalf("applySettings: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected a retry (2 PATCH bodies), got %d", len(bodies))
	}
	if strings.Contains(bodies[1], "web_commit_signoff_required") {
		t.Error("retry body should omit web_commit_signoff_required")
	}
}

type signoffError struct{}

func (signoffError) Error() string {
	return "HTTP 422: Commit signoff is enforced by the organization and cannot be disabled"
}
