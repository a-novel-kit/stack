package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// branchMaster is the default branch name across the org's repos.
const branchMaster = "master"

// Git runs `git -C dir <args...>` with LFS smudging disabled, so LFS blobs
// stay unfetched, and returns its trimmed stdout. A failure carries what git
// printed on stderr.
func Git(ctx context.Context, dir string, args ...string) (string, error) {
	return gitEnv(ctx, dir, nil, args...)
}

// GitRemote is Git for a command that reaches GitHub. It rides the shared
// SSH connection once the running command has opened one.
func GitRemote(ctx context.Context, dir string, args ...string) (string, error) {
	env, release := GitHubMux.acquire(ctx)
	defer release()
	return gitEnv(ctx, dir, env, args...)
}

// gitEnv is Git with env added to the process environment.
func gitEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = slices.Concat(os.Environ(), env, []string{"GIT_LFS_SKIP_SMUDGE=1"})
	// A cancelled git gets SIGTERM, on which it removes its lock files and a
	// half-made clone; SIGKILL would leave them for the next sync to trip on.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// Toplevel resolves the root of the repository containing dir.
func Toplevel(ctx context.Context, dir string) (string, error) {
	out, err := Git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not inside a git repository (%s): %w", dir, err)
	}
	return out, nil
}

// Origin returns the GitHub repository the origin remote of the repository at
// dir names.
func Origin(ctx context.Context, dir string) (Repo, error) {
	url, err := Git(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return Repo{}, fmt.Errorf("not in a git repo with an 'origin' remote: %w", err)
	}
	repo, ok := ParseURL(url)
	if !ok {
		return Repo{}, fmt.Errorf("cannot parse owner/repo from origin url %q", url)
	}
	return repo, nil
}

// RootRepo returns the repository checked out at the workspace root, and
// whether root holds a checkout with an origin. The root is the stack
// repository: its origin names it or the fork it is, and an origin outside
// GitHub, such as a local mirror, still counts as the stack, so the root's
// unsaved work stays guarded.
func RootRepo(ctx context.Context, root string) (Repo, bool) {
	url, err := Git(ctx, root, "remote", "get-url", "origin")
	if err != nil {
		return Repo{}, false
	}
	if repo, ok := ParseURL(url); ok {
		return repo, true
	}
	return StackRepo, true
}

// ParseURL returns the GitHub repository a remote URL names, in SSH
// (git@github.com:org/name.git, ssh://git@github.com/org/name) or HTTPS
// (https://github.com/org/name) form. A URL on another host, or a local path,
// names none.
func ParseURL(url string) (Repo, bool) {
	url = strings.TrimSuffix(strings.TrimSpace(url), ".git")
	scheme, rest, found := strings.Cut(url, "github.com")
	hostStart := scheme == "" || strings.HasSuffix(scheme, "@") || strings.HasSuffix(scheme, "//")
	if !found || !hostStart || rest == "" || (rest[0] != ':' && rest[0] != '/') {
		return Repo{}, false
	}
	org, name, ok := strings.Cut(rest[1:], "/")
	if !ok || org == "" || name == "" || strings.Contains(name, "/") {
		return Repo{}, false
	}
	return Repo{Org: org, Name: name}, true
}

// DefaultBranch reads the upstream's default branch from origin/HEAD, with no
// network, falling back to master when the symref is missing.
func DefaultBranch(ctx context.Context, dir string) string {
	out, err := Git(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if branch, ok := strings.CutPrefix(out, "origin/"); err == nil && ok {
		return branch
	}
	return branchMaster
}

// OngoingWork names the work in progress dir holds, such as a detached HEAD or
// uncommitted changes. It returns "" for a clean checkout of the default branch.
func OngoingWork(ctx context.Context, dir string) string {
	branch, err := Git(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch == "" {
		return "detached HEAD"
	}
	if branch != DefaultBranch(ctx, dir) {
		return "on " + branch
	}
	if out, err := Git(ctx, dir, "status", "--porcelain"); err == nil && out != "" {
		return "uncommitted changes"
	}
	return ""
}

// UnpushedCommits counts commits on HEAD its upstream lacks. A checkout with no
// upstream counts as zero: there is no remote to have lost them to.
func UnpushedCommits(ctx context.Context, dir string) int {
	out, err := Git(ctx, dir, "rev-list", "--count", "@{upstream}..HEAD")
	if err != nil {
		return 0
	}
	// git prints a bare count. Anything else counts as zero.
	n, _ := strconv.Atoi(out)
	return n
}
