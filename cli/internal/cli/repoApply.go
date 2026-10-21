package cli

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

// applyPlan executes every operation in a plan against GitHub via `gh`,
// printing one line per outcome. Managed files land before rulesets so a new
// repository never requires checks whose workflow callers are still absent.
// Independent operations continue after failures, but rulesets are skipped
// when managed-file staging or sync fails.
func applyPlan(out io.Writer, t *repocfg.RepoTarget, plan *repocfg.Plan) error {
	run := &planRun{
		out:    out,
		target: t,
		// The live rulesets are read once: a reconcile only adds kept rulesets,
		// so the prune's candidates are fixed before the first write.
		rulesets: sync.OnceValues(func() (map[string]string, error) { return liveRulesets(t.Org, t.Repo) }),
		repoID:   sync.OnceValues(func() (string, error) { return githubRepoID(t.Org, t.Repo) }),
	}
	for _, op := range plan.Ops {
		switch {
		case isManagedFile(op):
			run.stage(op)
		case op.RulesetName != "" || op.PruneRulesets:
			run.applyRulesetOp(op)
		default:
			run.note(run.apply(op))
		}
	}

	// Plans without rulesets still flush their managed files at the end.
	run.flushStaged()

	if len(run.failures) > 0 {
		return fmt.Errorf("%d operation(s) failed:\n  - %s", len(run.failures), strings.Join(run.failures, "\n  - "))
	}
	return nil
}

// isManagedFile reports whether op writes or deletes a managed file, which the
// next sync commit carries.
func isManagedFile(op repocfg.Op) bool {
	return op.Content != "" || (op.Method == http.MethodDelete && strings.Contains(op.Path, "/contents/"))
}

// planRun is one applyPlan pass: the outcome log it prints, the failures it
// returns, and the managed-file changes staged for the next sync commit.
type planRun struct {
	out    io.Writer
	target *repocfg.RepoTarget
	// rulesets and repoID read the repository's live state on first use.
	rulesets func() (map[string]string, error)
	repoID   func() (string, error)
	failures []string
	staged   []contentChange
	// managedFilesFailed reports that a managed file failed to stage or sync,
	// which skips every ruleset after it.
	managedFilesFailed bool
}

// apply runs an operation other than a managed file, and returns the label of
// its outcome line with the detail and the error.
func (r *planRun) apply(op repocfg.Op) (string, string, error) {
	org, repo := r.target.Org, r.target.Repo
	switch {
	case op.PruneRulesets:
		detail, err := pruneRulesets(org, repo, r.rulesets, op)
		return "rulesets (prune)", detail, err
	case op.RulesetName != "":
		detail, err := applyRuleset(op, r.rulesets)
		return "ruleset " + op.RulesetName, detail, err
	case strings.HasSuffix(op.Path, "/vulnerability-alerts"):
		detail, err := applyToggle(op)
		return "Dependabot alerts", detail, err
	case strings.HasSuffix(op.Path, "/immutable-releases"):
		detail, err := applyToggle(op)
		return "immutable releases", detail, err
	case strings.HasSuffix(op.Path, "/pages"):
		detail, err := applyPages(op)
		return "pages", detail, err
	case strings.HasSuffix(op.Path, "/code-scanning/default-setup"):
		detail, err := applyCodeScanning(op)
		return "code scanning", detail, err
	case strings.HasSuffix(op.Path, "/code-quality/setup"):
		detail, err := applyCodeQuality(op)
		return "code quality", detail, err
	case strings.HasSuffix(op.Path, "/labels"):
		detail, err := applyLabels(org, repo, op)
		return "labels", detail, err
	case strings.HasSuffix(op.Path, "/deployment-branch-policies"):
		detail, err := applyDeploymentPolicies(op)
		return path.Base(path.Dir(op.Path)) + " environment policies", detail, err
	case op.Governance != nil:
		detail, err := applyEnvSecrets(org, repo, op)
		return "governance secrets", detail, err
	case len(op.Reviewers) > 0:
		return path.Base(op.Path) + " environment", "", applyEnvironment(org, op)
	case len(op.Teams) > 0:
		detail, err := applyTeams(org, repo, op)
		return "tier teams", detail, err
	case op.Secret != "":
		detail, err := applySecretAccess(op, r.repoID)
		return "secret " + op.Secret, detail, err
	default: // settings
		return "settings (" + op.Method + " " + shortPath(op.Path) + ")", "", applySettings(op)
	}
}

// note prints one outcome line and records a failure. A failure without a
// detail shows the first line of its error.
func (r *planRun) note(label, detail string, err error) {
	mark := "✓"
	if err != nil {
		mark = "✗"
		if detail == "" {
			detail = firstLine(err.Error())
		}
		r.failures = append(r.failures, label+": "+detail)
	}
	line := "  " + mark + " " + label
	if detail != "" {
		line += " — " + detail
	}
	_, _ = fmt.Fprintln(r.out, line)
}

// applyRulesetOp lands the staged managed files, then applies a ruleset op
// unless they failed. A ruleset never requires a check whose workflow is absent.
func (r *planRun) applyRulesetOp(op repocfg.Op) {
	r.flushStaged()
	if r.managedFilesFailed {
		r.note(op.Title(), "skipped because managed-file sync failed", errSkipped)
		return
	}
	r.note(r.apply(op))
}

// stage queues the changes a managed-file op needs for the next sync commit.
func (r *planRun) stage(op repocfg.Op) {
	stageOp := stageContents
	if op.Method == http.MethodDelete {
		stageOp = stageContentDeletion
	}
	changes, unchanged, err := stageOp(op)
	if err != nil {
		r.managedFilesFailed = true
		r.note(shortPath(op.Path), "", err)
		return
	}
	if unchanged {
		r.note(shortPath(op.Path), opUnchanged, nil)
	}
	// An unchanged .github/CODEOWNERS can still stage the stray root copy's
	// deletion.
	r.staged = append(r.staged, changes...)
}

// flushStaged commits the staged managed files, if any.
func (r *planRun) flushStaged() {
	if len(r.staged) == 0 {
		return
	}
	detail, err := commitSync(r.target.Org, r.target.Repo, r.target.DefaultBranch, r.staged)
	if err != nil {
		r.managedFilesFailed = true
	} else {
		for _, change := range r.staged {
			r.note(change.path, change.outcome, nil)
		}
	}
	r.note("sync commit", detail, err)
	r.staged = nil
}

// githubRepoID returns the numeric id GitHub gives the repository.
func githubRepoID(org, repo string) (string, error) {
	out, err := gh("api", fmt.Sprintf("repos/%s/%s", org, repo), "--jq", ".id")
	return strings.TrimSpace(out), err
}

// errSkipped marks an operation applyPlan did not attempt.
var errSkipped = errors.New("skipped")

// applySettings sends a settings op's body with its method. The a-novel org
// enforces web_commit_signoff_required org-wide, which locks the repo-level
// field: a PATCH that includes it is rejected with 422 even when the value
// matches. Detect that and retry once without the field.
func applySettings(op repocfg.Op) error {
	body, ok := op.Body.(map[string]any)
	if !ok {
		return fmt.Errorf("settings body is %T, want map", op.Body)
	}
	if err := ghJSON(op.Method, op.Path, body); err != nil {
		if isSignoffLocked(err) {
			retry := maps.Clone(body)
			delete(retry, "web_commit_signoff_required")
			return ghJSON(op.Method, op.Path, retry)
		}
		return err
	}
	return nil
}

// applyEnvironment writes an environment with its reviewer teams, resolved from
// slugs to the ids GitHub expects. A reviewer may approve a run they dispatched.
func applyEnvironment(org string, op repocfg.Op) error {
	body, ok := op.Body.(map[string]any)
	if !ok {
		return fmt.Errorf("environment body is %T, want map", op.Body)
	}
	reviewers := make([]map[string]any, 0, len(op.Reviewers))
	for _, slug := range op.Reviewers {
		out, err := gh("api", "orgs/"+org+"/teams/"+slug, "--jq", ".id")
		if err != nil {
			return fmt.Errorf("reviewer team %s: %w", slug, err)
		}
		id, err := strconv.Atoi(strings.TrimSpace(out))
		if err != nil {
			return fmt.Errorf("reviewer team %s id %q: %w", slug, out, err)
		}
		reviewers = append(reviewers, map[string]any{"type": "Team", "id": id})
	}
	body = maps.Clone(body)
	body["reviewers"] = reviewers
	body["prevent_self_review"] = false
	return ghJSON(op.Method, op.Path, body)
}

// applyEnvSecrets reconciles an environment's secrets with the org's Bitwarden
// project. A live secret the op does not name is deleted first. A named secret
// is then written when the environment lacks it or Bitwarden holds a newer
// revision. Without an access token, named secrets stay as they are, and one
// the environment lacks fails the op. Values travel on stdin only.
func applyEnvSecrets(org, repo string, op repocfg.Op) (string, error) {
	live, err := envSecretDates(op.Path)
	if err != nil {
		return "", err
	}
	env, target := path.Base(path.Dir(op.Path)), org+"/"+repo
	var set, deleted int
	for name := range live {
		if !slices.Contains(op.Governance.Secrets, name) {
			if _, err := gh("secret", "delete", name, "--env", env, "--repo", target); err != nil {
				return "", err
			}
			deleted++
		}
	}
	source, err := bwsSecrets(op.Governance)
	if err != nil {
		return "", err
	}
	if err := checkSecretSources(op.Governance, live, source); err != nil {
		return "", err
	}
	if source == nil {
		return fmt.Sprintf("%d deleted, none compared with Bitwarden (no access token)", deleted), nil
	}
	for _, name := range op.Governance.Secrets {
		if at, found := live[name]; found && !at.Before(source[name].RevisionDate) {
			continue
		}
		if _, err := ghRun(source[name].Value, "secret", "set", name, "--env", env, "--repo", target); err != nil {
			return "", err
		}
		set++
	}
	return fmt.Sprintf("%d set, %d deleted", set, deleted), nil
}

// envSecretDates lists the secrets at secretsPath, an environment's secrets
// endpoint, with the time each was last written.
func envSecretDates(secretsPath string) (map[string]time.Time, error) {
	out, err := gh("api", "--paginate", secretsPath+"?per_page=100", "--jq", `.secrets[] | "\(.name)\t\(.updated_at)"`)
	if err != nil {
		return nil, err
	}
	live := map[string]time.Time{}
	for name, at := range tabPairs(out) {
		// An unreadable date parses as the zero time, so the secret is rewritten.
		live[name], _ = time.Parse(time.RFC3339, at)
	}
	return live, nil
}

// checkSecretSources fails before any secret is set unless every secret
// governance names has a value: in Bitwarden when source holds the project's
// secrets, or already in the environment when no access token is stored.
func checkSecretSources(governance *repocfg.Governance, live map[string]time.Time, source map[string]bwsSecret) error {
	if source == nil {
		missing := slices.DeleteFunc(slices.Clone(governance.Secrets), func(name string) bool {
			_, found := live[name]
			return found
		})
		if len(missing) > 0 {
			return fmt.Errorf("%s missing; store a Bitwarden access token with `a-novel secrets set %s`",
				strings.Join(missing, ", "), bwsTokenID)
		}
		return nil
	}

	// A token whose machine account cannot read the project lists no secret at all.
	if len(source) == 0 {
		return fmt.Errorf("the Bitwarden access token reads no secret in project %s; store the token of a machine account "+
			"that reads it with `a-novel secrets set %s`", governance.BWSProject, bwsTokenID)
	}
	for _, name := range governance.Secrets {
		// A missing name reads as the zero bwsSecret, whose Value is empty.
		if source[name].Value == "" {
			return fmt.Errorf("the Bitwarden project %s holds no %s", governance.BWSProject, name)
		}
	}
	return nil
}

// applySecretAccess grants or revokes the repo's access to a scoped org secret.
// GitHub refuses both with 409 while the secret is still visible to every repo,
// and answers a revoke with 404 when the repo had no access to remove.
func applySecretAccess(op repocfg.Op, repoID func() (string, error)) (string, error) {
	id, err := repoID()
	if err != nil {
		return "", err
	}
	if _, err := gh("api", "-X", op.Method, op.Path+"/"+id, "--silent"); err != nil {
		switch {
		case errContains(err, "409"):
			return "switch its visibility to selected repositories in the org settings", err
		case op.Method == http.MethodDelete && errContains(err, "404"):
			return "already revoked", nil
		}
		return "", err
	}
	if op.Method == http.MethodPut {
		return "granted", nil
	}
	return "revoked", nil
}

// applyRuleset reconciles a ruleset by name: PUT when one with the same name
// already exists, POST otherwise, so a ruleset is never duplicated. The plan's
// prune op owns removal (see pruneRulesets).
func applyRuleset(op repocfg.Op, live func() (map[string]string, error)) (string, error) {
	body, ok := op.Body.(*repocfg.APIRuleset)
	if !ok {
		return "", fmt.Errorf("ruleset body is %T, want *repocfg.APIRuleset", op.Body)
	}
	ids, err := live()
	if err != nil {
		return "", err
	}
	if id := ids[op.RulesetName]; id != "" {
		if err := ghJSON("PUT", op.Path+"/"+id, body); err != nil {
			return "", err
		}
		return opUpdated, nil
	}
	if err := ghJSON("POST", op.Path, body); err != nil {
		return "", err
	}
	return opCreated, nil
}

// contentChange is one staged managed-file write, applied as part of the
// repo's single sync commit.
type contentChange struct {
	path    string // repo-relative file path
	content string // new file text; unused for a deletion
	outcome string // opCreated, opUpdated or opDeleted
}

// codeownersName is the CODEOWNERS file name, at the repo root (where a stray
// copy is deleted from) and under .github/ (where the managed copy lives).
const codeownersName = "CODEOWNERS"

// stageContents turns a managed-file op into the changes the sync commit must
// carry. unchanged reports that the file already matches the desired content,
// so nothing is staged for it and a sync that changes nothing commits nothing.
//
// A stray root CODEOWNERS is staged as a deletion even when the .github/ copy
// is unchanged, so a repo never carries two. For a file that pins
// a-novel-kit/workflows actions, deployed pins newer than the template's
// survive (see preserveNewerPins).
func stageContents(op repocfg.Op) ([]contentChange, bool, error) {
	changes := strayRootCodeowners(op)

	// Pin-bearing files are compared against the deployed text, at the cost of
	// one extra read, so a newer deployed pin survives; everything else takes
	// the cheaper sha-only path.
	var deployed, sha string
	var err error
	if workflowsPinRe.MatchString(op.Content) {
		deployed, sha, err = contentText(op.Path)
	} else {
		sha, err = contentSHA(op.Path)
	}
	if err != nil {
		return nil, false, err
	}

	desired := preserveNewerPins(op.Content, deployed)
	if sha == blobSHA(desired) {
		return changes, true, nil
	}
	outcome := opCreated
	if sha != "" {
		outcome = opUpdated
	}
	return append(changes, contentChange{path: shortPath(op.Path), content: desired, outcome: outcome}), false, nil
}

// strayRootCodeowners stages the deletion of a root CODEOWNERS when op writes
// the managed .github/ copy. A failed read leaves the stray copy for the next
// reconcile.
func strayRootCodeowners(op repocfg.Op) []contentChange {
	base, ok := strings.CutSuffix(op.Path, "/contents/.github/"+codeownersName)
	if !ok {
		return nil
	}
	if sha, err := contentSHA(base + "/contents/" + codeownersName); err != nil || sha == "" {
		return nil
	}
	return []contentChange{{path: codeownersName, outcome: opDeleted}}
}

// stageContentDeletion stages the deletion of a managed file. unchanged
// reports that the file is already absent.
func stageContentDeletion(op repocfg.Op) ([]contentChange, bool, error) {
	sha, err := contentSHA(op.Path)
	if err != nil {
		return nil, false, err
	}
	if sha == "" {
		return nil, true, nil
	}
	return []contentChange{{path: shortPath(op.Path), outcome: opDeleted}}, false, nil
}

// syncCommitQuery applies every staged change as a single commit on the
// branch. createCommitOnBranch signs the commit as GitHub (verified) and
// rejects the write with STALE_DATA when the branch tip no longer matches
// expectedHeadOid.
const syncCommitQuery = `mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }`

// commitSync commits the staged changes to the branch in one
// createCommitOnBranch mutation and returns the short commit id.
func commitSync(org, repo, branch string, changes []contentChange) (string, error) {
	input := syncCommitInput(org, repo, branch, changes)

	// One retry when the branch tip moves between the oid read and the commit
	// (STALE_DATA). repocfg is the only writer of the staged files, so a moved
	// tip means unrelated commits landed, and the staged content stays valid.
	for attempt := 0; ; attempt++ {
		oid, err := branchHeadOid(org, repo, branch)
		if err != nil {
			// A freshly created repo has no commit, so createCommitOnBranch has
			// no branch to build on (a ref read answers 409 "empty"). Seed the
			// first commit via the Contents API, then land the rest here.
			if isEmptyRepo(err) {
				return bootstrapEmpty(org, repo, branch, changes)
			}
			return "", err
		}
		input["expectedHeadOid"] = oid
		request := map[string]any{"query": syncCommitQuery, "variables": map[string]any{"input": input}}
		payload, err := json.Marshal(request)
		if err != nil {
			return "", err
		}
		out, err := ghRun(string(payload), "api", "graphql",
			"--jq", ".data.createCommitOnBranch.commit.oid", "--input", "-")
		switch {
		case err == nil:
			return shortOid(out), nil
		case isStaleHead(out, err) && attempt == 0:
			continue
		case isWorkflowScope(err):
			return "needs the `workflow` token scope (gh auth refresh -s workflow)", err
		default:
			return "", err
		}
	}
}

// syncCommitInput is the createCommitOnBranch input that lands changes on the
// branch. commitSync adds expectedHeadOid on each attempt.
func syncCommitInput(org, repo, branch string, changes []contentChange) map[string]any {
	headline, body := syncCommitMessage(changes)
	// The mutation accepts [], so both slices start empty.
	additions, deletions := []map[string]string{}, []map[string]string{}
	for _, change := range changes {
		if change.outcome == opDeleted {
			deletions = append(deletions, map[string]string{"path": change.path})
			continue
		}
		additions = append(additions, map[string]string{
			"path":     change.path,
			"contents": base64.StdEncoding.EncodeToString([]byte(change.content)),
		})
	}
	return map[string]any{
		"branch":      map[string]string{"repositoryNameWithOwner": org + "/" + repo, "branchName": branch},
		"message":     map[string]string{"headline": headline, "body": body},
		"fileChanges": map[string]any{"additions": additions, "deletions": deletions},
	}
}

// bootstrapEmpty seeds a repository that has no commits yet.
// createCommitOnBranch and the Git Data API answer 409 on an empty repository.
// The Contents API creates the first commit and the default branch, so the
// first added file lands through it, as a GitHub-verified commit. The other
// files then land in one sync commit. An empty repository holds no file to
// delete, so deletions drop.
func bootstrapEmpty(org, repo, branch string, changes []contentChange) (string, error) {
	adds := slices.DeleteFunc(slices.Clone(changes), func(change contentChange) bool { return change.outcome == opDeleted })
	if len(adds) == 0 {
		return "", nil
	}
	seeded, err := putContent(org, repo, branch, adds[0])
	if err != nil {
		return "", fmt.Errorf("bootstrap %s: %w", adds[0].path, err)
	}
	if len(adds) == 1 {
		return seeded, nil
	}
	// The branch now exists; the rest land in one normal sync commit.
	return commitSync(org, repo, branch, adds[1:])
}

// putContent writes one file through the Contents API and returns the short id
// of the commit it makes.
func putContent(org, repo, branch string, change contentChange) (string, error) {
	headline, body := syncCommitMessage([]contentChange{change})
	request := map[string]string{
		"message": headline + "\n\n" + body,
		"content": base64.StdEncoding.EncodeToString([]byte(change.content)),
		"branch":  branch,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	out, err := ghRun(string(payload), "api", "-X", "PUT",
		fmt.Sprintf("repos/%s/%s/contents/%s", org, repo, change.path),
		"--jq", ".commit.sha", "--input", "-")
	if err != nil {
		return "", err
	}
	return shortOid(out), nil
}

// syncCommitMessage derives the sync commit's headline and body from the
// staged changes: the headline names the touched files (or counts them when
// the names would overflow a 72-char subject), the body is the per-file
// manifest plus a stable marker line history stays greppable by.
func syncCommitMessage(changes []contentChange) (string, string) {
	names := make([]string, len(changes))
	for i, change := range changes {
		names[i] = strings.TrimSuffix(path.Base(change.path), path.Ext(change.path))
	}
	headline := "ci: sync managed config (" + strings.Join(names, ", ") + ")"
	if len(headline) > 72 {
		headline = fmt.Sprintf("ci: sync managed config (%d files)", len(changes))
	}
	var body strings.Builder
	for _, change := range changes {
		fmt.Fprintf(&body, "%s %s\n", change.outcome, change.path)
	}
	body.WriteString("\nManaged by a-novel repo sync.")
	return headline, body.String()
}

// branchHeadOid returns the commit id at the tip of the branch.
func branchHeadOid(org, repo, branch string) (string, error) {
	out, err := gh("api", fmt.Sprintf("repos/%s/%s/git/ref/heads/%s", org, repo, branch), "--jq", ".object.sha")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// applyCodeScanning asserts the code-scanning default setup state the plan asks
// for. Reasserting the current state changes nothing.
func applyCodeScanning(op repocfg.Op) (string, error) {
	if err := ghJSON(http.MethodPatch, op.Path, op.Body); err != nil {
		return "", err
	}
	return "default setup off", nil
}

// applyCodeQuality reconciles the GitHub Code Quality setup state. It writes
// only on a change: an update starts a setup run, so reasserting a matching
// state would spend one for nothing.
func applyCodeQuality(op repocfg.Op) (string, error) {
	body, ok := op.Body.(map[string]any)
	if !ok {
		return "", fmt.Errorf("code quality body is %T, want map", op.Body)
	}
	current, err := gh("api", op.Path, "--jq", ".state")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(current) == body["state"] {
		return opUnchanged, nil
	}
	if err := ghJSON(http.MethodPatch, op.Path, body); err != nil {
		return "", err
	}
	return fmt.Sprintf("%v", body["state"]), nil
}

// applyPages reconciles the Pages site. A missing site is already disabled; an
// existing site reported by POST is already enabled. Other errors still fail.
func applyPages(op repocfg.Op) (string, error) {
	switch op.Method {
	case http.MethodPost:
		if err := ghJSON(op.Method, op.Path, op.Body); err != nil {
			if isAlreadyExists(err) {
				return "already enabled", nil
			}
			return "", err
		}
		return "enabled", nil
	case http.MethodDelete:
		if _, err := gh("api", "-X", op.Method, op.Path); err != nil {
			if isNotFound(err) {
				return "already disabled", nil
			}
			return "", err
		}
		return "disabled", nil
	default:
		return "", fmt.Errorf("unsupported Pages method %q", op.Method)
	}
}

// applyToggle reconciles a setting with a bodyless enable/disable endpoint,
// such as Dependabot alerts or immutable releases. Both operations are safe to
// repeat.
func applyToggle(op repocfg.Op) (string, error) {
	if op.Method != http.MethodPut && op.Method != http.MethodDelete {
		return "", fmt.Errorf("unsupported %s method %q", path.Base(op.Path), op.Method)
	}
	if _, err := gh("api", "-X", op.Method, op.Path); err != nil {
		if op.Method == http.MethodDelete && isNotFound(err) {
			return "already disabled", nil
		}
		return "", err
	}
	if op.Method == http.MethodPut {
		return "enabled", nil
	}
	return "disabled", nil
}

// applyLabels reconciles a repo's labels against the canonical set: every
// `ensure` label is created, or PATCHed when its color / description drifts;
// every `retire` label is deleted. Labels in neither list are left untouched, so
// a repo's incidental labels survive.
func applyLabels(org, repo string, op repocfg.Op) (string, error) {
	cfg, ok := op.Body.(*repocfg.LabelsConfig)
	if !ok {
		return "", fmt.Errorf("labels body is %T, want *repocfg.LabelsConfig", op.Body)
	}
	existing, err := listLabels(org, repo)
	if err != nil {
		return "", err
	}
	base := fmt.Sprintf("repos/%s/%s/labels", org, repo)
	var created, updated, retired int
	for _, l := range cfg.Ensure {
		switch cur, found := existing[l.Name]; {
		case !found:
			body := map[string]any{"name": l.Name, "color": l.Color, keyDescription: l.Description}
			if err := ghJSON(http.MethodPost, base, body); err != nil {
				return "", err
			}
			created++
		case cur.Color != l.Color || cur.Description != l.Description:
			body := map[string]any{"new_name": l.Name, "color": l.Color, keyDescription: l.Description}
			if err := ghJSON(http.MethodPatch, base+"/"+url.PathEscape(l.Name), body); err != nil {
				return "", err
			}
			updated++
		}
	}
	for _, name := range cfg.Retire {
		if _, found := existing[name]; found {
			if _, err := gh("api", "-X", "DELETE", base+"/"+url.PathEscape(name)); err != nil {
				return "", err
			}
			retired++
		}
	}
	return fmt.Sprintf("%d created, %d updated, %d retired", created, updated, retired), nil
}

// applyDeploymentPolicies reconciles an environment's branch and tag patterns:
// a missing pattern is created and any other is deleted, so the environment
// admits exactly the plan's refs.
func applyDeploymentPolicies(op repocfg.Op) (string, error) {
	want, ok := op.Body.([]repocfg.DeploymentPolicy)
	if !ok {
		return "", fmt.Errorf("deployment policies body is %T, want []repocfg.DeploymentPolicy", op.Body)
	}
	out, err := gh("api", "--paginate", op.Path+"?per_page=100",
		"--jq", `.branch_policies[] | "\(.id)\t\(.type)\t\(.name)"`)
	if err != nil {
		return "", err
	}
	live := map[repocfg.DeploymentPolicy]string{}
	for id, policy := range tabPairs(out) {
		kind, name, _ := strings.Cut(policy, "\t")
		live[repocfg.DeploymentPolicy{Name: name, Type: kind}] = id
	}
	var created, deleted int
	for _, policy := range want {
		if _, found := live[policy]; found {
			delete(live, policy)
			continue
		}
		if err := ghJSON(http.MethodPost, op.Path, policy); err != nil {
			return "", err
		}
		created++
	}
	for _, id := range live {
		if _, err := gh("api", "-X", http.MethodDelete, op.Path+"/"+id, "--silent"); err != nil {
			return "", err
		}
		deleted++
	}
	return fmt.Sprintf("%d created, %d deleted", created, deleted), nil
}

// applyTeams reconciles a repo's tier-team grants. It creates a missing tier
// or area team, the area team under its tier, and grants each area team its
// role. It then removes the grant of any other team in a tier's namespace.
// Teams outside the tiers keep their grants. Core reaches every repo through
// its org role, which no repository grant can remove.
func applyTeams(org, repo string, op repocfg.Op) (string, error) {
	out, err := gh("api", "--paginate", "orgs/"+org+"/teams?per_page=100", "--jq", `.[] | "\(.slug)\t\(.id)"`)
	if err != nil {
		return "", err
	}
	teams := &orgTeams{org: org, ids: tabPairs(out), operator: sync.OnceValues(ghLogin)}

	granted := make([]string, 0, len(op.Teams))
	tiers := make([]string, 0, len(op.Teams))
	for _, g := range op.Teams {
		parentID, err := teams.ensure(g.Parent, "")
		if err != nil {
			return "", err
		}
		if _, err := teams.ensure(g.Team, parentID); err != nil {
			return "", err
		}
		grant := fmt.Sprintf("orgs/%s/teams/%s/repos/%s/%s", org, g.Team, org, repo)
		if err := ghJSON(http.MethodPut, grant, map[string]any{"permission": g.Permission}); err != nil {
			return "", fmt.Errorf("grant team %q: %w", g.Team, err)
		}
		granted = append(granted, g.Team)
		tiers = append(tiers, g.Parent)
	}

	removed, err := removeStrayTierGrants(org, repo, tiers, granted)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d granted, %d created, %d removed", len(granted), teams.created, removed), nil
}

// ghLogin returns the login of the account gh is authenticated as.
func ghLogin() (string, error) {
	out, err := gh("api", "user", "--jq", ".login")
	return strings.TrimSpace(out), err
}

// orgTeams is an org's teams by slug, as applyTeams creates the missing ones.
type orgTeams struct {
	org      string
	ids      map[string]string
	created  int
	operator func() (string, error)
}

// ensure returns the id of the named team, creating it under parentID when the
// org lacks it.
//
// GitHub makes a team's creator its maintainer, so the operator leaves each team
// it creates: tier membership is the record of who holds the tier.
func (t *orgTeams) ensure(name, parentID string) (string, error) {
	if id, ok := t.ids[name]; ok {
		return id, nil
	}
	body := map[string]any{"name": name, "privacy": "closed"}
	if parentID != "" {
		parent, err := strconv.ParseInt(parentID, 10, 64)
		if err != nil {
			return "", fmt.Errorf("parent of team %q: %w", name, err)
		}
		body["parent_team_id"] = parent
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	id, err := ghRun(string(raw), "api", "-X", http.MethodPost, "orgs/"+t.org+"/teams", "--input", "-", "--jq", ".id")
	if err != nil {
		// `repo update --all` applies several repos at once, and another one may have
		// created the team since the listing.
		if id, getErr := gh("api", "orgs/"+t.org+"/teams/"+name, "--jq", ".id"); getErr == nil && strings.TrimSpace(id) != "" {
			t.ids[name] = strings.TrimSpace(id)
			return t.ids[name], nil
		}
		return "", fmt.Errorf("create team %q: %w", name, err)
	}
	login, err := t.operator()
	if err != nil {
		return "", err
	}
	if _, err := gh("api", "-X", http.MethodDelete, fmt.Sprintf("orgs/%s/teams/%s/memberships/%s", t.org, name, login)); err != nil {
		return "", fmt.Errorf("leave team %q: %w", name, err)
	}
	t.ids[name] = strings.TrimSpace(id)
	t.created++
	return t.ids[name], nil
}

// ghLabel is the subset of a GitHub label object applyLabels reconciles.
type ghLabel struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// listLabels returns a repo's current labels keyed by name. Repos carry far
// fewer than 100 labels, so it reads a single page.
func listLabels(org, repo string) (map[string]ghLabel, error) {
	out, err := gh("api", fmt.Sprintf("repos/%s/%s/labels?per_page=100", org, repo))
	if err != nil {
		return nil, err
	}
	var labels []ghLabel
	if err := json.Unmarshal([]byte(out), &labels); err != nil {
		return nil, fmt.Errorf("parse labels: %w", err)
	}
	m := make(map[string]ghLabel, len(labels))
	for _, l := range labels {
		m[l.Name] = l
	}
	return m, nil
}

// liveRulesets maps the repo's own ruleset names to their ids. The listing
// leaves out the org rulesets that also apply to the repo: apply and prune
// manage repo rulesets only.
func liveRulesets(org, repo string) (map[string]string, error) {
	out, err := gh("api", fmt.Sprintf("repos/%s/%s/rulesets?includes_parents=false", org, repo),
		"--jq", `.[]|"\(.name)\t\(.id)"`)
	if err != nil {
		return nil, err
	}
	return tabPairs(out), nil
}

// tabPairs maps the first field of each "key\tvalue" line of out to its second.
func tabPairs(out string) map[string]string {
	pairs := map[string]string{}
	for line := range strings.Lines(out) {
		if key, value, ok := strings.Cut(strings.TrimRight(line, "\r\n"), "\t"); ok {
			pairs[key] = value
		}
	}
	return pairs
}

// removeStrayTierGrants removes the grant of every team in a tier's namespace
// that granted does not name, and returns how many it removed.
func removeStrayTierGrants(org, repo string, tiers, granted []string) (int, error) {
	live, err := gh("api", "--paginate", fmt.Sprintf("repos/%s/%s/teams", org, repo), "--jq", ".[].slug")
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, slug := range strings.Fields(live) {
		tier, _, _ := strings.Cut(slug, "-")
		if !slices.Contains(tiers, tier) || slices.Contains(granted, slug) {
			continue
		}
		if _, err := gh("api", "-X", http.MethodDelete, fmt.Sprintf("orgs/%s/teams/%s/repos/%s/%s", org, slug, org, repo)); err != nil {
			return 0, fmt.Errorf("remove team %q: %w", slug, err)
		}
		removed++
	}
	return removed, nil
}

// pruneRulesets deletes every live ruleset the op's keep list does not name.
func pruneRulesets(org, repo string, rulesets func() (map[string]string, error), op repocfg.Op) (string, error) {
	live, err := rulesets()
	if err != nil {
		return "", err
	}
	drop := rulesetsToDrop(live, op.KeepRulesets)
	for _, name := range drop {
		if _, err := gh("api", "-X", "DELETE", fmt.Sprintf("repos/%s/%s/rulesets/%s", org, repo, live[name])); err != nil {
			return "", fmt.Errorf("delete ruleset %q: %w", name, err)
		}
	}
	if len(drop) == 0 {
		return opUnchanged, nil
	}
	return opDeleted + " " + strings.Join(drop, ", "), nil
}

// pruneImpact names the rulesets a plan's prune op would delete, read live.
// The prune op lists only the rulesets that survive, so a dry run prints this
// line to show the deletions. A failed read prints UNRESOLVED, never "none".
func pruneImpact(t *repocfg.RepoTarget, plan *repocfg.Plan) string {
	i := slices.IndexFunc(plan.Ops, func(op repocfg.Op) bool { return op.PruneRulesets })
	if i < 0 {
		return ""
	}
	live, err := liveRulesets(t.Org, t.Repo)
	if err != nil {
		return "# rulesets to delete: UNRESOLVED — " + err.Error()
	}
	drop := rulesetsToDrop(live, plan.Ops[i].KeepRulesets)
	if len(drop) == 0 {
		return "# rulesets to delete: none"
	}
	return "# rulesets to DELETE: " + strings.Join(drop, ", ")
}

// rulesetsToDrop lists, sorted, the live rulesets keep does not name.
func rulesetsToDrop(live map[string]string, keep []string) []string {
	return slices.DeleteFunc(slices.Sorted(maps.Keys(live)), func(name string) bool {
		return slices.Contains(keep, name)
	})
}

// keyDescription is the "description" field of a GitHub label.
const keyDescription = "description"

// Outcome details an applied operation reports on its line.
const (
	opCreated   = "created"
	opUpdated   = "updated"
	opUnchanged = "unchanged"
	opDeleted   = "deleted"
)

// contentSHA returns the blob sha of an existing file at path, or "" when the
// file does not exist yet. Only a 404 reads as the create case, and any other
// error, such as an auth, network or rate-limit failure, is returned.
func contentSHA(path string) (string, error) {
	out, err := gh("api", path, "--jq", ".sha")
	if err != nil {
		if isNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// ghContent is the subset of the contents API's file object contentText reads:
// the blob sha and the base64-encoded content.
type ghContent struct {
	SHA      string `json:"sha"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// contentText returns the decoded text and blob sha of an existing file at
// path, or ("", "", nil) when it does not exist yet (a 404). Like contentSHA,
// any other error is propagated. A file the contents API does not base64,
// which happens over 1MB, yields ("", sha, nil). The managed files are a few
// KB, so that is a safe fallback that keeps the template's pins.
func contentText(path string) (string, string, error) {
	out, err := gh("api", path)
	if err != nil {
		if isNotFound(err) {
			return "", "", nil
		}
		return "", "", err
	}
	var obj ghContent
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		return "", "", fmt.Errorf("parse contents %s: %w", path, err)
	}
	if obj.Encoding != "base64" {
		return "", obj.SHA, nil
	}
	// The contents API wraps its base64 at 60 columns; strip the newlines the
	// standard decoder rejects.
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(obj.Content, "\n", ""))
	if err != nil {
		return "", "", fmt.Errorf("decode contents %s: %w", path, err)
	}
	return string(raw), obj.SHA, nil
}

// workflowsPinRe matches an a-novel-kit/workflows action pin, capturing the
// action sub-path and the exact vX.Y.Z tag: e.g.
// `a-novel-kit/workflows/generic-actions/merge-gate@v1.14.0` →
// ("generic-actions/merge-gate", "v1.14.0").
var workflowsPinRe = regexp.MustCompile(`a-novel-kit/workflows/([^@\s"']+)@(v\d+\.\d+\.\d+)`)

// preserveNewerPins rewrites each a-novel-kit/workflows pin in desired to the
// version the deployed file already carries whenever that deployed version is
// a higher semver, matched by action sub-path. It stops `repo update` from
// downgrading a pin Renovate has already advanced on the deployed caller while
// the CLI template lags a workflows release. Pins only move forward, and every
// other byte of the template is left intact so a genuine template edit still
// lands.
func preserveNewerPins(desired, deployed string) string {
	pins := highestPins(deployed)
	if len(pins) == 0 {
		return desired
	}
	return workflowsPinRe.ReplaceAllStringFunc(desired, pins.raise)
}

// pinVersions maps an a-novel-kit/workflows action sub-path to a version.
type pinVersions map[string]string

// highestPins returns the highest version text pins each action at.
func highestPins(text string) pinVersions {
	pins := pinVersions{}
	for _, m := range workflowsPinRe.FindAllStringSubmatch(text, -1) {
		subpath, ver := m[1], m[2]
		if cur, ok := pins[subpath]; !ok || semver.Compare(ver, cur) > 0 {
			pins[subpath] = ver
		}
	}
	return pins
}

// raise returns pin at the version p holds for its action when that version is
// higher, and pin itself otherwise.
func (p pinVersions) raise(pin string) string {
	m := workflowsPinRe.FindStringSubmatch(pin)
	subpath, ver := m[1], m[2]
	if higher, ok := p[subpath]; ok && semver.Compare(higher, ver) > 0 {
		return "a-novel-kit/workflows/" + subpath + "@" + higher
	}
	return pin
}

// blobSHA returns the git blob object id of content: SHA-1 over the
// "blob <len>\0" header plus the bytes. That is the .sha the contents API
// reports for an existing file, so an unchanged write is detected without an
// extra API call.
func blobSHA(content string) string {
	//nolint:gosec // git object identity, not a cryptographic guarantee.
	sum := sha1.Sum([]byte("blob " + strconv.Itoa(len(content)) + "\x00" + content))
	return hex.EncodeToString(sum[:])
}

// shortOid abbreviates a commit id to the usual 7 characters for display.
func shortOid(out string) string {
	oid := strings.TrimSpace(out)
	if len(oid) > 7 {
		oid = oid[:7]
	}
	return oid
}

// shortPath trims the repos/<org>/<repo>/contents/ prefix for a readable label.
func shortPath(path string) string {
	if i := strings.Index(path, "/contents/"); i >= 0 {
		return path[i+len("/contents/"):]
	}
	return path
}
