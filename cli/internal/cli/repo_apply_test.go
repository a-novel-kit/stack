package cli

import (
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

// fakeGH installs a stub gh runner for the test, returning canned output per
// matcher and recording every invocation (args, then any stdin payload).
// Restores the real runner on cleanup.
func fakeGH(t *testing.T, responses map[string]string) *[]string {
	t.Helper()
	orig := ghStdin
	var calls []string
	ghStdin = func(stdin string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		if stdin != "" {
			joined += " " + stdin
		}
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
		"git/ref/heads/master":                       "headoid123\n",
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
		"api -X PATCH repos/o/r --input -",                    // settings
		"code-scanning/default-setup -f state=not-configured", // codeql: disable default setup first
		"api repos/o/r/git/ref/heads/master --jq .object.sha", // sync commit: read the branch tip
		`"expectedHeadOid":"headoid123"`,                      // sync commit: optimistic lock on that tip
		`"path":".github/workflows/codeql.yml"`,               // sync commit: carries the staged file
		"api -X POST repos/o/r/rulesets --input -",            // master ruleset created (no existing id)
		"api -X PUT repos/o/r/rulesets/777 --input -",         // codecov ruleset updated (existing id 777)
	}
	for _, w := range wants {
		if !strings.Contains(joined, w) {
			t.Errorf("expected a gh call containing %q; calls:\n%s", w, joined)
		}
	}
	// The per-file contents API path is gone — a REST PUT would mean a commit
	// per file again.
	if strings.Contains(joined, "-X PUT repos/o/r/contents/") {
		t.Errorf("managed files must land via the sync commit, not per-file PUTs; calls:\n%s", joined)
	}
}

func TestStageContents(t *testing.T) {
	// Not parallel: sub-tests swap the package-level ghStdin seam.
	t.Run("unchanged content stages nothing", func(t *testing.T) {
		content := "name: CodeQL\n"
		fakeGH(t, map[string]string{"--jq .sha": blobSHA(content) + "\n"})

		changes, unchanged, err := stageContents("o", "r", repocfg.Op{
			Path: "repos/o/r/contents/.github/workflows/codeql.yml", Content: content,
		})
		if err != nil {
			t.Fatalf("stageContents: %v", err)
		}
		if !unchanged || len(changes) != 0 {
			t.Fatalf("got (changes=%v, unchanged=%v), want nothing staged", changes, unchanged)
		}
	})

	t.Run("new file stages a creation", func(t *testing.T) {
		fakeGH(t, nil) // sha GET yields "" → the file does not exist yet

		changes, unchanged, err := stageContents("o", "r", repocfg.Op{
			Path: "repos/o/r/contents/.github/workflows/codeql.yml", Content: "name: CodeQL\n",
		})
		if err != nil || unchanged {
			t.Fatalf("stageContents: (unchanged=%v, err=%v)", unchanged, err)
		}
		want := contentChange{path: ".github/workflows/codeql.yml", content: "name: CodeQL\n", outcome: opCreated}
		if len(changes) != 1 || changes[0] != want {
			t.Fatalf("changes = %+v, want [%+v]", changes, want)
		}
	})

	t.Run("drifted file stages an update", func(t *testing.T) {
		fakeGH(t, map[string]string{"--jq .sha": "someothersha\n"})

		changes, unchanged, err := stageContents("o", "r", repocfg.Op{
			Path: "repos/o/r/contents/.github/workflows/codeql.yml", Content: "name: CodeQL\n",
		})
		if err != nil || unchanged {
			t.Fatalf("stageContents: (unchanged=%v, err=%v)", unchanged, err)
		}
		if len(changes) != 1 || changes[0].outcome != opUpdated {
			t.Fatalf("changes = %+v, want one update", changes)
		}
	})

	t.Run("stray root CODEOWNERS staged as deletion even when unchanged", func(t *testing.T) {
		content := "* @a-novel-kit/maintainers\n"
		fakeGH(t, map[string]string{
			// Both the root copy and the .github/ copy report this sha: the
			// .github/ copy matches (unchanged), the root copy just exists.
			"--jq .sha": blobSHA(content) + "\n",
		})

		changes, unchanged, err := stageContents("o", "r", repocfg.Op{
			Path: "repos/o/r/contents/.github/CODEOWNERS", Content: content,
		})
		if err != nil {
			t.Fatalf("stageContents: %v", err)
		}
		want := contentChange{path: "CODEOWNERS", outcome: opDeleted}
		if !unchanged || len(changes) != 1 || changes[0] != want {
			t.Fatalf("got (changes=%+v, unchanged=%v), want the root deletion only", changes, unchanged)
		}
	})
}

func TestCommitSync(t *testing.T) {
	// Not parallel: sub-tests swap the package-level ghStdin seam.
	changes := []contentChange{
		{path: ".github/workflows/codeql.yml", content: "name: CodeQL\n", outcome: opUpdated},
		{path: "CODEOWNERS", outcome: opDeleted},
	}

	t.Run("one mutation carries additions and deletions", func(t *testing.T) {
		calls := fakeGH(t, map[string]string{
			"git/ref/heads/master": "headoid123\n",
			"api graphql":          "fc89f28c5489\n",
		})

		detail, err := commitSync("o", "r", branchMaster, changes)
		if err != nil {
			t.Fatalf("commitSync: %v", err)
		}
		if detail != "fc89f28" {
			t.Fatalf("detail = %q, want the short commit id", detail)
		}
		joined := strings.Join(*calls, "\n")
		for _, w := range []string{
			`"expectedHeadOid":"headoid123"`,
			`"additions":[{"contents":"` + base64.StdEncoding.EncodeToString([]byte("name: CodeQL\n")),
			`"deletions":[{"path":"CODEOWNERS"}]`,
			`"headline":"ci: sync managed config (codeql, CODEOWNERS)"`,
		} {
			if !strings.Contains(joined, w) {
				t.Errorf("expected the mutation payload to contain %q; calls:\n%s", w, joined)
			}
		}
	})

	t.Run("stale branch tip retries once with a fresh oid", func(t *testing.T) {
		orig := ghStdin
		t.Cleanup(func() { ghStdin = orig })
		var mutations, headReads int
		ghStdin = func(_ string, args ...string) (string, error) {
			j := strings.Join(args, " ")
			if strings.Contains(j, "git/ref/heads") {
				headReads++
				return "headoid123\n", nil
			}
			mutations++
			if mutations == 1 {
				return `{"errors":[{"type":"STALE_DATA"}]}`, errors.New("exit 1: gh: Expected branch to point to \"x\" but it did not.")
			}
			return "fc89f28c5489\n", nil
		}

		if _, err := commitSync("o", "r", branchMaster, changes); err != nil {
			t.Fatalf("commitSync after retry: %v", err)
		}
		if mutations != 2 || headReads != 2 {
			t.Fatalf("got %d mutations / %d head reads, want 2 / 2", mutations, headReads)
		}
	})

	t.Run("missing workflow scope hints the fix", func(t *testing.T) {
		orig := ghStdin
		t.Cleanup(func() { ghStdin = orig })
		ghStdin = func(_ string, args ...string) (string, error) {
			if strings.Contains(strings.Join(args, " "), "git/ref/heads") {
				return "headoid123\n", nil
			}
			return "", errors.New("exit 1: gh: refusing to update workflow files without the workflow scope")
		}

		detail, err := commitSync("o", "r", branchMaster, changes)
		if err == nil {
			t.Fatal("expected the scope failure to surface as an error")
		}
		if !strings.Contains(detail, "workflow") || !strings.Contains(detail, "scope") {
			t.Fatalf("detail should hint the `workflow` scope; got %q", detail)
		}
	})
}

func TestSyncCommitMessage(t *testing.T) {
	t.Run("short change lists name the files", func(t *testing.T) {
		headline, body := syncCommitMessage([]contentChange{
			{path: ".github/workflows/codeql.yml", outcome: opUpdated},
			{path: "CODEOWNERS", outcome: opDeleted},
		})
		if headline != "ci: sync managed config (codeql, CODEOWNERS)" {
			t.Errorf("headline = %q", headline)
		}
		for _, w := range []string{"updated .github/workflows/codeql.yml", "deleted CODEOWNERS", "Managed by a-novel repo sync."} {
			if !strings.Contains(body, w) {
				t.Errorf("body should contain %q; got:\n%s", w, body)
			}
		}
	})

	t.Run("long change lists fall back to a count", func(t *testing.T) {
		headline, _ := syncCommitMessage([]contentChange{
			{path: ".github/CODEOWNERS"},
			{path: ".github/workflows/codeql.yml"},
			{path: ".github/workflows/merge-gate.yaml"},
			{path: ".github/workflows/approve-pr.yaml"},
			{path: ".github/workflows/derive-status.yaml"},
			{path: ".github/workflows/auto-approve-dependabot.yaml"},
		})
		if headline != "ci: sync managed config (6 files)" {
			t.Errorf("headline = %q, want the count fallback", headline)
		}
	})
}

func TestBlobSHA(t *testing.T) {
	// Pinned against `printf 'test\n' | git hash-object --stdin`.
	if got := blobSHA("test\n"); got != "9daeafb9864cf43055ae93beb0afd6c7d144bfa4" {
		t.Fatalf("blobSHA = %q, want the git blob id", got)
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

func TestApplyLabels(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	t.Run("reconcile create+update+retire", func(t *testing.T) {
		calls := fakeGH(t, map[string]string{
			// meta matches; documentation's colour has drifted; triage lingers.
			"labels?per_page=100": `[
				{"name":"meta","color":"bfd4f2","description":"No-PR work (manual ops / config); also marks Meta Epics"},
				{"name":"documentation","color":"000000","description":"Improvements or additions to documentation"},
				{"name":"triage","color":"fbca04","description":"old"}
			]`,
		})
		op := repocfg.Op{Path: "repos/o/r/labels", Body: &repocfg.LabelsConfig{
			Ensure: []repocfg.LabelDef{
				{Name: "meta", Color: "bfd4f2", Description: "No-PR work (manual ops / config); also marks Meta Epics"},
				{Name: "documentation", Color: "0075ca", Description: "Improvements or additions to documentation"},
				{Name: "good first issue", Color: "7057ff", Description: "Good for newcomers"},
			},
			Retire: []string{"triage"},
		}}

		detail, err := applyLabels("o", "r", op)
		if err != nil {
			t.Fatalf("applyLabels: %v", err)
		}
		if detail != "1 created, 1 updated, 1 retired" {
			t.Fatalf("summary = %q, want %q", detail, "1 created, 1 updated, 1 retired")
		}

		joined := strings.Join(*calls, "\n")
		for _, w := range []string{
			"repos/o/r/labels?per_page=100",                         // list existing
			"api -X POST repos/o/r/labels --input -",                // create the missing one
			"api -X PATCH repos/o/r/labels/documentation --input -", // recolour the drifted one
			"api -X DELETE repos/o/r/labels/triage",                 // retire
		} {
			if !strings.Contains(joined, w) {
				t.Errorf("expected a gh call containing %q; calls:\n%s", w, joined)
			}
		}
		// meta matched the canonical set exactly — it must not be re-written.
		if strings.Contains(joined, "labels/meta") {
			t.Errorf("meta matched and must not be PATCHed; calls:\n%s", joined)
		}
	})

	t.Run("bad body type is an error", func(t *testing.T) {
		fakeGH(t, nil)
		op := repocfg.Op{Path: "repos/o/r/labels", Body: map[string]any{"ensure": nil}}
		if _, err := applyLabels("o", "r", op); err == nil {
			t.Fatal("expected an error for a non-*LabelsConfig body")
		}
	})
}
