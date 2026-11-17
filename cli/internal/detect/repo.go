package detect

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/workspace"
)

// RepoGuard fails, before any scan, when dir is not inside a git repository of
// the a-novel or a-novel-kit orgs, as any of its remotes names them. A fork's
// clone passes through its upstream remote.
func RepoGuard(ctx context.Context, dir string) error {
	top, err := workspace.Toplevel(ctx, dir)
	if err != nil {
		return fmt.Errorf("%q is not inside a git repository; the a-novel CLI only "+
			"runs inside an a-novel / a-novel-kit repository", dir)
	}
	remotes, err := workspace.Git(ctx, dir, "remote", "-v")
	if err != nil || remotes == "" {
		return fmt.Errorf("git repository %s has no remote; cannot verify it "+
			"belongs to a-novel / a-novel-kit", top)
	}
	for line := range strings.Lines(remotes) {
		// "<name>\t<url> (fetch)"
		if fields := strings.Fields(line); len(fields) > 1 {
			if repo, ok := workspace.ParseURL(fields[1]); ok && repo.InOrgs() {
				return nil
			}
		}
	}
	return fmt.Errorf("git repository %s is not an a-novel / a-novel-kit repository", top)
}

// gitIgnoredDirs returns the absolute paths of directories git ignores under
// absRoot (e.g. the stack repo's gitignored app/ and kit/ checkouts, plus
// node_modules and friends). `ls-files --directory` collapses an ignored tree
// to its top dir, so a single set membership check prunes the whole subtree.
// On any git error it returns an empty set, and the depth and name guards
// still bound the walk.
func gitIgnoredDirs(ctx context.Context, absRoot string) map[string]struct{} {
	set := map[string]struct{}{}
	out, err := workspace.Git(ctx, absRoot, "ls-files", "-z", "--others", "--ignored",
		"--exclude-standard", "--directory")
	if err != nil {
		return set
	}
	for p := range strings.SplitSeq(out, "\x00") {
		p = strings.TrimSuffix(strings.TrimSpace(p), "/")
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(absRoot, p)
		}
		set[p] = struct{}{}
	}
	return set
}
