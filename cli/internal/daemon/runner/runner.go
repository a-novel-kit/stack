// Package runner owns the daemon-side process supervision: spawning targets
// in either go-exec or container mode, tracking each instance's lifecycle
// (phase / exit-reason / health), and enforcing the invariants spec §5
// requires (mutual exclusion, dependency-walk gating, idempotency).
//
// The package exposes one type — Runner — and a set of typed methods. The
// daemon's server package calls into it from the RPC handlers; the runner
// in turn is split across files by concern:
//
//	runner.go    — Runner type, tracker, lifecycle invariants
//	goexec.go    — go-exec spawn/wait/kill plumbing
//	container.go — container mode (added in a follow-up chunk)
//
// Internal state is guarded by a single sync.RWMutex (Runner.mu). For local
// dev with a handful of targets the contention is negligible and the
// reasoning is simple (last-write-wins per target).
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

// Runner supervises every spawned target across every stack.
type Runner struct {
	mu        sync.RWMutex
	instances map[string]*Instance // keyed by Instance.ID

	// discovery is queried to translate a target ID into a discovery.Target
	// at start time (we need the cmd dir, dockerfile path, deps, etc.).
	discovery []*discovery.Stack
	// alloc is the port allocator. The runner calls alloc.Release(id)
	// when an instance terminates so refcounted slots free correctly.
	// nil-safe: if no allocator is passed, releases are skipped (useful
	// for tests).
	alloc *env.Allocator
	// builder synthesizes env blocks per target. Used by the infra path
	// (StartInfra auto-runs one-shots, each of which needs an env).
	builder *env.Builder
	// logs is the per-target log store. The runner pipes go-exec
	// stdout/stderr through it; container mode currently leaves logs in
	// podman's journal (the container-log-piping is a phase-5 follow-up).
	logs *logs.Store

	// sessMu guards infraSessions. Separate from mu so the (sometimes
	// long) `compose up` invocations don't block per-instance state
	// reads in the rest of the package.
	sessMu        sync.RWMutex
	infraSessions map[string]*infraSession // keyed by sessionKey(stack, service)
}

// New returns an empty Runner with the discovery snapshot it'll resolve
// target IDs against, the env allocator for refcount release on
// termination, the env builder for one-shot env synthesis, and the log
// store for capturing target stdout/stderr.
func New(disc []*discovery.Stack, alloc *env.Allocator, builder *env.Builder, logStore *logs.Store) *Runner {
	return &Runner{
		instances:     make(map[string]*Instance),
		discovery:     disc,
		alloc:         alloc,
		builder:       builder,
		logs:          logStore,
		infraSessions: make(map[string]*infraSession),
	}
}

// Instance returns the live record for id, or false if no instance is
// known for it. Caller does not need to hold the lock — the method copies
// the relevant fields before returning so the caller has a stable snapshot.
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

// canStart enforces the start-time invariants in spec §5.3:
//   - already-running in the same mode → idempotent (return existing)
//   - already-running in a different mode → refuse with hint
//   - currently stopping → refuse (caller should wait or use restart)
//
// Returns (existing-instance, true, nil) when the call is a no-op.
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
	inst.Phase = anovelv1.Phase_PHASE_STOPPING
	r.mu.Unlock()

	// Mode-specific stop dispatch.
	switch inst.Mode {
	case ModeGoExec:
		return r.killGoExec(ctx, id, grace)
	case ModeContainer:
		return r.killContainer(ctx, id, grace)
	default:
		return fmt.Errorf("kill: unknown mode for %s", id)
	}
}

// markTerminated transitions the instance into PHASE_TERMINATED with the
// supplied reason. Idempotent — calling on an already-terminated instance
// is a no-op so the watch goroutine racing kill() doesn't corrupt state.
// Also releases the instance's env-allocation refcounts.
func (r *Runner) markTerminated(id string, reason anovelv1.ExitReason, errMsg string) {
	r.mu.Lock()
	inst, ok := r.instances[id]
	if !ok || inst.Phase == anovelv1.Phase_PHASE_TERMINATED {
		r.mu.Unlock()
		return
	}
	inst.Phase = anovelv1.Phase_PHASE_TERMINATED
	inst.ExitReason = reason
	inst.TerminatedAt = time.Now()
	if errMsg != "" {
		inst.LastErr = errMsg
	}
	inst.cmd = nil
	inst.cancel = nil
	r.mu.Unlock()
	// Release allocator refcounts OUTSIDE the runner's lock — the
	// allocator has its own lock and we don't want to interleave them.
	if r.alloc != nil {
		r.alloc.Release(id)
	}
}
