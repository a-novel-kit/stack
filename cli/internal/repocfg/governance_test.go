package repocfg_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDependabotGovernance(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("templates/governance/auto-approve-dependabot.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				ID   string `yaml:"id"`
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		t.Fatal(err)
	}
	scripts := map[string]string{}
	for _, step := range workflow.Jobs["dependabot"].Steps {
		if step.ID == "pr" {
			scripts["resolve"] = step.Run
		}
		if step.Name == "Approve and queue the verified commit" {
			scripts["approve"] = step.Run
		}
	}
	for _, stage := range []string{"resolve", "approve"} {
		if scripts[stage] == "" {
			t.Fatalf("missing %s script", stage)
		}
	}
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const bot = "dependabot[bot]"
	type record = map[string]any
	type testCase struct {
		name       string
		mutate     func(record)
		commits    string
		duplicate  bool
		wantOutput bool
		wantError  bool
	}
	for _, tc := range []testCase{
		{name: "Success", wantOutput: true},
		{name: "Ignore/StaleHead", mutate: func(p record) { p["head"].(record)["sha"] = strings.Repeat("b", 40) }},
		{name: "Ignore/Fork", mutate: func(p record) { p["head"].(record)["repo"].(record)["full_name"] = "outsider/repo" }},
		{name: "Ignore/OtherAuthor", mutate: func(p record) { p["user"].(record)["login"] = "outsider" }},
		{name: "Ignore/OtherBase", mutate: func(p record) { p["base"].(record)["ref"] = "feature" }},
		{name: "Ignore/Draft", mutate: func(p record) { p["draft"] = true }},
		{name: "Ignore/Closed", mutate: func(p record) { p["state"] = "closed" }},
		{name: "Ignore/UnknownCommitAuthor", commits: `[[{"author":null}]]`},
		{name: "Ignore/HumanOnSecondPage", commits: `[[{"author":{"login":"dependabot[bot]"}}],[{"author":{"login":"human"}}]]`},
		{name: "Ignore/EmptyCommits", commits: `[[]]`},
		{name: "Error/Ambiguous", duplicate: true, wantError: true},
	} {
		t.Run("Resolve/"+tc.name, func(t *testing.T) {
			t.Parallel()
			pr := record{"number": 522, "node_id": "PR_fixture", "state": "open", "draft": false, "user": record{"login": bot}, "head": record{"sha": sha, "repo": record{"full_name": "example/repo"}}, "base": record{"ref": "master", "repo": record{"full_name": "example/repo", "default_branch": "master"}}}
			if tc.mutate != nil {
				tc.mutate(pr)
			}
			prs := []record{pr}
			if tc.duplicate {
				prs = append(prs, pr)
			}
			payload, err := json.Marshal([][]record{prs})
			if err != nil {
				t.Fatal(err)
			}
			commits := tc.commits
			if commits == "" {
				commits = `[[{"author":{"login":"dependabot[bot]"}}]]`
			}
			output, calls, err := runGovernanceScript(t, scripts["resolve"], []string{"CANDIDATES=" + string(payload), "COMMITS=" + commits, "HEAD_SHA=" + sha, "BOT=" + bot})
			if (err != nil) != tc.wantError {
				t.Fatalf("script error=%v, output=%s", err, output)
			}
			if strings.Contains(output, "number=522") != tc.wantOutput {
				t.Fatalf("unexpected output %q", output)
			}
			if strings.Contains(calls, "reviews") || strings.Contains(calls, "enqueuePullRequest") {
				t.Fatal("resolution performed a write")
			}
		})
	}
	for _, tc := range []struct {
		name, state, head, queue string
		wantWrites               bool
	}{
		{"Success", "OPEN", sha, "null", true},
		{"Ignore/StaleHead", "OPEN", strings.Repeat("b", 40), "null", false},
		{"Ignore/Closed", "CLOSED", sha, "null", false},
		{"Ignore/AlreadyQueued", "OPEN", sha, `{"id":"queued"}`, false},
	} {
		t.Run("Approve/"+tc.name, func(t *testing.T) {
			t.Parallel()
			current := `{"data":{"node":{"state":"` + tc.state + `","headRefOid":"` + tc.head + `","mergeQueueEntry":` + tc.queue + `}}}`
			output, calls, err := runGovernanceScript(t, scripts["approve"], []string{"CURRENT=" + current, "HEAD_SHA=" + sha, "PR_NUMBER=522", "PR_NODE=PR_fixture"})
			if err != nil {
				t.Fatalf("script failed: %v: %s", err, output)
			}
			reviewed := strings.Contains(calls, "commit_id="+sha)
			queued := strings.Contains(calls, "expectedHeadOid: $sha") && strings.Contains(calls, "sha="+sha)
			if reviewed != tc.wantWrites || queued != tc.wantWrites {
				t.Fatalf("unexpected writes: %s", calls)
			}
			if strings.Contains(calls, "--squash") || strings.Contains(calls, "--admin") {
				t.Fatal("queue bypass")
			}
		})
	}
}

// TestRecoverPRsGovernance pins what recover-prs may restart. It re-arms only a
// queue drop for checks_timed_out, and through the [Agent] App: a merge armed by
// GITHUB_TOKEN pushes a commit that triggers no workflow on master.
func TestRecoverPRsGovernance(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("templates/governance/recover-prs.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On struct {
			PullRequest struct {
				Types []string `yaml:"types"`
			} `yaml:"pull_request"`
		} `yaml:"on"`
		Jobs map[string]struct {
			Steps []struct {
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(workflow.On.PullRequest.Types, ","); got != "dequeued,closed" {
		t.Errorf("recover-prs pull_request types = %q, want dequeued,closed", got)
	}
	requeue := workflow.Jobs["requeue"].Steps
	if len(requeue) != 2 || !strings.Contains(requeue[0].Run, `[ "$last" = checks_timed_out ]`) {
		t.Error("requeue must re-arm only after a checks_timed_out drop")
	}
	if !strings.Contains(requeue[len(requeue)-1].Uses, "generic-actions/enable-auto-merge@") ||
		requeue[len(requeue)-1].With["dry_run"] != "false" {
		t.Error("requeue must re-arm through the [Agent] App's enable-auto-merge action")
	}
	if !strings.Contains(workflow.Jobs["rebase"].Steps[0].Run, `index("CONFLICTING")`) {
		t.Error("rebase must dispatch Renovate only when a dependency PR conflicts")
	}
}

// TestAutoApproveHoldsReleaseAge pins what the approval engine needs to enforce
// Renovate's release age: the status event that reports it, and the statuses
// read the App lacks. A nested job asking for more than its caller grants fails.
func TestAutoApproveHoldsReleaseAge(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("templates/governance/auto-approve-dependabot.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On   map[string]any `yaml:"on"`
		Jobs map[string]struct {
			If          string            `yaml:"if"`
			Permissions map[string]string `yaml:"permissions"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		t.Fatal(err)
	}
	if _, ok := workflow.On["status"]; !ok {
		t.Error("auto-approve must trigger on status to approve once renovate/stability-days turns green")
	}
	job := workflow.Jobs["auto-approve"]
	if job.Permissions["statuses"] != "read" {
		t.Errorf("auto-approve permissions = %v, want statuses: read", job.Permissions)
	}
	if !strings.Contains(job.If, "'renovate/stability-days'") || !strings.Contains(job.If, "'success'") {
		t.Error("auto-approve must only run for the stability status turning green")
	}
}

// TestHotfixGovernance pins the two hotfix paths of the caller. A dispatch
// backports, and a backport merged into a release line cuts the patch. The cut
// rides pull_request_target, the event GitHub runs from the default branch's
// copy of the caller, and names its line from the merged pull request's base.
func TestHotfixGovernance(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("templates/governance/hotfix.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On struct {
			Dispatch struct {
				Inputs map[string]any `yaml:"inputs"`
			} `yaml:"workflow_dispatch"`
			PullRequestTarget struct {
				Types    []string `yaml:"types"`
				Branches []string `yaml:"branches"`
			} `yaml:"pull_request_target"`
			Push any `yaml:"push"`
		} `yaml:"on"`
		Concurrency any `yaml:"concurrency"`
		Jobs        map[string]struct {
			If          string            `yaml:"if"`
			Uses        string            `yaml:"uses"`
			With        map[string]string `yaml:"with"`
			Concurrency struct {
				Group string `yaml:"group"`
			} `yaml:"concurrency"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		t.Fatal(err)
	}
	if _, ok := workflow.On.Dispatch.Inputs["fix_refs"]; !ok {
		t.Error("the dispatch takes no fix_refs input")
	}
	if _, ok := workflow.On.Dispatch.Inputs["fix_ref"]; ok {
		t.Error("the dispatch still takes the fix_ref range input")
	}
	if got := strings.Join(workflow.On.PullRequestTarget.Types, ","); got != "closed" ||
		strings.Join(workflow.On.PullRequestTarget.Branches, ",") != "release/**" {
		t.Errorf("pull_request_target = %v on %v, want closed on release/**", got, workflow.On.PullRequestTarget.Branches)
	}
	// A push trigger would run the release line's own, older copy of the caller.
	if workflow.On.Push != nil {
		t.Error("the caller triggers on push")
	}
	// Workflow-level concurrency would let a skipped run replace a queued cut.
	if workflow.Concurrency != nil {
		t.Error("the caller sets workflow-level concurrency")
	}
	for job, want := range map[string]struct{ uses, condition, group string }{
		"backport": {"backport-run.yaml@", "github.event_name == 'workflow_dispatch'", "hotfix-${{ github.repository }}"},
		"cut": {
			"release-line-run.yaml@", "github.event.pull_request.merged",
			"release-line-${{ github.repository }}-${{ github.event.pull_request.base.ref }}",
		},
	} {
		got := workflow.Jobs[job]
		if !strings.Contains(got.Uses, want.uses) || !strings.Contains(got.If, want.condition) ||
			got.Concurrency.Group != want.group {
			t.Errorf("job %s = uses %q if %q group %q, want %q, %q, %q",
				job, got.Uses, got.If, got.Concurrency.Group, want.uses, want.condition, want.group)
		}
	}
	if got := workflow.Jobs["cut"].With["branch"]; got != "${{ github.event.pull_request.base.ref }}" {
		t.Errorf("cut branch = %q, want the merged pull request's base", got)
	}
}

// TestLockClosedGovernance pins when conversations lock: only once an issue or
// pull request closes. A locked conversation refuses a GitHub App's review, so
// locking an open pull request would block the dependency bots' approval.
func TestLockClosedGovernance(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile("templates/governance/lock-closed.yaml")
	if err != nil {
		t.Fatal(err)
	}
	type trigger struct {
		Types []string `yaml:"types"`
	}
	var workflow struct {
		On struct {
			Issues      trigger `yaml:"issues"`
			PullRequest trigger `yaml:"pull_request"`
		} `yaml:"on"`
	}
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		t.Fatal(err)
	}
	for event, types := range map[string][]string{"issues": workflow.On.Issues.Types, "pull_request": workflow.On.PullRequest.Types} {
		if got := strings.Join(types, ","); got != "closed,reopened" {
			t.Errorf("lock-closed %s types = %q, want closed,reopened", event, got)
		}
	}
}
