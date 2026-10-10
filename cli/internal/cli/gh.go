package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ghStdin runs `gh` with optional stdin and returns stdout; on failure it folds
// gh's output into the error (see ghError). A package var so tests intercept
// every GitHub call without a live `gh`.
var ghStdin = func(stdin string, args ...string) (string, error) {
	c := exec.Command("gh", args...)
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		return out.String(), ghError(err, out.String(), errb.String())
	}
	return out.String(), nil
}

// rateLimitWait is how long a call waits after GitHub's secondary rate limit,
// which concurrent repo runs can trip. GitHub asks for at least a minute.
var rateLimitWait = time.Minute

// ghRun is ghStdin, retried twice after a secondary-rate-limit rejection. The
// limit rejects a request before acting on it, so a retried write is safe.
func ghRun(stdin string, args ...string) (string, error) {
	for attempt := 1; ; attempt++ {
		out, err := ghStdin(stdin, args...)
		if attempt == 3 || !isRateLimited(err) {
			return out, err
		}
		time.Sleep(rateLimitWait)
	}
}

func gh(args ...string) (string, error) { return ghRun("", args...) }

// ghJSON runs `gh api -X <method> <path> --input -` with body marshalled to
// JSON on stdin.
func ghJSON(method, path string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = ghRun(string(raw), "api", "-X", method, path, "--input", "-")
	return err
}

// ghError folds a failed gh run's output into err. gh prints the HTTP status
// on stderr and GitHub's JSON error on stdout; only the JSON's errors list
// names the field GitHub rejected, so a JSON stdout joins the message.
func ghError(err error, stdout, stderr string) error {
	msg := strings.TrimSpace(stderr)
	var body bytes.Buffer
	if json.Compact(&body, []byte(stdout)) == nil {
		msg = strings.TrimSpace(msg + " " + body.String())
	}
	if msg == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, msg)
}

// errContains reports whether err's message holds every one of parts, case
// insensitively; GitHub's error wording is the only signal gh exposes.
func errContains(err error, parts ...string) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, p := range parts {
		if !strings.Contains(msg, p) {
			return false
		}
	}
	return true
}

func isNotFound(err error) bool      { return errContains(err, "404") }
func isRateLimited(err error) bool   { return errContains(err, "rate limit") }
func isSignoffLocked(err error) bool { return errContains(err, "signoff", "enforced") }
func isWorkflowScope(err error) bool { return errContains(err, "workflow", "scope") }

// isEmptyRepo reports GitHub's 409 for a repository with no commits yet, where
// a branch-ref read answers "Git Repository is empty.".
func isEmptyRepo(err error) bool { return errContains(err, "git repository is empty") }

func isAlreadyExists(err error) bool {
	return errContains(err, "409") || errContains(err, "already exists")
}

// isStaleHead reports the typed STALE_DATA error createCommitOnBranch returns
// when the branch tip no longer matches expectedHeadOid.
func isStaleHead(out string, err error) bool {
	return err != nil && (strings.Contains(out, "STALE_DATA") || errContains(err, "expected branch to point to"))
}

// firstLine is s up to its first newline.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
