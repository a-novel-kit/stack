package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
	"github.com/a-novel-kit/stack/cli/internal/tui"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

const (
	// modeGoExec is the canonical token for go-exec mode across flag
	// defaults, palette args and user-visible labels.
	modeGoExec = "go-exec"
	// modeContainer is the canonical token for container mode on the same
	// surfaces.
	modeContainer = "container"
	// kindTarget selects targets in the --kind filter.
	kindTarget = "target"
	// kindInfra selects infra containers in the --kind filter, and is the
	// sentinel segment the entity-ID parser looks for.
	kindInfra      = "infra"
	cmdNameService = "service"
)

// stackScope holds the flag values shared across the daemon-backed commands.
type stackScope struct {
	stack     string
	allStacks bool
}

// bind adds the --stack flag to cmd.
func (s *stackScope) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&s.stack, stackLabel, "", "stack name (default: first in $A_NOVEL_STACKS)")
}

// bindAll adds --stack and --all-stacks to cmd.
func (s *stackScope) bindAll(cmd *cobra.Command) {
	s.bind(cmd)
	cmd.Flags().BoolVar(&s.allStacks, "all-stacks", false, "operate across every registered stack")
}

// selector is the stack argument of a listing call: "*" with --all-stacks.
func (s *stackScope) selector() string {
	if s.allStacks {
		return "*"
	}
	return s.stack
}

// =============================================================================
// Stacks
// =============================================================================

func newStacksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stacks",
		Short: "List the registered stacks",
		Long: `Print the stacks the daemon manages, one per row: path, a '*' on the
default, and what each holds. Stacks come from $A_NOVEL_STACKS, parsed at
daemon start. Same as 'a-novel core stacks list'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listStacks(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

// =============================================================================
// Discovery
// =============================================================================

// psOpts holds the flags of `run ps`.
type psOpts struct {
	scope   stackScope
	service string
	kind    string
	jsonOut bool
	watch   bool
}

func newPsCmd() *cobra.Command {
	var o psOpts
	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List services with their targets and infra containers",
		Long: `Show every service of the stack with its targets and infra containers, one
row each: the kind (1shot, longr or infra), the name, the state, and the
fully-qualified ID that kill, restart and logs take.

--watch prints the table again on every state change. --json prints one JSON
object per service, one per line, and with --watch one more per state change.`,
		Example: `  a-novel run ps
  a-novel run ps --stack=branch-foo
  a-novel run ps --service=service-json-keys --json
  a-novel run ps --kind=infra
  a-novel run ps --watch`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listServices(cmd, o)
		},
	}
	o.scope.bindAll(cmd)
	cmd.Flags().StringVar(&o.service, "service", "", "filter to one service")
	cmd.Flags().StringVar(&o.kind, "kind", "", "filter by entity kind (target|infra|all; default all)")
	cmd.Flags().BoolVar(&o.jsonOut, "json", false, "emit JSON (machine-readable)")
	cmd.Flags().BoolVar(&o.watch, "watch", false, "stream state changes instead of a snapshot")
	return cmd
}

// listServices prints the services ps selects and, with --watch, keeps
// printing on every state change: the table again, or the event as JSON.
func listServices(cmd *cobra.Command, o psOpts) error {
	if o.kind != "" && o.kind != "all" && o.kind != kindTarget && o.kind != kindInfra {
		return usageError(fmt.Errorf("--kind=%q: must be 'target', 'infra', or 'all'", o.kind))
	}
	ctx, out, c := cmd.Context(), cmd.OutOrStdout(), rpc.New("")
	resp, err := c.ListServices(ctx, o.scope.selector())
	if err != nil {
		return err
	}
	renderPs(out, filterServices(resp.GetServices(), o.service, o.kind), o.jsonOut)
	if !o.watch {
		return nil
	}
	handle := func(ev *anovelv1.StateEvent) { repaintPs(ctx, c, out, o, ev) }
	if o.jsonOut {
		handle = func(ev *anovelv1.StateEvent) { encodeEvent(out, ev) }
	}
	return watchEvents(ctx, c, o.scope.selector(), o.service, "", handle)
}

// repaintPs redraws the ps table in place after ev, so it reads like `top`
// and the frames stay out of the scrollback. One ListServices call per event
// keeps the table current.
func repaintPs(ctx context.Context, c *rpc.Client, out io.Writer, o psOpts, ev *anovelv1.StateEvent) {
	resp, err := c.ListServices(ctx, o.scope.selector())
	if err != nil {
		_, _ = fmt.Fprintf(out, "watch: refresh failed: %v\n", err)
		return
	}
	// Move the cursor home and clear the screen; on a pipe the escape
	// sequences are harmless filler.
	_, _ = fmt.Fprint(out, "\x1b[H\x1b[2J")
	renderPs(out, filterServices(resp.GetServices(), o.service, o.kind), false)
	_, _ = fmt.Fprintf(out, "\n%s  %s\n", ev.GetTs().AsTime().Format("15:04:05"), ev.GetDescription())
}

// filterServices keeps the named service (all when empty) and drops the entity
// kind --kind leaves out.
func filterServices(services []*anovelv1.Service, service, kind string) []*anovelv1.Service {
	if service != "" {
		services = slices.DeleteFunc(services, func(s *anovelv1.Service) bool { return s.GetName() != service })
	}
	for _, s := range services {
		switch kind {
		case kindTarget:
			s.Infra = nil
		case kindInfra:
			s.Targets = nil
		}
	}
	return services
}

// renderPs prints services as a human-readable table or as JSON lines.
func renderPs(w io.Writer, services []*anovelv1.Service, asJSON bool) {
	if asJSON {
		enc := json.NewEncoder(w)
		for _, s := range services {
			_ = enc.Encode(s)
		}
		return
	}
	if len(services) == 0 {
		_, _ = fmt.Fprintln(w, "(no services)")
		return
	}
	for i, s := range services {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
		}
		_, _ = fmt.Fprintf(w, "%s  (stack %s)\n", s.GetName(), s.GetStack())
		// Targets first, then infra. The kind column and the trailing
		// fully-qualified ID keep the two distinguishable at a glance, and
		// the ID pastes straight into kill/restart/logs.
		for _, t := range s.GetTargets() {
			kindStr := targetKindShort(t.GetKind())
			id := s.GetStack() + "/" + s.GetName() + "/" + t.GetName()
			_, _ = fmt.Fprintf(w, "  %-6s %-30s  %-12s  %s\n",
				kindStr, t.GetName(), targetStatusLabel(t), id)
		}
		for _, in := range s.GetInfra() {
			label := phaseLabel(in.GetPhase())
			switch in.GetHealth() {
			case anovelv1.Health_HEALTH_HEALTHY:
				label += " healthy"
			case anovelv1.Health_HEALTH_UNHEALTHY:
				label += " unhealthy"
			case anovelv1.Health_HEALTH_STARTING:
				label += " starting"
			}
			id := s.GetStack() + "/" + s.GetName() + "/infra/" + in.GetName()
			_, _ = fmt.Fprintf(w, "  %-6s %-30s  %-10s  %s\n",
				kindInfra, in.GetName(), label, id)
		}
	}
}

// targetStatusLabel returns the user-facing status word for a target, folding
// the terminal state into the label so a finished one-shot reads as "ok" or
// "failed":
//
//	long-runner running       → "running"
//	long-runner clean-exit    → "stopped"
//	long-runner errored       → "crashed"
//	one-shot running          → "running"
//	one-shot exit success     → "ok"
//	one-shot exit error       → "failed"
//	never started             → "idle"
func targetStatusLabel(t *anovelv1.Target) string {
	if t.GetPhase() == anovelv1.Phase_PHASE_TERMINATED {
		isOneShot := t.GetKind() == anovelv1.TargetKind_TARGET_KIND_ONE_SHOT
		switch t.GetExitReason() {
		case anovelv1.ExitReason_EXIT_REASON_SUCCESS:
			if isOneShot {
				return "ok"
			}
			return "stopped"
		case anovelv1.ExitReason_EXIT_REASON_KILLED:
			return "stopped"
		case anovelv1.ExitReason_EXIT_REASON_CRASHED, anovelv1.ExitReason_EXIT_REASON_ERROR:
			if isOneShot {
				return "failed"
			}
			return "crashed"
		}
	}
	return phaseLabel(t.GetPhase())
}

// phaseLabel renders a Phase enum as a short human-readable token; an
// unspecified phase reads as "idle".
func phaseLabel(p anovelv1.Phase) string {
	switch p {
	case anovelv1.Phase_PHASE_PENDING:
		return "pending"
	case anovelv1.Phase_PHASE_STARTING:
		return "starting"
	case anovelv1.Phase_PHASE_RUNNING:
		return "running"
	case anovelv1.Phase_PHASE_STOPPING:
		return "stopping"
	case anovelv1.Phase_PHASE_TERMINATED:
		return "terminated"
	default:
		return "idle"
	}
}

func targetKindShort(k anovelv1.TargetKind) string {
	switch k {
	case anovelv1.TargetKind_TARGET_KIND_ONE_SHOT:
		return "1shot"
	case anovelv1.TargetKind_TARGET_KIND_LONG_RUNNER:
		return "longr"
	default:
		return "??"
	}
}

func newTopologyCmd() *cobra.Command {
	var ss stackScope
	var service string
	cmd := &cobra.Command{
		Use:   "topology",
		Short: "Show the dependency graph as ASCII",
		Long: `Print each service's dependency graph as a text tree, read from its compose
depends_on entries: the infra first, then the one-shots, then the
long-runners, each with its kind and what it depends on.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return printTopology(cmd, ss.stack, service)
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&service, "service", "", "limit to one service")
	return cmd
}

// printTopology prints the dependency graph of one service, or of every
// service of the stack when service is empty.
func printTopology(cmd *cobra.Command, stack, service string) error {
	resp, err := rpc.New("").GetTopology(cmd.Context(), stack, service)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprint(cmd.OutOrStdout(), resp.GetRendered())
	return nil
}

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdNameService,
		Short: "Operate on a service's infrastructure and overall state",
	}
	cmd.AddCommand(newServiceStatusCmd())
	cmd.AddCommand(newServiceInfraCmd())
	return cmd
}

func newServiceStatusCmd() *cobra.Command {
	var ss stackScope
	cmd := &cobra.Command{
		Use:   "status <service>",
		Short: "Show one service's infrastructure and targets",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return printService(cmd, ss.stack, args[0])
		},
	}
	ss.bind(cmd)
	return cmd
}

// printService prints one service as `run ps` does.
func printService(cmd *cobra.Command, stack, service string) error {
	resp, err := rpc.New("").DescribeService(cmd.Context(), stack, service)
	if err != nil {
		return err
	}
	renderPs(cmd.OutOrStdout(), []*anovelv1.Service{resp.GetService()}, false)
	return nil
}

func newServiceInfraCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   kindInfra,
		Short: "Manage a service's infrastructure (containers without Go counterparts)",
		Long: `Start or stop a service's infrastructure: the compose services with no cmd/
target, such as its database. 'infra start' also runs every one-shot target of
the service, such as migrations, so the database is ready to query without
starting a server.`,
	}
	cmd.AddCommand(newServiceInfraStartCmd())
	cmd.AddCommand(newServiceInfraKillCmd())
	return cmd
}

func newServiceInfraStartCmd() *cobra.Command {
	var ss stackScope
	var oneShotsMode string
	cmd := &cobra.Command{
		Use:   "start <service>",
		Short: "Bring up a service's infrastructure and run its one-shots",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return startInfra(cmd, ss.stack, args[0], oneShotsMode)
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&oneShotsMode, "one-shots", modeGoExec,
		"mode for one-shot targets auto-run with infra (go-exec | container)")
	return cmd
}

// startInfra brings up a service's infra and runs its one-shots in the mode
// the --one-shots token names.
func startInfra(cmd *cobra.Command, stack, service, oneShotsMode string) error {
	mode := parseMode(oneShotsMode, anovelv1.Mode_MODE_GO_EXEC)
	resp, err := rpc.New("").StartInfra(cmd.Context(), stack, service, mode)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "infra-up for %s (one-shots ran in %s mode)\n",
		resp.GetService().GetName(), modeLabel(mode))
	return nil
}

func newServiceInfraKillCmd() *cobra.Command {
	var ss stackScope
	var force bool
	cmd := &cobra.Command{
		Use:   "kill <service>",
		Short: "Tear down a service's infrastructure",
		Long: `Refuses while a target of the service runs. Stop it first with
'a-novel run kill', or pass --force to stop the targets with the infra.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return killInfra(cmd, ss.stack, args[0], force)
		},
	}
	ss.bind(cmd)
	cmd.Flags().BoolVarP(&force, "force", "f", false, "cascade-kill all running targets first")
	return cmd
}

// killInfra tears down a service's infra, stopping its targets first with
// force.
func killInfra(cmd *cobra.Command, stack, service string, force bool) error {
	resp, err := rpc.New("").KillInfra(cmd.Context(), stack, service, force)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "infra-down for %s\n", resp.GetService().GetName())
	return nil
}

// =============================================================================
// Target lifecycle
// =============================================================================

func newStartCmd() *cobra.Command {
	var ss stackScope
	var mode string
	cmd := &cobra.Command{
		Use:   "start <target>",
		Short: "Start a target in go-exec (default) or container mode",
		Long: `Start a target, named '<stack>/<service>/<name>', or '<service>/<name>' in the
--stack or default stack. Its dependencies come up first: the infra waits for
health, and one-shots such as migrations run to success.

The target runs from source (go-exec), or from its image with --mode=container.
A target running in the other mode is refused, and one running in the same
mode is left as it is.`,
		Example: `  a-novel run start default/service-json-keys/rest
  a-novel run start service-json-keys/rest                     # default stack inferred
  a-novel run start service-json-keys/rest --mode=container
  a-novel run start service-template/rest --stack=branch-foo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return startTarget(cmd, parseEntityID(args[0], ss.stack), mode)
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&mode, "mode", modeGoExec, "execution mode (go-exec | container)")
	return cmd
}

// startTarget starts the target ref names, in the mode the --mode token names.
func startTarget(cmd *cobra.Command, ref entityRef, mode string) error {
	resp, err := rpc.New("").StartTarget(cmd.Context(), ref.ID, parseMode(mode, anovelv1.Mode_MODE_GO_EXEC))
	if err != nil {
		return err
	}
	t := resp.GetTarget()
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "started %s (mode=%s, phase=%s, %s)\n",
		t.GetId(), modeLabel(t.GetMode()), phaseLabel(t.GetPhase()), runtimeIdentLabel(t))
	return nil
}

// runtimeIdentLabel renders the right-side identifier for `start`/`restart`
// output: `pid=<n>` in go-exec mode, `container=<short>` (12-char hex) in
// container mode, which has no meaningful pid.
func runtimeIdentLabel(t *anovelv1.Target) string {
	switch t.GetMode() {
	case anovelv1.Mode_MODE_GO_EXEC:
		return fmt.Sprintf("pid=%d", t.GetPid())
	case anovelv1.Mode_MODE_CONTAINER:
		cid := t.GetContainerId()
		if len(cid) > 12 {
			cid = cid[:12]
		}
		return "container=" + cid
	default:
		return "(no runtime ident)"
	}
}

func newKillCmd() *cobra.Command {
	var ss stackScope
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "kill <target-or-infra>",
		Short: "Stop a running target or infra container",
		Long: `Stop a target, named as 'run start' takes it, or an infra container, named
'<service>/infra/<name>'. A target gets SIGTERM, then SIGKILL once --timeout
passes, and an infra container gets 'podman stop'. Stopping what already
stopped succeeds.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return killEntity(cmd, parseEntityID(args[0], ss.stack), timeout)
		},
	}
	ss.bind(cmd)
	cmd.Flags().DurationVar(&timeout, "timeout", killGrace, "SIGTERM-to-SIGKILL grace")
	return cmd
}

// killEntity stops the target or the infra container ref names. A target gets
// timeout between SIGTERM and SIGKILL.
func killEntity(cmd *cobra.Command, ref entityRef, timeout time.Duration) error {
	ctx, out, c := cmd.Context(), cmd.OutOrStdout(), rpc.New("")
	if ref.IsInfra {
		if _, err := c.KillInfraContainer(ctx, ref.Stack, ref.Service, ref.Infra); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "stopped infra %s\n", ref.ID)
		return nil
	}
	resp, err := c.KillTarget(ctx, ref.ID, timeout)
	if err != nil {
		return err
	}
	t := resp.GetTarget()
	if t == nil || t.GetId() == "" {
		_, _ = fmt.Fprintf(out, "%s: not running (no-op)\n", ref.ID)
		return nil
	}
	_, _ = fmt.Fprintf(out, "killed %s (phase=%s, exit=%s)\n",
		t.GetId(), phaseLabel(t.GetPhase()), exitLabel(t.GetExitReason()))
	return nil
}

func newRestartCmd() *cobra.Command {
	var ss stackScope
	var mode string
	cmd := &cobra.Command{
		Use:   "restart <target-or-infra>",
		Short: "Kill then start (optionally swapping mode for targets)",
		Long: `Stop then start a target in one step, in the mode --mode names, or its current
one. An infra container gets 'podman restart', which keeps the container and
its data.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return restartEntity(cmd, parseEntityID(args[0], ss.stack), mode)
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&mode, "mode", "", "switch to this mode (go-exec | container); empty keeps current")
	return cmd
}

// restartEntity restarts the target ref names, in the mode the --mode token
// names or its current one, or restarts the infra container in place.
func restartEntity(cmd *cobra.Command, ref entityRef, mode string) error {
	ctx, out, c := cmd.Context(), cmd.OutOrStdout(), rpc.New("")
	if ref.IsInfra {
		if _, err := c.RestartInfraContainer(ctx, ref.Stack, ref.Service, ref.Infra); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "restarted infra %s\n", ref.ID)
		return nil
	}
	resp, err := c.RestartTarget(ctx, ref.ID, parseMode(mode, anovelv1.Mode_MODE_UNSPECIFIED))
	if err != nil {
		return err
	}
	t := resp.GetTarget()
	_, _ = fmt.Fprintf(out, "restarted %s (mode=%s, phase=%s, %s)\n",
		t.GetId(), modeLabel(t.GetMode()), phaseLabel(t.GetPhase()), runtimeIdentLabel(t))
	return nil
}

// entityRef is a parsed CLI argument identifying either a target or an infra
// container, told apart by the literal "infra" segment (see parseEntityID).
// kill, restart and logs dispatch to the right RPC from it, since a slash
// count alone cannot separate a 3-segment target ID from a 3-segment infra
// shorthand.
type entityRef struct {
	IsInfra bool
	Stack   string
	Service string
	Target  string // set when IsInfra is false
	Infra   string // set when IsInfra is true
	// ID is the canonical full ID: "<stack>/<service>/<target>" for a
	// target, "<stack>/<service>/infra/<name>" for an infra container.
	ID string
}

// parseEntityID classifies a CLI argument as a target or infra
// reference and returns its canonical form. Recognized shapes:
//
//	<service>/<target>                      → target shorthand
//	<stack>/<service>/<target>              → target canonical
//	<service>/infra/<name>                  → infra shorthand
//	<stack>/<service>/infra/<name>          → infra canonical
//
// Only an "infra" in the second-to-last segment marks an infra reference;
// anywhere else the word is an ordinary target name. A shorthand resolves
// against defaultStack, or the default stack, the first $A_NOVEL_STACKS entry.
func parseEntityID(arg, defaultStack string) entityRef {
	if defaultStack == "" {
		defaultStack = stacks.DefaultName
		if stack, err := stacks.Default(); err == nil {
			defaultStack = stack.Name
		}
	}
	parts := strings.Split(arg, "/")
	// Infra: 3 segments (svc/infra/name) or 4 (stack/svc/infra/name).
	if len(parts) >= 3 && parts[len(parts)-2] == kindInfra {
		ref := entityRef{IsInfra: true, Infra: parts[len(parts)-1]}
		if len(parts) == 4 {
			ref.Stack = parts[0]
			ref.Service = parts[1]
		} else {
			ref.Stack = defaultStack
			ref.Service = parts[0]
		}
		ref.ID = ref.Stack + "/" + ref.Service + "/infra/" + ref.Infra
		return ref
	}
	// Target.
	ref := entityRef{}
	switch len(parts) {
	case 3:
		ref.Stack = parts[0]
		ref.Service = parts[1]
		ref.Target = parts[2]
	case 2:
		ref.Stack = defaultStack
		ref.Service = parts[0]
		ref.Target = parts[1]
	default:
		// Unrecognized: pass it through as a bare name and let the daemon
		// 404 with a message pointing at the right shape.
		ref.Stack = defaultStack
		ref.Target = arg
	}
	ref.ID = ref.Stack + "/" + ref.Service + "/" + ref.Target
	return ref
}

// parseMode reads a --mode token, an empty or unknown one yielding fallback.
func parseMode(token string, fallback anovelv1.Mode) anovelv1.Mode {
	switch token {
	case modeGoExec:
		return anovelv1.Mode_MODE_GO_EXEC
	case modeContainer:
		return anovelv1.Mode_MODE_CONTAINER
	default:
		return fallback
	}
}

// modeLabel renders a Mode enum as its canonical CLI token.
func modeLabel(m anovelv1.Mode) string {
	switch m {
	case anovelv1.Mode_MODE_GO_EXEC:
		return modeGoExec
	case anovelv1.Mode_MODE_CONTAINER:
		return modeContainer
	default:
		return "?"
	}
}

// exitLabel renders an ExitReason enum as a short human-readable token.
func exitLabel(r anovelv1.ExitReason) string {
	switch r {
	case anovelv1.ExitReason_EXIT_REASON_SUCCESS:
		return "success"
	case anovelv1.ExitReason_EXIT_REASON_ERROR:
		return "error"
	case anovelv1.ExitReason_EXIT_REASON_KILLED:
		return "killed"
	case anovelv1.ExitReason_EXIT_REASON_CRASHED:
		return "crashed"
	default:
		return "-"
	}
}

// =============================================================================
// Logs
// =============================================================================

// logsOpts holds the flags of `run logs`.
type logsOpts struct {
	scope    stackScope
	follow   bool
	tail     int
	since    time.Duration
	stream   string
	previous bool
	runID    string
}

func newLogsCmd() *cobra.Command {
	var o logsOpts
	cmd := &cobra.Command{
		Use:   "logs <target-or-infra>",
		Short: "Print or stream logs for a target or infra container",
		Long: `Print the log of a target's current run, or of an archived run with
--previous or --run-id. --follow keeps printing new lines as they arrive.

An infra container's log, named '<service>/infra/<name>', comes from podman,
with stdout and stderr merged. It keeps no archived runs, so --previous and
--run-id refuse it, and --stream does not filter it.`,
		Example: `  a-novel run logs service-json-keys/rest
  a-novel run logs service-json-keys/rest --follow
  a-novel run logs service-json-keys/infra/postgres-json-keys --follow
  a-novel run logs service-json-keys/rest --tail=100 --stream=stderr
  a-novel run logs service-json-keys/rest --previous`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return streamLogs(cmd, parseEntityID(args[0], o.scope.stack), o)
		},
	}
	o.scope.bind(cmd)
	cmd.Flags().BoolVarP(&o.follow, "follow", "f", false, "stream new lines as they arrive")
	cmd.Flags().IntVar(&o.tail, "tail", 0, "last N lines (0 = all)")
	cmd.Flags().DurationVar(&o.since, "since", 0, "only lines newer than this duration")
	cmd.Flags().StringVar(&o.stream, "stream", "", "filter to stdout or stderr only")
	cmd.Flags().BoolVar(&o.previous, "previous", false, "read the most recent archived run instead of the current")
	cmd.Flags().StringVar(&o.runID, "run-id", "", "read a specific archived run by timestamp")
	return cmd
}

// streamLogs prints the log of the run o selects, of the entity ref names.
func streamLogs(cmd *cobra.Command, ref entityRef, o logsOpts) error {
	// Podman owns infra log files, so infra entries have no run archives.
	if ref.IsInfra && (o.previous || o.runID != "") {
		return errors.New("--previous / --run-id are not supported for infra (podman owns its log file)")
	}
	ctx, c := cmd.Context(), rpc.New("")
	runID := o.runID
	if o.previous && runID == "" {
		runs, err := c.ListRuns(ctx, ref.ID)
		if err != nil {
			return err
		}
		if len(runs.GetRunIds()) == 0 {
			return fmt.Errorf("%s: no archived runs yet", ref.ID)
		}
		runID = runs.GetRunIds()[0]
	}
	stream, err := c.StreamLogs(ctx, ref.ID, runID, o.follow, logStreams[o.stream])
	if err != nil {
		return err
	}
	return printLogs(cmd.OutOrStdout(), stream, o.since, o.tail)
}

// logStreams maps a --stream value to the stream it keeps; any other value
// keeps both.
var logStreams = map[string]anovelv1.LogStream{
	"stdout": anovelv1.LogStream_LOG_STREAM_STDOUT,
	"stderr": anovelv1.LogStream_LOG_STREAM_STDERR,
}

// printLogs prints the lines of stream newer than since (when set), keeping
// only the last tail lines (when set). Both filters run client-side.
func printLogs(w io.Writer, stream iter.Seq2[*anovelv1.LogLine, error], since time.Duration, tail int) error {
	var cutoff time.Time
	if since > 0 {
		cutoff = time.Now().Add(-since)
	}
	var last []*anovelv1.LogLine
	for ln, err := range stream {
		if err != nil {
			return err
		}
		switch {
		case !cutoff.IsZero() && ln.GetTs().AsTime().Before(cutoff):
		case tail > 0:
			last = append(last, ln)
			if len(last) > tail {
				last = last[1:]
			}
		default:
			printLogLine(w, ln)
		}
	}
	for _, ln := range last {
		printLogLine(w, ln)
	}
	return nil
}

// printLogLine renders one log entry as a short timestamp, a stream tag and
// the raw line, whose ANSI escapes survive so the target's own colors show.
func printLogLine(w io.Writer, ln *anovelv1.LogLine) {
	tag := "out"
	if ln.GetStream() == anovelv1.LogStream_LOG_STREAM_STDERR {
		tag = "err"
	}
	_, _ = fmt.Fprintf(w, "%s %s %s\n",
		ln.GetTs().AsTime().Format("15:04:05.000"),
		tag,
		ln.GetLine())
}

// =============================================================================
// Environment
// =============================================================================

// envOpts holds the flags of `run env`.
type envOpts struct {
	scope  stackScope
	only   string
	format string
}

func newEnvCmd() *cobra.Command {
	var o envOpts
	cmd := &cobra.Command{
		Use:   "env [<service>]",
		Short: "Print a service's env vars in shell / JSON / dotenv form",
		Long: `Print the environment the daemon gives a service's targets: the compose
constants, the ports it allocates, and the URLs and DSNs it derives from them.
A port not allocated yet prints empty. Without <service>, print every service
of the stack, or of every stack with --all-stacks.

The default shell format is ready for eval; --format=json and --format=dotenv
suit scripts and .env files.`,
		Example: `  a-novel run env service-json-keys
  eval "$(a-novel run env service-json-keys)"
  curl "$REST_URL/ping"

  a-novel run env service-json-keys --only='REST*'
  a-novel run env --format=json
  a-novel run env --all-stacks`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return printEnv(cmd, strings.Join(args, ""), o)
		},
	}
	o.scope.bindAll(cmd)
	cmd.Flags().StringVar(&o.only, "only", "", "glob filter on key names (e.g. 'REST*')")
	cmd.Flags().StringVar(&o.format, "format", "shell", "output format (shell | json | dotenv)")
	return cmd
}

// printEnv prints the env of service, or of every service in scope when it is
// empty, keeping the keys --only matches.
func printEnv(cmd *cobra.Command, service string, o envOpts) error {
	resp, err := rpc.New("").GetEnv(cmd.Context(), o.scope.stack, service, o.scope.allStacks)
	if err != nil {
		return err
	}
	// --only filters client-side, so it applies to every format.
	unmatched := func(e *anovelv1.EnvEntry) bool { return !matchGlob(o.only, e.GetKey()) }
	renderEnv(cmd.OutOrStdout(), slices.DeleteFunc(resp.GetEntries(), unmatched), o.format)
	return nil
}

// renderEnv writes the env entries in the requested format. shell is
// eval-able; json is one-record-per-entry; dotenv is plain KEY=VALUE.
func renderEnv(w io.Writer, entries []*anovelv1.EnvEntry, format string) {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		for _, e := range entries {
			_ = enc.Encode(e)
		}
	case "dotenv":
		for _, e := range entries {
			_, _ = fmt.Fprintf(w, "%s=%s\n", e.GetKey(), e.GetValue())
		}
	default: // "shell"
		var lastSvc string
		for _, e := range entries {
			svc := e.GetService()
			if e.GetStack() != "" {
				svc = e.GetStack() + "/" + svc
			}
			if svc != lastSvc {
				if lastSvc != "" {
					_, _ = fmt.Fprintln(w)
				}
				_, _ = fmt.Fprintf(w, "# %s\n", svc)
				lastSvc = svc
			}
			// Single-quote the value, escaping embedded single quotes with
			// the standard '"'"' trick, so the line is safe to eval.
			esc := strings.ReplaceAll(e.GetValue(), "'", `'"'"'`)
			_, _ = fmt.Fprintf(w, "export %s='%s'\n", e.GetKey(), esc)
		}
	}
}

// =============================================================================
// Volumes
// =============================================================================

func newVolumeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "volume",
		Short: "Manage service volumes (backup, restore, clear, list)",
		Long: `A service's volumes hold its data, such as its database, for all its targets.
Backup, restore and clear refuse while a target or the infra of the service
runs; --force stops them first.

Backups are tar.zst archives under
$XDG_DATA_HOME/a-novel/backups/<stack>/<service>/<volume>/, the latest five
per volume. Stopping a service or the daemon keeps its volumes.`,
	}
	cmd.AddCommand(newVolumeListCmd())
	cmd.AddCommand(newVolumeBackupCmd())
	cmd.AddCommand(newVolumeRestoreCmd())
	cmd.AddCommand(newVolumeClearCmd())
	return cmd
}

func newVolumeListCmd() *cobra.Command {
	var ss stackScope
	cmd := &cobra.Command{
		Use:   "list <service>",
		Short: "List a service's volumes with sizes and backup counts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return listVolumes(cmd, ss.stack, args[0])
		},
	}
	ss.bind(cmd)
	return cmd
}

// listVolumes prints one row per volume of a service: its name, size and
// backup count.
func listVolumes(cmd *cobra.Command, stack, service string) error {
	resp, err := rpc.New("").ListVolumes(cmd.Context(), stack, service)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(resp.GetVolumes()) == 0 {
		_, _ = fmt.Fprintln(out, "(no volumes declared)")
		return nil
	}
	_, _ = fmt.Fprintf(out, "%-40s %12s %8s\n", "VOLUME", "SIZE", "BACKUPS")
	for _, v := range resp.GetVolumes() {
		_, _ = fmt.Fprintf(out, "%-40s %12s %8d\n", v.GetName(), humanBytes(v.GetSizeBytes()), v.GetBackupCount())
	}
	return nil
}

func newVolumeBackupCmd() *cobra.Command {
	var ss stackScope
	var tag string
	var force bool
	cmd := &cobra.Command{
		Use:   "backup <service>",
		Short: "Snapshot a service's volumes (refuses while the service runs)",
		Long: `Archive each of the service's volumes into the backups directory, and print
each archive's path. An archive of a volume in use can be corrupt, so the
command refuses while the service runs; --force stops it first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return backupVolumes(cmd, ss.stack, args[0], tag, force)
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&tag, "tag", "", "user-supplied label included in the archive filename")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "cascade-stop the service before backing up")
	return cmd
}

// backupVolumes archives a service's volumes, tag in the file names, and prints
// each archive's path.
func backupVolumes(cmd *cobra.Command, stack, service, tag string, force bool) error {
	resp, err := rpc.New("").BackupVolume(cmd.Context(), stack, service, tag, force)
	if err != nil {
		return err
	}
	for _, p := range resp.GetArchivePaths() {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), p)
	}
	return nil
}

func newVolumeRestoreCmd() *cobra.Command {
	var ss stackScope
	var from string
	var force bool
	cmd := &cobra.Command{
		Use:   "restore <service>",
		Short: "Restore volumes from a backup (latest by default)",
		Long: `Restore each of the service's volumes from its latest backup, or from the one
whose file name starts with --from, such as the timestamp 'volume backup'
printed. Every archive is checked before any volume changes. Refuses while
the service runs; --force stops it first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return restoreVolumes(cmd, ss.stack, args[0], from, force)
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&from, "from", "", "specific backup timestamp (default: latest)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "cascade-stop the service before restoring")
	return cmd
}

// restoreVolumes restores a service's volumes from the backup from selects,
// the latest when empty, and prints the volumes it restored.
func restoreVolumes(cmd *cobra.Command, stack, service, from string, force bool) error {
	resp, err := rpc.New("").RestoreVolume(cmd.Context(), stack, service, from, force)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "restored: %v\n", resp.GetRestoredVolumes())
	return nil
}

func newVolumeClearCmd() *cobra.Command {
	var ss stackScope
	var noBackup bool
	var force bool
	cmd := &cobra.Command{
		Use:   "clear <service>",
		Short: "Destroy a service's volumes (backs them up first)",
		Long: `Delete every volume of the service, after a backup that 'volume restore' takes
back. --no-backup skips the backup, and nothing can undo the deletion then.
Refuses while the service runs; --force stops it first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return clearVolumes(cmd, ss.stack, args[0], noBackup, force)
		},
	}
	ss.bind(cmd)
	cmd.Flags().BoolVar(&noBackup, "no-backup", false, "skip the auto-backup (irreversible)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "cascade-stop the service before clearing")
	return cmd
}

// clearVolumes empties a service's volumes, after a backup unless noBackup is
// set, and prints the volumes it cleared.
func clearVolumes(cmd *cobra.Command, stack, service string, noBackup, force bool) error {
	resp, err := rpc.New("").ClearVolume(cmd.Context(), stack, service, noBackup, force)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cleared: %v\n", resp.GetClearedVolumes())
	return nil
}

// matchGlob reports whether an env key matches the --only pattern, a
// filepath.Match glob such as "REST*" or "*_PORT"; an empty pattern matches
// every key. A malformed pattern falls back to substring matching, so a typo
// still does something reasonable.
func matchGlob(pattern, name string) bool {
	if pattern == "" {
		return true
	}
	if ok, err := filepath.Match(pattern, name); err == nil {
		return ok
	}
	return strings.Contains(name, pattern)
}

// humanBytes renders an int64 byte count in a short human form.
func humanBytes(n int64) string {
	const (
		k = 1024
		m = k * 1024
		g = m * 1024
	)
	switch {
	case n >= g:
		return fmt.Sprintf("%.1fG", float64(n)/g)
	case n >= m:
		return fmt.Sprintf("%.1fM", float64(n)/m)
	case n >= k:
		return fmt.Sprintf("%.1fK", float64(n)/k)
	case n > 0:
		return fmt.Sprintf("%dB", n)
	default:
		return "-"
	}
}

// =============================================================================
// Exec / debug
// =============================================================================

func newExecCmd() *cobra.Command {
	var ss stackScope
	cmd := &cobra.Command{
		Use:   "exec <target> -- <cmd> [args...]",
		Short: "Run a command alongside a target (container exec, or sibling process)",
		Long: `Run a command beside a target. In a running container target, the command
runs inside the container through 'podman exec'. Otherwise it runs on the host,
in the service's directory with the target's environment, so
'run exec <service>/migrations -- psql' opens psql on the service's database.
The command's exit status becomes the CLI's.`,
		Example: `  a-novel run exec service-json-keys/rest -- sh
  a-novel run exec service-json-keys/migrations -- psql
  a-novel run exec default/service-json-keys/rest -- curl -s localhost:8080`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Cobra strips "--", so args[0] is the target and args[1:] the command.
			return execBeside(cmd, parseEntityID(args[0], ss.stack), args[1:])
		},
	}
	ss.bind(cmd)
	return cmd
}

// execBeside runs argv beside the target ref names and prints its output,
// stderr lines on stderr. An infra container is refused: podman reaches it
// directly.
func execBeside(cmd *cobra.Command, ref entityRef, argv []string) error {
	if ref.IsInfra {
		return fmt.Errorf("exec: %s is an infra container; use 'podman exec %s_%s_%s_1 …' directly",
			ref.ID, ref.Stack, ref.Service, ref.Infra)
	}
	ctx := cmd.Context()
	stream, err := rpc.New("").Exec(ctx, ref.ID, argv)
	if err != nil {
		return err
	}
	// A pointer, so an unreported exit status stays distinct from a reported 0.
	var exitCode *int32
	for ev, err := range stream {
		if err != nil {
			if ctx.Err() == nil {
				return err
			}
			break
		}
		if ev.ExitCode != nil {
			exitCode = ev.ExitCode
			continue
		}
		w := cmd.OutOrStdout()
		if ev.GetStream() == anovelv1.LogStream_LOG_STREAM_STDERR {
			w = cmd.ErrOrStderr()
		}
		_, _ = fmt.Fprintln(w, ev.GetLine())
	}
	return execResult(exitCode)
}

// execResult turns the exec stream's terminal exit code into the CLI's own
// exit status. A nil exitCode means the daemon ended the stream without
// reporting one. That says nothing about the child, so it yields an error.
func execResult(exitCode *int32) error {
	if exitCode == nil {
		return errors.New("exec: daemon closed the stream without reporting an exit status " +
			"(a daemon older than this CLI does not send one — run 'a-novel core restart')")
	}

	if *exitCode != 0 {
		return &ExitError{Code: int(*exitCode)}
	}

	return nil
}

func newDebugCmd() *cobra.Command {
	var ss stackScope
	cmd := &cobra.Command{
		Use:   "debug <target>",
		Short: "Print the dlv attach command for a running go-exec target",
		Long: `Print the 'dlv attach' command for a running go-exec target, with its PID and a
headless port. Run it in your own terminal, then connect your IDE to that
port. Container targets are refused.`,
		Example: `  a-novel run debug service-json-keys/grpc
  a-novel run debug default/service-json-keys/grpc`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return printDebugHint(cmd, parseEntityID(args[0], ss.stack))
		},
	}
	ss.bind(cmd)
	return cmd
}

// printDebugHint prints the daemon's dlv attach command for the target ref
// names.
func printDebugHint(cmd *cobra.Command, ref entityRef) error {
	if ref.IsInfra {
		return fmt.Errorf("debug: %s is an infra container; dlv attach is only meaningful on go-exec targets", ref.ID)
	}
	resp, err := rpc.New("").Debug(cmd.Context(), ref.ID)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), resp.GetHint())
	return nil
}

// =============================================================================
// Watch
// =============================================================================

// watchOpts holds the flags of `run watch`.
type watchOpts struct {
	scope   stackScope
	service string
	target  string
	jsonOut bool
}

func newWatchCmd() *cobra.Command {
	var o watchOpts
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream the targets' phase changes",
		Long: `Print each phase change of the daemon's targets as it happens, one JSON object
per line, or one line of text with --json=false. --service and --target narrow
the stream.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return watchPhases(cmd, o)
		},
	}
	o.scope.bindAll(cmd)
	cmd.Flags().StringVar(&o.service, "service", "", "filter to one service")
	cmd.Flags().StringVar(&o.target, "target", "", "filter to one target")
	cmd.Flags().BoolVar(&o.jsonOut, "json", true, "print JSON lines (--json=false prints text)")
	return cmd
}

// watchPhases prints each state event of the targets the flags select, as a
// JSON line or as text, until the command's context ends.
func watchPhases(cmd *cobra.Command, o watchOpts) error {
	out := cmd.OutOrStdout()
	write := printEvent
	if o.jsonOut {
		write = encodeEvent
	}
	handle := func(ev *anovelv1.StateEvent) { write(out, ev) }
	return watchEvents(cmd.Context(), rpc.New(""), o.scope.selector(), o.service, o.target, handle)
}

// watchEvents streams the daemon's state events to handle until ctx ends.
func watchEvents(ctx context.Context, c *rpc.Client, stack, service, target string, handle func(*anovelv1.StateEvent)) error {
	stream, err := c.Watch(ctx, stack, service, target)
	if err != nil {
		return err
	}
	for ev, err := range stream {
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		handle(ev)
	}
	return nil
}

// printEvent writes a state event as one line of text: its time and its
// description.
func printEvent(w io.Writer, ev *anovelv1.StateEvent) {
	_, _ = fmt.Fprintf(w, "%s  %s\n", ev.GetTs().AsTime().Format("15:04:05.000"), ev.GetDescription())
}

// encodeEvent writes a state event as one JSON line.
func encodeEvent(w io.Writer, ev *anovelv1.StateEvent) {
	event := map[string]any{
		"ts":          ev.GetTs().AsTime().Format(time.RFC3339Nano),
		stackLabel:    ev.GetStack(),
		"service":     ev.GetService(),
		"target_id":   ev.GetTargetId(),
		"old_phase":   ev.GetOldPhase().String(),
		"new_phase":   ev.GetNewPhase().String(),
		"description": ev.GetDescription(),
	}
	_ = json.NewEncoder(w).Encode(event)
}

// =============================================================================
// UI
// =============================================================================

func newUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ui",
		Short: "Open the terminal UI",
		Long: `Open the full-screen view of every service: the service list, each service's
targets and infra as tabs, and the selected tab's live log. Esc opens the
command palette, ? lists every key, and q quits without stopping anything.
The UI talks to the same daemon as the CLI, so each sees what the other
starts.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return tui.Run()
		},
	}
}
