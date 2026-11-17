// Package workspacetest builds the git repositories workspace and command tests
// run against.
package workspacetest

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
)

// IsolateGit runs every git command of the test binary under a fixed identity
// and no global or system config, so a developer's commit.gpgsign cannot stall
// fixture commits. Call it from TestMain.
func IsolateGit() {
	env := map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME":     "test",
		"GIT_AUTHOR_EMAIL":    "test@a-novel.dev",
		"GIT_COMMITTER_NAME":  "test",
		"GIT_COMMITTER_EMAIL": "test@a-novel.dev",
	}
	for key, value := range env {
		if err := os.Setenv(key, value); err != nil {
			panic(err)
		}
	}
}

// Git runs a git command in dir and returns its trimmed output.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		panic(fmt.Sprintf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out))
	}
	return strings.TrimSpace(string(out))
}

// Commit writes content to dir/name and commits it.
func Commit(t *testing.T, dir, name, content string) {
	t.Helper()

	fsutiltest.Write(t, dir, map[string]string{name: content})
	Git(t, dir, "add", "-A")
	Git(t, dir, "commit", "--quiet", "-m", "update "+name)
}

// Init creates a repository on master in a new temp dir.
func Init(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	Git(t, dir, "init", "--quiet", "--initial-branch=master")
	return dir
}

// InitBare creates a bare repository on master in a new temp dir.
func InitBare(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	Git(t, dir, "init", "--bare", "--quiet", "--initial-branch=master")
	return dir
}

// InitSync builds a bare "origin" with master holding a.txt ("a0") and b.txt
// ("b0"), and returns (local, seed): a clone of it, and a second clone that
// pushes new commits so tests can put local "behind". The clone sets
// origin/HEAD, so the default branch reads "master" as it does in the field.
func InitSync(t *testing.T) (string, string) {
	t.Helper()

	origin := InitBare(t)
	seed := Init(t)
	Commit(t, seed, "a.txt", "a0\n")
	Commit(t, seed, "b.txt", "b0\n")
	Git(t, seed, "remote", "add", "origin", origin)
	Git(t, seed, "push", "--quiet", "-u", "origin", "master")

	local := t.TempDir()
	Git(t, seed, "clone", "--quiet", origin, local)
	return local, seed
}

// AdvanceOrigin pushes a.txt = "a1" from the seed clone, so origin is one
// fast-forwardable commit ahead of the local clone.
func AdvanceOrigin(t *testing.T, seed string) {
	t.Helper()

	Commit(t, seed, "a.txt", "a1\n")
	Git(t, seed, "push", "--quiet", "origin", "master")
}

// InitPushed creates a repository whose one commit on master, a package.json at
// version 1.0.0, is already pushed to a bare "origin", so code comparing HEAD
// against origin has a real remote.
func InitPushed(t *testing.T) string {
	t.Helper()

	origin := InitBare(t)
	root := Init(t)
	Commit(t, root, "package.json", `{"version": "1.0.0"}`)
	Git(t, root, "remote", "add", "origin", origin)
	Git(t, root, "push", "--quiet", "-u", "origin", "master")
	return root
}
