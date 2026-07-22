package runner

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Mode is how a target instance was spawned. Mirrors anovelv1.Mode but
// keeps the runner package independent of the proto types.
type Mode int

const (
	ModeGoExec    Mode = 1
	ModeContainer Mode = 2
)

func (m Mode) String() string {
	switch m {
	case ModeGoExec:
		return "go-exec"
	case ModeContainer:
		return "container"
	default:
		return "unknown"
	}
}

// Instance is one running (or recently-terminated) target. The runner owns
// every Instance; the server reads them through Runner methods.
type Instance struct {
	// Identity. Set at construction, immutable thereafter.
	ID      string // <stack>/<service>/<target>
	Target  string // bare target name (e.g., "rest")
	Service string
	Stack   string

	// Lifecycle. Mutated by the runner; readers must hold Runner.mu (RLock).
	Phase        anovelv1.Phase
	ExitReason   anovelv1.ExitReason
	Health       anovelv1.Health
	Mode         Mode
	PID          int32
	ContainerID  string
	StartedAt    time.Time
	TerminatedAt time.Time
	// LastErr captures the most recent non-fatal error observed by the
	// supervisor (e.g., spawn failure, wait error). Surfaced via the
	// LastError method.
	LastErr string

	// Process handle. nil after termination.
	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// Runner supervises every spawned target across every stack. A single RWMutex
// guards the instance map, making per-target state last-write-wins.
type Runner struct {
	mu        sync.RWMutex
	instances map[string]*Instance // keyed by Instance.ID

	// discovery translates a target ID into a discovery.Target at start time.
	discovery []*discovery.Stack
	// alloc is the port allocator. The runner releases an instance's
	// refcounted slots when it terminates. A nil allocator skips those
	// releases, which tests rely on.
	alloc *env.Allocator
	// builder synthesizes env blocks per target, for the infra path where
	// StartInfra auto-runs one-shots that each need an env.
	builder *env.Builder
	// logs is the per-target log store. The runner pipes go-exec stdout and
	// stderr through it; container logs stay in podman's journal.
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
func New(disc []*discovery.Stack, alloc *env.Allocator, builder *env.Builder, logStore *logs.Store) *Runner {
	return &Runner{
		instances:       make(map[string]*Instance),
		discovery:       disc,
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
// Lifecycle invariants — used by every Start path
// =============================================================================

// resolveTarget looks up a target by ID across every registered stack.
// Returns the discovery.Target along with its owning Service for context.
func (r *Runner) resolveTarget(id string) (*discovery.Target, *discovery.Service, error) {
	for _, st := range r.discovery {
		for _, svc := range st.Services {
			for _, t := range svc.Targets {
				if targetID(st.Name, svc.Name, t.Name) == id {
					return t, svc, nil
				}
			}
		}
	}
	return nil, nil, fmt.Errorf("unknown target %q", id)
}

// canStart enforces the start-time invariants:
//   - running in the same mode is idempotent and returns the existing instance
//   - running in a different mode is refused, with a hint
//   - a stopping instance is refused; the caller waits or restarts
//
// It returns (existing-instance, true, nil) when the call is a no-op.
func (r *Runner) canStart(id string, mode Mode) (*Instance, bool, error) {
	r.mu.RLock()
	inst, ok := r.instances[id]
	r.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	switch inst.Phase {
	case anovelv1.Phase_PHASE_RUNNING, anovelv1.Phase_PHASE_STARTING, anovelv1.Phase_PHASE_PENDING:
		if inst.Mode == mode {
			// Idempotent: same target, same mode, already up.
			return inst, true, nil
		}
		return nil, false, fmt.Errorf(
			"%s already running in %s mode (pid %d); kill it first or use `a-novel run restart %s --mode=%s`",
			id, inst.Mode, inst.PID, id, mode)
	case anovelv1.Phase_PHASE_STOPPING:
		return nil, false, fmt.Errorf("%s is currently stopping; wait for it to settle", id)
	case anovelv1.Phase_PHASE_TERMINATED:
		// Re-use the slot: replace the dead instance below.
		return nil, false, nil
	}
	return nil, false, nil
}

// targetID is the canonical "<stack>/<service>/<target>" form. Must match
// the convertTarget rule in internal/daemon/server/convert.go.
func targetID(stack, service, target string) string {
	return stack + "/" + service + "/" + target
}

// =============================================================================
// Stop / kill
// =============================================================================

// Kill stops the named instance. Idempotent: killing an already-terminated
// target returns nil. Sends SIGTERM, waits up to `grace`, then SIGKILL.
//
// `grace` of 0 means "SIGKILL immediately" (no grace period).
func (r *Runner) Kill(ctx context.Context, id string, grace time.Duration) error {
	r.mu.Lock()
	inst, ok := r.instances[id]
	if !ok {
		r.mu.Unlock()
		return nil // idempotent
	}
	if inst.Phase == anovelv1.Phase_PHASE_TERMINATED {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	// transition emits the *→STOPPING PhaseEvent, so Watch subscribers can show
	// "stopping" briefly before the eventual TERMINATED.
	r.transition(id, anovelv1.Phase_PHASE_STOPPING)
	r.mu.RLock()
	inst = r.instances[id]
	r.mu.RUnlock()

	switch inst.Mode {
	case ModeGoExec:
		return r.killGoExec(ctx, id, grace)
	case ModeContainer:
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
	// Release the allocator refcounts outside the runner's lock, so the two
	// locks never interleave.
	if r.alloc != nil {
		r.alloc.Release(id)
	}
	r.emitPhase(ev)
}
