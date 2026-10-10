// `a-novel core sync` — clone or fast-forward the workspace repositories listed
// in workspace-repos.yaml, all at once.

package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/jobs"
	"github.com/a-novel-kit/stack/cli/internal/ui"
)

// Sync outcomes, counted in the report.
const (
	syncCloned   = "cloned"
	syncUpdated  = "updated"
	syncUpToDate = "up-to-date"
	syncSkipped  = "skipped"
)

func newCoreSyncCmd() *cobra.Command {
	var (
		rootDir       string
		allow, ignore []string
		limit         int
	)
	cmd := &cobra.Command{
		Use:   commandSync,
		Short: "Clone or fast-forward-pull the curated workspace repos",
		Long: `Clone or update every repo listed in workspace-repos.yaml at the workspace
root, the single source of truth (no built-in default; an absent file means
nothing to sync). Each "<org>/<repo>" entry lands in:

  a-novel-kit/<repo> → kit/<repo>
  a-novel/<repo>     → app/<repo>

Repos sync in parallel, up to --jobs at once (default: one per CPU).

Existing repos are fast-forward pulled on the default branch (or have their
default-branch ref updated when you are on a feature branch). sync never
switches branches and never stashes: unstaged changes are preserved — git
refuses (and sync skips) rather than overwrite them, and diverged defaults are
left untouched. The stack repo itself is never cloned into kit/.
GIT_LFS_SKIP_SMUDGE=1 keeps LFS blobs unfetched.

--allow / --ignore subset the list; both may be repeated, and --ignore wins.

Sub-agents working out of a fresh stack should run this first to populate kit/
and app/.`,
		Example: `  a-novel core sync
  a-novel core sync --allow=a-novel-kit/golib
  a-novel core sync --ignore=a-novel/service-template
  a-novel core sync --root=/tmp/agent-stack -j 4`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := workspaceRoot(rootDir)
			if err != nil {
				return err
			}
			repos, err := loadRepoWhitelist(root)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(repos) == 0 {
				_, _ = fmt.Fprintf(out, "no %s at %s — nothing to sync.\n"+
					"Create it with a `repos:` list of <org>/<name> entries.\n", repoWhitelistFile, root)
				return nil
			}

			allowed, ignored := normaliseFilter(allow), normaliseFilter(ignore)
			self, _ := repoFromGitRemote(root)
			var list []jobs.Job
			var skipped []string
			for _, r := range repos {
				switch {
				case len(allowed) > 0 && !allowed[r.FullName()]:
				case ignored[r.FullName()] || r == self:
					skipped = append(skipped, r.FullName())
				default:
					list = append(list, jobs.Job{
						Name: r.FullName(),
						Run: func(_ context.Context, out io.Writer) (string, error) {
							return syncRepo(root, r, out)
						},
					})
				}
			}
			_, _ = fmt.Fprintf(out, "▸ Syncing %d repo(s) at %s\n", len(list), root)
			if len(skipped) > 0 {
				_, _ = fmt.Fprintf(out, "  left out: %s\n", strings.Join(skipped, ", "))
			}
			if len(list) == 0 {
				return nil
			}
			batch := ui.RunJobs(cmd.Context(), list, limit, stdoutIsTTY())
			_, _ = fmt.Fprint(out, ui.Report("SYNC", batch, func(r jobs.Result) bool {
				return r.Err != nil || r.Status == syncSkipped
			}))
			return batchError(batch)
		},
	}
	cmd.Flags().StringVar(&rootDir, "root", "",
		"workspace root containing kit/ and app/ (default: the default stack, else the working directory)")
	cmd.Flags().StringSliceVar(&allow, "allow", nil,
		"only sync these repos (e.g. --allow=a-novel-kit/golib); may be repeated")
	cmd.Flags().StringSliceVar(&ignore, "ignore", nil, "skip these repos; wins over --allow")
	cmd.Flags().IntVarP(&limit, "jobs", "j", runtime.NumCPU(), "max repos synced at once")
	return cmd
}

// syncRepo clones r under root, or updates the checkout already there.
func syncRepo(root string, r repoEntry, out io.Writer) (string, error) {
	dir := r.Dir(root)
	if exists(filepath.Join(dir, ".git")) {
		return updateRepo(dir, out)
	}
	if exists(dir) {
		return "", fmt.Errorf("%s exists but is not a git repo", dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", fmt.Errorf("create parent: %w", err)
	}
	if gitOut, err := runGit(filepath.Dir(dir), "clone", "--quiet", r.SSHURL(), dir); err != nil {
		return "", fmt.Errorf("clone failed: %w\n%s", err, strings.TrimSpace(gitOut))
	}
	_, _ = fmt.Fprintf(out, "cloned → %s\n", dir)
	return syncCloned, nil
}

// updateRepo fetches dir and fast-forwards its default branch:
//   - off the default branch, `git fetch origin <def>:<def>` updates the ref
//     only when it fast-forwards, leaving HEAD and the working tree alone;
//   - on it, `git pull --ff-only` advances it in place, preserving unstaged
//     changes, and git's refusal to overwrite one becomes a skip;
//   - a diverged default is skipped.
func updateRepo(dir string, out io.Writer) (string, error) {
	def := defaultBranch(dir)
	if gitOut, err := runGit(dir, "fetch", "--quiet", "--tags", "--prune", "origin"); err != nil {
		return "", fmt.Errorf("fetch failed: %w\n%s", err, gitOut)
	}
	// The on-default path's pull needs a local branch tracking origin/<def>.
	if _, err := runGit(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+def); err != nil {
		_, _ = runGit(dir, "branch", "--quiet", "--track", def, "origin/"+def)
	}
	local, _ := runGit(dir, "rev-parse", "refs/heads/"+def)
	remote, _ := runGit(dir, "rev-parse", "refs/remotes/origin/"+def)
	if local == remote {
		_, _ = fmt.Fprintf(out, "%s up-to-date\n", def)
		return syncUpToDate, nil
	}

	current, _ := runGit(dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	args := []string{"fetch", "--quiet", "origin", def + ":" + def}
	if strings.TrimSpace(current) == def {
		args = []string{"pull", "--quiet", "--ff-only", "origin", def}
	}
	if gitOut, err := runGit(dir, args...); err != nil {
		reason := "local " + def + " diverged from origin"
		if strings.Contains(gitOut, "overwritten") {
			reason = "unstaged changes on " + def + " would be overwritten (changes kept)"
		}
		_, _ = fmt.Fprintf(out, "%s — skipped\n%s\n", reason, strings.TrimSpace(gitOut))
		return syncSkipped, nil
	}
	_, _ = fmt.Fprintf(out, "%s updated\n", def)
	return syncUpdated, nil
}

// exists reports whether path exists. A worktree's .git is a file, so a
// checkout test cannot require a directory.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
