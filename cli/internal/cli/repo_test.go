package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

func TestRepoCreateInfraDefaultsPublicAndBootstrapsBeforeRulesets(t *testing.T) {
	// Not parallel: swaps package-level command seams.
	swap(t, &stdinIsTTY, func() bool { return true })
	headReads := 0
	recorded := stubGH(t, func(call string) (string, error) {
		switch {
		case strings.Contains(call, "labels?per_page=100"):
			return "[]", nil
		case strings.Contains(call, "git/ref/heads/master"):
			headReads++
			if headReads == 1 {
				return "", errGHEmptyRepo
			}
			return "seedoid123", nil
		case strings.Contains(call, "api -X PUT repos/a-novel/infra/contents/"):
			return "seedcommit123", nil
		case strings.Contains(call, "/contents/"):
			return "", errGHNotFound
		case strings.Contains(call, "api graphql"):
			return "synccommit456", nil
		}
		return "", nil
	})

	cmd := newRepoCreateCmd()
	cmd.SetIn(strings.NewReader("yes\n"))
	out, err := runCmd(t, cmd, "a-novel", "infra")
	if err != nil {
		t.Fatalf("repo create infra: %v\n%s", err, out)
	}

	calls := *recorded
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
		strings.Contains(joined, `"name":"tags"`) {
		t.Errorf("infra create attempted release mechanics:\n%s", joined)
	}
	for _, want := range []string{"class infra", "created and configured"} {
		if !strings.Contains(out, want) {
			t.Errorf("create output missing %q:\n%s", want, out)
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

func indexCall(calls []string, contains string) int {
	for i, call := range calls {
		if strings.Contains(call, contains) {
			return i
		}
	}
	return -1
}

func TestRenderSummary(t *testing.T) {
	t.Parallel()
	target := &repocfg.RepoTarget{
		Org:  orgAnovel,
		Repo: "service-auth",
		Class: &repocfg.ClassPreset{
			Class:    repocfg.ClassService,
			Features: repocfg.Features{Issues: true, Projects: true},
			Merge:    repocfg.Merge{Squash: true, AutoMerge: true, SignoffRequired: true},
			Security: repocfg.SecurityToggles{SecretScanning: true, PushProtection: true, Dependabot: true},
			Rulesets: repocfg.ClassRulesets{Master: true, RequireApproval: true},
		},
		Discovered: &repocfg.Discovered{
			Checks: []repocfg.CheckRef{{Context: "lint-go"}, {Context: "test"}},
		},
	}

	var buf bytes.Buffer
	renderSummary(&buf, target)
	got := buf.String()

	for _, want := range []string{
		"a-novel/service-auth", "class service",
		"Features", "squash", "auto-merge", "signoff",
		"lint-go, test (2)", // discovered checks
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q\n--- got ---\n%s", want, got)
		}
	}
}

// TestRenderSummaryOmitsRetiredRulesets guards the summary against announcing a
// ruleset repocfg no longer applies. An operator reads the summary before
// confirming a reconcile, so it names only the protection the plan keeps.
func TestRenderSummaryOmitsRetiredRulesets(t *testing.T) {
	t.Parallel()
	target := &repocfg.RepoTarget{
		Org: orgAnovel, Repo: "lib-x",
		Class: &repocfg.ClassPreset{
			Class:    repocfg.ClassLibrary,
			Rulesets: repocfg.ClassRulesets{Master: true, RequireApproval: true, Tags: true},
		},
		Discovered: &repocfg.Discovered{},
	}
	var buf bytes.Buffer
	renderSummary(&buf, target)
	if strings.Contains(buf.String(), "codecov") {
		t.Errorf("summary still lists the retired codecov ruleset:\n%s", buf.String())
	}
}
