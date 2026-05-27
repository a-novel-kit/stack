package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// stackRemoteURL is the canonical SSH clone URL we expect every stack
// to point at.
const stackRemoteURL = "git@github.com:a-novel-kit/stack.git"

// bootstrapStack runs the three-way check for one stack per spec §3.5
// step 3:
//   - exists + valid (right repo + remote) → no-op
//   - exists + invalid (wrong remote or not a repo) → refuse
//   - missing → default: prompt; non-default: clone unconditionally
//
// Idempotent on re-run.
func bootstrapStack(s stacks.Stack, nonInteractive bool, prompter Prompter) StackResult {
	out := StackResult{Name: s.Name, Path: s.Path}
	info, statErr := os.Stat(s.Path)
	switch {
	case statErr == nil && info.IsDir():
		// Exists. Is it a valid stack repo?
		if isValidStackRepo(s.Path) {
			out.Status = "valid"
			out.Detail = "already cloned at " + s.Path
			return out
		}
		// Exists but not a valid repo with our remote.
		out.Status = "refused"
		out.Detail = "path exists at " + s.Path + " but is not a git repo for " + stackRemoteURL
		return out
	case statErr == nil:
		// Exists as a file — won't touch it.
		out.Status = "refused"
		out.Detail = "path " + s.Path + " exists and is not a directory"
		return out
	case os.IsNotExist(statErr):
		// Missing — clone or prompt.
		if s.IsDefault {
			if nonInteractive || prompter == nil {
				out.Status = "skipped"
				out.Detail = "default stack missing at " + s.Path + " (re-run interactively to clone, or set it up manually)"
				return out
			}
			yes, err := prompter.YesNo(fmt.Sprintf(
				"Default stack not found at %s. Clone %s into it?", s.Path, stackRemoteURL))
			if err != nil || !yes {
				out.Status = "skipped"
				out.Detail = "user declined; set up " + s.Path + " manually before running `a-novel core start`"
				return out
			}
		}
		if err := cloneStack(s.Path); err != nil {
			out.Status = "refused"
			out.Detail = "clone failed: " + err.Error()
			return out
		}
		out.Status = "cloned"
		out.Detail = "cloned " + stackRemoteURL + " → " + s.Path
		return out
	default:
		out.Status = "refused"
		out.Detail = "stat failed: " + statErr.Error()
		return out
	}
}

// isValidStackRepo reports whether path contains a git checkout with our
// remote URL set. Defensive — the remote may legitimately differ for
// agents forking from upstream; for now we hard-match. Loosen later if
// fork workflows become common.
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

// cloneStack clones the canonical stack repo into path. LFS disabled
// + partial blob filter per spec §3.4 (fast clone, no large assets).
func cloneStack(path string) error {
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
