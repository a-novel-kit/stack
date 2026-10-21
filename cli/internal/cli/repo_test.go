package cli

import (
	"bytes"
	"errors"
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
		case strings.Contains(call, "auth status"):
			return "admin:org, repo, workflow\n", nil
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
	wantCalls := []string{
		"repo create a-novel/infra --public",
		"api -X PUT repos/a-novel/infra/vulnerability-alerts",
		"api -X PUT repos/a-novel/infra/immutable-releases",
		"api -X DELETE repos/a-novel/infra/pages",
		`"context":"epic-freeze"`,
		`"context":"merge-gate"`,
	}

	for _, want := range wantCalls {
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

// TestRequireAdminOrg pins the preflight of `repo update`: a write passes only
// with the admin:org scope, and a preview never asks gh for the scopes.
func TestRequireAdminOrg(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	const hint = "gh auth refresh -h github.com -s admin:org"
	testCases := []struct {
		name      string
		flags     []string
		scopes    string
		ghErr     error
		wantErr   string // a substring of the error, "" for success
		wantCalls int
	}{
		{name: "Success", scopes: "admin:org, gist, repo, workflow\n", wantCalls: 1},
		{name: "Success/DryRunSkipsTheCheck", flags: []string{"--dry-run"}},
		{name: "Success/JSONSkipsTheCheck", flags: []string{"--json"}},
		{name: "Error/MissingScope", scopes: "gist, repo, workflow\n", wantErr: hint, wantCalls: 1},
		{name: "Error/LookalikeScope", scopes: "admin:org_hook, repo\n", wantErr: hint, wantCalls: 1},
		{name: "Error/GHFails", ghErr: errors.New("unknown flag: --json"), wantErr: "unknown flag", wantCalls: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			calls := stubGH(t, func(string) (string, error) { return testCase.scopes, testCase.ghErr })
			cmd := newRepoUpdateCmd()
			if err := cmd.ParseFlags(testCase.flags); err != nil {
				t.Fatal(err)
			}

			err := cmd.PreRunE(cmd, nil)
			switch {
			case testCase.wantErr == "" && err != nil:
				t.Fatalf("preflight: %v", err)
			case testCase.wantErr != "" && (err == nil || !strings.Contains(err.Error(), testCase.wantErr)):
				t.Fatalf("preflight error = %v, want one containing %q", err, testCase.wantErr)
			}
			if len(*calls) != testCase.wantCalls {
				t.Errorf("gh calls = %q, want %d", *calls, testCase.wantCalls)
			}
		})
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

	wantSummary := []string{
		"a-novel/service-auth", "class service",
		"Features", "squash", "auto-merge", "signoff",
		"lint-go, test (2)", // discovered checks
	}

	for _, want := range wantSummary {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q\n--- got ---\n%s", want, got)
		}
	}
}

// TestRenderSummaryOmitsRetiredRulesets pins that the summary names only the
// rulesets the plan keeps. An operator reads it before confirming a reconcile.
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
