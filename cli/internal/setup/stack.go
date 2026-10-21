package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// stackRemoteURL is the SSH clone URL every stack's origin points at.
const stackRemoteURL = "git@github.com:a-novel-kit/stack.git"

// bootstrapStack checks one stack's path and returns its status and a one-line
// detail. A valid checkout stays as it is, and anything else at the path is
// refused. A missing stack is cloned. The default stack is cloned only after
// the prompter confirms, and skipped when there is no prompter. Re-running it
// changes nothing.
func bootstrapStack(s stacks.Stack, prompter Prompter) (string, string) {
	info, statErr := os.Stat(s.Path)
	switch {
	case statErr == nil && info.IsDir():
		if isValidStackRepo(s.Path) {
			return statusValid, "already cloned at " + s.Path
		}
		return statusRefused, "path exists at " + s.Path + " but is not a git repo for " + stackRemoteURL
	case statErr == nil:
		return statusRefused, "path " + s.Path + " exists and is not a directory"
	case !os.IsNotExist(statErr):
		return statusRefused, "stat failed: " + statErr.Error()
	}

	if s.IsDefault {
		if ok, detail := confirmDefaultClone(s, prompter); !ok {
			return statusSkipped, detail
		}
	}
	if err := CloneStack(s.Path); err != nil {
		return statusRefused, "clone failed: " + err.Error()
	}
	return statusCloned, "cloned " + stackRemoteURL + " → " + s.Path
}

// confirmDefaultClone asks before cloning the missing default stack. A nil
// prompter declines. It returns the detail of a decline.
func confirmDefaultClone(s stacks.Stack, prompter Prompter) (bool, string) {
	if prompter == nil {
		return false, "default stack missing at " + s.Path + " (re-run interactively to clone, or set it up manually)"
	}
	yes, err := prompter.YesNo(fmt.Sprintf(
		"Default stack not found at %s. Clone %s into it?", s.Path, stackRemoteURL))
	if err != nil || !yes {
		return false, "user declined; set up " + s.Path + " manually before running `a-novel core start`"
	}
	return true, ""
}

// isValidStackRepo reports whether path contains a git checkout whose origin is
// stackRemoteURL. A fork's checkout fails the match.
func isValidStackRepo(path string) bool {
	gitDir := filepath.Join(path, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return false
	}
	cmd := exec.Command("git", "-C", path, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	url := strings.TrimSpace(string(out))
	// Accept both the SSH and HTTPS forms; users may have configured
	// either depending on their auth setup.
	return url == stackRemoteURL ||
		url == "https://github.com/a-novel-kit/stack.git" ||
		url == "https://github.com/a-novel-kit/stack"
}

// CloneStack clones the canonical stack repo into path, with LFS disabled and a
// partial blob filter for a fast, asset-free clone. It is exported so the
// stack-creation verb allocates a new workspace through the same clone shape
// setup uses to bootstrap a registered one.
func CloneStack(path string) error {
	// Ensure parent dir exists; clone creates the leaf.
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("mkdir parent %s: %w", parent, err)
	}
	cmd := exec.Command("git", "clone", "--filter=blob:none", stackRemoteURL, path)
	cmd.Env = append(os.Environ(), "GIT_LFS_SKIP_SMUDGE=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		// Best-effort cleanup of partial clone so a re-run starts
		// fresh.
		_ = os.RemoveAll(path)
		return fmt.Errorf("git clone: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
