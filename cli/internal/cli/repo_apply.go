package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
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

	for _, op := range plan.Ops {
		switch {
		case op.RulesetName != "":
			detail, err := applyRuleset(org, repo, op)
			note(err == nil, "ruleset "+op.RulesetName, ternErr(err, detail))
		case op.Content != "":
			detail, err := applyContents(org, repo, branch, op)
			note(err == nil, shortPath(op.Path), ternErr(err, detail))
		case strings.HasSuffix(op.Path, "/pages"):
			detail, err := applyPages(op)
			note(err == nil, "pages", ternErr(err, detail))
		default: // settings PATCH
			err := applySettings(op)
			note(err == nil, "settings ("+op.Method+" "+shortPath(op.Path)+")", errText(err))
		}
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

// applyContents commits a generated file (codeql.yml) to the default branch,
// creating or updating it. CodeQL advanced setup is mutually exclusive with
// default setup, so disable default setup first.
func applyContents(org, repo, branch string, op repocfg.Op) (string, error) {
	if strings.Contains(op.Path, "/workflows/codeql.yml") {
		// Best-effort: ignore the error when default setup is already off.
		_, _ = gh("api", "-X", "PATCH", fmt.Sprintf("repos/%s/%s/code-scanning/default-setup", org, repo), "-f", "state=not-configured")
	}
	sha, err := contentSHA(op.Path)
	if err != nil {
		return "", err
	}
	args := []string{
		"api", "-X", "PUT", op.Path,
		"-f", "message=ci: managed by a-novel repo",
		"-f", "content=" + base64.StdEncoding.EncodeToString([]byte(op.Content)),
		"-f", "branch=" + branch,
	}
	if sha != "" {
		args = append(args, "-f", "sha="+sha)
	}
	verb := opCreated
	if sha != "" {
		verb = opUpdated
	}
	if _, err := gh(args...); err != nil {
		// A missing `workflow` token scope surfaces here as either the worded
		// scope error or a bare 404 — GitHub obscures the scope failure on a
		// `.github/workflows/` write as Not Found. applyContents only ever writes
		// codeql.yml under that path (and the repo demonstrably exists by now, its
		// settings were just patched), so both mean the same thing: add the scope.
		if isWorkflowScope(err) || isNotFound(err) {
			return "needs the `workflow` token scope (gh auth refresh -s workflow)", err
		}
		return "", err
	}
	return verb, nil
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

// rulesetID returns the id of the named ruleset, or "" if none exists.
func rulesetID(org, repo, name string) (string, error) {
	out, err := gh("api", fmt.Sprintf("repos/%s/%s/rulesets", org, repo),
		"--jq", fmt.Sprintf(`.[]|select(.name==%q).id`, name))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// outcome labels for create-or-update operations.
const (
	opCreated = "created"
	opUpdated = "updated"
)

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
