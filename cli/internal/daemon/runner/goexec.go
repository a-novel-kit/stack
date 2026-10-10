package runner

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// startGoExec spawns t as a `go run ./cmd/<target>` invocation inside the
// owning service's directory, with env as the process environment, and moves
// it through PENDING, STARTING, and RUNNING.
func (r *Runner) startGoExec(_ context.Context, t *discovery.Target, env, warnings []string) (*Instance, error) {
	id := t.ID()
	// Running go from the service directory picks up that service's own
	// go.mod, and each target gets its own context so Kill can cancel cleanly.
	procCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(procCtx, "go", "run", "./cmd/"+t.Name)
	cmd.Dir = t.ServiceDir()
	cmd.Env = env
	// New process group so Kill can take down the entire subtree
	// (`go run` itself spawns a temp build + the actual binary).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Redirect stdout and stderr through the log store, which JSON-encodes
	// each line into the per-target current.log and fans it out to live
	// StreamLogs subscribers.
	logWriter, err := r.logs.OpenForWrite(id, t.Stack, t.Service, t.Name)
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

	// Register the instance in PENDING so concurrent Start callers see the
	// slot taken before the exec.
	inst := r.register(t, anovelv1.Mode_MODE_GO_EXEC, cmd, cancel)
	r.transition(id, anovelv1.Phase_PHASE_STARTING)
	if err := cmd.Start(); err != nil {
		_ = logWriter.Close()
		r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_ERROR, "spawn: "+err.Error())
		return nil, fmt.Errorf("start %s: %w", id, err)
	}
	r.mu.Lock()
	inst.PID = int32(cmd.Process.Pid)
	r.mu.Unlock()
	r.transition(id, anovelv1.Phase_PHASE_RUNNING)
	go r.watchGoExec(inst, cmd, logWriter)

	r.mu.RLock()
	defer r.mu.RUnlock()
	out := *inst
	return &out, nil
}

// watchGoExec blocks on cmd.Wait() and transitions the instance to
// TERMINATED with the appropriate ExitReason. The log writer is closed
// here so its file handle + subscriber channels release cleanly.
func (r *Runner) watchGoExec(inst *Instance, cmd *exec.Cmd, logWriter *logs.Writer) {
	err := cmd.Wait()
	_ = logWriter.Close()
	reason := anovelv1.ExitReason_EXIT_REASON_SUCCESS
	msg := ""
	if err != nil {
		msg = err.Error()
		reason = anovelv1.ExitReason_EXIT_REASON_CRASHED
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
			}
		}
	}
	r.markTerminated(inst.ID, reason, msg)
}

// killGoExec implements the SIGTERM → wait grace → SIGKILL escalation for
// a go-exec target. Returns once the process is reaped or the grace expires.
func (r *Runner) killGoExec(ctx context.Context, id string, grace time.Duration) error {
	r.mu.RLock()
	inst, ok := r.instances[id]
	var cmd *exec.Cmd
	if ok {
		cmd = inst.cmd
	}
	r.mu.RUnlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid

	// Signal the whole process group through the negative pid, so the binary
	// `go run` compiled and spawned receives it too.
	if grace > 0 {
		_ = syscall.Kill(-pid, syscall.SIGTERM)
		if done, err := r.awaitTerminated(ctx, inst, grace); done || err != nil {
			return err
		}
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	// Brief wait for the watcher to mark terminated.
	_, err := r.awaitTerminated(ctx, inst, 2*time.Second)
	return err
}

// awaitTerminated polls inst every 50ms until it reaches TERMINATED or timeout
// passes, reporting whether it did. It returns ctx's error when ctx ends first.
func (r *Runner) awaitTerminated(ctx context.Context, inst *Instance, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r.mu.RLock()
		done := inst.Phase == anovelv1.Phase_PHASE_TERMINATED
		r.mu.RUnlock()
		if done {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return false, nil
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
	ev := PhaseEvent{
		TargetID: inst.ID,
		Service:  inst.Service,
		Stack:    inst.Stack,
		OldPhase: inst.Phase,
		NewPhase: phase,
	}
	inst.Phase = phase
	r.mu.Unlock()
	r.emitPhase(ev)
}
