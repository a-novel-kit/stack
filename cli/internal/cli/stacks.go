package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	"github.com/a-novel-kit/stack/cli/internal/setup"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// killGrace is how long a target gets to exit on SIGTERM before the daemon
// escalates. It matches the `run kill` default.
const killGrace = 10 * time.Second

// newCoreStacksCmd allocates, lists and prunes workspace stacks. A stack holds
// a checkout, containers holding host ports, and podman volumes. Deleting the
// root reclaims only the checkout, and the other two are the daemon's to
// release, so pruning is a verb of its own.
func newCoreStacksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stacks",
		Short: "Create, list and prune workspace stacks",
		Long: `A stack is a whole workspace: a checkout of the stack repository with its app/
and kit/ repositories, registered in $A_NOVEL_STACKS. An extra stack lets a
session work without touching another one's checkouts, and pruning it gives
back its files, containers and volumes.`,
	}
	cmd.AddCommand(newStacksNewCmd())
	cmd.AddCommand(newStacksListCmd())
	cmd.AddCommand(newStacksPruneCmd())
	return cmd
}

// =============================================================================
// new
// =============================================================================

// defaultStackRoot is where an extra workspace lands when the caller does not
// choose. os.TempDir() honors $TMPDIR, resolving to the per-user
// /var/folders/…/T on macOS and /tmp on Linux. The OS reclaims both, so a
// stack that outlives its session is a disk leak with an expiry date.
//
// The default stack lives elsewhere: it is the workspace, and a workspace under
// a directory the OS empties is a trap.
func defaultStackRoot(name string) string {
	return filepath.Join(os.TempDir(), "a-novel-stacks", name)
}

func newStacksNewCmd() *cobra.Command {
	var root string
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Allocate an extra workspace stack",
		Long: `Clone the stack repository into a new root, and print the $A_NOVEL_STACKS
entry that registers it. The root defaults to the temporary directory,
$TMPDIR included, which the system empties in time; --root puts the stack
somewhere lasting.

The daemon reads $A_NOVEL_STACKS when it starts. Add the entry, run
'a-novel core sync --root=<root>', then
'a-novel core restart --preserve-targets'.`,
		Example: `  a-novel core stacks new agent-7b46
  a-novel core stacks new review-291 --root ~/scratch/review-291`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return createStack(cmd.OutOrStdout(), args[0], root)
		},
	}
	cmd.Flags().StringVar(&root, "root", "",
		"where to put the stack (default: <os temp dir>/a-novel-stacks/<name>)")
	return cmd
}

// createStack clones the workspace into root, or the default root for name,
// and prints the $A_NOVEL_STACKS entry that registers it. A root holding files
// is refused.
func createStack(out io.Writer, name, root string) error {
	if name == stacks.DefaultName {
		return fmt.Errorf("%q is reserved for the workspace stack", stacks.DefaultName)
	}
	if root == "" {
		root = defaultStackRoot(name)
	}
	if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty", root)
	}
	_, _ = fmt.Fprintf(out, "▸ cloning the workspace into %s\n", root)
	if err := setup.CloneStack(root); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "  ✓ %s\n", root)
	_, _ = fmt.Fprintf(out, "  add to %s: %s:%s\n", stacks.EnvVar, name, root)
	_, _ = fmt.Fprintf(out, "  then: a-novel core sync --root=%s && a-novel core restart --preserve-targets\n", root)
	return nil
}

// =============================================================================
// list
// =============================================================================

func newStacksListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every registered stack and what it is holding",
		Long: `Print one row per stack: its name and path, and the targets, infra containers
and volumes it holds. A '*' marks the default stack, which is never pruned. A
registration the daemon does not manage follows, with the reason.`,
		Example: `  a-novel core stacks list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listStacks(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

// listStacks prints every registered stack with what it holds, then the
// registrations the daemon does not manage. `core stacks list` and `run
// stacks` both render it.
func listStacks(ctx context.Context, out io.Writer) error {
	c := rpc.New("")
	resp, err := c.ListStacks(ctx)
	if err != nil {
		return err
	}
	managed := make(map[string]bool, len(resp.GetStacks()))
	_, _ = fmt.Fprintf(out, "  %-20s %-40s %8s %8s %8s\n", "NAME", "PATH", "TARGETS", "INFRA", "VOLUMES")
	for _, st := range resp.GetStacks() {
		marker := " "
		if st.GetIsDefault() {
			marker = "*"
		}
		held, err := inspectStack(ctx, c, st.GetName())
		if err != nil {
			return err
		}
		// A stack whose only live container is its database still holds a
		// host port, and the infra column is what shows it.
		_, _ = fmt.Fprintf(out, "%s %-20s %-40s %8s %8s %8d\n",
			marker, st.GetName(), st.GetPath(),
			fmt.Sprintf("%d up", len(held.liveTargets)),
			fmt.Sprintf("%d up", held.liveInfra),
			len(held.volumes))
		managed[st.GetName()] = true
	}
	return reportUnmanaged(out, managed)
}

// =============================================================================
// prune
// =============================================================================

func newStacksPruneCmd() *cobra.Command {
	var all bool
	var opts pruneOpts
	cmd := &cobra.Command{
		Use:   "prune [<name>]",
		Short: "Tear down a stack: kill its targets, clear its volumes, remove its files",
		Long: `Reclaim what a stack holds: stop its targets and infra, clear its volumes after
a backup, then delete its files. --all prunes every stack but the default,
which is never pruned.

A stack whose checkouts hold work that exists nowhere else is refused:
uncommitted changes, a branch other than the default, or commits not pushed.
--force prunes it anyway, and that work is lost. Prune prints the
$A_NOVEL_STACKS entry to drop from your shell profile.`,
		Example: `  a-novel core stacks list
  a-novel core stacks prune agent-7b46
  a-novel core stacks prune agent-7b46 --dry-run
  a-novel core stacks prune --all -y`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return pruneStacks(cmd, args, all, opts)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "prune every registered stack except the default")
	cmd.Flags().BoolVar(&opts.force, "force", false, "prune even when a checkout holds unsaved or unpushed work")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "skip the confirmation prompt")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "report what would be reclaimed, change nothing")
	cmd.Flags().BoolVar(&opts.purgeBackups, "purge-backups", false,
		"also delete the stack's volume backups (otherwise they outlive it)")
	return cmd
}

// pruneStacks prunes the stack args names, or every stack but the default
// with all. A stack that fails is reported and the others still run.
func pruneStacks(cmd *cobra.Command, args []string, all bool, opts pruneOpts) error {
	ctx, out := cmd.Context(), cmd.OutOrStdout()
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
		if err := pruneOne(ctx, c, cmd, st, opts); err != nil {
			_, _ = fmt.Fprintf(out, "  ✗ %s: %v\n", st.GetName(), err)
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d stack(s) failed to prune", failed)
	}
	return nil
}

// pruneOpts holds the flags that shape each stack's prune.
type pruneOpts struct {
	force        bool
	yes          bool
	dryRun       bool
	purgeBackups bool
}

// resolvePruneTargets maps the command's arguments onto the stacks the daemon
// knows about. --all silently filters the default stack out, since a sweep is
// meant to run without reading the list first and "everything disposable" is
// the useful reading of it.
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
			if isLive(t.GetPhase()) {
				h.liveTargets = append(h.liveTargets, t.GetId())
			}
		}
		for _, in := range svc.GetInfra() {
			if isLive(in.GetPhase()) {
				h.liveInfra++
			}
		}
		for _, v := range svc.GetVolumes() {
			h.volumes = append(h.volumes, v.GetName())
		}
	}
	return h
}

// isLive reports whether an entity in phase has, or is about to have, a
// process or container behind it.
func isLive(phase anovelv1.Phase) bool {
	return phase != anovelv1.Phase_PHASE_TERMINATED && phase != anovelv1.Phase_PHASE_UNSPECIFIED
}

// pruneOne prints what a stack holds, then reclaims it unless the run is a dry
// run, the stack holds unsaved work, or the operator declines.
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
	printPrunePlan(out, st, held, blockers, opts)

	if opts.dryRun {
		_, _ = fmt.Fprintf(out, "  %s\n", dryRunVerdict(blockers, opts.force))
		return nil
	}
	if len(blockers) > 0 && !opts.force {
		return fmt.Errorf("holds work that exists nowhere else (%s) — push it, or --force to discard",
			strings.Join(blockers, "; "))
	}
	if !opts.yes && !confirm(cmd, fmt.Sprintf("  Prune %s and everything it holds?", name)) {
		_, _ = fmt.Fprintln(out, "  skipped")
		return nil
	}
	return reclaimStack(ctx, c, out, st, held, opts.purgeBackups)
}

// printPrunePlan prints what pruning st reclaims, and the work it would lose.
func printPrunePlan(out io.Writer, st *anovelv1.Stack, held stackHoldings, blockers []string, opts pruneOpts) {
	_, _ = fmt.Fprintf(out, "▸ %s (%s)\n", st.GetName(), st.GetPath())
	_, _ = fmt.Fprintf(out, "  %d target(s) up, %d infra container(s), %d volume(s), %d service(s)\n",
		len(held.liveTargets), held.liveInfra, len(held.volumes), len(held.services))
	if opts.purgeBackups {
		_, _ = fmt.Fprintf(out, "  purging backups at %s\n", stackBackupDir(st.GetName()))
	}
	verb := "holds"
	if opts.force {
		verb = "discarding"
	}
	for _, b := range blockers {
		_, _ = fmt.Fprintf(out, "  ! %s: %s\n", verb, b)
	}
}

// reclaimStack kills what st runs, clears its volumes and removes its checkout,
// in that order, so nothing is left holding a port or a mount.
func reclaimStack(
	ctx context.Context, c *rpc.Client, out io.Writer, st *anovelv1.Stack, held stackHoldings, purgeBackups bool,
) error {
	name, root := st.GetName(), st.GetPath()
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
	if purgeBackups {
		// Opt-in, because ClearVolume just took a backup on the way past: the
		// default keeps the one artifact that can undo this command, and this
		// flag is how the operator says the data really was disposable.
		dir := stackBackupDir(name)
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("purge backups %s: %w", dir, err)
		}
		_, _ = fmt.Fprintf(out, "  ✓ purged backups %s\n", dir)
	}
	_, _ = fmt.Fprintf(out, "  ✓ reclaimed %s\n", root)
	_, _ = fmt.Fprintf(out, "  ! drop from %s: %s:%s\n", stacks.EnvVar, name, root)
	return nil
}

// stackBackupDir is where every volume backup for a stack lives, the parent of
// the per-service, per-volume directories `volumes.BackupDir` writes into.
func stackBackupDir(stack string) string {
	return filepath.Join(paths.BackupsRoot(), stack)
}

// reportUnmanaged names the stacks $A_NOVEL_STACKS registers that the daemon is
// not managing. The daemon skips a stack whose files are gone and starts
// anyway, so a registration can outlive the stack it points at. This names
// those entries.
//
// It reads the caller's own environment, so a shell whose $A_NOVEL_STACKS
// differs from the one the daemon started with is reported as unmanaged too:
// either way, this shell's registrations and the daemon's view disagree.
func reportUnmanaged(out io.Writer, managed map[string]bool) error {
	registered, err := stacks.ParseEnv()
	if err != nil {
		// A malformed $A_NOVEL_STACKS is the daemon's problem to report at
		// start; the listing above is still valid without this footnote.
		return nil //nolint:nilerr
	}
	for _, s := range registered {
		if managed[s.Name] {
			continue
		}
		reason := "not managed — restart the daemon to pick it up"
		if info, statErr := os.Stat(s.Path); statErr != nil || !info.IsDir() {
			reason = "files are gone — drop it from " + stacks.EnvVar
		}
		_, _ = fmt.Fprintf(out, "! %-20s %-40s %s\n", s.Name, s.Path, reason)
	}
	return nil
}

// dryRunVerdict is the line a dry run prints in place of acting. A blocked
// stack gets the reason the real run would refuse.
func dryRunVerdict(blockers []string, force bool) string {
	if len(blockers) > 0 && !force {
		return "(dry run — would refuse: unsaved work above; --force overrides)"
	}
	return "(dry run — nothing changed)"
}

// pruneBlockers lists the reasons a stack's checkouts should outlive the prune,
// one per checkout: work in progress (see ongoingWork), or unpushed commits,
// since a clean checkout on its default branch can still hold the only copy of
// a commit.
func pruneBlockers(root string) []string {
	checkouts, err := workspaceCheckouts(root)
	if err != nil {
		// An unreadable root has no recoverable work in it by definition;
		// removing it is the whole point of the command.
		return nil
	}
	var blockers []string
	for _, c := range checkouts {
		if reason := ongoingWork(c.dir); reason != "" {
			blockers = append(blockers, c.Name+": "+reason)
		} else if n := unpushedCommits(c.dir); n > 0 {
			blockers = append(blockers, fmt.Sprintf("%s: %d unpushed commit(s)", c.Name, n))
		}
	}
	return blockers
}
