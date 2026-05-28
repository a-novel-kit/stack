package runner

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// StartGoExec spawns target `id` as a `go run ./cmd/<target>` invocation
// inside the owning service's directory, with `env` as the process
// environment (callers — typically the env-synthesis layer in phase 4 —
// build this list).
//
// Returns the resulting Instance. Already-running idempotency, mutual
// exclusion vs container mode, and the PHASE_PENDING → PHASE_STARTING →
// PHASE_RUNNING transitions are handled here so callers don't reimplement
// them per RPC.
func (r *Runner) StartGoExec(ctx context.Context, id string, env []string) (*Instance, error) {
	// 1. Resolve target metadata from discovery.
	tgt, svc, err := r.resolveTarget(id)
	if err != nil {
		return nil, err
	}
	_ = svc

	// 2. Invariant checks (mutual exclusion, already-running idempotency).
	if existing, idempotent, err := r.canStart(id, ModeGoExec); err != nil {
		return nil, err
	} else if idempotent {
		out := *existing
		return &out, nil
	}

	// 3. Build the command. We use the service's own go.mod context by
	//    invoking go from the service directory (cmd/<target>/main.go is
	//    resolved relative to that). Each target gets its own context so
	//    Kill can cancel cleanly.
	procCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(procCtx, "go", "run", "./cmd/"+tgt.Name)
	// CmdDir is `.../service-X/cmd/<target>/`. Service dir is its
	// grandparent — that's where go.mod lives.
	cmd.Dir = filepath.Dir(filepath.Dir(tgt.CmdDir))
	cmd.Env = env
	// New process group so Kill can take down the entire subtree
	// (`go run` itself spawns a temp build + the actual binary).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Redirect stdout/stderr through the log store. Each line is
	// JSON-encoded ({ts, stream, line}) into the per-target current.log
	// AND fanned out to any live subscribers (StreamLogs RPC).
	logWriter, err := r.logs.OpenForWrite(id, tgt.Stack, tgt.Service, tgt.Name)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open log writer for %s: %w", id, err)
	}
	cmd.Stdout = logWriter.Stdout()
	cmd.Stderr = logWriter.Stderr()

	// 4. Register the instance in PENDING so concurrent Start callers see
	//    the slot taken before we exec.
	now := time.Now()
	inst := &Instance{
		ID:        id,
		Target:    tgt.Name,
		Service:   tgt.Service,
		Stack:     tgt.Stack,
		Phase:     anovelv1.Phase_PHASE_PENDING,
		Mode:      ModeGoExec,
		StartedAt: now,
		cmd:       cmd,
		cancel:    cancel,
	}
	r.mu.Lock()
	r.instances[id] = inst
	r.mu.Unlock()

	// 5. Transition to STARTING + actually exec.
	r.transition(id, anovelv1.Phase_PHASE_STARTING)
	if err := cmd.Start(); err != nil {
		cancel()
		_ = logWriter.Close()
		r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_ERROR, "spawn: "+err.Error())
		return nil, fmt.Errorf("start %s: %w", id, err)
	}
	// 6. PID known; transition to RUNNING and start the watcher.
	r.mu.Lock()
	inst.PID = int32(cmd.Process.Pid)
	r.mu.Unlock()
	// transition() emits the STARTING→RUNNING PhaseEvent for Watch
	// subscribers; don't mutate inst.Phase directly here or the event is
	// lost.
	r.transition(id, anovelv1.Phase_PHASE_RUNNING)
	go r.watchGoExec(id, logWriter)

	out := *inst
	return &out, nil
}

// watchGoExec blocks on cmd.Wait() and transitions the instance to
// TERMINATED with the appropriate ExitReason. The log writer is closed
// here so its file handle + subscriber channels release cleanly.
func (r *Runner) watchGoExec(id string, logWriter *logs.Writer) {
	r.mu.RLock()
	inst, ok := r.instances[id]
	cmd := inst.cmd
	r.mu.RUnlock()
	if !ok || cmd == nil {
		_ = logWriter.Close()
		return
	}
	err := cmd.Wait()
	_ = logWriter.Close()
	reason := anovelv1.ExitReason_EXIT_REASON_SUCCESS
	msg := ""
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Classify based on (a) how the process ended and
			// (b) whether we asked for it. Going via `go run` —
			// the parent process supervised here — means SIGTERM
			// sent to the process group always shows up as a
			// signal exit, even when the actual binary handled it
			// cleanly and exited 0. The fix: treat any
			// signal-exit during a daemon-initiated stop as
			// SUCCESS — clean shutdown by request. CRASHED is
			// reserved for signals received without us asking
			// (e.g., OOMKiller, manual `pkill`).
			r.mu.RLock()
			wasStopping := inst.Phase == anovelv1.Phase_PHASE_STOPPING
			r.mu.RUnlock()
			switch {
			case exitErr.Exited():
				// Process exited with a non-zero status code on
				// its own — a real error (panic, config issue,
				// etc.). Surface as ERROR for both kinds.
				reason = anovelv1.ExitReason_EXIT_REASON_ERROR
			case wasStopping:
				// We asked for the stop, the process is gone.
				// Whether SIGTERM caught the binary mid-graceful
				// or had to escalate to SIGKILL, the user-visible
				// outcome is the same: "done".
				reason = anovelv1.ExitReason_EXIT_REASON_SUCCESS
			default:
				reason = anovelv1.ExitReason_EXIT_REASON_CRASHED
			}
			msg = err.Error()
		} else {
			reason = anovelv1.ExitReason_EXIT_REASON_CRASHED
			msg = err.Error()
		}
	}
	r.markTerminated(id, reason, msg)
}

// killGoExec implements the SIGTERM → wait grace → SIGKILL escalation for
// a go-exec target. Returns once the process is reaped or the grace expires.
func (r *Runner) killGoExec(ctx context.Context, id string, grace time.Duration) error {
	r.mu.RLock()
	inst, ok := r.instances[id]
	r.mu.RUnlock()
	if !ok || inst.cmd == nil || inst.cmd.Process == nil {
		return nil
	}
	pid := inst.cmd.Process.Pid

	// SIGTERM the whole process group (we spawned with Setpgid). Use the
	// negative-pid signal target so children of `go run` (the compiled
	// binary in particular) get the message.
	if grace > 0 {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		// Poll for termination up to grace.
		deadline := time.Now().Add(grace)
		for time.Now().Before(deadline) {
			r.mu.RLock()
			done := inst.Phase == anovelv1.Phase_PHASE_TERMINATED
			r.mu.RUnlock()
			if done {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
	}

	// SIGKILL.
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	// Brief wait for the watcher to mark terminated.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.RLock()
		done := inst.Phase == anovelv1.Phase_PHASE_TERMINATED
		r.mu.RUnlock()
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return nil
}

// transition is a small helper for atomic phase changes when there's
// nothing else to update. Emits a PhaseEvent on every actual change so
// Watch subscribers observe the transition. No-op when the phase didn't
// actually change (avoids spamming subscribers with redundant events).
func (r *Runner) transition(id string, phase anovelv1.Phase) {
	r.mu.Lock()
	inst, ok := r.instances[id]
	if !ok || inst.Phase == phase {
		r.mu.Unlock()
		return
	}
	old := inst.Phase
	inst.Phase = phase
	ev := PhaseEvent{
		TargetID: inst.ID,
		Service:  inst.Service,
		Stack:    inst.Stack,
		OldPhase: old,
		NewPhase: phase,
	}
	r.mu.Unlock()
	r.emitPhase(ev)
}

// Silence "imported and not used: discovery" — we'll use it shortly for
// container mode / dep resolution.
var _ = discovery.TargetKindOneShot
