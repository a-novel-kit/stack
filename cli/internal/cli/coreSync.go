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

	"github.com/a-novel-kit/stack/cli/internal/batchview"
	"github.com/a-novel-kit/stack/cli/internal/jobs"
	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil"
	"github.com/a-novel-kit/stack/cli/internal/workspace"
)

// Sync outcomes, counted in the report.
const (
	syncCloned   = "cloned"
	syncUpdated  = "updated"
	syncUpToDate = "up-to-date"
	syncSkipped  = "skipped"
)

// syncOpts holds the flags of `core sync`.
type syncOpts struct {
	rootDir       string
	allow, ignore []string
	limit         int
}

// newCoreSyncCmd clones or fast-forwards the workspace repositories
// workspace-repos.yaml lists, all at once.
func newCoreSyncCmd() *cobra.Command {
	o := syncOpts{limit: runtime.NumCPU()}
	cmd := &cobra.Command{
		Use:   commandSync,
		Short: "Clone or fast-forward the workspace repositories",
		Long: `Clone each repository workspace-repos.yaml lists, or fast-forward the checkout
already there: an a-novel-kit repository lands under kit/, an a-novel one under
app/.

Repositories sync in parallel over one shared SSH connection to GitHub, so a
security key asks for a single touch. A checkout's default branch only moves
forward: sync never switches branches, never overwrites a change, and skips a
branch that diverged. LFS files stay unfetched.`,
		Example: `  a-novel core sync
  a-novel core sync --allow=a-novel-kit/golib
  a-novel core sync --ignore=a-novel/service-template
  a-novel core sync --root=/tmp/agent-stack -j 4`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return syncWorkspace(cmd, o)
		},
	}
	cmd.Flags().StringVar(&o.rootDir, "root", "",
		"workspace root containing kit/ and app/ (default: the default stack, else the working directory)")
	cmd.Flags().StringSliceVar(&o.allow, "allow", nil,
		"only sync these repos (<org>/<name> or bare <name>); may be repeated")
	cmd.Flags().StringSliceVar(&o.ignore, "ignore", nil,
		"skip these repos (<org>/<name> or bare <name>); wins over --allow")
	cmd.Flags().VarP(countFlag{&o.limit}, "jobs", "j", "max repos synced at once")
	return cmd
}

// syncWorkspace syncs the whitelisted repositories the filters keep, in
// parallel over one SSH connection, then reports each failed or skipped one.
func syncWorkspace(cmd *cobra.Command, o syncOpts) error {
	root, err := workspace.Root(o.rootDir)
	if err != nil {
		return err
	}
	repos, err := workspace.LoadRepos(root)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(repos) == 0 {
		_, _ = fmt.Fprintf(out, "no %s at %s: nothing to sync.\n"+
			"Create it with a `repos:` list of <org>/<name> entries.\n", workspace.ReposFile, root)
		return nil
	}

	list, skipped := syncJobs(cmd.Context(), root, repos, normalizeFilter(o.allow), normalizeFilter(o.ignore))
	_, _ = fmt.Fprintf(out, "▸ Syncing %d repo(s) at %s\n", len(list), root)
	if len(skipped) > 0 {
		_, _ = fmt.Fprintf(out, "  left out: %s\n", strings.Join(skipped, ", "))
	}
	if len(list) == 0 {
		return nil
	}
	workspace.GitHubMux.Open(cmd.Context())
	batch := batchview.RunJobs(cmd.Context(), out, list, o.limit, stdoutIsTTY())
	_, _ = fmt.Fprint(out, batchview.Report("SYNC", batch, syncNeedsAttention))
	return batchError(batch)
}

// syncNeedsAttention selects the sync results the report details: the failed
// repositories and the skipped ones.
func syncNeedsAttention(r jobs.Result) bool {
	return r.Err != nil || r.Status == syncSkipped
}

// syncJobs returns a sync job for each of repos the filters keep. Both filters
// name a repo by its full or its bare name. It also returns the names left out:
// those --ignore names, and the stack repository at root, which never syncs into
// itself. --allow drops the others silently.
func syncJobs(ctx context.Context, root string, repos []workspace.Repo, allowed, ignored map[string]bool) ([]jobs.Job, []string) {
	self, _ := workspace.RootRepo(ctx, root)
	var list []jobs.Job
	var skipped []string
	for _, r := range repos {
		name := r.FullName()
		if len(allowed) > 0 && !r.Listed(allowed) {
			continue
		}
		if r.Listed(ignored) || r == self {
			skipped = append(skipped, name)
			continue
		}
		list = append(list, jobs.Job{
			Name: name,
			Run:  func(ctx context.Context, out io.Writer) (string, error) { return syncRepo(ctx, root, r, out) },
		})
	}
	return list, skipped
}

// syncRepo clones r under root, or updates the checkout already there.
func syncRepo(ctx context.Context, root string, r workspace.Repo, out io.Writer) (string, error) {
	dir := r.Dir(root)
	if fsutil.Exists(filepath.Join(dir, ".git")) {
		return updateRepo(ctx, dir, out)
	}
	if fsutil.Exists(dir) {
		return "", fmt.Errorf("%s exists but is not a git repo", dir)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", fmt.Errorf("create parent: %w", err)
	}
	if _, err := workspace.GitRemote(ctx, filepath.Dir(dir), "clone", "--quiet", r.SSHURL(), dir); err != nil {
		return "", fmt.Errorf("clone failed: %w", err)
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
func updateRepo(ctx context.Context, dir string, out io.Writer) (string, error) {
	def := workspace.DefaultBranch(ctx, dir)
	if _, err := workspace.GitRemote(ctx, dir, "fetch", "--quiet", "--tags", "--prune", "origin"); err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	// The on-default path's pull needs a local branch tracking origin/<def>.
	if _, err := workspace.Git(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+def); err != nil {
		_, _ = workspace.Git(ctx, dir, "branch", "--quiet", "--track", def, "origin/"+def)
	}
	// A failed rev-parse leaves the refs unequal, and the fast-forward below
	// reports the problem.
	local, localErr := workspace.Git(ctx, dir, "rev-parse", "refs/heads/"+def)
	remote, remoteErr := workspace.Git(ctx, dir, "rev-parse", "refs/remotes/origin/"+def)
	if localErr == nil && remoteErr == nil && local == remote {
		_, _ = fmt.Fprintf(out, "%s up-to-date\n", def)
		return syncUpToDate, nil
	}

	current, _ := workspace.Git(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	args := []string{"fetch", "--quiet", "origin", def + ":" + def}
	if current == def {
		args = []string{"pull", "--quiet", "--ff-only", "origin", def}
	}
	if _, err := workspace.GitRemote(ctx, dir, args...); err != nil {
		reason := "local " + def + " diverged from origin"
		if strings.Contains(err.Error(), "overwritten") {
			reason = "unstaged changes on " + def + " would be overwritten (changes kept)"
		}
		_, _ = fmt.Fprintf(out, "%s: skipped\n%s\n", reason, err)
		return syncSkipped, nil
	}
	_, _ = fmt.Fprintf(out, "%s updated\n", def)
	return syncUpdated, nil
}

// normalizeFilter turns the values of a repeatable flag, which pflag already
// split on commas, into a set of trimmed names.
func normalizeFilter(values []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out[v] = true
		}
	}
	return out
}
