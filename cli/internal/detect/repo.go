package detect

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// anovelRemote matches a git remote URL belonging to the a-novel or
// a-novel-kit GitHub orgs, in SSH (git@github.com:a-novel/x) or HTTPS
// (https://github.com/a-novel-kit/x) form.
var anovelRemote = regexp.MustCompile(`github\.com[:/]a-novel(-kit)?/`)

// RepoGuard fails fast (no filesystem scan) when dir is not inside a git
// repository owned by the a-novel / a-novel-kit orgs. Walking up for .git and
// reading the remotes is O(path depth) plus two quick git calls, so an invalid
// directory errors almost instantly.
func RepoGuard(dir string) error {
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("%q is not inside a git repository; the a-novel CLI only "+
			"runs inside an a-novel / a-novel-kit repository", dir)
	}
	remotes, err := git(dir, "remote", "-v")
	if err != nil || strings.TrimSpace(remotes) == "" {
		return fmt.Errorf("git repository %s has no remote; cannot verify it "+
			"belongs to a-novel / a-novel-kit", top)
	}
	if !anovelRemote.MatchString(remotes) {
		return fmt.Errorf("git repository %s is not an a-novel / a-novel-kit "+
			"repository", top)
	}
	return nil
}

// gitIgnoredDirs returns the absolute paths of directories git ignores under
// absRoot (e.g. the stack repo's gitignored app/ and kit/ checkouts, plus
// node_modules and friends). `ls-files --directory` collapses an ignored tree
// to its top dir, so a single set membership check prunes the whole subtree.
// On any git error it returns an empty set — the depth/name guards still
// bound the walk.
func gitIgnoredDirs(absRoot string) map[string]struct{} {
	set := map[string]struct{}{}
	out, err := git(absRoot, "ls-files", "-z", "--others", "--ignored",
		"--exclude-standard", "--directory")
	if err != nil {
		return set
	}
	for _, p := range strings.Split(out, "\x00") {
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

// git runs `git -C dir <args...>` and returns trimmed stdout.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}
