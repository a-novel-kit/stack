package repocfg_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

// botCommit is a commit as the REST API lists Dependabot's own: GitHub signs it
// and records web-flow as its committer.
const botCommit = `{"author":{"login":"dependabot[bot]"},"committer":{"login":"web-flow"},"commit":{"verification":{"verified":true}}}`

// TestDependabotGovernance runs the auto-approval scripts against a stubbed gh.
// The engine must also hold Renovate's release age: it triggers on the status
// event that reports it and reads the statuses the App lacks, since a nested
// job asking for more than its caller grants fails.
// TestGovernanceEnvironment pins where the privileged keys are read: a template
// job that runs steps with one names the governance environment, which no
// feature branch opens, without recording a deployment. A job calling a
// reusable workflow leaves the environment to the called job.
func TestGovernanceEnvironment(t *testing.T) {
	t.Parallel()
	var privileged []string
	for _, org := range []string{"a-novel", "a-novel-kit"} {
		profile, err := repocfg.LoadOrg(org)
		if err != nil {
			t.Fatalf("LoadOrg(%s): %v", org, err)
		}
		privileged = append(privileged, profile.Governance.Secrets...)
	}
	if len(privileged) == 0 {
		t.Fatal("no org profile names a privileged secret")
	}
	entries, err := os.ReadDir(filepath.Join("templates", "governance"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		var workflow struct {
			Jobs map[string]map[string]any `yaml:"jobs"`
		}
		loadWorkflow(t, entry.Name(), &workflow)
		for id, job := range workflow.Jobs {
			if _, calls := job["uses"]; calls {
				continue
			}
			raw, err := yaml.Marshal(job)
			if err != nil {
				t.Fatalf("%s %s: %v", entry.Name(), id, err)
			}
			named := slices.ContainsFunc(privileged, func(name string) bool {
				return strings.Contains(string(raw), "secrets."+name)
			})
			want := map[string]any{"name": "governance", "deployment": false}
			if environment := job["environment"]; named && !reflect.DeepEqual(environment, want) {
				t.Errorf("%s job %s reads a privileged secret with environment %v, want %v", entry.Name(), id, environment, want)
			}
		}
	}
}

func TestDependabotGovernance(t *testing.T) {
	t.Parallel()
	var workflow struct {
		On   map[string]any `yaml:"on"`
		Jobs map[string]struct {
			If          string            `yaml:"if"`
			Permissions map[string]string `yaml:"permissions"`
			With        map[string]string `yaml:"with"`
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
	// Infra deploys on merge, so its bot PRs merge alone only as release pins or Renovate's own updates.
	if job.With["strict"] != "${{ github.repository == 'a-novel/infra' }}" {
		t.Errorf("auto-approve strict = %q, want strict mode on a-novel/infra only", job.With["strict"])
	}
	// Renovate arms auto-merge after opening, which re-runs the strict check; the agent's arming does not.
	pullRequest, _ := workflow.On["pull_request_target"].(map[string]any)
	if types, _ := pullRequest["types"].([]any); !slices.Contains(types, any("auto_merge_enabled")) {
		t.Errorf("auto-approve pull_request_target types = %v, want auto_merge_enabled", types)
	}
	if !strings.Contains(job.If, "github.event.action != 'auto_merge_enabled' || github.event.sender.login == vars.DEPENDENCY_BOT_LOGIN") {
		t.Error("auto-approve must re-run on auto_merge_enabled only when the dependency bot armed it")
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
	resolveCases := []struct {
		name       string
		mutate     func(record)
		commits    string
		duplicate  bool
		wantOutput bool
		wantError  bool
	}{
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
	}

	for _, testCase := range resolveCases {
		t.Run("Resolve/"+testCase.name, func(t *testing.T) {
			t.Parallel()
			pr := record{
				"number":  522,
				"node_id": "PR_fixture",
				"state":   "open",
				"draft":   false,
				"user":    record{"login": bot},
				"head":    record{"sha": sha, "repo": record{"full_name": "example/repo"}},
				"base":    record{"ref": "master", "repo": record{"full_name": "example/repo", "default_branch": "master"}},
			}
			if testCase.mutate != nil {
				testCase.mutate(pr)
			}
			prs := []record{pr}
			if testCase.duplicate {
				prs = append(prs, pr)
			}
			payload, err := json.Marshal([][]record{prs})
			if err != nil {
				t.Fatal(err)
			}
			commits := testCase.commits
			if commits == "" {
				commits = "[[" + botCommit + "]]"
			}
			env := []string{"CANDIDATES=" + string(payload), "COMMITS=" + commits, "HEAD_SHA=" + sha, "BOT=" + bot}
			output, calls, err := runGovernanceScript(t, scripts["resolve"], env)
			if (err != nil) != testCase.wantError {
				t.Fatalf("script error=%v, output=%s", err, output)
			}
			if strings.Contains(output, "number=522") != testCase.wantOutput {
				t.Fatalf("unexpected output %q", output)
			}
			if strings.Contains(calls, "reviews") || strings.Contains(calls, "enqueuePullRequest") {
				t.Fatal("resolution performed a write")
			}
		})
	}

	approveCases := []struct {
		name, state, head, queue string
		wantWrites               bool
	}{
		{"Success", "OPEN", sha, "null", true},
		{"Ignore/StaleHead", "OPEN", strings.Repeat("b", 40), "null", false},
		{"Ignore/Closed", "CLOSED", sha, "null", false},
		{"Ignore/AlreadyQueued", "OPEN", sha, `{"id":"queued"}`, false},
	}

	for _, testCase := range approveCases {
		t.Run("Approve/"+testCase.name, func(t *testing.T) {
			t.Parallel()
			current := `{"data":{"node":{"state":"` + testCase.state + `","headRefOid":"` + testCase.head + `","mergeQueueEntry":` + testCase.queue + `}}}`
			env := []string{"CURRENT=" + current, "HEAD_SHA=" + sha, "PR_NUMBER=522", "PR_NODE=PR_fixture"}
			output, calls, err := runGovernanceScript(t, scripts["approve"], env)
			if err != nil {
				t.Fatalf("script failed: %v: %s", err, output)
			}
			reviewed := strings.Contains(calls, "commit_id="+sha)
			queued := strings.Contains(calls, "expectedHeadOid: $sha") && strings.Contains(calls, "sha="+sha)
			if reviewed != testCase.wantWrites || queued != testCase.wantWrites {
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
			} `yaml:"pull_request_target"`
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
		t.Errorf("recover-prs pull_request_target types = %q, want dequeued,closed", got)
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
// copy of the caller, names its line from the merged pull request's base, and
// passes the release App's credentials, whose key only the callee's environment
// resolves.
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
			Secrets     map[string]string `yaml:"secrets"`
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
	wantJobs := map[string]struct{ uses, condition, group string }{
		"backport": {"backport-run.yaml@", "github.event_name == 'workflow_dispatch'", "hotfix-${{ github.repository }}"},
		"cut": {
			"release-line-run.yaml@", "github.event.pull_request.merged",
			"release-line-${{ github.repository }}-${{ github.event.pull_request.base.ref }}",
		},
	}

	for job, want := range wantJobs {
		got := workflow.Jobs[job]
		if !strings.Contains(got.Uses, want.uses) || !strings.Contains(got.If, want.condition) ||
			got.Concurrency.Group != want.group {
			t.Errorf("job %s = uses %q if %q group %q, want %q, %q, %q",
				job, got.Uses, got.If, got.Concurrency.Group, want.uses, want.condition, want.group)
		}
	}
	cut := workflow.Jobs["cut"]
	if got := cut.With["branch"]; got != "${{ github.event.pull_request.base.ref }}" {
		t.Errorf("cut branch = %q, want the merged pull request's base", got)
	}
	// A missing release line is created by the release App, the one bot its ruleset admits.
	if backport := workflow.Jobs["backport"]; backport.With["release_client_id"] != "${{ vars.RELEASE_BOT_CLIENT_ID }}" ||
		backport.Secrets["release_private_key"] != "${{ secrets.RELEASE_BOT_PRIVATE_KEY }}" {
		t.Errorf("backport release credentials = %q, %q, want the release App's",
			backport.With["release_client_id"], backport.Secrets["release_private_key"])
	}
	// A clean backport is opened by the Dependencies App, so the agent can approve it.
	if backport := workflow.Jobs["backport"]; backport.With["dependency_client_id"] != "${{ vars.DEPENDENCY_BOT_CLIENT_ID }}" ||
		backport.Secrets["dependency_private_key"] != "${{ secrets.DEPENDENCY_BOT_PRIVATE_KEY }}" {
		t.Errorf("backport Dependencies credentials = %q, %q, want the Dependencies App's",
			backport.With["dependency_client_id"], backport.Secrets["dependency_private_key"])
	}
	if cut.With["release_client_id"] != "${{ vars.RELEASE_BOT_CLIENT_ID }}" ||
		cut.Secrets["release_private_key"] != "${{ secrets.RELEASE_BOT_PRIVATE_KEY }}" {
		t.Errorf("cut release credentials = %q, %q, want the release App's", cut.With["release_client_id"], cut.Secrets["release_private_key"])
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

// TestReviewGateGovernance pins the review-gate caller: it runs from the default
// branch, re-evaluates on every event that can change the verdict (reviews and merge
// groups through pr-relay), runs under an id clear of the check the App posts, runs
// one evaluation at a time since nothing re-posts the check, and leaves Dependabot
// to the CI-completion job, which gates its pull requests against the same
// privileged labels.
func TestReviewGateGovernance(t *testing.T) {
	t.Parallel()
	type step struct {
		ID   string            `yaml:"id"`
		If   string            `yaml:"if"`
		Uses string            `yaml:"uses"`
		With map[string]string `yaml:"with"`
	}
	var workflow struct {
		On struct {
			PullRequestTarget struct {
				Types []string `yaml:"types"`
			} `yaml:"pull_request_target"`
			WorkflowRun struct {
				Workflows []string `yaml:"workflows"`
			} `yaml:"workflow_run"`
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
	loadWorkflow(t, "review-gate.yaml", &workflow)
	if got := strings.Join(workflow.On.PullRequestTarget.Types, ","); got != "opened,reopened,synchronize,labeled,unlabeled" {
		t.Errorf("review-gate pull_request_target types = %q", got)
	}
	if !slices.Equal(workflow.On.WorkflowRun.Workflows, []string{"pr-relay"}) {
		t.Errorf("review-gate workflow_run = %v, want pr-relay, which carries reviews and merge groups", workflow.On.WorkflowRun.Workflows)
	}
	if workflow.Concurrency.CancelInProgress || !strings.Contains(workflow.Concurrency.Group, "github.event.pull_request.number") ||
		!strings.Contains(workflow.Concurrency.Group, "github.event.workflow_run.pull_requests[0].number") {
		t.Errorf("concurrency = %+v, want a per-pull-request group across both triggers that queues runs without cancelling a running one", workflow.Concurrency)
	}
	if _, ok := workflow.Jobs["review-gate"]; ok {
		t.Error("a review-gate job would collide with the check the App posts")
	}
	job, ok := workflow.Jobs["evaluate"]
	if !ok {
		t.Fatal("evaluate job missing")
	}
	if !strings.Contains(job.If, "vars.DEPENDABOT_SECURITY_LOGIN") || !maps.Equal(job.Permissions, map[string]string{"pull-requests": "read"}) ||
		len(job.Steps) != 2 {
		t.Errorf("evaluate job = %+v, want the Dependabot skip, pull-requests read for relay-context, and two steps", job)
	}
	relay, gate := job.Steps[0], job.Steps[1]
	if relay.ID != "relay" || !strings.HasPrefix(relay.Uses, "a-novel-kit/workflows/generic-actions/relay-context@") ||
		relay.If != "github.event_name == 'workflow_run'" {
		t.Errorf("first step = %+v, want relay-context on workflow_run only", relay)
	}
	if !strings.HasPrefix(gate.Uses, "a-novel-kit/workflows/generic-actions/review-gate@") || gate.With["privileged_labels"] == "" ||
		gate.With["event"] != "${{ steps.relay.outputs.event || github.event_name }}" ||
		gate.With["head_sha"] != "${{ steps.relay.outputs.head_sha || github.event.pull_request.head.sha }}" {
		t.Fatalf("evaluate step = %+v, want review-gate fed by relay-context, with its privileged labels", gate)
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

// TestPRRelayGovernance pins the relay that keeps the Agent key out of branch
// workflows. Reviews and merge groups run a pull request's own copy of a workflow,
// so the relay holds no permission and names no secret, and no workflow that reads
// the Agent key runs on those events or on pull_request: each runs from the default
// branch, reaching reviews and merge groups through the relay's workflow_run.
func TestPRRelayGovernance(t *testing.T) {
	t.Parallel()
	var relay struct {
		On          map[string]any `yaml:"on"`
		Permissions map[string]any `yaml:"permissions"`
		Jobs        map[string]struct {
			Permissions map[string]any `yaml:"permissions"`
		} `yaml:"jobs"`
	}
	loadWorkflow(t, "pr-relay.yaml", &relay)
	if len(relay.On) != 2 || relay.On["pull_request_review"] == nil {
		t.Errorf("pr-relay triggers = %v, want pull_request_review and merge_group", relay.On)
	}
	if _, ok := relay.On["merge_group"]; !ok {
		t.Error("pr-relay must run on merge_group, or a queued pull request waits out the timeout")
	}
	if relay.Permissions == nil || len(relay.Permissions) != 0 {
		t.Errorf("pr-relay permissions = %v, want {}", relay.Permissions)
	}
	for name, job := range relay.Jobs {
		if job.Permissions == nil || len(job.Permissions) != 0 {
			t.Errorf("pr-relay job %s permissions = %v, want {}", name, job.Permissions)
		}
	}
	content, err := os.ReadFile(filepath.Join("templates", "governance", "pr-relay.yaml"))
	if err != nil {
		panic(err)
	}
	if strings.Contains(string(content), "secrets.") {
		t.Error("pr-relay must name no secret: a pull request's own copy of it runs")
	}

	agentWorkflows := []string{"merge-gate.yaml", "review-gate.yaml", "epic-freeze.yaml", "derive-status.yaml", "recover-prs.yaml", "auto-approve-dependabot.yaml"}
	for _, name := range agentWorkflows {
		var workflow struct {
			On map[string]struct {
				Workflows []string `yaml:"workflows"`
			} `yaml:"on"`
		}
		loadWorkflow(t, name, &workflow)
		untrustedEvents := []string{"pull_request", "pull_request_review", "merge_group"}
		for _, branch := range untrustedEvents {
			if _, ok := workflow.On[branch]; ok {
				t.Errorf("%s runs on %s, which runs a pull request's own copy with the Agent key", name, branch)
			}
		}
		if _, ok := workflow.On["pull_request_target"]; !ok {
			t.Errorf("%s does not run on pull_request_target", name)
		}
	}
	relayedWorkflows := []string{"merge-gate.yaml", "review-gate.yaml", "epic-freeze.yaml", "derive-status.yaml"}
	for _, name := range relayedWorkflows {
		var workflow struct {
			On map[string]struct {
				Workflows []string `yaml:"workflows"`
			} `yaml:"on"`
		}
		loadWorkflow(t, name, &workflow)
		if !slices.Equal(workflow.On["workflow_run"].Workflows, []string{"pr-relay"}) {
			t.Errorf("%s workflow_run = %v, want pr-relay", name, workflow.On["workflow_run"].Workflows)
		}
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

	testCases := []struct {
		name, labelError string
		wantError        bool
	}{
		{name: "Removed"},
		{name: "AlreadyRemoved", labelError: "gh: Label does not exist (HTTP 404)"},
		{name: "Error", labelError: "gh: Resource not accessible by integration (HTTP 403)", wantError: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			env := []string{"GITHUB_REPOSITORY=example/repo", "NUMBER=7", "LABEL_ERROR=" + testCase.labelError}
			output, calls, err := runGovernanceScript(t, job.Steps[0].Run, env)
			if (err != nil) != testCase.wantError {
				t.Fatalf("script error = %v, output = %s", err, output)
			}
			if !strings.Contains(calls, "--method DELETE repos/example/repo/issues/7/labels/with-feature-env") {
				t.Errorf("calls = %q, want the label removed", calls)
			}
		})
	}
}

// TestCleanImagesGovernance pins who may delete images and when: a closed same-repository pull request
// deletes its own tag, while only the dispatched sweep deletes the rest, one uncancelled
// job per package. Packages are named by the images the repository's own workflows publish, since a
// workflow token cannot list the organization's.
func TestCleanImagesGovernance(t *testing.T) {
	t.Parallel()
	type job struct {
		If          string            `yaml:"if"`
		Permissions map[string]string `yaml:"permissions"`
		Strategy    struct {
			MaxParallel int `yaml:"max-parallel"`
		} `yaml:"strategy"`
		Concurrency struct {
			Group            string `yaml:"group"`
			CancelInProgress bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Steps []struct {
			ID   string            `yaml:"id"`
			Run  string            `yaml:"run"`
			With map[string]string `yaml:"with"`
		} `yaml:"steps"`
	}
	var workflow struct {
		On          map[string]any    `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]job    `yaml:"jobs"`
	}
	loadWorkflow(t, "clean-images.yaml", &workflow)
	pullRequest, _ := workflow.On["pull_request"].(map[string]any)
	// a-novel/infra's Cloud Scheduler dispatches the sweep, since GitHub starts scheduled runs late.
	if len(workflow.On) != 2 || workflow.On["workflow_dispatch"] == nil || fmt.Sprint(pullRequest["types"]) != "[closed]" {
		t.Errorf("clean-images triggers = %v, want workflow_dispatch and closed pull requests", workflow.On)
	}
	if len(workflow.Permissions) != 0 {
		t.Errorf("clean-images workflow permissions = %v, want none", workflow.Permissions)
	}
	packages, closing, sweep := workflow.Jobs["packages"], workflow.Jobs["close"], workflow.Jobs["sweep"]
	if len(packages.Permissions) != 1 || packages.Permissions["contents"] != "read" {
		t.Errorf("packages permissions = %v, want contents: read only", packages.Permissions)
	}
	// A closed pull request's merge ref may be gone, so its base branch names the images.
	if ref := packages.Steps[0].With["ref"]; ref != "${{ github.event.pull_request.base.ref || github.ref }}" {
		t.Errorf("packages checkout ref = %q, want the pull request's base or the triggering ref", ref)
	}
	if len(closing.Permissions) != 1 || closing.Permissions["packages"] != "write" ||
		!strings.Contains(closing.If, "github.event_name == 'pull_request'") ||
		!strings.Contains(closing.If, "github.event.pull_request.head.repo.full_name == github.repository") {
		t.Errorf("close = %q / %v, want same-repository pull requests with packages: write only", closing.If, closing.Permissions)
	}
	if !strings.Contains(sweep.If, "github.event_name != 'pull_request'") {
		t.Errorf("sweep if = %q, want no pull request event to start a sweep", sweep.If)
	}
	if !maps.Equal(sweep.Permissions, map[string]string{"packages": "write", "pull-requests": "read", "attestations": "read"}) {
		t.Errorf("sweep permissions = %v, want packages: write, pull-requests: read and attestations: read", sweep.Permissions)
	}
	if sweep.Concurrency.Group != "clean-images-${{ matrix.package }}" || sweep.Concurrency.CancelInProgress {
		t.Errorf("sweep concurrency = %+v, want one uncancelled group per package", sweep.Concurrency)
	}
	if sweep.Strategy.MaxParallel != 1 {
		t.Errorf("sweep max-parallel = %d, want 1: deletions share the repository's API quota with its CI", sweep.Strategy.MaxParallel)
	}

	var list string
	for _, step := range packages.Steps {
		if step.ID == "list" {
			list = step.Run
		}
	}
	testCases := []struct {
		name      string
		workflows map[string]string
		want      string
	}{
		{
			name: "Images",
			workflows: map[string]string{
				"main.yaml":    "image_name: ${{ github.repository }}/database\nimage_name: ${{ github.repository }}/jobs/migrations\n",
				"release.yaml": "image_name: ${{ github.repository }}/database\nimage_name: \"ghcr.io/other\"\n",
				"studio.yaml":  "          image_name: ${{ github.repository }}\n",
			},
			want: `["service","service/database","service/jobs/migrations"]`,
		},
		// A template repository publishes no image, and the close and sweep jobs skip an empty list.
		{name: "NoImages", workflows: map[string]string{"clean-images.yaml": "name: clean images\n"}, want: `[]`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			workflows := filepath.Join(directory, ".github", "workflows")
			if err := os.MkdirAll(workflows, 0o700); err != nil {
				t.Fatal(err)
			}
			for name, content := range testCase.workflows {
				if err := os.WriteFile(filepath.Join(workflows, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.CommandContext(t.Context(), "bash", "-c", list)
			command.Dir = directory
			command.Env = append(os.Environ(), "GITHUB_REPOSITORY=a-novel/service", "GITHUB_OUTPUT="+filepath.Join(directory, "output"))
			if logs, err := command.CombinedOutput(); err != nil {
				t.Fatalf("list script: %v: %s", err, logs)
			}
			output, err := os.ReadFile(filepath.Join(directory, "output"))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := string(output), "names="+testCase.want+"\n"; got != want {
				t.Errorf("listed packages = %q, want %q", got, want)
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
		Concurrency struct {
			Group            string `yaml:"group"`
			CancelInProgress bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Jobs map[string]struct {
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	loadWorkflow(t, "lock-closed.yaml", &workflow)
	eventTypes := map[string][]string{"issues": workflow.On.Issues.Types, "pull_request": workflow.On.PullRequest.Types}
	for event, types := range eventTypes {
		if got := strings.Join(types, ","); got != "closed,reopened" {
			t.Errorf("lock-closed %s types = %q, want closed,reopened", event, got)
		}
	}
	// A close and a quick reopen queue in event order instead of racing.
	want := "lock-closed-${{ github.event.issue.number || github.event.pull_request.number }}"
	if workflow.Concurrency.Group != want || workflow.Concurrency.CancelInProgress {
		t.Errorf("concurrency = %+v, want group %q without cancel-in-progress", workflow.Concurrency, want)
	}

	// Whatever the event, the step acts on the conversation's live state.
	lock, unlock := "api --method PUT repos/example/repo/issues/7/lock", "api --method DELETE repos/example/repo/issues/7/lock"
	testCases := []struct {
		name, state, want string
	}{
		{name: "ClosedAndUnlocked", state: "closed false", want: lock},
		{name: "OpenAndLocked", state: "open true", want: unlock},
		{name: "ReopenedBeforeTheLock", state: "open false"},
		{name: "AlreadyLocked", state: "closed true"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			env := []string{"GITHUB_REPOSITORY=example/repo", "NUMBER=7", "ISSUE=" + testCase.state}
			output, calls, err := runGovernanceScript(t, workflow.Jobs["lock"].Steps[0].Run, env)
			if err != nil {
				t.Fatalf("lock step: %v\n%s", err, output)
			}
			writes := slices.DeleteFunc(strings.Split(strings.TrimSpace(calls), "\n"), func(call string) bool {
				return !strings.HasSuffix(call, "/lock")
			})
			if got := strings.Join(writes, ""); got != testCase.want {
				t.Errorf("writes = %q, want %q", got, testCase.want)
			}
		})
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
			Release           events  `yaml:"release"`
			PullRequestTarget events  `yaml:"pull_request_target"`
			PullRequestReview *events `yaml:"pull_request_review"`
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
			want: "opened,ready_for_review,review_requested,review_request_removed,closed,dequeued",
		},
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
	// A review runs a pull request's own copy of a workflow, so notify-reviews posts it from pr-relay.
	if workflow.On.PullRequestReview != nil {
		t.Error("notify runs on pull_request_review, which runs a pull request's own copy with the Agent key")
	}
	var reviews struct {
		On struct {
			WorkflowRun struct {
				Workflows []string `yaml:"workflows"`
			} `yaml:"workflow_run"`
		} `yaml:"on"`
		Jobs map[string]struct {
			If   string `yaml:"if"`
			Uses string `yaml:"uses"`
		} `yaml:"jobs"`
	}
	loadWorkflow(t, "notify-reviews.yaml", &reviews)
	if !slices.Equal(reviews.On.WorkflowRun.Workflows, []string{"pr-relay"}) || len(reviews.Jobs) != 1 ||
		reviews.Jobs["review"].If != "github.event.workflow_run.event == 'pull_request_review'" ||
		!strings.HasPrefix(reviews.Jobs["review"].Uses, "a-novel-kit/workflows/.github/workflows/notify-pr-run.yaml@") {
		t.Errorf("notify-reviews = %+v, want one job posting relayed reviews through notify-pr-run", reviews)
	}
}
