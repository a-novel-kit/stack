package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustGit runs a git command in dir and fails the test on error.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := runGit(dir, args...); err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
}

// gitOut runs a git command in dir and returns its trimmed output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGit(dir, args...)
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(out)
}

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func readFixture(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// initSyncRepo builds a bare "origin" with one commit on master (files a.txt and
// b.txt) plus a local clone of it, and returns (local, seed). The seed is a
// second working clone used to push new commits into origin so tests can put
// the local clone "behind". origin/HEAD is set by the clone, so
// resolveDefaultBranch reads "master" the same way it does in the field.
func initSyncRepo(t *testing.T) (string, string) {
	t.Helper()
	origin := t.TempDir()
	seed := t.TempDir()
	local := t.TempDir()

	mustGit(t, origin, "init", "--bare", "--quiet", "--initial-branch=master")
	mustGit(t, seed, "init", "--quiet", "--initial-branch=master")
	mustGit(t, seed, "config", "user.email", "test@a-novel.dev")
	mustGit(t, seed, "config", "user.name", "test")
	writeFixture(t, seed, "a.txt", "a0\n")
	writeFixture(t, seed, "b.txt", "b0\n")
	mustGit(t, seed, "add", "-A")
	mustGit(t, seed, "commit", "--quiet", "-m", "init")
	mustGit(t, seed, "remote", "add", "origin", origin)
	mustGit(t, seed, "push", "--quiet", "-u", "origin", "master")

	// Clone into the (empty) local TempDir; the origin URL is absolute so the
	// -C directory is irrelevant.
	mustGit(t, seed, "clone", "--quiet", origin, local)
	mustGit(t, local, "config", "user.email", "test@a-novel.dev")
	mustGit(t, local, "config", "user.name", "test")
	return local, seed
}

// advanceOrigin rewrites a.txt to "a1" in the seed clone and pushes it, so a
// subsequent updateExistingRepo on the local clone sees origin ahead by one
// fast-forwardable commit.
func advanceOrigin(t *testing.T, seed string) {
	t.Helper()
	writeFixture(t, seed, "a.txt", "a1\n")
	mustGit(t, seed, "add", "-A")
	mustGit(t, seed, "commit", "--quiet", "-m", "advance a.txt")
	mustGit(t, seed, "push", "--quiet", "origin", "master")
}

// initPublishRepo creates a git repo with one commit on master and a bare
// "origin" remote that already has that commit, so commands that compare HEAD
// against origin have something real to talk to.
func initPublishRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	origin := t.TempDir()

	mustGit := func(dir string, args ...string) {
		t.Helper()
		if out, err := runGit(dir, args...); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	mustGit(origin, "init", "--bare", "--quiet", "--initial-branch=master")
	mustGit(root, "init", "--quiet", "--initial-branch=master")
	mustGit(root, "config", "user.email", "test@a-novel.dev")
	mustGit(root, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"version": "1.0.0"}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	mustGit(root, "add", "-A")
	mustGit(root, "commit", "--quiet", "-m", "init")
	mustGit(root, "remote", "add", "origin", origin)
	mustGit(root, "push", "--quiet", "-u", "origin", "master")
	return root
}

// Tests never wait out GitHub's secondary rate limit.
func init() { rateLimitWait = 0 }
