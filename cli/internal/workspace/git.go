package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// branchMaster is the default branch name across the org's repos.
const branchMaster = "master"

// Git runs `git -C dir <args...>` with LFS smudging disabled, so LFS blobs
// stay unfetched, and returns the combined output.
func Git(dir string, args ...string) (string, error) {
	return runGitEnv(dir, nil, args...)
}

// GitRemote is Git for a command that reaches GitHub. It rides the shared
// SSH connection once the running command has opened one.
func GitRemote(dir string, args ...string) (string, error) {
	env, release := GitHubMux.acquire()
	defer release()
	return runGitEnv(dir, env, args...)
}

// runGitEnv is Git with env added to the process environment.
func runGitEnv(dir string, env []string, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = slices.Concat(os.Environ(), env, []string{"GIT_LFS_SKIP_SMUDGE=1"})
	out, err := c.CombinedOutput()
	return string(out), err
}

// Toplevel resolves the root of the repository containing dir.
func Toplevel(dir string) (string, error) {
	out, err := Git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not inside a git repository (%s): %w", dir, err)
	}
	return strings.TrimSpace(out), nil
}

// Origin reads the origin URL of the repository at dir and returns
// the GitHub repository it names. The URL may be SSH or HTTPS.
func Origin(dir string) (Repo, error) {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return Repo{}, fmt.Errorf("not in a git repo with an 'origin' remote: %w", err)
	}
	url := strings.TrimSuffix(strings.TrimSpace(string(out)), ".git")
	if _, rest, ok := strings.Cut(url, "github.com"); ok {
		url = strings.TrimLeft(rest, ":/")
	}
	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return Repo{}, fmt.Errorf("cannot parse owner/repo from origin url %q", strings.TrimSpace(string(out)))
	}
	return Repo{Org: parts[len(parts)-2], Name: parts[len(parts)-1]}, nil
}

// DefaultBranch reads the upstream's default branch from origin/HEAD, with no
// network, falling back to master when the symref is missing.
func DefaultBranch(dir string) string {
	out, err := Git(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if branch, ok := strings.CutPrefix(strings.TrimSpace(out), "origin/"); err == nil && ok {
		return branch
	}
	return branchMaster
}

// OngoingWork names the work in progress dir holds, such as a detached HEAD or
// uncommitted changes. It returns "" for a clean checkout of the default branch.
func OngoingWork(dir string) string {
	out, err := Git(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	branch := strings.TrimSpace(out)
	if err != nil || branch == "" {
		return "detached HEAD"
	}
	if branch != DefaultBranch(dir) {
		return "on " + branch
	}
	if out, err := Git(dir, "status", "--porcelain"); err == nil && strings.TrimSpace(out) != "" {
		return "uncommitted changes"
	}
	return ""
}

// UnpushedCommits counts commits on HEAD its upstream lacks. A checkout with no
// upstream counts as zero: there is no remote to have lost them to.
func UnpushedCommits(dir string) int {
	out, err := Git(dir, "rev-list", "--count", "@{upstream}..HEAD")
	if err != nil {
		return 0
	}
	// git prints a bare count. Anything else counts as zero.
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	return n
}
