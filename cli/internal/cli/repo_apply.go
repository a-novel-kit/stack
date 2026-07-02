package cli

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path"
	"strconv"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

// applyPlan executes every operation in a plan against GitHub via `gh`,
// printing one line per op. It keeps going after a failed op (so one fiddly
// step — e.g. CodeQL needing the `workflow` token scope — doesn't block the
// rest) and returns a combined error if any failed.
func applyPlan(out io.Writer, org, repo, branch string, plan *repocfg.Plan) error {
	var failures []string
	note := func(ok bool, label, detail string) {
		mark := "✓"
		if !ok {
			mark = "✗"
			failures = append(failures, label+": "+detail)
		}
		line := fmt.Sprintf("  %s %s", mark, label)
		if detail != "" {
			line += " — " + detail
		}
		_, _ = fmt.Fprintln(out, line)
	}

	var staged []contentChange
	for _, op := range plan.Ops {
		switch {
		case op.RulesetName != "":
			detail, err := applyRuleset(org, repo, op)
			note(err == nil, "ruleset "+op.RulesetName, ternErr(err, detail))
		case op.Content != "":
			changes, unchanged, err := stageContents(org, repo, op)
			if err != nil {
				note(false, shortPath(op.Path), ternErr(err, ""))
				continue
			}
			if unchanged {
				note(true, shortPath(op.Path), opUnchanged)
			}
			staged = append(staged, changes...)
		case strings.HasSuffix(op.Path, "/pages"):
			detail, err := applyPages(op)
			note(err == nil, "pages", ternErr(err, detail))
		case strings.HasSuffix(op.Path, "/labels"):
			detail, err := applyLabels(org, repo, op)
			note(err == nil, "labels", ternErr(err, detail))
		default: // settings PATCH
			err := applySettings(op)
			note(err == nil, "settings ("+op.Method+" "+shortPath(op.Path)+")", errText(err))
		}
	}

	// Every staged change lands in one commit — one push, one CI run on the
	// target repo. No staged change means no commit at all: the mutation would
	// otherwise record an empty commit, exactly like the contents API.
	if len(staged) > 0 {
		detail, err := commitSync(org, repo, branch, staged)
		if err == nil {
			for _, change := range staged {
				note(true, change.path, change.outcome)
			}
		}
		note(err == nil, fmt.Sprintf("sync commit (%d files)", len(staged)), ternErr(err, detail))
	}

	if len(failures) > 0 {
		return fmt.Errorf("%d operation(s) failed:\n  - %s", len(failures), strings.Join(failures, "\n  - "))
	}
	return nil
}

// applySettings PATCHes the repo settings. The a-novel org enforces
// web_commit_signoff_required org-wide, which locks the repo-level field: a
// PATCH that includes it is rejected with 422 even when the value matches.
// Detect that and retry once without the field.
func applySettings(op repocfg.Op) error {
	body, ok := op.Body.(map[string]any)
	if !ok {
		return fmt.Errorf("settings body is %T, want map", op.Body)
	}
	if err := ghJSON("PATCH", op.Path, body); err != nil {
		if isSignoffLocked(err) {
			retry := cloneMap(body)
			delete(retry, "web_commit_signoff_required")
			return ghJSON("PATCH", op.Path, retry)
		}
		return err
	}
	return nil
}

// applyRuleset reconciles a ruleset by name: PUT when one with the same name
// already exists, POST otherwise (so we never duplicate a ruleset and never
// touch one we don't manage).
func applyRuleset(org, repo string, op repocfg.Op) (string, error) {
	body, ok := op.Body.(*repocfg.APIRuleset)
	if !ok {
		return "", fmt.Errorf("ruleset body is %T, want *repocfg.APIRuleset", op.Body)
	}
	id, err := rulesetID(org, repo, op.RulesetName)
	if err != nil {
		return "", err
	}
	if id != "" {
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
// carry. unchanged reports that the file already matches the desired content —
// nothing is staged for it, so a sync that changes nothing commits nothing.
// CodeQL advanced setup is mutually exclusive with default setup, so default
// setup is switched off before the workflow lands; a stray root CODEOWNERS is
// staged as a deletion (GitHub honours the .github/ copy) even when the
// .github/ copy itself is unchanged, so a repo never carries two.
func stageContents(org, repo string, op repocfg.Op) ([]contentChange, bool, error) {
	if strings.Contains(op.Path, "/workflows/codeql.yml") {
		// Best-effort: ignore the error when default setup is already off.
		_, _ = gh("api", "-X", "PATCH", fmt.Sprintf("repos/%s/%s/code-scanning/default-setup", org, repo), "-f", "state=not-configured")
	}
	var changes []contentChange
	if strings.HasSuffix(op.Path, "/contents/.github/"+codeownersName) {
		rootPath := strings.Replace(op.Path, "/contents/.github/"+codeownersName, "/contents/"+codeownersName, 1)
		if rootSHA, shaErr := contentSHA(rootPath); shaErr == nil && rootSHA != "" {
			changes = append(changes, contentChange{path: codeownersName, outcome: opDeleted})
		}
	}
	sha, err := contentSHA(op.Path)
	if err != nil {
		return nil, false, err
	}
	if sha == blobSHA(op.Content) {
		return changes, true, nil
	}
	outcome := opCreated
	if sha != "" {
		outcome = opUpdated
	}
	return append(changes, contentChange{path: shortPath(op.Path), content: op.Content, outcome: outcome}), false, nil
}

// syncCommitQuery applies every staged change as a single commit on the
// branch. createCommitOnBranch signs the commit as GitHub (verified) and
// rejects the write with STALE_DATA when the branch tip no longer matches
// expectedHeadOid.
const syncCommitQuery = `mutation($input: CreateCommitOnBranchInput!) { createCommitOnBranch(input: $input) { commit { oid } } }`

// commitSync commits the staged changes to the branch in one
// createCommitOnBranch mutation and returns the short commit id.
func commitSync(org, repo, branch string, changes []contentChange) (string, error) {
	headline, body := syncCommitMessage(changes)
	var additions, deletions []map[string]string
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

	// One retry when the branch tip moves between the oid read and the commit
	// (STALE_DATA). repocfg is the only writer of the staged files, so a moved
	// tip means unrelated commits landed — the staged content stays valid.
	for attempt := 0; ; attempt++ {
		oid, err := branchHeadOid(org, repo, branch)
		if err != nil {
			return "", err
		}
		payload, err := json.Marshal(map[string]any{
			"query": syncCommitQuery,
			"variables": map[string]any{"input": map[string]any{
				"branch":          map[string]string{"repositoryNameWithOwner": org + "/" + repo, "branchName": branch},
				"expectedHeadOid": oid,
				"message":         map[string]string{"headline": headline, "body": body},
				"fileChanges":     map[string]any{"additions": additions, "deletions": deletions},
			}},
		})
		if err != nil {
			return "", err
		}
		out, err := ghStdin(string(payload), "api", "graphql",
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

// applyPages enables a Pages site; a 409 (or an "already exists" message)
// means one already exists, which is fine. Any other error — including a 422
// validation failure — is surfaced rather than swallowed.
func applyPages(op repocfg.Op) (string, error) {
	if err := ghJSON("POST", op.Path, op.Body); err != nil {
		if isAlreadyExists(err) {
			return "already enabled", nil
		}
		return "", err
	}
	return "enabled", nil
}

// applyLabels reconciles a repo's labels against the canonical set: every
// `ensure` label is created, or PATCHed when its colour / description drifts;
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
			if err := ghJSON("POST", base, map[string]any{
				"name": l.Name, "color": l.Color, keyDescription: l.Description,
			}); err != nil {
				return "", err
			}
			created++
		case cur.Color != l.Color || cur.Description != l.Description:
			if err := ghJSON("PATCH", base+"/"+url.PathEscape(l.Name), map[string]any{
				"new_name": l.Name, "color": l.Color, keyDescription: l.Description,
			}); err != nil {
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

// ghLabel is the subset of a GitHub label object we reconcile against.
type ghLabel struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// listLabels returns a repo's current labels keyed by name. Repos carry far
// fewer than 100 labels, so a single page is enough — no pagination.
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

// rulesetID returns the id of the named ruleset, or "" if none exists.
func rulesetID(org, repo, name string) (string, error) {
	out, err := gh("api", fmt.Sprintf("repos/%s/%s/rulesets", org, repo),
		"--jq", fmt.Sprintf(`.[]|select(.name==%q).id`, name))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// outcome labels for staged managed-file changes.
const (
	opCreated   = "created"
	opUpdated   = "updated"
	opUnchanged = "unchanged"
	opDeleted   = "deleted"
)

// keyDescription is the JSON "description" field name, lifted to a constant so
// the package's repeated use of it satisfies goconst.
const keyDescription = "description"

// ghJSON runs `gh api -X <method> <path> --input -` with body marshalled to
// JSON on stdin.
func ghJSON(method, path string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = ghStdin(string(raw), "api", "-X", method, path, "--input", "-")
	return err
}

func gh(args ...string) (string, error) { return ghStdin("", args...) }

// ghStdin runs `gh` with optional stdin and returns stdout; on failure it
// folds stderr into the error. A package var so tests can intercept every
// GitHub API call without a live `gh`.
var ghStdin = func(stdin string, args ...string) (string, error) {
	c := exec.Command("gh", args...)
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return out.String(), fmt.Errorf("%w: %s", err, msg)
		}
		return out.String(), err
	}
	return out.String(), nil
}

// contentSHA returns the blob sha of an existing file at path, or "" when the
// file does not exist yet (a 404 — the create case). Any other error (auth,
// network, rate limit) is returned so a transient failure isn't silently
// misread as "create a new file", which would then PUT without the required
// sha and fail with a confusing error.
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

// blobSHA returns the git blob object id of content — SHA-1 over the
// "blob <len>\0" header plus the bytes — which is exactly the .sha the
// contents API reports for an existing file, so an unchanged write is
// detected without any extra API call.
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

// isStaleHead reports the typed STALE_DATA error createCommitOnBranch returns
// when the branch tip no longer matches expectedHeadOid.
func isStaleHead(out string, err error) bool {
	return err != nil && (strings.Contains(out, "STALE_DATA") ||
		strings.Contains(err.Error(), "Expected branch to point to"))
}

func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "404")
}

func isSignoffLocked(err error) bool {
	return err != nil && strings.Contains(err.Error(), "signoff") && strings.Contains(err.Error(), "enforced")
}

func isWorkflowScope(err error) bool {
	return err != nil && strings.Contains(err.Error(), "workflow") && strings.Contains(strings.ToLower(err.Error()), "scope")
}

func isAlreadyExists(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "409") || strings.Contains(s, "already exists")
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return firstLine(err.Error())
}

// ternErr returns detail on success, the error's first line on failure.
func ternErr(err error, detail string) string {
	if err != nil {
		if detail != "" {
			return detail
		}
		return firstLine(err.Error())
	}
	return detail
}

// shortPath trims the repos/<org>/<repo>/contents/ prefix for a readable label.
func shortPath(path string) string {
	if i := strings.Index(path, "/contents/"); i >= 0 {
		return path[i+len("/contents/"):]
	}
	return path
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
