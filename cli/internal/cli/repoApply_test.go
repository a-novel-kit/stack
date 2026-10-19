package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

// testTarget is the repository the applyPlan tests reconcile.
var testTarget = &repocfg.RepoTarget{Org: "o", Repo: "r", DefaultBranch: branchMaster}

// liveOf reads a repository's live rulesets through the gh seam, as applyPlan
// does once per run.
func liveOf(org, repo string) func() (map[string]string, error) {
	return func() (map[string]string, error) { return liveRulesets(org, repo) }
}

// contentsJSON builds the contents-API body GitHub returns for a deployed file
// holding text.
func contentsJSON(t *testing.T, text string) string {
	t.Helper()
	raw, err := json.Marshal(ghContent{
		SHA:      blobSHA(text),
		Content:  base64.StdEncoding.EncodeToString([]byte(text)),
		Encoding: "base64",
	})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func TestApplyPlan(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	// No master ruleset exists yet, so it is created; the codecov one is updated.
	responses := map[string]string{
		"rulesets --jq":        "codecov\t777\n",
		"git/ref/heads/master": "headoid123\n",
		"repos/o/r --jq .id":   "4321\n",
	}
	calls := fakeGH(t, responses)

	plan := &repocfg.Plan{Ops: []repocfg.Op{
		{Method: "PATCH", Path: "repos/o/r", Body: map[string]any{"has_wiki": false}},
		{Method: "PATCH", Path: "repos/o/r/code-scanning/default-setup", Body: map[string]any{"state": "not-configured"}},
		{Method: "PUT", Path: "repos/o/r/actions/permissions/workflow", Body: map[string]any{"default_workflow_permissions": "read"}},
		{Method: "PUT", Path: "orgs/o/actions/secrets/USED/repositories", Secret: "USED"},
		{Method: "DELETE", Path: "orgs/o/actions/secrets/UNUSED/repositories", Secret: "UNUSED"},
		{Method: "PUT", Path: "repos/o/r/contents/.github/workflows/managed.yaml", Content: "name: Managed\n"},
		{RulesetName: branchMaster, Path: "repos/o/r/rulesets", Body: &repocfg.APIRuleset{Name: branchMaster}},
		{RulesetName: "codecov", Path: "repos/o/r/rulesets", Body: &repocfg.APIRuleset{Name: "codecov"}},
	}}

	if err := applyPlan(io.Discard, testTarget, plan); err != nil {
		t.Fatalf("applyPlan: %v", err)
	}

	joined := strings.Join(*calls, "\n")
	wantCalls := []string{
		"api -X PATCH repos/o/r --input -",                              // settings
		"api -X PATCH repos/o/r/code-scanning/default-setup --input -",  // code scanning stays off
		"api -X PUT repos/o/r/actions/permissions/workflow --input -",   // a settings op keeps its method
		"api -X PUT orgs/o/actions/secrets/USED/repositories/4321",      // scoped secret granted by repo id
		"api -X DELETE orgs/o/actions/secrets/UNUSED/repositories/4321", // and the unused one revoked
		"api repos/o/r/git/ref/heads/master --jq .object.sha",           // sync commit: read the branch tip
		`"expectedHeadOid":"headoid123"`,                                // sync commit: optimistic lock on that tip
		`"path":".github/workflows/managed.yaml"`,                       // sync commit: carries the staged file
		"api -X POST repos/o/r/rulesets --input -",                      // master ruleset created (no existing id)
		"api -X PUT repos/o/r/rulesets/777 --input -",                   // codecov ruleset updated (existing id 777)
	}

	for _, want := range wantCalls {
		if !strings.Contains(joined, want) {
			t.Errorf("expected a gh call containing %q; calls:\n%s", want, joined)
		}
	}
	if n := countCalls(*calls, "repos/o/r --jq .id"); n != 1 {
		t.Errorf("repository id read %d times, want once for every secret op", n)
	}
	// Managed files land in the one sync commit, never as per-file REST PUTs.
	if strings.Contains(joined, "-X PUT repos/o/r/contents/") {
		t.Errorf("managed files must land via the sync commit, not per-file PUTs; calls:\n%s", joined)
	}
}

func TestStageContents(t *testing.T) {
	// Not parallel: sub-tests swap the package-level ghStdin seam.
	const (
		managed  = "name: Managed\n"
		owners   = "* @a-novel-kit/maintainers\n"
		wfPath   = "repos/o/r/contents/.github/workflows/managed.yaml"
		gatePath = "repos/o/r/contents/.github/workflows/merge-gate.yaml"
	)
	pin := func(v string) string {
		return "      - uses: a-novel-kit/workflows/generic-actions/merge-gate@" + v + "\n"
	}
	deployedGate := map[string]string{gatePath: contentsJSON(t, pin("v1.15.0"))}

	testCases := []struct {
		name          string
		responses     map[string]string
		op            repocfg.Op
		wantChanges   []contentChange
		wantUnchanged bool
	}{
		{
			name:          "Success/Unchanged",
			responses:     map[string]string{"--jq .sha": blobSHA(managed) + "\n"},
			op:            repocfg.Op{Path: wfPath, Content: managed},
			wantUnchanged: true,
		},
		{
			// The sha read yields "": the file does not exist yet.
			name:        "Success/NewFile",
			op:          repocfg.Op{Path: wfPath, Content: managed},
			wantChanges: []contentChange{{path: ".github/workflows/managed.yaml", content: managed, outcome: opCreated}},
		},
		{
			name:        "Success/Drifted",
			responses:   map[string]string{"--jq .sha": "someothersha\n"},
			op:          repocfg.Op{Path: wfPath, Content: managed},
			wantChanges: []contentChange{{path: ".github/workflows/managed.yaml", content: managed, outcome: opUpdated}},
		},
		{
			// Both copies report this sha: the .github/ copy is unchanged, and
			// the stray root copy is deleted anyway.
			name:          "Success/StrayRootCODEOWNERS",
			responses:     map[string]string{"--jq .sha": blobSHA(owners) + "\n"},
			op:            repocfg.Op{Path: "repos/o/r/contents/.github/CODEOWNERS", Content: owners},
			wantChanges:   []contentChange{{path: "CODEOWNERS", outcome: opDeleted}},
			wantUnchanged: true,
		},
		{
			// Renovate bumped the deployed caller past the template's pin; the
			// sync must not write the older pin back.
			name:          "Success/KeepsNewerDeployedPin",
			responses:     deployedGate,
			op:            repocfg.Op{Path: gatePath, Content: pin("v1.14.0")},
			wantUnchanged: true,
		},
		{
			name:        "Success/UpgradesToNewerTemplatePin",
			responses:   deployedGate,
			op:          repocfg.Op{Path: gatePath, Content: pin("v1.16.0")},
			wantChanges: []contentChange{{path: ".github/workflows/merge-gate.yaml", content: pin("v1.16.0"), outcome: opUpdated}},
		},
		{
			// A genuine template edit still lands, but the pin is not downgraded.
			name:      "Success/TemplateEditKeepsNewerPin",
			responses: deployedGate,
			op:        repocfg.Op{Path: gatePath, Content: "# refreshed comment\n" + pin("v1.14.0")},
			wantChanges: []contentChange{{
				path: ".github/workflows/merge-gate.yaml", content: "# refreshed comment\n" + pin("v1.15.0"), outcome: opUpdated,
			}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fakeGH(t, testCase.responses)
			changes, unchanged, err := stageContents(testCase.op)
			if err != nil || unchanged != testCase.wantUnchanged || !slices.Equal(changes, testCase.wantChanges) {
				t.Fatalf("stageContents = (%+v, %v, %v), want (%+v, %v, nil)", changes, unchanged, err, testCase.wantChanges, testCase.wantUnchanged)
			}
		})
	}
}

func TestCommitSync(t *testing.T) {
	// Not parallel: sub-tests swap the package-level ghStdin seam.
	changes := []contentChange{
		{path: ".github/workflows/managed.yaml", content: "name: Managed\n", outcome: opUpdated},
		{path: "CODEOWNERS", outcome: opDeleted},
	}

	t.Run("Success", func(t *testing.T) {
		// One mutation carries both the additions and the deletions.
		calls := fakeGH(t, map[string]string{"git/ref/heads/master": "headoid123\n", "api graphql": "fc89f28c5489\n"})
		detail, err := commitSync("o", "r", branchMaster, changes)
		if err != nil || detail != "fc89f28" {
			t.Fatalf("commitSync = (%q, %v), want the short commit id", detail, err)
		}
		joined := strings.Join(*calls, "\n")
		wantPayload := []string{
			`"expectedHeadOid":"headoid123"`,
			`"additions":[{"contents":"` + base64.StdEncoding.EncodeToString([]byte("name: Managed\n")),
			`"deletions":[{"path":"CODEOWNERS"}]`,
			`"headline":"ci: sync managed config (managed, CODEOWNERS)"`,
		}

		for _, want := range wantPayload {
			if !strings.Contains(joined, want) {
				t.Errorf("expected the mutation payload to contain %q; calls:\n%s", want, joined)
			}
		}
	})

	t.Run("Success/EmptyRepoSeedsViaContentsAPI", func(t *testing.T) {
		// A fresh repo has no commit: the ref read answers 409 "empty", and so
		// would the Git Data endpoints. Only the Contents API can create the
		// first commit, so the lone added file lands through a contents PUT,
		// never createCommitOnBranch.
		calls := stubGH(t, func(call string) (string, error) {
			switch {
			case strings.Contains(call, "git/ref/heads/master"):
				return "", errGHEmptyRepo
			case strings.Contains(call, "PUT repos/o/r/contents/"):
				return "c0mm1t5ha0000\n", nil // .commit.sha
			}
			return "", nil
		})
		detail, err := commitSync("o", "r", branchMaster, changes[:1])
		if err != nil || detail != "c0mm1t5" {
			t.Fatalf("commitSync = (%q, %v), want the short seed-commit id", detail, err)
		}
		joined := strings.Join(*calls, "\n")
		wantSeed := []string{
			"api -X PUT repos/o/r/contents/.github/workflows/managed.yaml", // the seed file
			`"branch":"master"`, // ...onto the default branch
			`"content":"`,       // ...carrying its base64 content
		}

		for _, want := range wantSeed {
			if !strings.Contains(joined, want) {
				t.Errorf("expected a Contents-API seed call containing %q; calls:\n%s", want, joined)
			}
		}
		if strings.Contains(joined, "api graphql") {
			t.Errorf("empty-repo seed must not use createCommitOnBranch; calls:\n%s", joined)
		}
	})

	t.Run("Success/EmptyRepoSeedsOneFileThenSyncs", func(t *testing.T) {
		// The first added file seeds the initial commit; the branch then
		// exists, so the rest land in one normal createCommitOnBranch.
		headReads := 0
		calls := stubGH(t, func(call string) (string, error) {
			switch {
			case strings.Contains(call, "git/ref/heads/master"):
				headReads++
				if headReads == 1 {
					return "", errGHEmptyRepo
				}
				return "seedoid\n", nil // the seed commit is now the tip
			case strings.Contains(call, "PUT repos/o/r/contents/"):
				return "seedcommit123\n", nil
			case strings.Contains(call, "api graphql"):
				return "restcommit456\n", nil
			}
			return "", nil
		})
		created := []contentChange{
			{path: ".github/workflows/managed.yaml", content: "name: Managed\n", outcome: opCreated},
			{path: ".github/CODEOWNERS", content: "* @team\n", outcome: opCreated},
		}
		detail, err := commitSync("o", "r", branchMaster, created)
		if err != nil || detail != "restcom" {
			t.Fatalf("commitSync = (%q, %v), want the follow-up sync commit's short id", detail, err)
		}
		if puts, mutations := countCalls(*calls, "PUT repos/o/r/contents/"), countCalls(*calls, "api graphql"); puts != 1 || mutations != 1 {
			t.Fatalf("got %d Contents PUT / %d mutation(s), want 1 / 1", puts, mutations)
		}
	})

	t.Run("Success/StaleTipRetriesOnce", func(t *testing.T) {
		mutations := 0
		calls := stubGH(t, func(call string) (string, error) {
			if strings.Contains(call, "git/ref/heads") {
				return "headoid123\n", nil
			}
			mutations++
			if mutations == 1 {
				return `{"errors":[{"type":"STALE_DATA"}]}`, errors.New(`exit 1: gh: Expected branch to point to "x" but it did not.`)
			}
			return "fc89f28c5489\n", nil
		})
		if _, err := commitSync("o", "r", branchMaster, changes); err != nil {
			t.Fatalf("commitSync after retry: %v", err)
		}
		if headReads := countCalls(*calls, "git/ref/heads"); mutations != 2 || headReads != 2 {
			t.Fatalf("got %d mutations / %d head reads, want 2 / 2", mutations, headReads)
		}
	})

	t.Run("Error/MissingWorkflowScope", func(t *testing.T) {
		stubGH(t, func(call string) (string, error) {
			if strings.Contains(call, "git/ref/heads") {
				return "headoid123\n", nil
			}
			return "", errors.New("exit 1: gh: refusing to update workflow files without the workflow scope")
		})
		detail, err := commitSync("o", "r", branchMaster, changes)
		if err == nil || !strings.Contains(detail, "workflow") || !strings.Contains(detail, "scope") {
			t.Fatalf("commitSync = (%q, %v), want an error whose detail hints the `workflow` scope", detail, err)
		}
	})
}

func TestSyncCommitMessage(t *testing.T) {
	t.Run("short change lists name the files", func(t *testing.T) {
		headline, body := syncCommitMessage([]contentChange{
			{path: ".github/workflows/managed.yaml", outcome: opUpdated},
			{path: "CODEOWNERS", outcome: opDeleted},
		})
		if headline != "ci: sync managed config (managed, CODEOWNERS)" {
			t.Errorf("headline = %q", headline)
		}
		wantBody := []string{"updated .github/workflows/managed.yaml", "deleted CODEOWNERS", "Managed by a-novel repo sync."}
		for _, want := range wantBody {
			if !strings.Contains(body, want) {
				t.Errorf("body should contain %q; got:\n%s", want, body)
			}
		}
	})

	t.Run("long change lists fall back to a count", func(t *testing.T) {
		headline, _ := syncCommitMessage([]contentChange{
			{path: ".github/CODEOWNERS"},
			{path: ".github/workflows/managed.yaml"},
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
	// Not parallel: swaps the package-level ghStdin seam. The org locks the
	// signoff field, so the first PATCH is rejected and retried without it.
	rejected := false
	calls := stubGH(t, func(string) (string, error) {
		if !rejected {
			rejected = true
			return "", errors.New("HTTP 422: Commit signoff is enforced by the organization and cannot be disabled")
		}
		return "", nil
	})

	op := repocfg.Op{Method: "PATCH", Path: "repos/o/r", Body: map[string]any{
		"has_wiki":                    false,
		"web_commit_signoff_required": true,
	}}
	if err := applySettings(op); err != nil {
		t.Fatalf("applySettings: %v", err)
	}
	if len(*calls) != 2 || strings.Contains((*calls)[1], "web_commit_signoff_required") {
		t.Fatalf("calls = %q, want a retry PATCH without web_commit_signoff_required", *calls)
	}
}

func TestApplySecretAccess(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam. GitHub answers 409
	// while the secret is visible to every repo, and 404 to a revoke of a repo
	// it never selected, as every revoke of the first fleet reconcile showed.
	testCases := []struct {
		name, method, status, wantDetail string
		wantErr                          bool
	}{
		{name: "Success/Granted", method: "PUT", wantDetail: "granted"},
		{name: "Success/Revoked", method: "DELETE", wantDetail: "revoked"},
		{name: "Success/AlreadyRevoked", method: "DELETE", status: "HTTP 404: Not Found", wantDetail: "already revoked"},
		{name: "Error/GrantNotFound", method: "PUT", status: "HTTP 404: Not Found", wantErr: true},
		{name: "Error/NotSelected", method: "DELETE", status: "HTTP 409: Conflict", wantDetail: "selected repositories", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stubGH(t, func(string) (string, error) {
				if testCase.status == "" {
					return "", nil
				}
				return "", errors.New(testCase.status)
			})
			op := repocfg.Op{Method: testCase.method, Path: "orgs/o/actions/secrets/S/repositories", Secret: "S"}
			detail, err := applySecretAccess(op, func() (string, error) { return "4321", nil })
			if (err != nil) != testCase.wantErr || !strings.Contains(detail, testCase.wantDetail) {
				t.Fatalf("detail, err = %q, %v; want %q, error %v", detail, err, testCase.wantDetail, testCase.wantErr)
			}
		})
	}
}

func TestApplyRulesetBadBody(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam. A malformed plan
	// (wrong body type) must fail fast, not POST a null body.
	calls := fakeGH(t, nil)
	op := repocfg.Op{RulesetName: branchMaster, Path: "repos/o/r/rulesets", Body: map[string]any{"name": "x"}}
	if _, err := applyRuleset(op, liveOf("o", "r")); err == nil {
		t.Fatal("expected an error for a non-*APIRuleset body")
	}
	if slices.ContainsFunc(*calls, func(c string) bool { return strings.Contains(c, "-X POST") || strings.Contains(c, "-X PUT") }) {
		t.Errorf("must not write a ruleset with a bad body; calls %q", *calls)
	}
}

func TestPreserveNewerPins(t *testing.T) {
	t.Parallel()

	const (
		gate    = "a-novel-kit/workflows/generic-actions/merge-gate@"
		autoMrg = "a-novel-kit/workflows/generic-actions/enable-auto-merge@"
	)

	testCases := []struct{ name, desired, deployed, want string }{
		{"Success/DeployedNewerIsPreserved", gate + "v1.14.0", gate + "v1.15.0", gate + "v1.15.0"},
		{"Success/DeployedOlderKeepsTemplate", gate + "v1.14.0", gate + "v1.13.0", gate + "v1.14.0"},
		{"Success/Equal", gate + "v1.14.0", gate + "v1.14.0", gate + "v1.14.0"},
		{"Success/PerPath", gate + "v1.14.0\n" + autoMrg + "v1.14.0", gate + "v1.15.0\n" + autoMrg + "v1.14.0", gate + "v1.15.0\n" + autoMrg + "v1.14.0"},
		{"Success/DeployedHasNoPins", gate + "v1.14.0", "no workflows pins here", gate + "v1.14.0"},
		// v1.10.0 > v1.2.0 by semver, though lexically the reverse.
		{"Success/SemverNotLexical", gate + "v1.2.0", gate + "v1.10.0", gate + "v1.10.0"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := preserveNewerPins(testCase.desired, testCase.deployed); got != testCase.want {
				t.Fatalf("preserveNewerPins() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestContentText(t *testing.T) {
	// Not parallel: sub-tests swap the package-level ghStdin seam.
	const path = "repos/o/r/contents/x.yaml"
	text := "hello: world\n"
	testCases := []struct {
		name              string
		out               string
		err               error
		wantText, wantSHA string
		wantErr           bool
	}{
		{name: "Success", out: contentsJSON(t, text), wantText: text, wantSHA: blobSHA(text)},
		{name: "Success/NotFound", err: errGHNotFound},
		// Files over 1MB are not base64-encoded: sha only, a safe no-splice fallback.
		{name: "Success/NonBase64", out: `{"sha":"deadbeef","content":"","encoding":"none"}`, wantSHA: "deadbeef"},
		{name: "Error/Propagated", err: errGHBadCredentials, wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			calls := stubGH(t, func(string) (string, error) { return testCase.out, testCase.err })
			got, sha, err := contentText(path)
			if got != testCase.wantText || sha != testCase.wantSHA || (err != nil) != testCase.wantErr || !strings.Contains((*calls)[0], path) {
				t.Fatalf("contentText = (%q, %q, %v) via %q, want (%q, %q, error %v)", got, sha, err, *calls, testCase.wantText, testCase.wantSHA, testCase.wantErr)
			}
		})
	}
}

func TestContentSHA(t *testing.T) {
	// Not parallel: sub-tests swap the package-level ghStdin seam.
	const path = "repos/o/r/contents/x.yml"
	testCases := []struct {
		name, out, wantSHA string
		err                error
		wantErr            bool
	}{
		{name: "Success", out: "abc123\n", wantSHA: "abc123"},
		// A 404 is the create case.
		{name: "Success/NotFound", err: errGHNotFound},
		{name: "Error/Propagated", err: errGHBadCredentials, wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			calls := stubGH(t, func(string) (string, error) { return testCase.out, testCase.err })
			sha, err := contentSHA(path)
			if sha != testCase.wantSHA || (err != nil) != testCase.wantErr || !strings.Contains((*calls)[0], path) {
				t.Fatalf("contentSHA = (%q, %v) via %q, want (%q, error %v)", sha, err, *calls, testCase.wantSHA, testCase.wantErr)
			}
		})
	}
}

func TestApplyLabels(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	t.Run("Success", func(t *testing.T) {
		// meta matches, documentation's color has drifted, and triage lingers.
		responses := map[string]string{
			"labels?per_page=100": `[
				{"name":"meta","color":"bfd4f2","description":"No-PR work (manual ops / config); also marks Meta Epics"},
				{"name":"documentation","color":"000000","description":"Improvements or additions to documentation"},
				{"name":"triage","color":"fbca04","description":"old"}
			]`,
		}
		calls := fakeGH(t, responses)
		op := repocfg.Op{Path: "repos/o/r/labels", Body: &repocfg.LabelsConfig{
			Ensure: []repocfg.LabelDef{
				{Name: "meta", Color: "bfd4f2", Description: "No-PR work (manual ops / config); also marks Meta Epics"},
				{Name: "documentation", Color: "0075ca", Description: "Improvements or additions to documentation"},
				{Name: "good first issue", Color: "7057ff", Description: "Good for newcomers"},
			},
			Retire: []string{"triage"},
		}}

		detail, err := applyLabels("o", "r", op)
		if err != nil || detail != "1 created, 1 updated, 1 retired" {
			t.Fatalf("applyLabels = (%q, %v), want %q", detail, err, "1 created, 1 updated, 1 retired")
		}

		joined := strings.Join(*calls, "\n")
		wantCalls := []string{
			"repos/o/r/labels?per_page=100",                         // list existing
			"api -X POST repos/o/r/labels --input -",                // create the missing one
			"api -X PATCH repos/o/r/labels/documentation --input -", // recolour the drifted one
			"api -X DELETE repos/o/r/labels/triage",                 // retire
		}

		for _, want := range wantCalls {
			if !strings.Contains(joined, want) {
				t.Errorf("expected a gh call containing %q; calls:\n%s", want, joined)
			}
		}
		// meta matched the canonical set exactly — it must not be re-written.
		if strings.Contains(joined, "labels/meta") {
			t.Errorf("meta matched and must not be PATCHed; calls:\n%s", joined)
		}
	})

	t.Run("Error/BadBody", func(t *testing.T) {
		fakeGH(t, nil)
		op := repocfg.Op{Path: "repos/o/r/labels", Body: map[string]any{"ensure": nil}}
		if _, err := applyLabels("o", "r", op); err == nil {
			t.Fatal("expected an error for a non-*LabelsConfig body")
		}
	})
}

func TestApplyEnvironment(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	calls := fakeGH(t, map[string]string{"orgs/o/teams/core ": "11\n", "orgs/o/teams/owners-service ": "12\n"})
	op := repocfg.Op{
		Method: http.MethodPut, Path: "repos/o/r/environments/release", Reviewers: []string{"core", "owners-service"},
		Body: map[string]any{"deployment_branch_policy": map[string]any{"custom_branch_policies": true}},
	}
	if err := applyEnvironment("o", op); err != nil {
		t.Fatalf("applyEnvironment: %v", err)
	}
	put := (*calls)[len(*calls)-1]
	want := `"prevent_self_review":false,"reviewers":[{"id":11,"type":"Team"},{"id":12,"type":"Team"}]`
	if !strings.HasPrefix(put, "api -X PUT repos/o/r/environments/release --input -") || !strings.Contains(put, want) {
		t.Errorf("environment write = %q, want the reviewers resolved to team ids and self-review allowed", put)
	}
	if _, ok := op.Body.(map[string]any)["reviewers"]; ok {
		t.Error("applyEnvironment mutated the plan's body")
	}
}

func TestApplyDeploymentPolicies(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	path := "repos/o/r/environments/release/deployment-branch-policies"
	t.Run("Success", func(t *testing.T) {
		// master matches; the v* pattern exists as a branch, not a tag; release/v* is missing.
		calls := fakeGH(t, map[string]string{"--paginate": "11\tbranch\tmaster\n12\tbranch\tv*\n"})
		op := repocfg.Op{Method: http.MethodPut, Path: path, Body: []repocfg.DeploymentPolicy{
			{Name: "master", Type: "branch"},
			{Name: "release/v*", Type: "branch"},
			{Name: "v*", Type: "tag"},
		}}

		detail, err := applyDeploymentPolicies(op)
		if err != nil || detail != "2 created, 1 deleted" {
			t.Fatalf("applyDeploymentPolicies = (%q, %v), want %q", detail, err, "2 created, 1 deleted")
		}
		joined := strings.Join(*calls, "\n")
		if countCalls(*calls, "api -X POST "+path+" --input -") != 2 || !strings.Contains(joined, "api -X DELETE "+path+"/12") {
			t.Errorf("want two creates and the branch v* deleted; calls:\n%s", joined)
		}
		if strings.Contains(joined, path+"/11") {
			t.Errorf("master matched and must not be deleted; calls:\n%s", joined)
		}
	})

	t.Run("Error/BadBody", func(t *testing.T) {
		fakeGH(t, nil)
		if _, err := applyDeploymentPolicies(repocfg.Op{Path: path, Body: map[string]any{}}); err == nil {
			t.Fatal("expected an error for a non-[]DeploymentPolicy body")
		}
	})
}

// TestApplyEnvSecrets pins the governance environment's reconcile: a named
// secret is written when it is missing or Bitwarden holds a newer revision, a
// secret no workflow names is deleted, and without a Bitwarden token only a
// missing secret fails. No message ever carries a value.
func TestApplyEnvSecrets(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin and bwsSecrets seams.
	path := "repos/o/r/environments/governance/secrets"
	op := repocfg.Op{Path: path, Governance: &repocfg.Governance{
		BWSProject: "governance-a-novel", Secrets: []string{"AGENT_BOT_PRIVATE_KEY", "DEPENDENCY_BOT_PRIVATE_KEY"},
	}}
	// Bitwarden revised the secret at that hour of 2026-10-14; GitHub's copies date from 10:00.
	secret := func(key string, hour int) bwsSecret {
		return bwsSecret{Key: key, Value: key + "-value", RevisionDate: time.Date(2026, 10, 14, hour, 0, 0, 0, time.UTC)}
	}
	agent, dependency := "AGENT_BOT_PRIVATE_KEY", "DEPENDENCY_BOT_PRIVATE_KEY"
	set := func(name string) string {
		return "secret set " + name + " --env governance --repo o/r " + name + "-value"
	}

	testCases := []struct {
		name       string
		live       string
		source     map[string]bwsSecret
		wantDetail string
		wantErr    string
		wantCalls  []string
	}{
		{
			name:       "WritesMissingAndDeletesUnnamed",
			live:       agent + "\t2026-10-14T10:00:00Z\nOLD_KEY\t2026-10-01T00:00:00Z\n",
			source:     map[string]bwsSecret{agent: secret(agent, 9), dependency: secret(dependency, 9)},
			wantDetail: "1 set, 1 deleted",
			wantCalls:  []string{"secret delete OLD_KEY --env governance --repo o/r", set(dependency)},
		},
		{
			name:       "RewritesRotated",
			live:       agent + "\t2026-10-14T10:00:00Z\n" + dependency + "\t2026-10-14T10:00:00Z\n",
			source:     map[string]bwsSecret{agent: secret(agent, 11), dependency: secret(dependency, 9)},
			wantDetail: "1 set, 0 deleted",
			wantCalls:  []string{set(agent)},
		},
		{
			name:       "WithoutTokenKeepsPresent",
			live:       agent + "\t2026-10-14T10:00:00Z\n" + dependency + "\t2026-10-14T10:00:00Z\n",
			wantDetail: "0 deleted, none compared with Bitwarden (no access token)",
		},
		{
			name:    "Error/WithoutTokenMissing",
			live:    agent + "\t2026-10-14T10:00:00Z\n",
			wantErr: dependency + " missing; store a Bitwarden access token with `a-novel secrets set BWS_ACCESS_TOKEN`",
		},
		{
			name:    "Error/UnreadableProject",
			source:  map[string]bwsSecret{},
			wantErr: "the Bitwarden access token reads no secret in project governance-a-novel; store the token of a machine account that reads it with `a-novel secrets set BWS_ACCESS_TOKEN`",
		},
		{
			name:    "Error/AbsentFromBitwarden",
			source:  map[string]bwsSecret{agent: secret(agent, 9)},
			wantErr: "the Bitwarden project governance-a-novel holds no " + dependency,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			calls := fakeGH(t, map[string]string{"--paginate": testCase.live})
			swap(t, &bwsSecrets, func(*repocfg.Governance) (map[string]bwsSecret, error) { return testCase.source, nil })

			detail, err := applyEnvSecrets("o", "r", op)
			got := detail
			if err != nil {
				got = err.Error()
			}
			if want := testCase.wantDetail + testCase.wantErr; got != want || (err != nil) != (testCase.wantErr != "") {
				t.Fatalf("applyEnvSecrets = (%q, %v), want %q", detail, err, want)
			}
			if strings.Contains(got, "-value") {
				t.Errorf("message %q carries a secret value", got)
			}
			writes := slices.DeleteFunc(slices.Clone(*calls), func(call string) bool { return strings.HasPrefix(call, "api ") })
			slices.Sort(writes)
			if !slices.Equal(writes, testCase.wantCalls) {
				t.Errorf("writes = %q, want %q", writes, testCase.wantCalls)
			}
		})
	}
}

// TestApplyTeams pins the tier-team reconcile: a missing team is created under
// its tier, each area team is granted its role, and only grants inside the tier
// namespace are removed, since core reaches every repo through its org role and
// other teams belong to whoever made them.
func TestApplyTeams(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	op := repocfg.Op{Path: "repos/o/r/teams", Teams: []repocfg.TeamGrant{
		{Team: "developers-service", Parent: "developers", Permission: "push"},
		{Team: "maintainers-service", Parent: "maintainers", Permission: "maintain"},
		{Team: "owners-service", Parent: "owners", Permission: "admin"},
	}}

	t.Run("Success", func(t *testing.T) {
		next := 10
		calls := stubGH(t, func(call string) (string, error) {
			switch {
			case strings.Contains(call, "orgs/o/teams --jq"):
				return "developers\t1\ncore\t9\n", nil
			case strings.Contains(call, "-X POST orgs/o/teams"):
				next++
				return strconv.Itoa(next) + "\n", nil
			case strings.Contains(call, "api user"):
				return "operator\n", nil
			case strings.Contains(call, "repos/o/r/teams"):
				return "core\ndevelopers-service\ndevelopers-platform\nmaintainers\nreleasers\n", nil
			}
			return "", nil
		})

		detail, err := applyTeams("o", "r", op)
		if want := "3 granted, 5 created, 2 removed"; err != nil || detail != want {
			t.Fatalf("applyTeams = (%q, %v), want %q", detail, err, want)
		}
		joined := strings.Join(*calls, "\n")
		wantCalls := []string{
			`-X POST orgs/o/teams --input - --jq .id {"name":"developers-service","parent_team_id":1,"privacy":"closed"}`,
			`-X POST orgs/o/teams --input - --jq .id {"name":"maintainers","privacy":"closed"}`,
			`-X POST orgs/o/teams --input - --jq .id {"name":"maintainers-service","parent_team_id":12,"privacy":"closed"}`,
			`-X PUT orgs/o/teams/developers-service/repos/o/r --input - {"permission":"push"}`,
			`-X PUT orgs/o/teams/owners-service/repos/o/r --input - {"permission":"admin"}`,
			"-X DELETE orgs/o/teams/developers-platform/repos/o/r",
			"-X DELETE orgs/o/teams/maintainers/repos/o/r",
			// GitHub made the operator a maintainer of each team it created.
			"-X DELETE orgs/o/teams/developers-service/memberships/operator",
			"-X DELETE orgs/o/teams/owners/memberships/operator",
		}

		for _, want := range wantCalls {
			if !strings.Contains(joined, want) {
				t.Errorf("expected a gh call containing %q; calls:\n%s", want, joined)
			}
		}
		untouched := []string{
			"teams/core/", "teams/releasers/", "DELETE orgs/o/teams/developers-service/repos", `"name":"developers",`,
			"teams/developers/memberships",
		}

		for _, kept := range untouched {
			if strings.Contains(joined, kept) {
				t.Errorf("must not touch %q; calls:\n%s", kept, joined)
			}
		}
	})

	t.Run("Success/CreatedConcurrently", func(t *testing.T) {
		// Another repo's apply in the same --all run created developers after the listing.
		calls := stubGH(t, func(call string) (string, error) {
			switch {
			case strings.Contains(call, `-X POST orgs/o/teams --input - --jq .id {"name":"developers",`):
				return "", errors.New("gh: Validation Failed (HTTP 422)")
			case strings.Contains(call, "-X POST orgs/o/teams"):
				return "20\n", nil
			case strings.Contains(call, "orgs/o/teams/developers --jq .id"):
				return "7\n", nil
			}
			return "", nil
		})
		if _, err := applyTeams("o", "r", op); err != nil {
			t.Fatalf("applyTeams: %v", err)
		}
		joined := strings.Join(*calls, "\n")
		if !strings.Contains(joined, `{"name":"developers-service","parent_team_id":7,"privacy":"closed"}`) {
			t.Errorf("developers-service must nest under the team found by name; calls:\n%s", joined)
		}
		if strings.Contains(joined, "teams/developers/memberships") {
			t.Errorf("must not leave a team another run created; calls:\n%s", joined)
		}
	})

	t.Run("Error/CreateFails", func(t *testing.T) {
		calls := stubGH(t, func(call string) (string, error) {
			if strings.Contains(call, "-X POST") {
				return "", errGHNotFound
			}
			return "", nil
		})
		if _, err := applyTeams("o", "r", op); err == nil {
			t.Fatal("expected an error when a team cannot be created")
		}
		if slices.ContainsFunc(*calls, func(c string) bool { return strings.Contains(c, "-X PUT") }) {
			t.Errorf("must not grant after a failed create; calls %q", *calls)
		}
	})
}

// TestPruneRulesets covers the one operation in repocfg that DESTROYS live
// configuration. Anything the plan does not name is drift, so the keep-list is
// the only thing standing between a reconcile and a repo's protection, and a
// bug here is silent until a merge that should have been gated goes through.
func TestPruneRulesets(t *testing.T) {
	// Not parallel: sub-tests swap the package-level ghStdin seam.
	const copilot = "Code Quality Copilot review for default branch"
	testCases := []struct {
		name        string
		keep        []string
		listErr     error
		wantDeleted []string
		wantDetail  string
		wantErr     bool
	}{
		{
			// codecov and the hand-made Copilot ruleset are drift.
			name:        "Success",
			keep:        []string{"master", "require-approval", "tags"},
			wantDeleted: []string{"4", "5"},
			wantDetail:  "deleted " + copilot + ", codecov",
		},
		{
			name:       "Success/KeepEverything",
			keep:       []string{"master", "require-approval", "tags", "codecov", copilot},
			wantDetail: opUnchanged,
		},
		{
			// A class that declares no rulesets genuinely wants none.
			name:        "Success/EmptyKeepPrunesBare",
			wantDeleted: []string{"1", "2", "3", "4", "5"},
			wantDetail:  "deleted " + copilot + ", codecov, master, require-approval, tags",
		},
		{
			// Fail closed: a read error leaves every ruleset in place.
			name:    "Error/ListingFails",
			keep:    []string{"master"},
			listErr: errors.New("gh: API rate limit exceeded"),
			wantErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			calls := stubGH(t, func(call string) (string, error) {
				if strings.Contains(call, "--jq") {
					return "master\t1\nrequire-approval\t2\ntags\t3\ncodecov\t4\n" + copilot + "\t5\n", testCase.listErr
				}
				return "", nil
			})
			detail, err := pruneRulesets("a-novel", "service-auth", liveOf("a-novel", "service-auth"),
				repocfg.Op{PruneRulesets: true, KeepRulesets: testCase.keep})
			var deleted []string
			for _, c := range *calls {
				if strings.Contains(c, "-X DELETE") {
					deleted = append(deleted, c[strings.LastIndex(c, "/")+1:])
				}
			}
			slices.Sort(deleted)
			if (err != nil) != testCase.wantErr || detail != testCase.wantDetail || !slices.Equal(deleted, testCase.wantDeleted) {
				t.Errorf("pruneRulesets = (%q, %v) deleting ids %v, want (%q, error %v) deleting %v",
					detail, err, deleted, testCase.wantDetail, testCase.wantErr, testCase.wantDeleted)
			}
		})
	}
}

// TestPruneImpact covers the preview an operator confirms a destructive
// reconcile from: the offline plan only states what SURVIVES, so this is the
// only place the deletions are visible. A read failure must never render as
// "none", which reads as safe and gets waved through.
func TestPruneImpact(t *testing.T) {
	// Not parallel: sub-tests swap the package-level ghStdin seam.
	target := &repocfg.RepoTarget{Org: "a-novel", Repo: "service-auth"}
	plan := &repocfg.Plan{Ops: []repocfg.Op{
		{RulesetName: "master"},
		{PruneRulesets: true, KeepRulesets: []string{"master", "require-approval", "tags"}},
	}}
	testCases := []struct {
		name, listed string
		listErr      error
		want         string
	}{
		{name: "Success", listed: "master\t1\ntags\t2\ncodecov\t3\nCopilot review\t4\n", want: "# rulesets to DELETE: Copilot review, codecov"},
		{name: "Success/NothingToDelete", listed: "master\t1\ntags\t2\n", want: "# rulesets to delete: none"},
		{name: "Error/ReadFails", listErr: errors.New("gh: rate limited"), want: "# rulesets to delete: UNRESOLVED — gh: rate limited"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stubGH(t, func(string) (string, error) { return testCase.listed, testCase.listErr })
			if got := pruneImpact(target, plan); got != testCase.want {
				t.Errorf("pruneImpact = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestApplyCodeQuality(t *testing.T) {
	op := repocfg.Op{
		Method: "PATCH",
		Path:   "repos/o/r/code-quality/setup",
		Body:   map[string]any{"state": "not-configured"},
	}
	testCases := []struct {
		name, current, wantDetail string
		wantPatch                 bool
	}{
		{name: "Success/AlreadyOff", current: "not-configured\n", wantDetail: opUnchanged},
		{name: "Success/TurnsOff", current: "configured\n", wantDetail: "not-configured", wantPatch: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			calls := fakeGH(t, map[string]string{"api repos/o/r/code-quality/setup --jq": testCase.current})
			detail, err := applyCodeQuality(op)
			if err != nil {
				t.Fatalf("applyCodeQuality: %v", err)
			}
			if detail != testCase.wantDetail {
				t.Fatalf("detail = %q, want %q", detail, testCase.wantDetail)
			}
			if patched := countCalls(*calls, "-X PATCH repos/o/r/code-quality/setup") > 0; patched != testCase.wantPatch {
				t.Fatalf("PATCH sent = %v, want %v (calls %q)", patched, testCase.wantPatch, *calls)
			}
		})
	}
}

func TestApplyPlanSkipsRulesetsWhenManagedSyncFails(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	calls := stubGH(t, func(call string) (string, error) {
		switch {
		case strings.Contains(call, "/contents/"):
			return "", errGHNotFound
		case strings.Contains(call, "git/ref/heads/master"):
			return "headoid123", nil
		case strings.Contains(call, "api graphql"):
			return "", errors.New("workflow scope missing")
		}
		return "", nil
	})

	plan := &repocfg.Plan{Ops: []repocfg.Op{
		{Method: http.MethodPut, Path: "repos/o/r/contents/.github/workflows/merge-gate.yaml", Content: "name: merge gate\n"},
		{RulesetName: branchMaster, Path: "repos/o/r/rulesets", Body: &repocfg.APIRuleset{Name: branchMaster}},
	}}
	if err := applyPlan(io.Discard, testTarget, plan); err == nil {
		t.Fatal("managed sync failure must fail the apply")
	}
	if n := countCalls(*calls, "/rulesets"); n != 0 {
		t.Errorf("issued %d ruleset call(s) after managed sync failed, want 0", n)
	}
}

func TestInfraDisabledStateOperationsAreIdempotent(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	pagesGone := false
	calls := stubGH(t, func(call string) (string, error) {
		if strings.Contains(call, "/pages") {
			if pagesGone {
				return "", errGHNotFound
			}
			pagesGone = true
		}
		return "", nil
	})

	pages := repocfg.Op{Method: http.MethodDelete, Path: "repos/o/infra/pages"}
	alerts := repocfg.Op{Method: http.MethodPut, Path: "repos/o/infra/vulnerability-alerts"}
	for _, wantPages := range []string{"disabled", "already disabled"} {
		if got, err := applyPages(pages); err != nil || got != wantPages {
			t.Fatalf("Pages disable = (%q, %v), want (%s, nil)", got, err, wantPages)
		}
		if got, err := applyToggle(alerts); err != nil || got != "enabled" {
			t.Fatalf("alerts enable = (%q, %v), want (enabled, nil)", got, err)
		}
	}
	if p, a := countCalls(*calls, "/pages"), countCalls(*calls, "/vulnerability-alerts"); p != 2 || a != 2 {
		t.Errorf("Pages/alerts calls = %d/%d, want 2/2", p, a)
	}
}

func TestStageContentDeletionIsIdempotent(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	exists := true
	stubGH(t, func(string) (string, error) {
		if exists {
			exists = false
			return "existing-sha", nil
		}
		return "", errGHNotFound
	})
	op := repocfg.Op{Method: http.MethodDelete, Path: "repos/o/infra/contents/.github/workflows/release-train.yaml"}

	changes, unchanged, err := stageContentDeletion(op)
	if err != nil || unchanged || len(changes) != 1 || changes[0].outcome != opDeleted {
		t.Fatalf("existing deletion = (%+v, %v, %v), want one staged deletion", changes, unchanged, err)
	}
	changes, unchanged, err = stageContentDeletion(op)
	if err != nil || !unchanged || len(changes) != 0 {
		t.Fatalf("missing deletion = (%+v, %v, %v), want unchanged", changes, unchanged, err)
	}
}
