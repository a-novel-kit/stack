// `a-novel core sync` — clone or fast-forward-pull the curated set of
// a-novel / a-novel-kit repositories that make up the local workspace.
//
// The repo set is an explicit whitelist read from workspace-repos.yaml at the
// workspace root: each `repos:` entry is an "<org>/<repo>", and
// `--allow`/`--ignore` subset that list. The file is the only source of truth,
// so an absent file means nothing to sync. Git over SSH is the only dependency.
//
// Behavior:
//   - Skip the workspace's own repo, so kit/stack never appears as a
//     duplicate of the dir the binary was launched from.
//   - Existing repos: ff-only pull on the default branch, ref-update
//     when off-branch, skip on divergence. Branches are never switched and
//     nothing is ever stashed; unstaged changes are left exactly as they are.
//   - GIT_LFS_SKIP_SMUDGE=1 on every git invocation, so LFS blobs stay
//     unfetched.
//   - Per-run summary at the end.

package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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

// The two GitHub organizations the stack orchestrates. They are constants
// because the literals appear across the whole CLI surface, which goconst
// flags.
const (
	orgAnovel    = "a-novel"
	orgAnovelKit = "a-novel-kit"
)

// repoWhitelistFile is the runtime whitelist, read from the workspace root and
// the single source of truth for which repos `core sync` touches. There is no
// baked-in default, so the list can be edited without rebuilding the CLI.
const repoWhitelistFile = "workspace-repos.yaml"

// loadRepoWhitelist reads the curated repo list from <root>/workspace-repos.yaml.
// A missing file yields an empty list and no error, and the caller reports
// "nothing to sync". Each `repos:` entry is an "<org>/<repo>" string; the org
// must be one of the two the workspace routes (a-novel → app/, a-novel-kit →
// kit/). Any other org is an error, so a typo never routes a clone.
func loadRepoWhitelist(root string) ([]repoEntry, error) {
	data, err := os.ReadFile(filepath.Join(root, repoWhitelistFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", repoWhitelistFile, err)
	}
	var doc struct {
		Repos []string `yaml:"repos"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", repoWhitelistFile, err)
	}
	entries := make([]repoEntry, 0, len(doc.Repos))
	for _, raw := range doc.Repos {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		org, name, ok := strings.Cut(raw, "/")
		if !ok || org == "" || name == "" {
			return nil, fmt.Errorf("%s: invalid repo %q (want <org>/<name>)", repoWhitelistFile, raw)
		}
		switch org {
		case orgAnovel, orgAnovelKit:
		default:
			return nil, fmt.Errorf("%s: unknown org %q in %q (want %s or %s)",
				repoWhitelistFile, org, raw, orgAnovel, orgAnovelKit)
		}
		entries = append(entries, repoEntry{Org: org, Name: name})
	}
	return entries, nil
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
		Use:   commandSync,
		Short: "Clone or fast-forward-pull the curated workspace repos",
		Long: `Clone or update every repo listed in the workspace whitelist file
(workspace-repos.yaml at the workspace root). The file is the single
source of truth — there is no built-in default — so each entry under
its ` + "`repos:`" + ` list is an "<org>/<repo>" cloned or fast-forwarded into:

  a-novel-kit/<repo> → kit/<repo>
  a-novel/<repo>     → app/<repo>

Edit the file to add or drop a repo; no rebuild is needed. If the file
is absent, sync has nothing to do.

Existing repos are fast-forward pulled on the default branch (or have
their default-branch ref updated when you are on a feature branch).
sync never switches branches and never stashes: unstaged changes are
preserved — git refuses (and sync skips) rather than overwrite them,
and diverged defaults are left untouched. The workspace's own repo
(the stack the CLI itself lives in) is automatically skipped to avoid
a duplicate clone under kit/stack.

GIT_LFS_SKIP_SMUDGE=1 is forced on every invocation so large LFS
blobs are not pulled.

--allow / --ignore both subset the whitelist. Both may be repeated.
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
			out := cmd.OutOrStdout()
			repos, err := loadRepoWhitelist(root)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "▸ Syncing workspace repos at %s\n", root)
			if len(repos) == 0 {
				_, _ = fmt.Fprintf(out, "  no %s found at %s — nothing to sync\n"+
					"  create it with a `repos:` list of <org>/<name> entries.\n", repoWhitelistFile, root)
				return nil
			}
			allow := normaliseFilter(allowFlags)
			ignore := normaliseFilter(ignoreFlags)
			selfURL := detectSelfRemote(root)
			if selfURL != "" {
				_, _ = fmt.Fprintf(out, "  (self-detected origin: %s — will be skipped)\n", selfURL)
			}
			counts := syncCounts{}
			for _, r := range repos {
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
//     the local default-branch ref only if fast-forwardable, leaving
//     HEAD and the working tree untouched.
//   - On the default branch  → `git pull --ff-only` advances it in
//     place. Unstaged changes are preserved, and a pull git refuses
//     over a dirty file is skipped.
//   - Divergence → skip.
func updateExistingRepo(out io.Writer, target string, r repoEntry, counts *syncCounts) {
	def := resolveDefaultBranch(target)
	// Refresh refs.
	if cmdOut, err := runGit(target, "fetch", "--quiet", "--tags", "--prune", "origin"); err != nil {
		_, _ = fmt.Fprintf(out, "  ✗ %s: fetch failed: %v\n%s\n", r.FullName(), err, cmdOut)
		counts.add("failed", r.FullName())
		return
	}
	current, _ := runGit(target, "symbolic-ref", "--quiet", "--short", "HEAD")
	currentBranch := strings.TrimSpace(current)
	// Ensure a local branch tracking origin/<def> exists: the
	// on-default-branch path's `git pull` needs one.
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
		// On the default branch, fast-forward it in place. `git pull
		// --ff-only` advances HEAD without disturbing unstaged changes, and
		// refuses when incoming commits would clobber a locally-modified
		// file. That refusal surfaces as a skip, so the working tree is
		// never rewritten behind the user's back.
		if cmdOut, err := runGit(target, "pull", "--quiet", "--ff-only", "origin", def); err != nil {
			if strings.Contains(cmdOut, "overwritten") {
				_, _ = fmt.Fprintf(out, "  ⚠ %s: unstaged changes on %s would be overwritten — skipped (changes kept)\n%s\n", r.FullName(), def, cmdOut)
				counts.add("skipped", r.FullName()+" (dirty "+def+")")
				return
			}
			_, _ = fmt.Fprintf(out, "  ⚠ %s: %s diverged — skipped\n%s\n", r.FullName(), def, cmdOut)
			counts.add("skipped", r.FullName()+" (diverged "+def+")")
			return
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

// appendLFSEnv returns a fresh slice with GIT_LFS_SKIP_SMUDGE=1 layered on.
// Applied per git invocation, so the caller's env stays clean.
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
