// Daemon-backed verbs: the CLI surface that connects to the daemon over its
// unix-socket RPC and renders the responses. Each command is a thin client
// holding no supervision state; the daemon owns the running targets, their
// phases, and their logs.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
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

func (s *stackScope) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&s.stack, "stack", "", "stack name (default: first in $A_NOVEL_STACKS)")
}

func (s *stackScope) bindAll(cmd *cobra.Command) {
	s.bind(cmd)
	cmd.Flags().BoolVar(&s.allStacks, "all-stacks", false, "operate across every registered stack")
}

// =============================================================================
// Stacks
// =============================================================================

func newStacksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stacks",
		Short: "List the registered stacks",
		Long: `Print the stacks the daemon manages, one per row, with their path and a
marker showing which is the default. Stacks come from $A_NOVEL_STACKS,
parsed at daemon start (see 'a-novel core setup' for the bootstrap flow).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			resp, err := c.ListStacks(ctx)
			if err != nil {
				return err
			}
			for _, st := range resp.GetStacks() {
				marker := " "
				if st.GetIsDefault() {
					marker = "*"
				}
				fmt.Printf("%s %-20s %s\n", marker, st.GetName(), st.GetPath())
			}
			return nil
		},
	}
	return cmd
}

// =============================================================================
// Discovery
// =============================================================================

func newPsCmd() *cobra.Command {
	var ss stackScope
	var service string
	var kind string
	var jsonOut bool
	var watch bool
	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List services with their targets AND infra containers",
		Long: `Show every service in the active stack with its targets' current phase,
exit reason (if terminated), and mode (go-exec vs container). Infrastructure
containers are listed alongside their owning service — distinguished by
the leading 'infra' kind column (vs '1shot' / 'longr' for targets).

Filters:
  --service=NAME    only this service
  --kind=infra      only infra containers (suppress targets)
  --kind=target     only targets (suppress infra)

The canonical fully-qualified IDs for each row are emitted in --json
mode so machine consumers don't have to reconstruct them:
  target:  <stack>/<service>/<name>
  infra:   <stack>/<service>/infra/<name>

With --watch, streams state changes as they happen instead of a snapshot.
With --json, emits one JSON object per service for machine consumption
(newline-delimited when combined with --watch).`,
		Example: `  a-novel run ps
  a-novel run ps --stack=branch-foo
  a-novel run ps --service=service-json-keys --json
  a-novel run ps --kind=infra
  a-novel run ps --watch`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			stack := ss.stack
			if ss.allStacks {
				stack = "*"
			}
			resp, err := c.ListServices(ctx, stack)
			if err != nil {
				return err
			}
			services := resp.GetServices()
			if service != "" {
				filtered := services[:0]
				for _, s := range services {
					if s.GetName() == service {
						filtered = append(filtered, s)
					}
				}
				services = filtered
			}
			switch kind {
			case "", "all":
				// default — show both
			case kindTarget:
				for _, s := range services {
					s.Infra = nil
				}
			case kindInfra:
				for _, s := range services {
					s.Targets = nil
				}
			default:
				return fmt.Errorf("--kind=%q: must be 'target', 'infra', or 'all'", kind)
			}
			if watch {
				return runPsWatch(ctx, c, cmd.OutOrStdout(), services, stack, service, kind, jsonOut)
			}
			renderPs(cmd.OutOrStdout(), services, jsonOut)
			return nil
		},
	}
	ss.bindAll(cmd)
	cmd.Flags().StringVar(&service, "service", "", "filter to one service")
	cmd.Flags().StringVar(&kind, "kind", "", "filter by entity kind (target|infra|all; default all)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit JSON (machine-readable)")
	cmd.Flags().BoolVar(&watch, "watch", false, "stream state changes instead of a snapshot")
	return cmd
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
		Long: `Render the per-service dependency graph as a text tree, derived from each
service's compose 'depends_on' chain. Long-runners show their (mode, phase,
health); one-shots show their last exit status; infrastructure shows
container health.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			resp, err := c.GetTopology(ctx, ss.stack, service)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(cmd.OutOrStdout(), resp.GetRendered())
			return nil
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&service, "service", "", "limit to one service")
	return cmd
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
		Short: "Show one service's infrastructure + per-target state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			resp, err := c.DescribeService(ctx, ss.stack, args[0])
			if err != nil {
				return err
			}
			renderPs(cmd.OutOrStdout(), []*anovelv1.Service{resp.GetService()}, false)
			return nil
		},
	}
	ss.bind(cmd)
	return cmd
}

func newServiceInfraCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   kindInfra,
		Short: "Manage a service's infrastructure (containers without Go counterparts)",
		Long: `Bring up or tear down the compose services that aren't tied to a 'cmd/'
target — typically the database (postgres-<service>). 'infra start' also
runs every one-shot target (migrations, rotate-keys, ...) that depends on
the infra, blocking long-runners until the one-shots succeed.

Useful explicitly when you want the database up so you can psql / pgcli
into it without starting any application targets.`,
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
		Short: "Bring up a service's infrastructure + run its one-shots",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			m := anovelv1.Mode_MODE_GO_EXEC
			if oneShotsMode == modeContainer {
				m = anovelv1.Mode_MODE_CONTAINER
			}
			resp, err := c.StartInfra(ctx, ss.stack, args[0], m)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "infra-up for %s (one-shots ran in %s mode)\n",
				resp.GetService().GetName(), modeLabel(m))
			return nil
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&oneShotsMode, "one-shots", modeGoExec,
		"mode for one-shot targets auto-run with infra (go-exec | container)")
	return cmd
}

func newServiceInfraKillCmd() *cobra.Command {
	var ss stackScope
	var force bool
	cmd := &cobra.Command{
		Use:   "kill <service>",
		Short: "Tear down a service's infrastructure",
		Long: `Refuses if any target of the service is still running — use 'a-novel run kill'
to stop them first, or pass --force to cascade-kill targets and infra
together.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			resp, err := c.KillInfra(ctx, ss.stack, args[0], force)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "infra-down for %s\n", resp.GetService().GetName())
			return nil
		},
	}
	ss.bind(cmd)
	cmd.Flags().BoolVarP(&force, "force", "f", false, "cascade-kill all running targets first")
	return cmd
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
		Long: `Start the named target. <target> is the canonical target ID —
'<stack>/<service>/<name>' — or the bare '<service>/<name>' shorthand
which resolves against the default stack.

Mode defaults to go-exec — faster feedback, no container build. Pass
--mode=container to bring up the containerized variant via compose.

Mutual exclusion is enforced: if the target is already running in the
other mode, the command refuses with a hint to 'kill' or 'restart'.
Idempotent: starting an already-running target in the same mode is a
no-op (returns 0).

Phase 3 status: go-exec mode is live. Container mode + dependency-walk
gating + one-shot auto-run are scheduled for the next phase-3 chunks;
attempting --mode=container today returns a clear Unimplemented error.`,
		Example: `  a-novel run start default/service-json-keys/rest
  a-novel run start service-json-keys/rest                     # default stack inferred
  a-novel run start service-json-keys/rest --mode=container    # next chunk
  a-novel run start service-template/rest --stack=branch-foo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			id := resolveTargetID(args[0], ss.stack)
			m := anovelv1.Mode_MODE_GO_EXEC
			if mode == modeContainer {
				m = anovelv1.Mode_MODE_CONTAINER
			}
			resp, err := c.StartTarget(ctx, id, m)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "started %s (mode=%s, phase=%s, %s)\n",
				resp.GetTarget().GetId(),
				modeLabel(resp.GetTarget().GetMode()),
				phaseLabel(resp.GetTarget().GetPhase()),
				runtimeIdentLabel(resp.GetTarget()))
			return nil
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&mode, "mode", modeGoExec, "execution mode (go-exec | container)")
	return cmd
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
		Long: `Send SIGTERM to the entity and wait up to --timeout (default 10s) for it
to exit before SIGKILL. Idempotent: killing an already-stopped entity
returns 0.

The argument can be a target ID ('service-X/grpc') or an infra ID
('service-X/infra/postgres-X'). The 'infra' segment is the
sentinel — target names colliding with that word would be
disambiguated by the stack-prefixed form.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			ref := parseEntityID(args[0], ss.stack)
			if ref.IsInfra {
				_, err := c.KillInfraContainer(ctx, ref.Stack, ref.Service, ref.Infra)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "stopped infra %s\n", ref.ID)
				return nil
			}
			resp, err := c.KillTarget(ctx, ref.ID, timeout)
			if err != nil {
				return err
			}
			t := resp.GetTarget()
			if t == nil || t.GetId() == "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s: not running (no-op)\n", ref.ID)
				return nil
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "killed %s (phase=%s, exit=%s)\n",
				t.GetId(), phaseLabel(t.GetPhase()), exitLabel(t.GetExitReason()))
			return nil
		},
	}
	ss.bind(cmd)
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Second, "SIGTERM-to-SIGKILL grace")
	return cmd
}

func newRestartCmd() *cobra.Command {
	var ss stackScope
	var mode string
	cmd := &cobra.Command{
		Use:   "restart <target-or-infra>",
		Short: "Kill then start (optionally swapping mode for targets)",
		Long: `Atomic kill-then-start. With --mode, swaps the target into the new mode
in one step — useful for go-exec → container migration (or back) without
the mutual-exclusion refusal you'd hit with a separate 'kill' + 'start'
race.

For an infra ID ('service-X/infra/postgres-X'), runs 'podman restart'
on the container — faster than kill+infra-start because the container
is reused. --mode is ignored for infra (infra is always containerized).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			ref := parseEntityID(args[0], ss.stack)
			if ref.IsInfra {
				_, err := c.RestartInfraContainer(ctx, ref.Stack, ref.Service, ref.Infra)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "restarted infra %s\n", ref.ID)
				return nil
			}
			m := anovelv1.Mode_MODE_UNSPECIFIED
			switch mode {
			case modeGoExec:
				m = anovelv1.Mode_MODE_GO_EXEC
			case modeContainer:
				m = anovelv1.Mode_MODE_CONTAINER
			}
			resp, err := c.RestartTarget(ctx, ref.ID, m)
			if err != nil {
				return err
			}
			t := resp.GetTarget()
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "restarted %s (mode=%s, phase=%s, %s)\n",
				t.GetId(), modeLabel(t.GetMode()), phaseLabel(t.GetPhase()), runtimeIdentLabel(t))
			return nil
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&mode, "mode", "", "switch to this mode (go-exec | container); empty keeps current")
	return cmd
}

// resolveTargetID accepts the canonical full ID or the bare service/target
// shorthand and returns the daemon-friendly form. Daemon-side lookup
// requires the full <stack>/<service>/<target> shape.
func resolveTargetID(arg, stack string) string {
	if strings.Count(arg, "/") >= 2 {
		return arg
	}
	if stack == "" {
		stack = stacks.DefaultName
	}
	return stack + "/" + arg
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
// anywhere else the word is an ordinary target name.
func parseEntityID(arg, defaultStack string) entityRef {
	if defaultStack == "" {
		defaultStack = stacks.DefaultName
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

func newLogsCmd() *cobra.Command {
	var ss stackScope
	var follow bool
	var tail int
	var since time.Duration
	var streamFilter string
	var previous bool
	var runID string
	cmd := &cobra.Command{
		Use:   "logs <target-or-infra>",
		Short: "Print or stream logs for a target or infra container",
		Long: `Read the entity's current log file (or an archived previous run with
--previous / --run-id=<ts>). With --follow, streams new lines through
the daemon as they arrive (multiple followers see the same stream).

For an infra ID ('service-X/infra/postgres-X') the daemon streams
'podman logs -f' for that container — both stdout and stderr are
merged into the stream. --previous / --run-id and the --stream
stdout/stderr filter are NOT supported for infra (podman owns the
log files, not us); the daemon will surface an error if combined.

--tail limits to the last N lines (client-side; daemon streams full).
--since accepts Go-style durations ('5m', '1h30m'). Past-run access
(--previous / --run-id) is CLI-only — the TUI always shows the
current/latest session.`,
		Example: `  a-novel run logs service-json-keys/rest
  a-novel run logs service-json-keys/rest --follow
  a-novel run logs service-json-keys/infra/postgres-json-keys --follow
  a-novel run logs service-json-keys/rest --tail=100 --stream=stderr
  a-novel run logs service-json-keys/rest --previous`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			ref := parseEntityID(args[0], ss.stack)
			id := ref.ID
			// Podman owns infra log files, so infra entries have no run
			// archives. --previous / --run-id are refused here, with a
			// message that names the reason.
			if ref.IsInfra && (previous || runID != "") {
				return errors.New("--previous / --run-id are not supported for infra (podman owns its log file)")
			}
			// Resolve --previous to the most-recent archived run id.
			if previous && runID == "" {
				runs, err := c.ListRuns(ctx, id)
				if err != nil {
					return err
				}
				if len(runs.GetRunIds()) == 0 {
					return fmt.Errorf("%s: no archived runs yet", id)
				}
				runID = runs.GetRunIds()[0]
			}
			sf := anovelv1.LogStream_LOG_STREAM_UNSPECIFIED
			switch streamFilter {
			case "stdout":
				sf = anovelv1.LogStream_LOG_STREAM_STDOUT
			case "stderr":
				sf = anovelv1.LogStream_LOG_STREAM_STDERR
			}
			stream, err := c.StreamLogs(ctx, id, runID, follow, sf)
			if err != nil {
				return err
			}
			defer func() { _ = stream.Close() }()
			// --since: filter client-side by line timestamp.
			var cutoff time.Time
			if since > 0 {
				cutoff = time.Now().Add(-since)
			}
			// --tail: ring buffer client-side.
			var ring []*anovelv1.LogLine
			for stream.Receive() {
				ln := stream.Msg()
				if !cutoff.IsZero() && ln.GetTs().AsTime().Before(cutoff) {
					continue
				}
				if tail > 0 {
					if len(ring) == tail {
						ring = append(ring[:0], ring[1:]...)
					}
					ring = append(ring, ln)
					continue
				}
				printLogLine(cmd.OutOrStdout(), ln)
			}
			if err := stream.Err(); err != nil {
				return err
			}
			// Drain the ring for --tail mode.
			for _, ln := range ring {
				printLogLine(cmd.OutOrStdout(), ln)
			}
			return nil
		},
	}
	ss.bind(cmd)
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream new lines as they arrive")
	cmd.Flags().IntVar(&tail, "tail", 0, "last N lines (0 = all)")
	cmd.Flags().DurationVar(&since, "since", 0, "only lines newer than this duration")
	cmd.Flags().StringVar(&streamFilter, "stream", "", "filter to stdout or stderr only")
	cmd.Flags().BoolVar(&previous, "previous", false, "read the most recent archived run instead of the current")
	cmd.Flags().StringVar(&runID, "run-id", "", "read a specific archived run by timestamp")
	return cmd
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

func newEnvCmd() *cobra.Command {
	var ss stackScope
	var only string
	var format string
	cmd := &cobra.Command{
		Use:   "env [<service>]",
		Short: "Print a service's env vars in shell / JSON / dotenv form",
		Long: `Emit the env vars the daemon would inject into a service's targets — a
mix of constants from the compose file (POSTGRES_USER/PASSWORD/DB,
APP_MASTER_KEY, ...), allocated values (random ports), derived values
(POSTGRES_DSN, REST_URL, GRPC_URL), and any cross-service references the
service consumes.

With a positional <service>, scopes to that service. Without one, prints
every service in the active stack. --all-stacks unions across all stacks.

--format defaults to 'shell' (eval-able 'export VAR=...' lines). Switch
to 'json' for agent consumption or 'dotenv' for .env-style key=value.
--only=PATTERN filters keys by glob.`,
		Example: `  a-novel run env service-json-keys
  eval "$(a-novel run env service-json-keys)"
  curl "$REST_URL/ping"

  a-novel run env service-json-keys --only=REST
  a-novel run env --format=json
  a-novel run env --all-stacks`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			var service string
			if len(args) == 1 {
				service = args[0]
			}
			resp, err := c.GetEnv(ctx, ss.stack, service, only, ss.allStacks)
			if err != nil {
				return err
			}
			// --only filters client-side: the server returns everything,
			// and filtering before render keeps the daemon simple while
			// applying to --json output too.
			entries := resp.GetEntries()
			if only != "" {
				filtered := entries[:0]
				for _, e := range entries {
					if matchGlob(only, e.GetKey()) {
						filtered = append(filtered, e)
					}
				}
				entries = filtered
			}
			renderEnv(cmd.OutOrStdout(), entries, format)
			return nil
		},
	}
	ss.bindAll(cmd)
	cmd.Flags().StringVar(&only, "only", "", "glob filter on key names (e.g. 'REST*')")
	cmd.Flags().StringVar(&format, "format", "shell", "output format (shell | json | dotenv)")
	return cmd
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
		Long: `Volumes are service-level (not target-level) — e.g., the postgres data
volume of service-json-keys is shared by every target of that service.

All destructive operations (clear, restore, backup) refuse while ANY
target or infrastructure of the service is up, to prevent inconsistent
snapshots and orphaned mounts. Pass --force to cascade-stop everything
first; the user owns the consistency implications of that override.

Backups land in $XDG_DATA_HOME/a-novel/backups/<stack>/<service>/<volume>/
as tar.zst archives; max 5 per volume, oldest pruned automatically.
Routine 'core kill' does NOT clear volumes — only an explicit 'volume
clear' destroys data.`,
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
			ctx := cmd.Context()
			c := rpc.New("")
			resp, err := c.ListVolumes(ctx, ss.stack, args[0])
			if err != nil {
				return err
			}
			if len(resp.GetVolumes()) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "(no volumes declared)")
				return nil
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-40s %12s %8s\n", "VOLUME", "SIZE", "BACKUPS")
			for _, v := range resp.GetVolumes() {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-40s %12s %8d\n",
					v.GetName(), humanBytes(v.GetSizeBytes()), v.GetBackupCount())
			}
			return nil
		},
	}
	ss.bind(cmd)
	return cmd
}

func newVolumeBackupCmd() *cobra.Command {
	var ss stackScope
	var tag string
	var force bool
	cmd := &cobra.Command{
		Use:   "backup <service>",
		Short: "Snapshot a service's volumes (refuses while service is up)",
		Long: `Tar-zstd archive each of the service's volumes into the backups dir.
Refuses while infra/targets are up (a live postgres tar is silently
corrupt). Pass --force to cascade-stop the service first — the resulting
archive's consistency is then your responsibility.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			resp, err := c.BackupVolume(ctx, ss.stack, args[0], tag, force)
			if err != nil {
				return err
			}
			for _, p := range resp.GetArchivePaths() {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), p)
			}
			return nil
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&tag, "tag", "", "user-supplied label included in the archive filename")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "cascade-stop the service before backing up")
	return cmd
}

func newVolumeRestoreCmd() *cobra.Command {
	var ss stackScope
	var from string
	var force bool
	cmd := &cobra.Command{
		Use:   "restore <service>",
		Short: "Restore volumes from a backup (latest by default)",
		Long: `Decompress a backup archive over the service's volumes. Without --from,
uses the latest backup. With --from=<ts>, uses that specific archive
(timestamps come from 'a-novel run volume list').

Refuses while the service is up. --force cascade-stops first. Backup
file is validated (decompresses cleanly) before any volume is touched.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			resp, err := c.RestoreVolume(ctx, ss.stack, args[0], from, force)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "restored: %v\n", resp.GetRestoredVolumes())
			return nil
		},
	}
	ss.bind(cmd)
	cmd.Flags().StringVar(&from, "from", "", "specific backup timestamp (default: latest)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "cascade-stop the service before restoring")
	return cmd
}

func newVolumeClearCmd() *cobra.Command {
	var ss stackScope
	var noBackup bool
	var force bool
	cmd := &cobra.Command{
		Use:   "clear <service>",
		Short: "Destroy a service's volumes (auto-backups first)",
		Long: `Delete every volume of the service. By default, takes an auto-backup
first (so undo is one 'restore --previous' away). Pass --no-backup to
skip the auto-backup — the destruction is then irreversible.

Refuses while the service is up. --force cascade-stops first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			resp, err := c.ClearVolume(ctx, ss.stack, args[0], noBackup, force)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "cleared: %v\n", resp.GetClearedVolumes())
			return nil
		},
	}
	ss.bind(cmd)
	cmd.Flags().BoolVar(&noBackup, "no-backup", false, "skip the auto-backup (irreversible)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "cascade-stop the service before clearing")
	return cmd
}

// matchGlob is a small glob matcher supporting * (any chars), enough for the
// `--only=PATTERN` filter on env keys ("REST*", "*_PORT", "POSTGRES_*"). A
// malformed pattern falls back to substring matching, so a typo still does
// something reasonable.
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
		Long: `Runs <cmd> "as if it were a sibling of the target":

  - Container-mode targets (currently RUNNING): the command runs INSIDE
    the target's container via 'podman exec'. Useful for sh/psql/curl
    in the container's network namespace.

  - Go-exec targets (or anything not currently running): the command
    runs on the host with the target's resolved env (POSTGRES_DSN,
    *_PORT, etc.) and the service directory as CWD. Useful for
    'exec migrations psql' to get psql with the right DSN.

Output is forwarded line-by-line from the daemon. Exit code reflects
the underlying command's success.`,
		Example: `  a-novel exec service-json-keys/rest -- sh
  a-novel exec service-json-keys/migrations -- psql
  a-novel exec default/service-json-keys/rest -- curl -s localhost:8080`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			// Cobra strips "--" — args[0] is the target, args[1:] is the command.
			targetSpec := args[0]
			cmdv := args[1:]
			ref := parseEntityID(targetSpec, ss.stack)
			if ref.IsInfra {
				return fmt.Errorf("exec: %s is an infra container; use 'podman exec %s/%s/...' directly", targetSpec, ref.Stack, ref.Service)
			}
			stream, err := c.Exec(ctx, ref.ID, cmdv)
			if err != nil {
				return err
			}
			defer func() { _ = stream.Close() }()
			// A pointer, so an unreported exit status stays distinct
			// from a reported 0.
			var exitCode *int32
			for stream.Receive() {
				ev := stream.Msg()
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
			if err := stream.Err(); err != nil && ctx.Err() == nil {
				return err
			}
			return execResult(exitCode)
		},
	}
	ss.bind(cmd)
	return cmd
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
		Long: `For go-exec targets currently in PHASE_RUNNING: returns a ready-to-paste
'dlv attach' command pinned to the target's PID + a suggested headless
port. Run the command in your own terminal so the dlv REPL is interactive;
your IDE then connects to dlv's headless listener.

Container-mode targets are rejected — attaching dlv inside a container
requires an in-image dlv install + a port-forward the daemon doesn't
manage.`,
		Example: `  a-novel debug service-json-keys/grpc
  a-novel debug default/service-json-keys/grpc`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			ref := parseEntityID(args[0], ss.stack)
			if ref.IsInfra {
				return fmt.Errorf("debug: %s is an infra container; dlv attach is only meaningful on go-exec targets", args[0])
			}
			resp, err := c.Debug(ctx, ref.ID)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), resp.GetHint())
			return nil
		},
	}
	ss.bind(cmd)
	return cmd
}

// =============================================================================
// Watch
// =============================================================================

func newWatchCmd() *cobra.Command {
	var ss stackScope
	var service string
	var target string
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream state-change events (for agents)",
		Long: `Subscribe to the daemon's state-event stream — every phase transition,
exit, health flip, etc. is emitted as a newline-delimited JSON object.

Designed for agentic workflows: polling 'ps' is wasteful; watch tells
you the moment a state changes. Filter by --service or --target to
narrow the stream.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			stack := ss.stack
			if ss.allStacks {
				stack = "*"
			}
			stream, err := c.Watch(ctx, stack, service, target)
			if err != nil {
				return err
			}
			defer func() { _ = stream.Close() }()
			enc := json.NewEncoder(cmd.OutOrStdout())
			for stream.Receive() {
				ev := stream.Msg()
				if jsonOut {
					_ = enc.Encode(map[string]any{
						"ts":          ev.GetTs().AsTime().Format(time.RFC3339Nano),
						"stack":       ev.GetStack(),
						"service":     ev.GetService(),
						"target_id":   ev.GetTargetId(),
						"old_phase":   ev.GetOldPhase().String(),
						"new_phase":   ev.GetNewPhase().String(),
						"description": ev.GetDescription(),
					})
				} else {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n",
						ev.GetTs().AsTime().Format("15:04:05.000"),
						ev.GetDescription())
				}
			}
			if err := stream.Err(); err != nil && ctx.Err() == nil {
				return err
			}
			return nil
		},
	}
	ss.bindAll(cmd)
	cmd.Flags().StringVar(&service, "service", "", "filter to one service")
	cmd.Flags().StringVar(&target, "target", "", "filter to one target")
	cmd.Flags().BoolVar(&jsonOut, "json", true, "emit JSON (default: true — this command is agent-oriented)")
	return cmd
}

// runPsWatch is the ps-style streaming variant: render the current snapshot
// once, then re-render after every StateEvent, using ANSI cursor control to
// overwrite the previous frame so it reads like `top`.
//
// In --json mode it emits one newline-delimited JSON object per state event on
// top of the initial snapshot, keeping machine parsing to "read a line, parse
// it".
func runPsWatch(
	ctx context.Context, c *rpc.Client, out io.Writer,
	services []*anovelv1.Service, stack, service, kind string, jsonOut bool,
) error {
	if !jsonOut {
		// Initial snapshot.
		renderPs(out, services, false)
	} else {
		renderPs(out, services, true)
	}
	stream, err := c.Watch(ctx, stack, service, "")
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()
	enc := json.NewEncoder(out)
	for stream.Receive() {
		ev := stream.Msg()
		if jsonOut {
			_ = enc.Encode(map[string]any{
				"ts":          ev.GetTs().AsTime().Format(time.RFC3339Nano),
				"stack":       ev.GetStack(),
				"service":     ev.GetService(),
				"target_id":   ev.GetTargetId(),
				"old_phase":   ev.GetOldPhase().String(),
				"new_phase":   ev.GetNewPhase().String(),
				"description": ev.GetDescription(),
			})
			continue
		}
		// Human mode: refresh the snapshot. One ListServices call per
		// event is cheap and keeps the rendered table current.
		// Repainting in place keeps frames out of the scrollback.
		resp, err := c.ListServices(ctx, stack)
		if err != nil {
			_, _ = fmt.Fprintf(out, "watch: refresh failed: %v\n", err)
			continue
		}
		fresh := resp.GetServices()
		if service != "" {
			filtered := fresh[:0]
			for _, s := range fresh {
				if s.GetName() == service {
					filtered = append(filtered, s)
				}
			}
			fresh = filtered
		}
		switch kind {
		case kindTarget:
			for _, s := range fresh {
				s.Infra = nil
			}
		case kindInfra:
			for _, s := range fresh {
				s.Targets = nil
			}
		}
		// Move the cursor home and clear the screen. On a non-TTY pipe the
		// escape sequences are harmless filler.
		_, _ = fmt.Fprint(out, "\x1b[H\x1b[2J")
		renderPs(out, fresh, false)
		_, _ = fmt.Fprintf(out, "\n%s  %s\n",
			ev.GetTs().AsTime().Format("15:04:05"), ev.GetDescription())
	}
	if err := stream.Err(); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// =============================================================================
// UI
// =============================================================================

func newUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ui",
		Short: "Open the terminal UI (Bubble Tea)",
		Long: `Full-screen TUI with a services nav (left), a tabbed target detail and
log viewer (right), a footer hint with the most-used commands, an
Esc-opened command palette covering the full CLI surface, and a
dedicated :help screen with searchable command reference.

The UI is a thin client over the same RPC as the CLI — every action you
take in the UI is observable from 'a-novel run watch' and vice-versa.

Press ? for the full key/command reference. Press Esc for the command
palette (':start', ':kill', ':infra-start', etc.). q or Ctrl-C quits.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return tui.Run()
		},
	}
}
