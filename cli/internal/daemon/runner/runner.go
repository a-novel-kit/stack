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

// errKilledWhileStarting fails a start that a kill reached before the target
// ran.
var errKilledWhileStarting = errors.New("killed while starting")

// Runner supervises every spawned target across every stack.
type Runner struct {
	mu        sync.RWMutex
	instances map[string]*Instance // keyed by Instance.ID
	// polling records that the container poller runs. mu guards it with
	// instances, so a container added while the poller stops still gets one.
	polling bool

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

	// sessMu guards infraSessions and infraLocks.
	sessMu        sync.RWMutex
	infraSessions map[string]*infraSession // keyed by sessionKey(stack, service)
	// infraLocks holds one single-slot channel per service. StartInfra and
	// KillInfra hold it for their whole call.
	infraLocks map[string]chan struct{}

	// subsMu + subs implement the phase-event broadcaster (see events.go).
	// Kept as its own lock so emitting an event doesn't block on
	// long-running state mutations.
	subsMu sync.RWMutex
	subs   []*eventSub

	// infraStateCache holds the latest InfraStatesOf scan per stack. Every
	// infra-mutating RPC invalidates it.
	infraStateMu    sync.Mutex
	infraStateCache map[string]infraStateCacheEntry // keyed by stack
	// infraStateGen counts invalidations. A scan caches its result only when
	// no invalidation happened while it ran.
	infraStateGen uint64
}

// infraStateCacheEntry is one (stack → states snapshot, when scanned).
type infraStateCacheEntry struct {
	at     time.Time
	states map[string]InfraState
}

// infraStateCacheTTL is how long a scan stays fresh. A podman call costs about
// 1s on rootless WSL2, and the TUI polls every 2s, so the cache answers every
// other poll and never serves data older than about one poll.
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
		infraLocks:      make(map[string]chan struct{}),
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
// Starting a target already starting or running in mode returns its instance.
// A target running in the other mode, or stopping, is refused. A failure to
// build the env wraps ErrEnv.
func (r *Runner) StartTarget(ctx context.Context, t *discovery.Target, svc *discovery.Service, mode anovelv1.Mode) (*Instance, error) {
	if err := r.EnsureDepsReady(ctx, t, svc, mode); err != nil {
		return nil, err
	}
	return r.launch(ctx, t, mode)
}

// Relaunch restarts target id as go-exec from a reinstall checkpoint, on the
// host ports the previous daemon gave it, listed as KEY=port entries in held.
// Reserving them before the env builds keeps the allocator in step with what
// the process binds, so every other consumer resolves the same ports. The
// claim comes first, so a target that already runs keeps its own ports.
func (r *Runner) Relaunch(ctx context.Context, id string, held []string) error {
	t, _ := r.stacks.Target(id)
	if t == nil {
		return fmt.Errorf("unknown target %q", id)
	}
	inst, _, err := r.claim(t, anovelv1.Mode_MODE_GO_EXEC)
	if inst == nil {
		return err
	}
	r.builder.KeepPorts(t, held)
	_, err = r.spawn(ctx, t, inst)
	return err
}

// launch claims t's start slot, then builds its env and spawns it in mode. The
// claim comes before the env claims any port, so a refused start leaves a live
// instance's ports alone.
func (r *Runner) launch(ctx context.Context, t *discovery.Target, mode anovelv1.Mode) (*Instance, error) {
	if mode != anovelv1.Mode_MODE_CONTAINER {
		mode = anovelv1.Mode_MODE_GO_EXEC
	}
	inst, live, err := r.claim(t, mode)
	if inst == nil {
		return live, err
	}
	return r.spawn(ctx, t, inst)
}

// spawn builds the env of the claimed instance inst and starts it in the
// claimed mode. Every failure marks inst terminated with the error, which
// releases what the env claimed, so a caller waiting on inst sees the failure.
func (r *Runner) spawn(ctx context.Context, t *discovery.Target, inst *Instance) (*Instance, error) {
	start := r.startGoExec
	if inst.Mode == anovelv1.Mode_MODE_CONTAINER {
		start = r.startContainer
	}
	// The builder's snapshot fill picks up ports infra-up allocated, such as
	// POSTGRES_PORT, so POSTGRES_DSN synthesizes to localhost:<port>.
	entries, warnings, err := r.builder.ForTarget(t)
	if err != nil {
		r.markTerminated(inst.ID, anovelv1.ExitReason_EXIT_REASON_ERROR, "env build: "+err.Error())
		return nil, fmt.Errorf("%w: %w", ErrEnv, err)
	}
	return start(ctx, inst, t, env.Environ(entries), warnings)
}

// claim checks the start-time invariants and, when t may start, records a
// PENDING instance of it in the same critical section, so a concurrent start
// finds the slot taken. It returns the claimed instance, or, when t already
// runs in mode, nil and a copy of the live instance.
func (r *Runner) claim(t *discovery.Target, mode anovelv1.Mode) (*Instance, *Instance, error) {
	id := t.ID()
	r.mu.Lock()
	defer r.mu.Unlock()
	if live, running, err := canStart(r.instances[id], id, mode); err != nil || running {
		return nil, live, err
	}
	inst := &Instance{
		ID:        id,
		Target:    t.Name,
		Service:   t.Service,
		Stack:     t.Stack,
		Mode:      mode,
		Phase:     anovelv1.Phase_PHASE_PENDING,
		StartedAt: time.Now(),
	}
	r.instances[id] = inst
	return inst, nil, nil
}

// begin hands the claimed instance its process handle and moves it from PENDING
// to STARTING. A kill that came first left the instance STOPPING: begin then
// marks it killed and returns an error, and the caller spawns nothing. cmd is
// nil for a container.
func (r *Runner) begin(inst *Instance, cmd *exec.Cmd, cancel context.CancelFunc) error {
	r.mu.Lock()
	inst.cmd, inst.cancel = cmd, cancel
	r.mu.Unlock()
	if !r.advance(inst.ID, anovelv1.Phase_PHASE_PENDING, anovelv1.Phase_PHASE_STARTING) {
		r.markTerminated(inst.ID, anovelv1.ExitReason_EXIT_REASON_KILLED, "")
		return fmt.Errorf("%s: %w", inst.ID, errKilledWhileStarting)
	}
	return nil
}

// canStart enforces the start-time invariants on inst, the target's current
// record, or nil when there is none:
//   - running in the same mode is idempotent and returns the existing instance
//   - running in a different mode is refused, with a hint
//   - a stopping instance is refused; the caller waits or restarts
//
// It returns (existing-instance, true, nil) when the call is a no-op. The
// caller holds r.mu.
func canStart(inst *Instance, id string, mode anovelv1.Mode) (*Instance, bool, error) {
	if inst == nil {
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

// =============================================================================
// Stop / kill
// =============================================================================

// Kill stops the named instance. Idempotent: killing an already-terminated
// target returns nil. Sends SIGTERM, waits up to `grace`, then SIGKILL.
//
// `grace` of 0 means "SIGKILL immediately" (no grace period).
//
// An instance still starting has no process or container to stop yet. Kill
// then leaves it STOPPING and returns, and the start stops what it brings up
// and fails.
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
	// Held to close outside the lock. Closing it ends the log stream of a container, which can
	// outlive its instance.
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
