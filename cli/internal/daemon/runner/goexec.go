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

// StartGoExec spawns target `id` as a `go run ./cmd/<target>` invocation inside
// the owning service's directory, with env as the process environment.
//
// It returns the resulting Instance. Idempotency for an already-running target,
// mutual exclusion against container mode, and the PENDING → STARTING → RUNNING
// transitions all happen here, so no RPC handler reimplements them.
func (r *Runner) StartGoExec(ctx context.Context, id string, env []string, warnings []string) (*Instance, error) {
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

	// 3. Build the command. Running go from the service directory picks up
	//    that service's own go.mod, and each target gets its own context so
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
	// Redirect stdout and stderr through the log store, which JSON-encodes
	// each line into the per-target current.log and fans it out to live
	// StreamLogs subscribers.
	logWriter, err := r.logs.OpenForWrite(id, tgt.Stack, tgt.Service, tgt.Name)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("open log writer for %s: %w", id, err)
	}
	cmd.Stdout = logWriter.Stdout()
	cmd.Stderr = logWriter.Stderr()
	// Put the value-free missing-secret warnings ahead of the target's own
	// output, so `run logs` and the TUI show the operator what to set.
	for _, w := range warnings {
		_, _ = fmt.Fprintln(logWriter.Stderr(), w)
	}

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
	// transition emits the STARTING→RUNNING PhaseEvent for Watch subscribers;
	// mutating inst.Phase directly here would lose the event.
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
			// Supervising `go run` means a SIGTERM to the process
			// group always surfaces as a signal exit, even when the
			// binary handled it and exited 0. A signal exit during a
			// daemon-initiated stop therefore counts as SUCCESS, and
			// CRASHED is reserved for signals nobody asked for, such
			// as the OOM killer or a manual `pkill`.
			r.mu.RLock()
			wasStopping := inst.Phase == anovelv1.Phase_PHASE_STOPPING
			r.mu.RUnlock()
			switch {
			case exitErr.Exited():
				// A non-zero status the process chose itself is a
				// real error, such as a panic or a bad config.
				reason = anovelv1.ExitReason_EXIT_REASON_ERROR
			case wasStopping:
				// The stop was requested and the process is gone,
				// whether SIGTERM or an escalated SIGKILL ended it.
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

	// Signal the whole process group through the negative pid, so the binary
	// `go run` compiled and spawned receives it too.
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

// transition applies an atomic phase change and emits a PhaseEvent so Watch
// subscribers observe it. A phase that already holds is a no-op, keeping
// redundant events off the subscriber channels.
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

// Keeps the discovery import referenced from this file.
var _ = discovery.TargetKindOneShot
