package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ghStdin is runGH, a package var so tests intercept every GitHub call without
// a live `gh`.
var ghStdin = runGH

// runGH runs `gh` with optional stdin and returns stdout; on failure it folds
// gh's output into the error (see ghError).
func runGH(stdin string, args ...string) (string, error) {
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

// errContains reports whether err's lowercased message holds every one of
// parts, which callers pass in lowercase. GitHub's error wording is the only
// signal gh exposes.
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

// hasHTTPStatus reports whether gh failed with the HTTP status code. gh prints
// the status after GitHub's message, as in "gh: Not Found (HTTP 404)", or
// alone, as in "gh: HTTP 404", when the response carries no message.
func hasHTTPStatus(err error, code int) bool {
	status := "http " + strconv.Itoa(code)
	return errContains(err, "("+status+")") || errContains(err, "gh: "+status)
}

func isNotFound(err error) bool    { return hasHTTPStatus(err, http.StatusNotFound) }
func isRateLimited(err error) bool { return errContains(err, "rate limit") }

func isAlreadyExists(err error) bool {
	return hasHTTPStatus(err, http.StatusConflict) || errContains(err, "already exists")
}
