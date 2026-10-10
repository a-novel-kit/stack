package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Tests never wait out GitHub's secondary rate limit.
func init() { rateLimitWait = 0 }

// Failures the stubbed gh returns, worded as gh reports them.
var (
	errGHNotFound       = errors.New("gh: Not Found (HTTP 404)")
	errGHBadCredentials = errors.New("gh: Bad credentials (HTTP 401)")
	errGHEmptyRepo      = errors.New("gh: Git Repository is empty. (HTTP 409)")
)

// swap sets *p to v for the test and restores it on cleanup. A test that swaps
// a package-level seam cannot run in parallel.
func swap[T any](t *testing.T, p *T, v T) {
	t.Helper()
	orig := *p
	*p = v
	t.Cleanup(func() { *p = orig })
}

// stubGH routes every gh call through respond, which sees the args joined with
// any stdin payload, and records each call.
func stubGH(t *testing.T, respond func(call string) (string, error)) *[]string {
	t.Helper()
	var calls []string
	swap(t, &ghStdin, func(stdin string, args ...string) (string, error) {
		call := strings.Join(args, " ")
		if stdin != "" {
			call += " " + stdin
		}
		calls = append(calls, call)
		return respond(call)
	})
	return &calls
}

// fakeGH stubs gh with the canned output of a matcher the call contains, or ""
// when none matches.
func fakeGH(t *testing.T, responses map[string]string) *[]string {
	t.Helper()
	return stubGH(t, func(call string) (string, error) {
		for substr, out := range responses {
			if strings.Contains(call, substr) {
				return out, nil
			}
		}
		return "", nil
	})
}

// countCalls counts the recorded gh calls containing substr.
func countCalls(calls []string, substr string) int {
	n := 0
	for _, call := range calls {
		if strings.Contains(call, substr) {
			n++
		}
	}
	return n
}

// runCmd executes cmd with args and returns its combined output.
func runCmd(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	return out.String(), err
}

// gitOut runs a git command in dir and returns its trimmed output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGit(dir, args...)
	if err != nil {
		panic(fmt.Sprintf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out))
	}
	return strings.TrimSpace(out)
}

// mustGit runs a git command in dir.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	gitOut(t, dir, args...)
}

// writeFixture writes content to dir/name, creating parent directories.
func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		panic(err)
	}
}

func readFixture(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		panic(err)
	}
	return string(b)
}

// commitFixture writes content to dir/name and commits it.
func commitFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	writeFixture(t, dir, name, content)
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "--quiet", "-m", "update "+name)
}

// initRepo creates a repository in a new temp dir with a committer configured.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "--quiet", "--initial-branch=master")
	mustGit(t, dir, "config", "user.email", "test@a-novel.dev")
	mustGit(t, dir, "config", "user.name", "test")
	return dir
}

// initBare creates a bare repository in a new temp dir.
func initBare(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "--bare", "--quiet", "--initial-branch=master")
	return dir
}

// initSyncRepo builds a bare "origin" with master holding a.txt ("a0") and
// b.txt ("b0"), and returns (local, seed): a clone of it, and a second clone
// that pushes new commits so tests can put local "behind". origin/HEAD is set
// by the clone, so resolveDefaultBranch reads "master" as it does in the field.
func initSyncRepo(t *testing.T) (string, string) {
	t.Helper()
	origin := initBare(t)
	seed := initRepo(t)
	commitFixture(t, seed, "a.txt", "a0\n")
	commitFixture(t, seed, "b.txt", "b0\n")
	mustGit(t, seed, "remote", "add", "origin", origin)
	mustGit(t, seed, "push", "--quiet", "-u", "origin", "master")

	local := t.TempDir()
	mustGit(t, seed, "clone", "--quiet", "-c", "user.email=test@a-novel.dev", "-c", "user.name=test", origin, local)
	return local, seed
}

// advanceOrigin pushes a.txt = "a1" from the seed clone, so origin is one
// fast-forwardable commit ahead of the local clone.
func advanceOrigin(t *testing.T, seed string) {
	t.Helper()
	commitFixture(t, seed, "a.txt", "a1\n")
	mustGit(t, seed, "push", "--quiet", "origin", "master")
}

// initPublishRepo creates a repo with one commit on master, already pushed to a
// bare "origin", so commands comparing HEAD against origin have a real remote.
func initPublishRepo(t *testing.T) string {
	t.Helper()
	origin := initBare(t)
	root := initRepo(t)
	commitFixture(t, root, "package.json", `{"version": "1.0.0"}`)
	mustGit(t, root, "remote", "add", "origin", origin)
	mustGit(t, root, "push", "--quiet", "-u", "origin", "master")
	return root
}
