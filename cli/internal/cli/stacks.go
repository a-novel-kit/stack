package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	"github.com/a-novel-kit/stack/cli/internal/setup"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// killGrace is how long a target gets to exit on SIGTERM before the daemon
// sends SIGKILL. It is the `run kill --timeout` default and the grace a prune
// gives each target.
const killGrace = 10 * time.Second

// newCoreStacksCmd allocates, lists and prunes workspace stacks. A stack holds
// a checkout, containers that hold host ports, and podman volumes. Deleting the
// root frees only the checkout. The daemon releases the containers and volumes,
// so pruning is a command of its own.
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

// defaultStackRoot is where an extra stack lands when the caller names no
// root. It sits under os.TempDir(), which the OS empties, so an abandoned
// stack frees its disk in time. The default stack is the workspace and never
// lives there.
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
	root, err := absRoot(root)
	if err != nil {
		return err
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
and volumes it holds. A '*' marks the default stack, which is never pruned.
Each problem discovery found in a stack follows its row, marked '!'. A
registration the daemon does not manage follows, with the reason.`,
		Example: `  a-novel core stacks list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listStacks(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

// listStacks prints every registered stack with what it holds and its
// discovery problems, then the registrations the daemon does not manage.
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
		printDiscoveryErrors(out, "    ", st)
		managed[st.GetName()] = true
	}
	reportUnmanaged(out, managed)
	return nil
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
	liveTargets []string // fully-qualified IDs of the targets isLive accepts
	liveInfra   int
	volumes     []string
}

// inspectStack snapshots what the stack name holds, from one ListServices call.
func inspectStack(ctx context.Context, c *rpc.Client, name string) (stackHoldings, error) {
	resp, err := c.ListServices(ctx, name)
	if err != nil {
		return stackHoldings{}, err
	}
	return holdingsOf(resp.GetServices()), nil
}

// holdingsOf reduces a stack's services to what teardown acts on. A target or
// infra container is live from PENDING until TERMINATED. A PENDING or STARTING
// entry has, or soon has, a process or container holding a port.
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
	const force, noBackup = true, false

	for _, id := range held.liveTargets {
		if _, err := c.KillTarget(ctx, id, killGrace); err != nil {
			return fmt.Errorf("kill target %s: %w", id, err)
		}
	}
	for _, svc := range held.services {
		// force stops what the infra still holds open. The targets above are
		// already down, so it orphans nothing.
		if _, err := c.KillInfra(ctx, name, svc, force); err != nil {
			return fmt.Errorf("kill infra %s/%s: %w", name, svc, err)
		}
		// A prune assumes the stack's data is disposable, and that is sometimes
		// wrong, so every volume is backed up first.
		if _, err := c.ClearVolume(ctx, name, svc, noBackup, force); err != nil {
			return fmt.Errorf("clear volumes %s/%s: %w", name, svc, err)
		}
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove %s: %w", root, err)
	}
	if purgeBackups {
		// ClearVolume just backed up every volume. Only this flag deletes those
		// backups, the one way to undo a prune.
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

// reportUnmanaged prints each $A_NOVEL_STACKS registration the daemon does not
// manage, with its fix. The daemon skips a stack whose files are gone, so a
// registration can outlive its stack. The registrations come from this shell,
// so a shell whose $A_NOVEL_STACKS differs from the daemon's reports the
// difference too.
func reportUnmanaged(out io.Writer, managed map[string]bool) {
	registered, err := stacks.ParseEnv()
	if err != nil {
		// The daemon reports a malformed $A_NOVEL_STACKS at start. The listing
		// above stays valid without this footnote.
		return
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
}

// printDiscoveryErrors prints each discovery problem of st on its own line,
// after indent. A problem spanning several lines, such as a YAML error, is
// folded onto one.
func printDiscoveryErrors(out io.Writer, indent string, st *anovelv1.Stack) {
	for _, problem := range st.GetDiscoveryErrors() {
		_, _ = fmt.Fprintf(out, "%s! %s\n", indent, strings.Join(strings.Fields(problem), " "))
	}
}

// dryRunVerdict is the line a dry run prints in place of acting. A blocked
// stack gets the reason the real run would refuse.
func dryRunVerdict(blockers []string, force bool) string {
	if len(blockers) > 0 && !force {
		return "(dry run — would refuse: unsaved work above; --force overrides)"
	}
	return "(dry run — nothing changed)"
}

// pruneBlockers lists the reasons a stack's checkouts should outlive the
// prune, at most one per checkout, in checkout order. It checks every checkout
// at once: each check runs several local git commands.
func pruneBlockers(root string) []string {
	checkouts, err := workspaceCheckouts(root)
	if err != nil {
		// An unreadable root has no recoverable work in it by definition;
		// removing it is the whole point of the command.
		return nil
	}

	reasons := make([]string, len(checkouts))
	var wg sync.WaitGroup
	for i, c := range checkouts {
		wg.Go(func() { reasons[i] = checkoutBlocker(c) })
	}
	wg.Wait()

	return slices.DeleteFunc(reasons, func(r string) bool { return r == "" })
}

// checkoutBlocker names the work c holds that exists nowhere else, or returns
// "". A clean checkout on its default branch can still hold the only copy of a
// commit, so unpushed commits block too.
func checkoutBlocker(c checkout) string {
	if reason := ongoingWork(c.dir); reason != "" {
		return c.Name + ": " + reason
	}
	if n := unpushedCommits(c.dir); n > 0 {
		return fmt.Sprintf("%s: %d unpushed commit(s)", c.Name, n)
	}
	return ""
}
