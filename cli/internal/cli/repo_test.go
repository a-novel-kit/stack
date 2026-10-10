package cli

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

func TestRepoCreateInfraDefaultsPublicAndBootstrapsBeforeRulesets(t *testing.T) {
	// Not parallel: swaps package-level command seams.
	origTTY := stdinIsTTY
	origGH := ghStdin
	t.Cleanup(func() {
		stdinIsTTY = origTTY
		ghStdin = origGH
	})
	stdinIsTTY = func() bool { return true }

	var calls []string
	var headReads int
	ghStdin = func(stdin string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		if stdin != "" {
			joined += " " + stdin
		}
		calls = append(calls, joined)

		switch {
		case strings.Contains(joined, "labels?per_page=100"):
			return "[]", nil
		case strings.Contains(joined, "git/ref/heads/master"):
			headReads++
			if headReads == 1 {
				return "", errors.New("gh: Git Repository is empty (HTTP 409)")
			}
			return "seedoid123", nil
		case strings.Contains(joined, "api -X PUT repos/a-novel/infra/contents/"):
			return "seedcommit123", nil
		case strings.Contains(joined, "/contents/"):
			return "", errors.New("gh: Not Found (HTTP 404)")
		case strings.Contains(joined, "api graphql"):
			return "synccommit456", nil
		default:
			return "", nil
		}
	}

	cmd := newRepoCreateCmd()
	var out bytes.Buffer
	cmd.SetIn(strings.NewReader("yes\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"a-novel", "infra"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("repo create infra: %v\n%s", err, out.String())
	}

	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"repo create a-novel/infra --public",
		"api -X PUT repos/a-novel/infra/vulnerability-alerts",
		"api -X DELETE repos/a-novel/infra/pages",
		`"context":"epic-freeze"`,
		`"context":"merge-gate"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("GitHub calls missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "repo create a-novel/infra --private") {
		t.Errorf("infra create unexpectedly private:\n%s", joined)
	}
	if strings.Contains(joined, "-X PUT repos/a-novel/infra/contents/.github/workflows/release-train.yaml") ||
		strings.Contains(joined, "-X PUT repos/a-novel/infra/contents/.github/workflows/hotfix.yaml") ||
		strings.Contains(joined, `select(.name=="tags")`) {
		t.Errorf("infra create attempted release mechanics:\n%s", joined)
	}
	for _, want := range []string{"class infra", "created and configured"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("create output missing %q:\n%s", want, out.String())
		}
	}
	if !strings.Contains(classFlagUsage(), "infra") {
		t.Errorf("class help omits infra: %q", classFlagUsage())
	}

	seedAt := indexCall(calls, "-X PUT repos/a-novel/infra/contents/.github/CODEOWNERS")
	syncAt := indexCall(calls, "api graphql")
	rulesetAt := indexCall(calls, "/rulesets")
	if seedAt < 0 || syncAt < 0 || rulesetAt < 0 {
		t.Fatalf("missing bootstrap calls (seed=%d sync=%d ruleset=%d):\n%s", seedAt, syncAt, rulesetAt, joined)
	}
	if seedAt > rulesetAt || syncAt > rulesetAt {
		t.Errorf("rulesets applied before managed workflow bootstrap (seed=%d sync=%d ruleset=%d)", seedAt, syncAt, rulesetAt)
	}
}

func TestApplyPlanSkipsRulesetsWhenManagedSyncFails(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	orig := ghStdin
	t.Cleanup(func() { ghStdin = orig })

	var rulesetCalls int
	ghStdin = func(_ string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "/contents/"):
			return "", errors.New("gh: Not Found (HTTP 404)")
		case strings.Contains(joined, "git/ref/heads/master"):
			return "headoid123", nil
		case strings.Contains(joined, "api graphql"):
			return "", errors.New("workflow scope missing")
		case strings.Contains(joined, "/rulesets"):
			rulesetCalls++
		}
		return "", nil
	}

	plan := &repocfg.Plan{Ops: []repocfg.Op{
		{Method: http.MethodPut, Path: "repos/o/r/contents/.github/workflows/merge-gate.yaml", Content: "name: merge gate\n"},
		{RulesetName: branchMaster, Path: "repos/o/r/rulesets", Body: &repocfg.APIRuleset{Name: branchMaster}},
	}}
	if err := applyPlan(io.Discard, "o", "r", branchMaster, plan); err == nil {
		t.Fatal("managed sync failure must fail the apply")
	}
	if rulesetCalls != 0 {
		t.Errorf("issued %d ruleset call(s) after managed sync failed, want 0", rulesetCalls)
	}
}

func TestInfraDisabledStateOperationsAreIdempotent(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	orig := ghStdin
	t.Cleanup(func() { ghStdin = orig })

	var pagesCalls, alertCalls int
	ghStdin = func(_ string, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "/pages"):
			pagesCalls++
			if pagesCalls > 1 {
				return "", errors.New("gh: Not Found (HTTP 404)")
			}
		case strings.Contains(joined, "/vulnerability-alerts"):
			alertCalls++
		}
		return "", nil
	}

	pages := repocfg.Op{Method: http.MethodDelete, Path: "repos/o/infra/pages"}
	if got, err := applyPages(pages); err != nil || got != "disabled" {
		t.Fatalf("first Pages disable = (%q, %v), want (disabled, nil)", got, err)
	}
	if got, err := applyPages(pages); err != nil || got != "already disabled" {
		t.Fatalf("second Pages disable = (%q, %v), want (already disabled, nil)", got, err)
	}

	alerts := repocfg.Op{Method: http.MethodPut, Path: "repos/o/infra/vulnerability-alerts"}
	for i := range 2 {
		if got, err := applyVulnerabilityAlerts(alerts); err != nil || got != "enabled" {
			t.Fatalf("alerts enable %d = (%q, %v), want (enabled, nil)", i+1, got, err)
		}
	}
	if pagesCalls != 2 || alertCalls != 2 {
		t.Errorf("Pages/alerts calls = %d/%d, want 2/2", pagesCalls, alertCalls)
	}
}

func TestStageContentDeletionIsIdempotent(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	orig := ghStdin
	t.Cleanup(func() { ghStdin = orig })

	var reads int
	ghStdin = func(string, ...string) (string, error) {
		reads++
		if reads == 1 {
			return "existing-sha", nil
		}
		return "", errors.New("gh: Not Found (HTTP 404)")
	}
	op := repocfg.Op{Method: http.MethodDelete, Path: "repos/o/infra/contents/.github/workflows/release-train.yaml"}

	change, unchanged, err := stageContentDeletion(op)
	if err != nil || unchanged || change.outcome != opDeleted {
		t.Fatalf("existing deletion = (%+v, %v, %v), want one staged deletion", change, unchanged, err)
	}
	change, unchanged, err = stageContentDeletion(op)
	if err != nil || !unchanged || change != (contentChange{}) {
		t.Fatalf("missing deletion = (%+v, %v, %v), want unchanged", change, unchanged, err)
	}
}

func indexCall(calls []string, contains string) int {
	for i, call := range calls {
		if strings.Contains(call, contains) {
			return i
		}
	}
	return -1
}
