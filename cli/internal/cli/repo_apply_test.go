package cli

import (
	"errors"
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

// A missing `workflow` scope surfaces as a bare 404 on the codeql.yml write
// (GitHub obscures it as Not Found); the detail must still hint the scope.
func TestApplyContentsWorkflowScope404(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	orig := ghStdin
	ghStdin = func(_ string, args ...string) (string, error) {
		j := strings.Join(args, " ")
		if strings.Contains(j, "-X PUT") && strings.Contains(j, "codeql.yml") {
			return "", errors.New("exit 1: gh: Not Found (HTTP 404)")
		}
		return "", nil // the sha-GET and the default-setup PATCH succeed
	}
	t.Cleanup(func() { ghStdin = orig })

	detail, err := applyContents("o", "r", branchMaster, repocfg.Op{
		Path: "repos/o/r/contents/.github/workflows/codeql.yml", Content: "name: CodeQL\n",
	})
	if err == nil {
		t.Fatal("expected the 404 to surface as an error")
	}
	if !strings.Contains(detail, "workflow") || !strings.Contains(detail, "scope") {
		t.Fatalf("a bare 404 on a workflow write should hint the `workflow` scope; got detail %q", detail)
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

func TestApplyRulesetBadBody(t *testing.T) {
	// A malformed plan (wrong body type) must fail fast, not POST a null body.
	calls := fakeGH(t, nil)
	op := repocfg.Op{RulesetName: branchMaster, Path: "repos/o/r/rulesets", Body: map[string]any{"name": "x"}}
	if _, err := applyRuleset("o", "r", op); err == nil {
		t.Fatal("expected an error for a non-*APIRuleset body")
	}
	for _, c := range *calls {
		if strings.Contains(c, "rulesets") && (strings.Contains(c, "POST") || strings.Contains(c, "PUT")) {
			t.Errorf("must not write a ruleset with a bad body; got call %q", c)
		}
	}
}

func TestContentSHA(t *testing.T) {
	orig := ghStdin
	t.Cleanup(func() { ghStdin = orig })

	t.Run("missing file (404) → empty sha, no error", func(t *testing.T) {
		ghStdin = func(_ string, _ ...string) (string, error) {
			return "", errors.New("exit 1: HTTP 404: Not Found")
		}
		sha, err := contentSHA("repos/o/r/contents/x.yml")
		if err != nil || sha != "" {
			t.Fatalf("404 should yield (\"\", nil); got (%q, %v)", sha, err)
		}
	})

	t.Run("other error → propagated", func(t *testing.T) {
		ghStdin = func(_ string, _ ...string) (string, error) {
			return "", errors.New("exit 1: HTTP 401: Bad credentials")
		}
		if _, err := contentSHA("repos/o/r/contents/x.yml"); err == nil {
			t.Fatal("a non-404 error must propagate, not be swallowed as create")
		}
	})

	t.Run("existing file → trimmed sha", func(t *testing.T) {
		ghStdin = func(_ string, _ ...string) (string, error) { return "abc123\n", nil }
		sha, err := contentSHA("repos/o/r/contents/x.yml")
		if err != nil || sha != "abc123" {
			t.Fatalf("got (%q, %v), want (\"abc123\", nil)", sha, err)
		}
	})
}
