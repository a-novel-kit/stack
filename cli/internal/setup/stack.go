package setup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
	"github.com/a-novel-kit/stack/cli/internal/workspace"
)

// stackRemoteURL is the SSH clone URL setup clones every stack from.
var stackRemoteURL = workspace.StackRepo.SSHURL()

// bootstrapStack checks one stack's path and returns its status and a one-line
// detail. A valid checkout stays as it is, and anything else at the path is
// refused. A missing stack is cloned. The default stack is cloned only after
// the prompter confirms, and skipped when there is no prompter. Re-running it
// changes nothing.
func bootstrapStack(ctx context.Context, s stacks.Stack, prompter Prompter) (string, string) {
	info, statErr := os.Stat(s.Path)
	switch {
	case statErr == nil && info.IsDir():
		if isValidStackRepo(ctx, s.Path) {
			return statusValid, "already cloned at " + s.Path
		}
		return statusRefused, "path exists at " + s.Path + " but is not a git repo for " + stackRemoteURL
	case statErr == nil:
		return statusRefused, "path " + s.Path + " exists and is not a directory"
	case !errors.Is(statErr, fs.ErrNotExist):
		return statusRefused, "stat failed: " + statErr.Error()
	}

	if s.IsDefault {
		if ok, detail := confirmDefaultClone(s, prompter); !ok {
			return statusSkipped, detail
		}
	}
	if err := CloneStack(ctx, s.Path); err != nil {
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

// isValidStackRepo reports whether path holds a git checkout whose origin is the
// stack repository, in any URL form. A fork's checkout fails the match.
func isValidStackRepo(ctx context.Context, path string) bool {
	if !fsutil.Exists(filepath.Join(path, ".git")) {
		return false
	}
	origin, err := workspace.Origin(ctx, path)
	return err == nil && origin == workspace.StackRepo
}

// CloneStack clones the canonical stack repo into path, with LFS disabled and a
// partial blob filter for a fast, asset-free clone. It is exported so the
// stack-creation verb allocates a new workspace through the same clone shape
// setup uses to bootstrap a registered one.
//
// A failed clone removes path only when this call created it. git refuses a
// path holding a file or a non-empty directory, and cleans up after itself in
// an existing empty one.
func CloneStack(ctx context.Context, path string) error {
	// A trailing slash would make the base name empty and the parent the stack
	// itself, so the clone would land one level too deep.
	path = filepath.Clean(path)
	// Ensure parent dir exists; clone creates the leaf.
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("mkdir parent %s: %w", parent, err)
	}
	_, statErr := os.Lstat(path)
	if _, err := workspace.Git(ctx, parent, "clone", "--filter=blob:none", stackRemoteURL, filepath.Base(path)); err != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			// Best-effort cleanup of the partial clone, so a re-run starts fresh.
			_ = os.RemoveAll(path)
		}
		return err
	}
	return nil
}
