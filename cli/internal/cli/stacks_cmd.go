package cli

// Stack lifecycle. `core sync --root` stands a stack up in one verb, so tearing
// one down is one verb too.
//
// Prune exists because a stack allocates three things and only one of them is a
// file. Deleting the root — by hand, or by a tmp sweep — reclaims the checkout
// and leaves the rest: containers keep running and holding host ports, and
// podman volumes sit under the container store until something names them. Both
// are the daemon's to release, which is why this is a CLI verb rather than a
// documented `rm -rf`.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// killGrace is how long a target gets to exit on SIGTERM before the daemon
// escalates. Matches the `run kill` default — a stack being torn down has no
// claim to a longer goodbye than a single target.
const killGrace = 10 * time.Second

func newCoreStacksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stacks",
		Short: "List and prune the registered workspace stacks",
		Long: `Inspect what each registered stack is holding, and reclaim the ones that
have served their purpose.

A stack is a whole checkout of the workspace ($A_NOVEL_STACKS entry +
its kit/ and app/ repos). Extra stacks let a session work without
disturbing a checkout someone else is mid-task in; pruning is how that
space is given back.`,
	}
	cmd.AddCommand(newStacksListCmd())
	cmd.AddCommand(newStacksPruneCmd())
	return cmd
}

// =============================================================================
// list
// =============================================================================

func newStacksListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every registered stack and what it is holding",
		Long: `Print one row per stack: its name, path, running targets, and volumes.

The default stack is marked '*' and is never prunable. Use this before a
prune sweep to see which stacks are still doing something and which are
leftovers from a finished session.`,
		Example: `  a-novel core stacks list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			resp, err := c.ListStacks(ctx)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "  %-20s %-40s %8s %8s\n", "NAME", "PATH", "TARGETS", "VOLUMES")
			for _, st := range resp.GetStacks() {
				marker := " "
				if st.GetIsDefault() {
					marker = "*"
				}
				held, err := inspectStack(ctx, c, st.GetName())
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(out, "%s %-20s %-40s %8s %8d\n",
					marker, st.GetName(), st.GetPath(),
					fmt.Sprintf("%d up", len(held.liveTargets)), len(held.volumes))
			}
			return nil
		},
	}
}

// =============================================================================
// prune
// =============================================================================

func newStacksPruneCmd() *cobra.Command {
	var all, force, yes, dryRun bool
	cmd := &cobra.Command{
		Use:   "prune [<name>]",
		Short: "Tear down a stack: kill its targets, clear its volumes, remove its files",
		Long: `Reclaim everything a stack holds, in the order that leaves nothing behind:
kill its running targets, stop its infra containers, clear its volumes
(auto-backed up first), then remove the checkout.

The default stack is never pruned — it is the workspace, not scratch
space. With --all, every OTHER registered stack is pruned, which is the
sweep to run after a batch of agent sessions.

Refuses a stack whose checkouts hold work that exists nowhere else: a
dirty tree, a branch other than the default, or commits not yet pushed.
--force overrides that refusal and throws the work away.

$A_NOVEL_STACKS lives in your shell config, so prune reports the entry
to drop rather than editing the file underneath you.`,
		Example: `  a-novel core stacks list
  a-novel core stacks prune agent-7b46
  a-novel core stacks prune agent-7b46 --dry-run
  a-novel core stacks prune --all -y`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			c := rpc.New("")

			targets, err := resolvePruneTargets(ctx, c, args, all)
			if err != nil {
				return err
			}
			if len(targets) == 0 {
				_, _ = fmt.Fprintln(out, "no prunable stacks (the default stack is never pruned)")
				return nil
			}

			failed := 0
			for _, st := range targets {
				if err := pruneOne(ctx, c, cmd, st, pruneOpts{
					force: force, yes: yes, dryRun: dryRun,
				}); err != nil {
					_, _ = fmt.Fprintf(out, "  ✗ %s: %v\n", st.GetName(), err)
					failed++
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d stack(s) failed to prune", failed)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "prune every registered stack except the default")
	cmd.Flags().BoolVar(&force, "force", false, "prune even when a checkout holds unsaved or unpushed work")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report what would be reclaimed, change nothing")
	return cmd
}

type pruneOpts struct {
	force  bool
	yes    bool
	dryRun bool
}

// resolvePruneTargets maps the command's arguments onto the stacks the daemon
// actually knows about. The default stack is filtered out of --all rather than
// reported as an error: a sweep is meant to be run without reading the list
// first, so "everything disposable" is the useful reading of --all.
func resolvePruneTargets(
	ctx context.Context, c *rpc.Client, args []string, all bool,
) ([]*anovelv1.Stack, error) {
	if all && len(args) > 0 {
		return nil, fmt.Errorf("--all takes no stack name (got %q)", args[0])
	}
	if !all && len(args) == 0 {
		return nil, errors.New("name a stack to prune, or pass --all")
	}
	resp, err := c.ListStacks(ctx)
	if err != nil {
		return nil, err
	}
	return selectPruneTargets(resp.GetStacks(), args, all)
}

// selectPruneTargets is resolvePruneTargets' decision half, split from the wire
// call so the default-stack protections are exercised without a daemon.
func selectPruneTargets(registered []*anovelv1.Stack, args []string, all bool) ([]*anovelv1.Stack, error) {
	if all {
		var out []*anovelv1.Stack
		for _, st := range registered {
			if !st.GetIsDefault() {
				out = append(out, st)
			}
		}
		return out, nil
	}
	for _, st := range registered {
		if st.GetName() != args[0] {
			continue
		}
		if st.GetIsDefault() {
			return nil, fmt.Errorf(
				"%q is the default stack — it is the workspace, not scratch space", st.GetName())
		}
		return []*anovelv1.Stack{st}, nil
	}
	return nil, fmt.Errorf("no registered stack named %q (see `a-novel core stacks list`)", args[0])
}

// stackHoldings is what a stack has allocated, as one snapshot, so the plan
// printed to the operator and the teardown that follows cannot disagree.
type stackHoldings struct {
	services    []string
	liveTargets []string // fully-qualified target IDs still short of TERMINATED
	liveInfra   int
	volumes     []string
}

func inspectStack(ctx context.Context, c *rpc.Client, name string) (stackHoldings, error) {
	resp, err := c.ListServices(ctx, name)
	if err != nil {
		return stackHoldings{}, err
	}
	return holdingsOf(resp.GetServices()), nil
}

// holdingsOf reduces a stack's services to what teardown has to act on. A target
// or container short of TERMINATED is live: PENDING and STARTING entries have a
// process or container behind them (or are about to), and skipping those is how
// a prune leaves a port held by something it reported as gone.
func holdingsOf(services []*anovelv1.Service) stackHoldings {
	var h stackHoldings
	for _, svc := range services {
		h.services = append(h.services, svc.GetName())
		for _, t := range svc.GetTargets() {
			if t.GetPhase() != anovelv1.Phase_PHASE_TERMINATED &&
				t.GetPhase() != anovelv1.Phase_PHASE_UNSPECIFIED {
				h.liveTargets = append(h.liveTargets, t.GetId())
			}
		}
		for _, in := range svc.GetInfra() {
			if in.GetPhase() != anovelv1.Phase_PHASE_TERMINATED &&
				in.GetPhase() != anovelv1.Phase_PHASE_UNSPECIFIED {
				h.liveInfra++
			}
		}
		for _, v := range svc.GetVolumes() {
			h.volumes = append(h.volumes, v.GetName())
		}
	}
	return h
}

func pruneOne(
	ctx context.Context, c *rpc.Client, cmd *cobra.Command, st *anovelv1.Stack, opts pruneOpts,
) error {
	out := cmd.OutOrStdout()
	name, root := st.GetName(), st.GetPath()

	held, err := inspectStack(ctx, c, name)
	if err != nil {
		return err
	}
	blockers := pruneBlockers(root)
	if len(blockers) > 0 && !opts.force {
		return fmt.Errorf("holds work that exists nowhere else (%s) — push it, or --force to discard",
			strings.Join(blockers, "; "))
	}

	_, _ = fmt.Fprintf(out, "▸ %s (%s)\n", name, root)
	_, _ = fmt.Fprintf(out, "  %d target(s) up, %d infra container(s), %d volume(s), %d service(s)\n",
		len(held.liveTargets), held.liveInfra, len(held.volumes), len(held.services))
	for _, b := range blockers {
		_, _ = fmt.Fprintf(out, "  ! discarding: %s\n", b)
	}
	if opts.dryRun {
		_, _ = fmt.Fprintln(out, "  (dry run — nothing changed)")
		return nil
	}
	if !opts.yes && !confirm(cmd, fmt.Sprintf("  Prune %s and everything it holds?", name)) {
		_, _ = fmt.Fprintln(out, "  skipped")
		return nil
	}

	for _, id := range held.liveTargets {
		if _, err := c.KillTarget(ctx, id, killGrace); err != nil {
			return fmt.Errorf("kill target %s: %w", id, err)
		}
	}
	for _, svc := range held.services {
		// force cascades onto whatever the infra is still holding open;
		// the targets above are already down, so nothing is orphaned by it.
		if _, err := c.KillInfra(ctx, name, svc, true); err != nil {
			return fmt.Errorf("kill infra %s/%s: %w", name, svc, err)
		}
		// Backups are kept: a stack is pruned on the assumption its data was
		// disposable, and that assumption is occasionally wrong.
		if _, err := c.ClearVolume(ctx, name, svc, false, true); err != nil {
			return fmt.Errorf("clear volumes %s/%s: %w", name, svc, err)
		}
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove %s: %w", root, err)
	}
	_, _ = fmt.Fprintf(out, "  ✓ reclaimed %s\n", root)
	_, _ = fmt.Fprintf(out, "  ! drop from %s: %s:%s\n", stacks.EnvVar, name, root)
	return nil
}

// pruneBlockers lists the reasons a stack's checkouts should outlive the prune,
// one per checkout. It reuses `repo update`'s ongoingWork for the dirty-tree and
// off-default-branch cases, and adds unpushed commits — a clean checkout sitting
// on its default branch can still be the only copy of a commit.
func pruneBlockers(root string) []string {
	cands, err := discoverUpdateCandidates(root)
	if err != nil {
		// An unreadable root has no recoverable work in it by definition;
		// removing it is the whole point of the command.
		return nil
	}
	var blockers []string
	for _, c := range cands {
		if reason := ongoingWork(c.dir); reason != "" {
			blockers = append(blockers, c.repo+": "+reason)
			continue
		}
		if n := unpushedCommits(c.dir); n > 0 {
			blockers = append(blockers, fmt.Sprintf("%s: %d unpushed commit(s)", c.repo, n))
		}
	}
	return blockers
}

// unpushedCommits counts commits on HEAD that its upstream does not have. A
// checkout with no upstream configured counts as zero: there is no remote to
// have lost them to, and `git clone` always sets one, so this is the
// never-pushed-anywhere case rather than a silent miss.
func unpushedCommits(dir string) int {
	out, err := runGit(dir, "rev-list", "--count", "@{upstream}..HEAD")
	if err != nil {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "%d", &n); err != nil {
		return 0
	}
	return n
}
