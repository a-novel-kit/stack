// `a-novel core sync` — clone or fast-forward-pull the curated set of
// a-novel / a-novel-kit repositories that make up the local workspace.
//
// The repo set is an explicit whitelist (defaultRepos), not `gh repo list`
// discovery: while the workspace is still narrow only the curated repos are
// pulled, and `--allow`/`--ignore` subset that list. Git over SSH is the only
// dependency.
//
// Behavior:
//   - Skip the workspace's own repo (so kit/stack never appears as a
//     duplicate of the dir the binary was launched from).
//   - Existing repos: ff-only pull on the default branch, ref-update
//     when off-branch, skip on divergence.
//   - GIT_LFS_SKIP_SMUDGE=1 for every git invocation so LFS blobs
//     don't get pulled.
//   - Per-run summary at the end.

package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// repoEntry is one whitelisted repository.
type repoEntry struct {
	Org  string // "a-novel" | "a-novel-kit"
	Name string // repo short name
}

// FullName returns "<org>/<name>" — the form users type for filter flags.
func (r repoEntry) FullName() string { return r.Org + "/" + r.Name }

// SSHURL is the canonical clone URL.
func (r repoEntry) SSHURL() string { return "git@github.com:" + r.Org + "/" + r.Name + ".git" }

// TargetSubdir is `app/` for a-novel and `kit/` for a-novel-kit.
func (r repoEntry) TargetSubdir() string {
	if r.Org == orgAnovelKit {
		return "kit"
	}
	return "app"
}

// orgAnovel / orgAnovelKit are the two GitHub organizations the stack
// orchestrates. Hoisted to constants so the goconst sweep doesn't
// complain about the dozen places these literals appear across the
// CLI surface (sync whitelist, bot config map, root help text, etc.).
const (
	orgAnovel    = "a-novel"
	orgAnovelKit = "a-novel-kit"
)

// defaultRepos is the curated whitelist. Intentionally narrow — the
// stack still discovers a lot of repos via `gh repo list`, but only
// these six need to be cloned locally for current work. Extending the
// list is one line; whittling it is the user's call.
var defaultRepos = []repoEntry{
	{Org: orgAnovelKit, Name: "workflows"},
	{Org: orgAnovelKit, Name: "golib"},
	{Org: orgAnovelKit, Name: "nodelib"},
	{Org: orgAnovel, Name: "service-template"},
	{Org: orgAnovel, Name: "service-json-keys"},
	{Org: orgAnovel, Name: "service-authentication"},
}

// syncCounts is the per-run summary buckets.
type syncCounts struct {
	Cloned, Updated, UpToDate, Skipped, Failed int
	Lines                                      []string // one per repo, for end-of-run replay
}

func (c *syncCounts) add(kind, label string) {
	switch kind {
	case "cloned":
		c.Cloned++
	case "updated":
		c.Updated++
	case "uptodate":
		c.UpToDate++
	case "skipped":
		c.Skipped++
	case "failed":
		c.Failed++
	}
	c.Lines = append(c.Lines, kind+": "+label)
}

func newCoreSyncCmd() *cobra.Command {
	var rootDir string
	var allowFlags []string
	var ignoreFlags []string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Clone or fast-forward-pull the curated workspace repos",
		Long: `Clone or update each repo in the curated workspace whitelist:

  a-novel-kit/workflows   → kit/workflows
  a-novel-kit/golib       → kit/golib
  a-novel-kit/nodelib     → kit/nodelib
  a-novel/service-template       → app/service-template
  a-novel/service-json-keys      → app/service-json-keys
  a-novel/service-authentication → app/service-authentication

Existing repos are fast-forward pulled on the default branch (or have
their default-branch ref updated when the user is on a feature
branch); diverged defaults are left untouched. The workspace's own
repo (the stack the CLI itself lives in) is automatically skipped to
avoid a duplicate clone under kit/stack.

GIT_LFS_SKIP_SMUDGE=1 is forced on every invocation so large LFS
blobs are not pulled.

--allow / --ignore both subset the default list. Both may be repeated.
--ignore wins over --allow when a repo appears in both.

Sub-agents working out of a fresh stack should invoke this command
first to populate kit/ and app/ before any other work.`,
		Example: `  a-novel core sync
  a-novel core sync --allow=a-novel-kit/golib
  a-novel core sync --ignore=a-novel/service-template
  a-novel core sync --root=/tmp/agent-stack`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveSyncRoot(rootDir)
			if err != nil {
				return err
			}
			allow := normaliseFilter(allowFlags)
			ignore := normaliseFilter(ignoreFlags)
			selfURL := detectSelfRemote(root)
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "▸ Syncing workspace repos at %s\n", root)
			if selfURL != "" {
				_, _ = fmt.Fprintf(out, "  (self-detected origin: %s — will be skipped)\n", selfURL)
			}
			counts := syncCounts{}
			for _, r := range defaultRepos {
				// Ignore-list wins over allow-list.
				if _, no := ignore[r.FullName()]; no {
					_, _ = fmt.Fprintf(out, "  ○ %s — ignored via --ignore\n", r.FullName())
					counts.add("skipped", r.FullName()+" (ignored)")
					continue
				}
				if len(allow) > 0 {
					if _, yes := allow[r.FullName()]; !yes {
						continue
					}
				}
				if selfURL != "" && r.SSHURL() == selfURL {
					_, _ = fmt.Fprintf(out, "  ○ %s — workspace self, skipping\n", r.FullName())
					counts.add("skipped", r.FullName()+" (self)")
					continue
				}
				syncOne(cmd.OutOrStdout(), root, r, &counts)
			}
			renderSyncSummary(out, &counts)
			if counts.Failed > 0 {
				return fmt.Errorf("%d repo(s) failed to sync", counts.Failed)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&rootDir, "root", "",
		"workspace root containing kit/ and app/ (defaults to the dir the daemon-default stack points at, or the cwd)")
	cmd.Flags().StringSliceVar(&allowFlags, "allow", nil,
		"only sync these repos (e.g. --allow=a-novel-kit/golib); may be repeated; empty == all whitelisted")
	cmd.Flags().StringSliceVar(&ignoreFlags, "ignore", nil,
		"skip these repos; wins over --allow")
	return cmd
}

// syncOne clones or updates one whitelisted repo at root/<subdir>/<name>.
// All git invocations get GIT_LFS_SKIP_SMUDGE=1 layered onto the env.
func syncOne(out io.Writer, root string, r repoEntry, counts *syncCounts) {
	target := filepath.Join(root, r.TargetSubdir(), r.Name)
	gitDir := filepath.Join(target, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		updateExistingRepo(out, target, r, counts)
		return
	}
	if _, err := os.Stat(target); err == nil {
		// Path exists but isn't a git repo — refuse to touch.
		_, _ = fmt.Fprintf(out, "  ✗ %s: %s exists but is not a git repo\n", r.FullName(), target)
		counts.add("failed", r.FullName())
		return
	}
	// Fresh clone — make sure the parent dir exists.
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		_, _ = fmt.Fprintf(out, "  ✗ %s: mkdir parent: %v\n", r.FullName(), err)
		counts.add("failed", r.FullName())
		return
	}
	c := exec.Command("git", "clone", "--quiet", r.SSHURL(), target)
	c.Env = appendLFSEnv(os.Environ())
	if cmdOut, err := c.CombinedOutput(); err != nil {
		_, _ = fmt.Fprintf(out, "  ✗ %s: clone failed: %v\n%s\n", r.FullName(), err, strings.TrimSpace(string(cmdOut)))
		counts.add("failed", r.FullName())
		return
	}
	_, _ = fmt.Fprintf(out, "  ✓ %s cloned → %s\n", r.FullName(), target)
	counts.add("cloned", r.FullName())
}

// updateExistingRepo performs the fetch-then-pull-or-update-ref dance
// for a repo that's already present locally:
//   - Off the default branch → `git fetch origin <def>:<def>` updates
//     the local default-branch ref only if fast-forwardable.
//   - On the default branch  → stash dirty work → ff-only pull → unstash.
//   - Divergence → skip.
func updateExistingRepo(out io.Writer, target string, r repoEntry, counts *syncCounts) {
	// Resolve the default branch from `origin/HEAD`, falling back to
	// "master" only if the symref is missing.
	def := resolveDefaultBranch(target)
	// Refresh refs.
	if cmdOut, err := runGit(target, "fetch", "--quiet", "--tags", "--prune", "origin"); err != nil {
		_, _ = fmt.Fprintf(out, "  ✗ %s: fetch failed: %v\n%s\n", r.FullName(), err, cmdOut)
		counts.add("failed", r.FullName())
		return
	}
	current, _ := runGit(target, "symbolic-ref", "--quiet", "--short", "HEAD")
	currentBranch := strings.TrimSpace(current)
	// Ensure a local branch tracking origin/<def> exists. Without this,
	// the off-default-branch path's `git fetch origin a:b` would create
	// a fresh local ref unconditionally — fine — but on-default-branch
	// path's `git pull` needs an existing local branch.
	if _, err := runGit(target, "rev-parse", "--verify", "--quiet", "refs/heads/"+def); err != nil {
		_, _ = runGit(target, "branch", "--quiet", "--track", def, "origin/"+def)
	}
	localSHA, _ := runGit(target, "rev-parse", "refs/heads/"+def)
	remoteSHA, _ := runGit(target, "rev-parse", "refs/remotes/origin/"+def)
	if strings.TrimSpace(localSHA) == strings.TrimSpace(remoteSHA) {
		_, _ = fmt.Fprintf(out, "  · %s up-to-date (%s)\n", r.FullName(), def)
		counts.add("uptodate", r.FullName())
		return
	}
	if currentBranch == def {
		// On the default branch — pull. Stash any uncommitted work
		// first so the pull doesn't refuse on a dirty tree.
		stashed := false
		if dirty(target) {
			if _, err := runGit(target, "stash", "push", "--include-untracked", "--quiet", "--message", "a-novel core sync auto-stash"); err == nil {
				stashed = true
			}
		}
		if cmdOut, err := runGit(target, "pull", "--quiet", "--ff-only", "origin", def); err != nil {
			if stashed {
				_, _ = runGit(target, "stash", "pop", "--quiet")
			}
			_, _ = fmt.Fprintf(out, "  ⚠ %s: %s diverged — skipped\n%s\n", r.FullName(), def, cmdOut)
			counts.add("skipped", r.FullName()+" (diverged "+def+")")
			return
		}
		if stashed {
			if cmdOut, err := runGit(target, "stash", "pop", "--quiet"); err != nil {
				_, _ = fmt.Fprintf(out, "  ⚠ %s: stash pop conflicted — resolve in this repo\n%s\n", r.FullName(), cmdOut)
				counts.add("failed", r.FullName())
				return
			}
		}
	} else {
		// Off the default branch — update its ref without touching HEAD.
		// `git fetch origin <a>:<a>` only succeeds when ff-able.
		if cmdOut, err := runGit(target, "fetch", "--quiet", "origin", def+":"+def); err != nil {
			_, _ = fmt.Fprintf(out, "  ⚠ %s: local %s diverged from origin — skipped\n%s\n", r.FullName(), def, cmdOut)
			counts.add("skipped", r.FullName()+" (diverged "+def+")")
			return
		}
	}
	_, _ = fmt.Fprintf(out, "  ✓ %s updated (%s)\n", r.FullName(), def)
	counts.add("updated", r.FullName())
}

// resolveDefaultBranch reads `origin/HEAD` to find the upstream's
// default branch. Falls back to "master" only when the symref is
// missing.
func resolveDefaultBranch(target string) string {
	out, err := runGit(target, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err == nil {
		// Output is like "origin/master" — strip the "origin/" prefix.
		ref := strings.TrimSpace(out)
		if strings.HasPrefix(ref, "origin/") {
			return strings.TrimPrefix(ref, "origin/")
		}
	}
	return branchMaster
}

// detectSelfRemote returns the SSH URL of the stack repo containing
// `root`, if any — so the sync loop can skip cloning it into itself.
func detectSelfRemote(root string) string {
	out, err := runGit(root, "config", "--get", "remote.origin.url")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// resolveSyncRoot picks the workspace root. Precedence:
//  1. --root flag (explicit override; trusted as-is).
//  2. The daemon-default stack's path (read from $A_NOVEL_STACKS —
//     same parsing the daemon uses at boot).
//  3. The current working directory.
//
// The chosen dir gets a quick `git rev-parse --show-toplevel` sanity
// check so a wrong --root surfaces immediately.
func resolveSyncRoot(override string) (string, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return "", fmt.Errorf("resolve --root: %w", err)
		}
		return abs, nil
	}
	defaultPath, err := defaultCLISource()
	if err == nil {
		// defaultCLISource returns "<stack>/cli"; the workspace root
		// is one level up.
		return filepath.Dir(defaultPath), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve cwd: %w", err)
	}
	return cwd, nil
}

// dirty reports whether the working tree at target has uncommitted
// changes. `git diff --quiet` exits non-zero when there's a diff;
// same for `--cached`. Either non-zero means "dirty."
func dirty(target string) bool {
	if _, err := runGit(target, "diff", "--quiet"); err != nil {
		return true
	}
	if _, err := runGit(target, "diff", "--cached", "--quiet"); err != nil {
		return true
	}
	return false
}

// runGit runs `git -C target <args...>` with LFS smudging disabled
// and returns combined output + error. Centralized so every git call
// shares the same env override.
func runGit(target string, args ...string) (string, error) {
	all := append([]string{"-C", target}, args...)
	c := exec.Command("git", all...)
	c.Env = appendLFSEnv(os.Environ())
	out, err := c.CombinedOutput()
	return string(out), err
}

// appendLFSEnv returns a fresh slice with GIT_LFS_SKIP_SMUDGE=1 layered
// on. Applied per-git-invocation rather than process-wide so the caller's
// env stays clean.
func appendLFSEnv(env []string) []string {
	return append(env, "GIT_LFS_SKIP_SMUDGE=1")
}

// normaliseFilter turns the StringSlice flag values into a set. Splits
// on comma too so `--allow=a,b` works alongside `--allow=a --allow=b`.
func normaliseFilter(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out[part] = struct{}{}
			}
		}
	}
	return out
}

// renderSyncSummary prints the end-of-run summary: counts per bucket plus
// the per-repo line list.
func renderSyncSummary(w io.Writer, c *syncCounts) {
	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Summary")
	_, _ = fmt.Fprintf(w, "  ✓ cloned     %d\n", c.Cloned)
	_, _ = fmt.Fprintf(w, "  ✓ updated    %d\n", c.Updated)
	_, _ = fmt.Fprintf(w, "  · up-to-date %d\n", c.UpToDate)
	_, _ = fmt.Fprintf(w, "  ⚠ skipped    %d\n", c.Skipped)
	_, _ = fmt.Fprintf(w, "  ✗ failed     %d\n", c.Failed)
}
