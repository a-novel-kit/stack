package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sync/atomic"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func newStacksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stacks",
		Short: "List the registered stacks",
		Long: `Print the stacks the daemon manages, one per row: path, a '*' on the
default, and what each holds. Stacks come from $A_NOVEL_STACKS, parsed at
daemon start. Same as 'a-novel core stacks list'.`,
		Deprecated: "use 'a-novel core stacks list' instead",
		Args:       cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listStacks(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

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

--watch redraws the table after each state change, once for a burst of them,
above the latest change. --json prints one JSON object per service, one per
line, and with --watch one more per state change.`,
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
	if !slices.Contains([]string{"", "all", kindTarget, kindInfra}, o.kind) {
		return usageError(fmt.Errorf("--kind=%q: must be 'target', 'infra', or 'all'", o.kind))
	}

	ctx, out := cmd.Context(), cmd.OutOrStdout()
	c := rpc.New("")
	resp, err := c.ListServices(ctx, o.scope.selector())
	if err != nil {
		return err
	}
	renderPs(out, filterServices(resp.GetServices(), o.service, o.kind), o.jsonOut)
	if !o.watch {
		return nil
	}
	handle := func(ev *anovelv1.WatchResponse) { encodeEvent(out, ev) }
	if !o.jsonOut {
		repaint, wait := coalesce(func(ev *anovelv1.WatchResponse) { repaintPs(ctx, c, out, o, ev) })
		defer wait()
		handle = repaint
	}
	return watchEvents(ctx, c, o.scope.selector(), o.service, "", handle)
}

// coalesce returns a handler that passes each event to paint on a goroutine of
// its own. Events that arrive while paint runs collapse into the newest, so a
// burst costs one paint instead of one per event.
//
// The second function returns once the newest event is painted. Call it after
// the handler's last call.
func coalesce(paint func(*anovelv1.WatchResponse)) (func(*anovelv1.WatchResponse), func()) {
	var newest atomic.Pointer[anovelv1.WatchResponse]
	pending := make(chan struct{}, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for range pending {
			if ev := newest.Swap(nil); ev != nil {
				paint(ev)
			}
		}
	}()

	handle := func(ev *anovelv1.WatchResponse) {
		newest.Store(ev)
		select {
		case pending <- struct{}{}:
		default:
		}
	}
	wait := func() {
		close(pending)
		<-done
	}
	return handle, wait
}

// repaintPs redraws the ps table in place after ev, so it reads like `top`
// and the frames stay out of the scrollback. It fetches the services again, so
// the table shows their state after ev.
func repaintPs(ctx context.Context, c *rpc.Client, out io.Writer, o psOpts, ev *anovelv1.WatchResponse) {
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
		renderService(w, s)
	}
}

// renderService prints a service's header, then one row per target, then one
// per infra container. The kind column and the trailing fully-qualified ID tell
// the two apart, and the ID pastes straight into kill, restart and logs.
func renderService(w io.Writer, s *anovelv1.Service) {
	prefix := s.GetStack() + "/" + s.GetName() + "/"
	_, _ = fmt.Fprintf(w, "%s  (stack %s)\n", s.GetName(), s.GetStack())

	for _, t := range s.GetTargets() {
		_, _ = fmt.Fprintf(w, "  %-6s %-30s  %-12s  %s\n",
			targetKindShort(t.GetKind()), t.GetName(), targetStatusLabel(t), prefix+t.GetName())
	}

	for _, in := range s.GetInfra() {
		_, _ = fmt.Fprintf(w, "  %-6s %-30s  %-10s  %s\n",
			kindInfra, in.GetName(), infraStatusLabel(in), prefix+"infra/"+in.GetName())
	}
}

// healthSuffix is the word an infra container's health adds to its phase.
var healthSuffix = map[anovelv1.Health]string{
	anovelv1.Health_HEALTH_HEALTHY:   " healthy",
	anovelv1.Health_HEALTH_UNHEALTHY: " unhealthy",
	anovelv1.Health_HEALTH_STARTING:  " starting",
}

// infraStatusLabel returns an infra container's phase word, followed by its
// health when the container reports one.
func infraStatusLabel(in *anovelv1.Infra) string {
	return phaseLabel(in.GetPhase()) + healthSuffix[in.GetHealth()]
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

// targetKindShort returns the kind column of a target row.
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
