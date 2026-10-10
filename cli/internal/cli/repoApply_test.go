package cli

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

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
	calls := fakeGH(t, map[string]string{
		// no existing master ruleset → POST; codecov ruleset exists → PUT.
		"rulesets --jq":        "codecov\t777\n",
		"git/ref/heads/master": "headoid123\n",
	})

	plan := &repocfg.Plan{Ops: []repocfg.Op{
		{Method: "PATCH", Path: "repos/o/r", Body: map[string]any{"has_wiki": false}},
		{Method: "PATCH", Path: "repos/o/r/code-scanning/default-setup", Body: map[string]any{"state": "not-configured"}},
		{Method: "PUT", Path: "repos/o/r/contents/.github/workflows/managed.yaml", Content: "name: Managed\n"},
		{RulesetName: branchMaster, Path: "repos/o/r/rulesets", Body: &repocfg.APIRuleset{Name: branchMaster}},
		{RulesetName: "codecov", Path: "repos/o/r/rulesets", Body: &repocfg.APIRuleset{Name: "codecov"}},
	}}

	if err := applyPlan(io.Discard, testTarget, plan); err != nil {
		t.Fatalf("applyPlan: %v", err)
	}

	joined := strings.Join(*calls, "\n")
	for _, w := range []string{
		"api -X PATCH repos/o/r --input -",                             // settings
		"api -X PATCH repos/o/r/code-scanning/default-setup --input -", // code scanning stays off
		"api repos/o/r/git/ref/heads/master --jq .object.sha",          // sync commit: read the branch tip
		`"expectedHeadOid":"headoid123"`,                               // sync commit: optimistic lock on that tip
		`"path":".github/workflows/managed.yaml"`,                      // sync commit: carries the staged file
		"api -X POST repos/o/r/rulesets --input -",                     // master ruleset created (no existing id)
		"api -X PUT repos/o/r/rulesets/777 --input -",                  // codecov ruleset updated (existing id 777)
	} {
		if !strings.Contains(joined, w) {
			t.Errorf("expected a gh call containing %q; calls:\n%s", w, joined)
		}
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

	for _, tc := range []struct {
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeGH(t, tc.responses)
			changes, unchanged, err := stageContents(tc.op)
			if err != nil || unchanged != tc.wantUnchanged || !slices.Equal(changes, tc.wantChanges) {
				t.Fatalf("stageContents = (%+v, %v, %v), want (%+v, %v, nil)", changes, unchanged, err, tc.wantChanges, tc.wantUnchanged)
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
		for _, w := range []string{
			`"expectedHeadOid":"headoid123"`,
			`"additions":[{"contents":"` + base64.StdEncoding.EncodeToString([]byte("name: Managed\n")),
			`"deletions":[{"path":"CODEOWNERS"}]`,
			`"headline":"ci: sync managed config (managed, CODEOWNERS)"`,
		} {
			if !strings.Contains(joined, w) {
				t.Errorf("expected the mutation payload to contain %q; calls:\n%s", w, joined)
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
		for _, w := range []string{
			"api -X PUT repos/o/r/contents/.github/workflows/managed.yaml", // the seed file
			`"branch":"master"`, // ...onto the default branch
			`"content":"`,       // ...carrying its base64 content
		} {
			if !strings.Contains(joined, w) {
				t.Errorf("expected a Contents-API seed call containing %q; calls:\n%s", w, joined)
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
		detail, err := commitSync("o", "r", branchMaster, []contentChange{
			{path: ".github/workflows/managed.yaml", content: "name: Managed\n", outcome: opCreated},
			{path: ".github/CODEOWNERS", content: "* @team\n", outcome: opCreated},
		})
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
		for _, w := range []string{"updated .github/workflows/managed.yaml", "deleted CODEOWNERS", "Managed by a-novel repo sync."} {
			if !strings.Contains(body, w) {
				t.Errorf("body should contain %q; got:\n%s", w, body)
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

	for _, tc := range []struct{ name, desired, deployed, want string }{
		{"Success/DeployedNewerIsPreserved", gate + "v1.14.0", gate + "v1.15.0", gate + "v1.15.0"},
		{"Success/DeployedOlderKeepsTemplate", gate + "v1.14.0", gate + "v1.13.0", gate + "v1.14.0"},
		{"Success/Equal", gate + "v1.14.0", gate + "v1.14.0", gate + "v1.14.0"},
		{"Success/PerPath", gate + "v1.14.0\n" + autoMrg + "v1.14.0", gate + "v1.15.0\n" + autoMrg + "v1.14.0", gate + "v1.15.0\n" + autoMrg + "v1.14.0"},
		{"Success/DeployedHasNoPins", gate + "v1.14.0", "no workflows pins here", gate + "v1.14.0"},
		// v1.10.0 > v1.2.0 by semver, though lexically the reverse.
		{"Success/SemverNotLexical", gate + "v1.2.0", gate + "v1.10.0", gate + "v1.10.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := preserveNewerPins(tc.desired, tc.deployed); got != tc.want {
				t.Fatalf("preserveNewerPins() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestContentText(t *testing.T) {
	// Not parallel: sub-tests swap the package-level ghStdin seam.
	const path = "repos/o/r/contents/x.yaml"
	text := "hello: world\n"
	for _, tc := range []struct {
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := stubGH(t, func(string) (string, error) { return tc.out, tc.err })
			got, sha, err := contentText(path)
			if got != tc.wantText || sha != tc.wantSHA || (err != nil) != tc.wantErr || !strings.Contains((*calls)[0], path) {
				t.Fatalf("contentText = (%q, %q, %v) via %q, want (%q, %q, error %v)", got, sha, err, *calls, tc.wantText, tc.wantSHA, tc.wantErr)
			}
		})
	}
}

func TestContentSHA(t *testing.T) {
	// Not parallel: sub-tests swap the package-level ghStdin seam.
	const path = "repos/o/r/contents/x.yml"
	for _, tc := range []struct {
		name, out, wantSHA string
		err                error
		wantErr            bool
	}{
		{name: "Success", out: "abc123\n", wantSHA: "abc123"},
		// A 404 is the create case.
		{name: "Success/NotFound", err: errGHNotFound},
		{name: "Error/Propagated", err: errGHBadCredentials, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := stubGH(t, func(string) (string, error) { return tc.out, tc.err })
			sha, err := contentSHA(path)
			if sha != tc.wantSHA || (err != nil) != tc.wantErr || !strings.Contains((*calls)[0], path) {
				t.Fatalf("contentSHA = (%q, %v) via %q, want (%q, error %v)", sha, err, *calls, tc.wantSHA, tc.wantErr)
			}
		})
	}
}

func TestApplyLabels(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	t.Run("Success", func(t *testing.T) {
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
		if err != nil || detail != "1 created, 1 updated, 1 retired" {
			t.Fatalf("applyLabels = (%q, %v), want %q", detail, err, "1 created, 1 updated, 1 retired")
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

	t.Run("Error/BadBody", func(t *testing.T) {
		fakeGH(t, nil)
		op := repocfg.Op{Path: "repos/o/r/labels", Body: map[string]any{"ensure": nil}}
		if _, err := applyLabels("o", "r", op); err == nil {
			t.Fatal("expected an error for a non-*LabelsConfig body")
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
	for _, tc := range []struct {
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := stubGH(t, func(call string) (string, error) {
				if strings.Contains(call, "--jq") {
					return "master\t1\nrequire-approval\t2\ntags\t3\ncodecov\t4\n" + copilot + "\t5\n", tc.listErr
				}
				return "", nil
			})
			detail, err := pruneRulesets("a-novel", "service-auth", liveOf("a-novel", "service-auth"),
				repocfg.Op{PruneRulesets: true, KeepRulesets: tc.keep})
			var deleted []string
			for _, c := range *calls {
				if strings.Contains(c, "-X DELETE") {
					deleted = append(deleted, c[strings.LastIndex(c, "/")+1:])
				}
			}
			slices.Sort(deleted)
			if (err != nil) != tc.wantErr || detail != tc.wantDetail || !slices.Equal(deleted, tc.wantDeleted) {
				t.Errorf("pruneRulesets = (%q, %v) deleting ids %v, want (%q, error %v) deleting %v",
					detail, err, deleted, tc.wantDetail, tc.wantErr, tc.wantDeleted)
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
	for _, tc := range []struct {
		name, listed string
		listErr      error
		want         string
	}{
		{name: "Success", listed: "master\t1\ntags\t2\ncodecov\t3\nCopilot review\t4\n", want: "# rulesets to DELETE: Copilot review, codecov"},
		{name: "Success/NothingToDelete", listed: "master\t1\ntags\t2\n", want: "# rulesets to delete: none"},
		{name: "Error/ReadFails", listErr: errors.New("gh: rate limited"), want: "# rulesets to delete: UNRESOLVED — gh: rate limited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubGH(t, func(string) (string, error) { return tc.listed, tc.listErr })
			if got := pruneImpact(target, plan); got != tc.want {
				t.Errorf("pruneImpact = %q, want %q", got, tc.want)
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
	for _, tc := range []struct {
		name, current, wantDetail string
		wantPatch                 bool
	}{
		{name: "Success/AlreadyOff", current: "not-configured\n", wantDetail: opUnchanged},
		{name: "Success/TurnsOff", current: "configured\n", wantDetail: "not-configured", wantPatch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := fakeGH(t, map[string]string{"api repos/o/r/code-quality/setup --jq": tc.current})
			detail, err := applyCodeQuality(op)
			if err != nil {
				t.Fatalf("applyCodeQuality: %v", err)
			}
			if detail != tc.wantDetail {
				t.Fatalf("detail = %q, want %q", detail, tc.wantDetail)
			}
			if patched := countCalls(*calls, "-X PATCH repos/o/r/code-quality/setup") > 0; patched != tc.wantPatch {
				t.Fatalf("PATCH sent = %v, want %v (calls %q)", patched, tc.wantPatch, *calls)
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
		if got, err := applyVulnerabilityAlerts(alerts); err != nil || got != "enabled" {
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

	change, unchanged, err := stageContentDeletion(op)
	if err != nil || unchanged || change.outcome != opDeleted {
		t.Fatalf("existing deletion = (%+v, %v, %v), want one staged deletion", change, unchanged, err)
	}
	change, unchanged, err = stageContentDeletion(op)
	if err != nil || !unchanged || change != (contentChange{}) {
		t.Fatalf("missing deletion = (%+v, %v, %v), want unchanged", change, unchanged, err)
	}
}
