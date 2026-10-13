package repocfg_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// botCommit is a commit as the REST API lists Dependabot's own: GitHub signs it
// and records web-flow as its committer.
const botCommit = `{"author":{"login":"dependabot[bot]"},"committer":{"login":"web-flow"},"commit":{"verification":{"verified":true}}}`

// TestDependabotGovernance runs the auto-approval scripts against a stubbed gh.
// The engine must also hold Renovate's release age: it triggers on the status
// event that reports it and reads the statuses the App lacks, since a nested
// job asking for more than its caller grants fails.
func TestDependabotGovernance(t *testing.T) {
	t.Parallel()
	var workflow struct {
		On   map[string]any `yaml:"on"`
		Jobs map[string]struct {
			If          string            `yaml:"if"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				ID   string `yaml:"id"`
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	loadWorkflow(t, "auto-approve-dependabot.yaml", &workflow)
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
		{name: "Ignore/HumanOnSecondPage", commits: "[[" + botCommit + `],[{"author":{"login":"human"}}]]`},
		// A push can claim Dependabot's email; only GitHub signs as web-flow.
		{
			name:    "Ignore/PushedAsBot",
			commits: `[[{"author":{"login":"dependabot[bot]"},"committer":{"login":"human"},"commit":{"verification":{"verified":true}}}]]`,
		},
		{
			name:    "Ignore/Unverified",
			commits: `[[{"author":{"login":"dependabot[bot]"},"committer":{"login":"web-flow"},"commit":{"verification":{"verified":false}}}]]`,
		},
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
				commits = "[[" + botCommit + "]]"
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
	loadWorkflow(t, "recover-prs.yaml", &workflow)
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

// TestHotfixGovernance pins the two hotfix paths of the caller. A dispatch
// backports, and a backport merged into a release line cuts the patch. The cut
// rides pull_request_target, the event GitHub runs from the default branch's
// copy of the caller, and names its line from the merged pull request's base.
func TestHotfixGovernance(t *testing.T) {
	t.Parallel()
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
	loadWorkflow(t, "hotfix.yaml", &workflow)
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

// TestCheckDependenciesGovernance pins what keeps the dependency gate honest:
// label changes re-run it, a push re-checks it, and it reports on merge-queue
// groups, which the required check would otherwise leave waiting. The job id is
// the check context checks.yaml requires.
func TestCheckDependenciesGovernance(t *testing.T) {
	t.Parallel()
	var workflow struct {
		On struct {
			PullRequest struct {
				Types []string `yaml:"types"`
			} `yaml:"pull_request"`
		} `yaml:"on"`
		Jobs map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
		} `yaml:"jobs"`
	}
	// merge_group takes no options, so only its key shows it is there.
	var triggers struct {
		On map[string]any `yaml:"on"`
	}
	loadWorkflow(t, "check-dependencies.yaml", &workflow)
	loadWorkflow(t, "check-dependencies.yaml", &triggers)
	if got := strings.Join(workflow.On.PullRequest.Types, ","); got != "opened,reopened,synchronize,labeled,unlabeled" {
		t.Errorf("check-dependencies pull_request types = %q", got)
	}
	if _, ok := triggers.On["merge_group"]; !ok {
		t.Error("check-dependencies must run on merge_group, or a queued pull request waits out the timeout")
	}
	job, ok := workflow.Jobs["check-dependencies"]
	if !ok {
		t.Fatal("check-dependencies job missing; its id is the required check context")
	}
	if got := job.Permissions["pull-requests"]; got != "write" {
		t.Errorf("check-dependencies permissions = %v, want pull-requests: write to remove the label", job.Permissions)
	}
}

// TestReviewGateGovernance pins the review-gate caller: it re-evaluates on every
// event that can change the verdict, runs under an id clear of the check the
// App posts, lets only the latest run finish since nothing re-posts the check,
// and leaves Dependabot to the CI-completion job, which gates its pull requests
// against the same privileged labels.
func TestReviewGateGovernance(t *testing.T) {
	t.Parallel()
	type step struct {
		Uses string            `yaml:"uses"`
		With map[string]string `yaml:"with"`
	}
	var workflow struct {
		On struct {
			PullRequest struct {
				Types []string `yaml:"types"`
			} `yaml:"pull_request"`
			PullRequestReview struct {
				Types []string `yaml:"types"`
			} `yaml:"pull_request_review"`
		} `yaml:"on"`
		Concurrency struct {
			Group            string `yaml:"group"`
			CancelInProgress bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Jobs map[string]struct {
			If          string            `yaml:"if"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []step            `yaml:"steps"`
		} `yaml:"jobs"`
	}
	var triggers struct {
		On map[string]any `yaml:"on"`
	}
	loadWorkflow(t, "review-gate.yaml", &workflow)
	loadWorkflow(t, "review-gate.yaml", &triggers)
	if got := strings.Join(workflow.On.PullRequest.Types, ","); got != "opened,reopened,synchronize,labeled,unlabeled" {
		t.Errorf("review-gate pull_request types = %q", got)
	}
	if got := strings.Join(workflow.On.PullRequestReview.Types, ","); got != "submitted,dismissed" {
		t.Errorf("review-gate pull_request_review types = %q", got)
	}
	if _, ok := triggers.On["merge_group"]; !ok {
		t.Error("review-gate must run on merge_group, or a queued pull request waits out the timeout")
	}
	if !workflow.Concurrency.CancelInProgress || !strings.Contains(workflow.Concurrency.Group, "github.event.pull_request.number") {
		t.Errorf("concurrency = %+v, want a per-pull-request group that cancels a superseded run", workflow.Concurrency)
	}
	if _, ok := workflow.Jobs["review-gate"]; ok {
		t.Error("a review-gate job would collide with the check the App posts")
	}
	job, ok := workflow.Jobs["evaluate"]
	if !ok {
		t.Fatal("evaluate job missing")
	}
	if !strings.Contains(job.If, "vars.DEPENDABOT_SECURITY_LOGIN") || len(job.Permissions) != 0 || len(job.Steps) != 1 {
		t.Errorf("evaluate job = %+v, want the Dependabot skip, no permissions and one review-gate step", job)
	}
	gate := job.Steps[0]
	if !strings.HasPrefix(gate.Uses, "a-novel-kit/workflows/generic-actions/review-gate@") || gate.With["privileged_labels"] == "" {
		t.Fatalf("evaluate step = %+v, want review-gate with its privileged labels", gate)
	}

	var dependabot struct {
		Jobs map[string]struct {
			Steps []step `yaml:"steps"`
		} `yaml:"jobs"`
	}
	loadWorkflow(t, "auto-approve-dependabot.yaml", &dependabot)
	i := slices.IndexFunc(dependabot.Jobs["dependabot"].Steps, func(s step) bool { return s.Uses == gate.Uses })
	if i < 0 {
		t.Fatalf("the dependabot job does not run %s", gate.Uses)
	}
	ci := dependabot.Jobs["dependabot"].Steps[i]
	if ci.With["privileged_labels"] != gate.With["privileged_labels"] {
		t.Errorf("Dependabot privileged labels = %q, want review-gate.yaml's %q", ci.With["privileged_labels"], gate.With["privileged_labels"])
	}
	if ci.With["pull_request_number"] == "" || ci.With["head_sha"] == "" {
		t.Errorf("Dependabot review-gate inputs = %v, want the pull request number and head its event lacks", ci.With)
	}
}

// TestFeatureEnvGovernance pins how long with-feature-env lasts: until the next
// push. The removal runs on GITHUB_TOKEN, which triggers no new run, and treats
// a label an earlier push already removed as done.
func TestFeatureEnvGovernance(t *testing.T) {
	t.Parallel()
	var workflow struct {
		On struct {
			PullRequest struct {
				Types []string `yaml:"types"`
			} `yaml:"pull_request"`
		} `yaml:"on"`
		Jobs map[string]struct {
			If          string            `yaml:"if"`
			Permissions map[string]string `yaml:"permissions"`
			Env         map[string]string `yaml:"env"`
			Steps       []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	loadWorkflow(t, "feature-env.yaml", &workflow)
	if got := strings.Join(workflow.On.PullRequest.Types, ","); got != "synchronize" {
		t.Errorf("feature-env pull_request types = %q, want synchronize", got)
	}
	job := workflow.Jobs["revoke-feature-env"]
	if job.If != "contains(github.event.pull_request.labels.*.name, 'with-feature-env')" {
		t.Errorf("revoke-feature-env if = %q, want the with-feature-env label", job.If)
	}
	if job.Permissions["pull-requests"] != "write" || job.Env["GH_TOKEN"] != "${{ github.token }}" {
		t.Errorf("revoke-feature-env = %v / %v, want pull-requests: write through GITHUB_TOKEN", job.Permissions, job.Env)
	}
	if len(job.Steps) != 1 {
		t.Fatalf("revoke-feature-env steps = %d, want 1", len(job.Steps))
	}

	for _, tc := range []struct {
		name, labelError string
		wantError        bool
	}{
		{name: "Removed"},
		{name: "AlreadyRemoved", labelError: "gh: Label does not exist (HTTP 404)"},
		{name: "Error", labelError: "gh: Resource not accessible by integration (HTTP 403)", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			output, calls, err := runGovernanceScript(t, job.Steps[0].Run, []string{
				"GITHUB_REPOSITORY=example/repo", "NUMBER=7", "LABEL_ERROR=" + tc.labelError,
			})
			if (err != nil) != tc.wantError {
				t.Fatalf("script error = %v, output = %s", err, output)
			}
			if !strings.Contains(calls, "--method DELETE repos/example/repo/issues/7/labels/with-feature-env") {
				t.Errorf("calls = %q, want the label removed", calls)
			}
		})
	}
}

// TestLockClosedGovernance pins when conversations lock: only once an issue or
// pull request closes. A locked conversation refuses a GitHub App's review, so
// locking an open pull request would block the dependency bots' approval.
func TestLockClosedGovernance(t *testing.T) {
	t.Parallel()
	type trigger struct {
		Types []string `yaml:"types"`
	}
	var workflow struct {
		On struct {
			Issues      trigger `yaml:"issues"`
			PullRequest trigger `yaml:"pull_request"`
		} `yaml:"on"`
	}
	loadWorkflow(t, "lock-closed.yaml", &workflow)
	for event, types := range map[string][]string{"issues": workflow.On.Issues.Types, "pull_request": workflow.On.PullRequest.Types} {
		if got := strings.Join(types, ","); got != "closed,reopened" {
			t.Errorf("lock-closed %s types = %q, want closed,reopened", event, got)
		}
	}
}

// TestAutoAssignAuthorGovernance pins that only a person's pull request is
// assigned. Dependabot's and Renovate's pull requests both carry the Bot user
// type, so the job never starts for them.
func TestAutoAssignAuthorGovernance(t *testing.T) {
	t.Parallel()
	var workflow struct {
		Jobs map[string]struct {
			If string `yaml:"if"`
		} `yaml:"jobs"`
	}
	loadWorkflow(t, "auto-assign-author.yaml", &workflow)
	if got, want := workflow.Jobs["assign-author"].If, "github.event.pull_request.user.type != 'Bot'"; got != want {
		t.Errorf("auto-assign-author if = %q, want %q", got, want)
	}
}

// TestNotifyGovernance pins what the Discord notifier watches. Watching
// release as well as main is what reports a release that fails after its tag,
// and main on the bots' and the merge queue's branches is what reports a bot's
// failing pull request. pull_request_target, unlike pull_request, still fires
// while a pull request conflicts. Both engines need actions: read to list runs.
func TestNotifyGovernance(t *testing.T) {
	t.Parallel()
	type events struct {
		Types []string `yaml:"types"`
	}
	var workflow struct {
		On struct {
			WorkflowRun struct {
				Workflows []string `yaml:"workflows"`
				Branches  []string `yaml:"branches"`
			} `yaml:"workflow_run"`
			Release           events `yaml:"release"`
			PullRequestTarget events `yaml:"pull_request_target"`
			PullRequestReview events `yaml:"pull_request_review"`
		} `yaml:"on"`
		Jobs map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
		} `yaml:"jobs"`
	}
	loadWorkflow(t, "notify.yaml", &workflow)
	testCases := []struct {
		name string
		got  []string
		want string
	}{
		{name: "workflow_run workflows", got: workflow.On.WorkflowRun.Workflows, want: "main,release"},
		{
			name: "workflow_run branches",
			got:  workflow.On.WorkflowRun.Branches,
			want: "master,renovate/**,dependabot/**,chore/deps/**,gh-readonly-queue/**",
		},
		{name: "release types", got: workflow.On.Release.Types, want: "published"},
		{
			name: "pull_request_target types",
			got:  workflow.On.PullRequestTarget.Types,
			want: "opened,ready_for_review,converted_to_draft,assigned,unassigned," +
				"review_requested,review_request_removed,closed,reopened,dequeued",
		},
		{name: "pull_request_review types", got: workflow.On.PullRequestReview.Types, want: "submitted,dismissed"},
	}
	for _, testCase := range testCases {
		if got := strings.Join(testCase.got, ","); got != testCase.want {
			t.Errorf("notify %s = %q, want %q", testCase.name, got, testCase.want)
		}
	}
	for _, job := range []string{"ci", "pull-request"} {
		if got := workflow.Jobs[job].Permissions["actions"]; got != "read" {
			t.Errorf("notify %s permissions = %v, want actions: read", job, workflow.Jobs[job].Permissions)
		}
	}
}
