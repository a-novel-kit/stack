package runner

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Instance is one running (or recently-terminated) target. The runner owns
// every Instance; the server reads them through Runner methods.
type Instance struct {
	// Identity. Set at construction, immutable thereafter.
	ID      string // <stack>/<service>/<target>
	Target  string // bare target name (e.g., "rest")
	Service string
	Stack   string
	// Mode is how the instance was spawned, go-exec or container. It is set
	// at construction and never changes.
	Mode anovelv1.Mode

	// Lifecycle. Mutated by the runner; readers must hold Runner.mu (RLock).
	Phase        anovelv1.Phase
	ExitReason   anovelv1.ExitReason
	Health       anovelv1.Health
	PID          int32
	ContainerID  string
	StartedAt    time.Time
	TerminatedAt time.Time
	// LastErr captures the most recent error the supervisor observed, such as
	// a spawn or wait failure. A failed one-shot quotes it.
	LastErr string

	// Process handle. nil after termination.
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// Live reports whether the instance is starting or running, the phases a
// shutdown or an infra teardown has to stop.
func (i *Instance) Live() bool {
	return i.Phase == anovelv1.Phase_PHASE_RUNNING || i.Phase == anovelv1.Phase_PHASE_STARTING
}

// ErrEnv marks a start that failed while building the target's env, a daemon
// fault rather than a refused precondition.
var ErrEnv = errors.New("env build")

// Runner supervises every spawned target across every stack. A single RWMutex
// guards the instance map, making per-target state last-write-wins.
type Runner struct {
	mu        sync.RWMutex
	instances map[string]*Instance // keyed by Instance.ID

	// stacks translates a target ID into a discovery.Target at start time.
	stacks discovery.Stacks
	// alloc is the port allocator. The runner releases an instance's
	// refcounted slots when it terminates. A nil allocator skips those
	// releases, which tests rely on.
	alloc *env.Allocator
	// builder synthesizes the env block of every target and infra the runner
	// spawns.
	builder *env.Builder
	// logs is the per-target log store. The runner pipes go-exec stdout and
	// stderr through it, and streams `podman logs` of its containers into it.
	logs *logs.Store

	// sessMu guards infraSessions. Separate from mu so the (sometimes
	// long) `compose up` invocations don't block per-instance state
	// reads in the rest of the package.
	sessMu        sync.RWMutex
	infraSessions map[string]*infraSession // keyed by sessionKey(stack, service)

	// subsMu + subs implement the phase-event broadcaster (see events.go).
	// Kept as its own lock so emitting an event doesn't block on
	// long-running state mutations.
	subsMu sync.RWMutex
	subs   []*eventSub

	// infraStateCache memoizes InfraStatesOf per stack, sparing every
	// ListServices RPC podman's ~1s cold-start scan, which otherwise makes the
	// TUI's 2-second poll feel laggy and back-to-back CLI `ps` calls slow. Any
	// RPC that mutates infra state calls InvalidateInfraStateCache, so a
	// user-initiated change surfaces at once.
	infraStateMu    sync.Mutex
	infraStateCache map[string]infraStateCacheEntry // keyed by stack
	// infraStateGen bumps on every InvalidateInfraStateCache call.
	// InfraStatesOf snapshots it before its long podman scan and writes the
	// cache only when the generation still matches, so a scan older than an
	// invalidation cannot resurrect just-killed state.
	infraStateGen uint64
}

// infraStateCacheEntry is one (stack → states snapshot, when scanned).
type infraStateCacheEntry struct {
	at     time.Time
	states map[string]InfraState
}

// infraStateCacheTTL is the freshness window for the batched podman scan. At
// 2.5s the TUI's 2-second poll hits cache every other iteration while never
// serving data older than roughly one poll, and a CLI caller pays the cold
// ~1s scan only on its first call.
const infraStateCacheTTL = 2500 * time.Millisecond

// New returns an empty Runner wired to the discovery snapshot it resolves
// target IDs against, the env allocator and builder, and the log store that
// captures target output.
func New(disc discovery.Stacks, alloc *env.Allocator, builder *env.Builder, logStore *logs.Store) *Runner {
	return &Runner{
		instances:       make(map[string]*Instance),
		stacks:          disc,
		alloc:           alloc,
		builder:         builder,
		logs:            logStore,
		infraSessions:   make(map[string]*infraSession),
		infraStateCache: make(map[string]infraStateCacheEntry),
	}
}

// InvalidateInfraStateCache drops the cached InfraStatesOf entries. Every RPC
// that changes infra state calls it, so the next ListServices reflects reality
// at once. It is safe to call when nothing is cached.
func (r *Runner) InvalidateInfraStateCache() {
	r.infraStateMu.Lock()
	r.infraStateCache = make(map[string]infraStateCacheEntry)
	r.infraStateGen++
	r.infraStateMu.Unlock()
}

// Instance returns the live record for id, or false when the runner knows none.
// The returned copy is a stable snapshot, so the caller needs no lock.
func (r *Runner) Instance(id string) (Instance, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	inst, ok := r.instances[id]
	if !ok {
		return Instance{}, false
	}
	return *inst, true
}

// AllInstances returns a snapshot of every instance the runner knows about.
func (r *Runner) AllInstances() []Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Instance, 0, len(r.instances))
	for _, inst := range r.instances {
		out = append(out, *inst)
	}
	return out
}

// =============================================================================
// Start
// =============================================================================

// StartTarget brings t up in mode once its dependencies are ready: infra up,
// one-shots satisfied, and long-runner deps already running. A mode other than
// container means go-exec. The dependency walk runs first, so the ports it
// allocates land in the env built next.
//
// Starting a target already running in mode returns its instance. A target
// running in the other mode, or stopping, is refused. A failure to build the
// env wraps ErrEnv.
func (r *Runner) StartTarget(ctx context.Context, t *discovery.Target, svc *discovery.Service, mode anovelv1.Mode) (*Instance, error) {
	if err := r.EnsureDepsReady(ctx, t, svc, mode); err != nil {
		return nil, err
	}
	return r.launch(ctx, t, mode)
}

// Relaunch restarts target id as go-exec from a reinstall checkpoint, on the
// host ports the previous daemon gave it, listed as KEY=port entries in held.
// Reserving them before the env builds keeps the allocator in step with what
// the process binds, so every other consumer resolves the same ports.
func (r *Runner) Relaunch(ctx context.Context, id string, held []string) error {
	t, _ := r.stacks.Target(id)
	if t == nil {
		return fmt.Errorf("unknown target %q", id)
	}
	if _, running, err := r.canStart(id, anovelv1.Mode_MODE_GO_EXEC); err != nil || running {
		return err
	}
	r.builder.KeepPorts(t, held)
	_, err := r.launch(ctx, t, anovelv1.Mode_MODE_GO_EXEC)
	return err
}

// launch builds t's env and spawns it in mode. The start invariants are
// checked before the env claims any port, so a refused start leaves a live
// instance's ports alone, and a failed spawn releases what the env claimed.
func (r *Runner) launch(ctx context.Context, t *discovery.Target, mode anovelv1.Mode) (*Instance, error) {
	start := r.startGoExec
	if mode == anovelv1.Mode_MODE_CONTAINER {
		start = r.startContainer
	} else {
		mode = anovelv1.Mode_MODE_GO_EXEC
	}
	if inst, running, err := r.canStart(t.ID(), mode); err != nil || running {
		return inst, err
	}
	// The builder's snapshot fill picks up ports infra-up allocated, such as
	// POSTGRES_PORT, so POSTGRES_DSN synthesizes to localhost:<port>.
	entries, warnings, err := r.builder.ForTarget(t)
	if err != nil {
		r.alloc.Release(t.ID())
		return nil, fmt.Errorf("%w: %w", ErrEnv, err)
	}
	inst, err := start(ctx, t, env.Environ(entries), warnings)
	if err != nil {
		r.alloc.Release(t.ID())
	}
	return inst, err
}

// canStart enforces the start-time invariants:
//   - running in the same mode is idempotent and returns the existing instance
//   - running in a different mode is refused, with a hint
//   - a stopping instance is refused; the caller waits or restarts
//
// It returns (existing-instance, true, nil) when the call is a no-op.
func (r *Runner) canStart(id string, mode anovelv1.Mode) (*Instance, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	inst, ok := r.instances[id]
	if !ok {
		return nil, false, nil
	}
	switch inst.Phase {
	case anovelv1.Phase_PHASE_RUNNING, anovelv1.Phase_PHASE_STARTING, anovelv1.Phase_PHASE_PENDING:
		if inst.Mode == mode {
			out := *inst
			return &out, true, nil
		}
		return nil, false, fmt.Errorf(
			"%s already running in %s mode (pid %d); kill it first or use `a-novel run restart %s --mode=%s`",
			id, modeLabel(inst.Mode), inst.PID, id, modeLabel(mode))
	case anovelv1.Phase_PHASE_STOPPING:
		return nil, false, fmt.Errorf("%s is currently stopping; wait for it to settle", id)
	default:
		// A terminated instance's slot is reused.
		return nil, false, nil
	}
}

// modeLabel names a mode the way the CLI's --mode flag spells it.
func modeLabel(m anovelv1.Mode) string {
	if m == anovelv1.Mode_MODE_CONTAINER {
		return "container"
	}
	return "go-exec"
}

// register records a PENDING instance of t, replacing a terminated one, so
// concurrent starts see the slot taken. cmd is nil for a container.
func (r *Runner) register(t *discovery.Target, mode anovelv1.Mode, cmd *exec.Cmd, cancel context.CancelFunc) *Instance {
	inst := &Instance{
		ID:        t.ID(),
		Target:    t.Name,
		Service:   t.Service,
		Stack:     t.Stack,
		Mode:      mode,
		Phase:     anovelv1.Phase_PHASE_PENDING,
		StartedAt: time.Now(),
		cmd:       cmd,
		cancel:    cancel,
	}
	r.mu.Lock()
	r.instances[inst.ID] = inst
	r.mu.Unlock()
	return inst
}

// =============================================================================
// Stop / kill
// =============================================================================

// Kill stops the named instance. Idempotent: killing an already-terminated
// target returns nil. Sends SIGTERM, waits up to `grace`, then SIGKILL.
//
// `grace` of 0 means "SIGKILL immediately" (no grace period).
func (r *Runner) Kill(ctx context.Context, id string, grace time.Duration) error {
	r.mu.RLock()
	inst, ok := r.instances[id]
	if !ok || inst.Phase == anovelv1.Phase_PHASE_TERMINATED {
		r.mu.RUnlock()
		return nil
	}
	mode := inst.Mode
	r.mu.RUnlock()
	// transition emits the *→STOPPING PhaseEvent, so Watch subscribers can show
	// "stopping" briefly before the eventual TERMINATED.
	r.transition(id, anovelv1.Phase_PHASE_STOPPING)

	switch mode {
	case anovelv1.Mode_MODE_GO_EXEC:
		return r.killGoExec(ctx, id, grace)
	case anovelv1.Mode_MODE_CONTAINER:
		return r.killContainer(ctx, id, grace)
	default:
		return fmt.Errorf("kill: unknown mode for %s", id)
	}
}

// markTerminated moves the instance into PHASE_TERMINATED with the supplied
// reason, releases its env-allocation refcounts, and emits the terminal
// PhaseEvent to Watch subscribers. It is idempotent, so a watch goroutine
// racing kill() cannot corrupt the state.
func (r *Runner) markTerminated(id string, reason anovelv1.ExitReason, errMsg string) {
	r.mu.Lock()
	inst, ok := r.instances[id]
	if !ok || inst.Phase == anovelv1.Phase_PHASE_TERMINATED {
		r.mu.Unlock()
		return
	}
	old := inst.Phase
	inst.Phase = anovelv1.Phase_PHASE_TERMINATED
	inst.ExitReason = reason
	inst.TerminatedAt = time.Now()
	if errMsg != "" {
		inst.LastErr = errMsg
	}
	inst.cmd = nil
	// Held to close outside the lock. The watch and log-stream goroutines run on the context this
	// closes, and a container that outlives its instance keeps both polling.
	cancel := inst.cancel
	inst.cancel = nil
	ev := PhaseEvent{
		TargetID:   inst.ID,
		Service:    inst.Service,
		Stack:      inst.Stack,
		OldPhase:   old,
		NewPhase:   anovelv1.Phase_PHASE_TERMINATED,
		ExitReason: reason,
	}
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	// Release the allocator refcounts outside the runner's lock, so the two
	// locks never interleave.
	if r.alloc != nil {
		r.alloc.Release(id)
	}
	r.emitPhase(ev)
}
