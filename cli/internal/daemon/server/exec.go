package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"

	"connectrpc.com/connect/v2"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// Exec runs a command beside a target:
//
//   - A running container target runs it inside its container through
//     `podman exec`, so `run exec <service>/rest -- sh` opens a shell next to
//     the rest binary.
//   - Any other target runs it on the host, in the target's directory with
//     the target's env, so `run exec <service>/migrations -- psql` gets the
//     right DSN. The target need only be discovered: the env builder allocates
//     what is not allocated yet, so the command works with only the infra up.
//
// The output streams as LOG_STREAM_STDOUT and LOG_STREAM_STDERR lines, the
// LogStream values logs use, so a client renders both the same way.
func (s *Server) Exec(ctx context.Context, req *anovelv1.ExecRequest, stream anovelv1connect.CoreServiceExecServerStream) error {
	id := req.GetTargetId()
	cmdv := req.GetCmd()
	if len(cmdv) == 0 {
		return connect.NewError(connect.CodeInvalidArgument, "exec: empty cmd")
	}
	if id == "" {
		return connect.NewError(connect.CodeInvalidArgument, "exec: target_id required")
	}
	tgt, _, err := s.findTarget(id)
	if err != nil {
		return err
	}
	execCmd, err := s.siblingCommand(ctx, tgt, cmdv)
	if err != nil {
		return err
	}
	stdoutR, err := execCmd.StdoutPipe()
	if err != nil {
		return connect.Errorf(connect.CodeInternal, "exec: stdout pipe: %v", err).WithCause(err)
	}
	stderrR, err := execCmd.StderrPipe()
	if err != nil {
		return connect.Errorf(connect.CodeInternal, "exec: stderr pipe: %v", err).WithCause(err)
	}
	if err := execCmd.Start(); err != nil {
		return connect.Errorf(connect.CodeInternal, "exec: start: %v", err).WithCause(err)
	}
	// Two readers, one stream: a goroutine per pipe forwards its lines.
	output := &execOutput{stream: stream}
	var wg sync.WaitGroup
	wg.Go(func() { output.pump(stdoutR, anovelv1.LogStream_LOG_STREAM_STDOUT) })
	wg.Go(func() { output.pump(stderrR, anovelv1.LogStream_LOG_STREAM_STDERR) })
	wg.Wait()

	waitErr := execCmd.Wait()

	code, err := execExitCode(waitErr)
	if err != nil {
		return connect.Errorf(connect.CodeInternal, "exec: wait: %v", err).WithCause(err)
	}

	if waitErr != nil {
		// The human-readable line survives being piped to a file, where the
		// process exit status does not. It rides the same stream as the
		// output, after everything the child wrote.
		output.send(&anovelv1.ExecResponse{Stream: anovelv1.LogStream_LOG_STREAM_STDERR, Line: "[exec exited: " + waitErr.Error() + "]"})
	}
	// The terminal message carries the exit code and no line, so the client can
	// propagate the child's status as its own.
	output.send(&anovelv1.ExecResponse{ExitCode: &code})

	return nil
}

// siblingCommand builds the command Exec runs beside tgt. A running container
// instance decides the mode. Without one, go-exec is the only mode that works,
// there being no live container to attach to.
func (s *Server) siblingCommand(ctx context.Context, tgt *discovery.Target, cmdv []string) (*exec.Cmd, error) {
	if inst, ok := s.runner.Instance(tgt.ID()); ok && inst.Phase == anovelv1.Phase_PHASE_RUNNING &&
		inst.Mode == anovelv1.Mode_MODE_CONTAINER {
		if inst.ContainerID == "" {
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				"exec: target running in container mode but containerId is empty")
		}
		return exec.CommandContext(ctx, "podman", append([]string{"exec", inst.ContainerID}, cmdv...)...), nil
	}
	// A go-exec sibling gets the same directory and env as the target, so
	// relative paths match what the target sees. The builder allocates when
	// needed under the target's own consumer ID, which the usual refcounting
	// releases.
	envEntries, _, err := s.envBuilder.ForTarget(tgt)
	if err != nil {
		return nil, connect.Errorf(connect.CodeInternal, "exec: env: %v", err).WithCause(err)
	}
	cmd := exec.CommandContext(ctx, cmdv[0], cmdv[1:]...)
	cmd.Dir = tgt.ServiceDir()
	cmd.Env = env.Environ(envEntries)
	// A cancelled Exec kills the command's whole process group. A background
	// job it started holds the output pipes, and so the handler, until it ends.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	return cmd, nil
}

// execOutput sends a child's output on an Exec stream. Stream.Send is unsafe
// for concurrent calls, so every send holds mu.
type execOutput struct {
	mu     sync.Mutex
	stream anovelv1connect.CoreServiceExecServerStream
}

func (o *execOutput) send(msg *anovelv1.ExecResponse) {
	o.mu.Lock()
	defer o.mu.Unlock()
	_ = o.stream.Send(msg)
}

// pump sends each line of r tagged with stream.
func (o *execOutput) pump(r io.Reader, stream anovelv1.LogStream) {
	for sc := lineScanner(r); sc.Scan(); {
		o.send(&anovelv1.ExecResponse{Stream: stream, Line: sc.Text()})
	}
}

// execExitCode maps a [exec.Cmd.Wait] error onto the child's exit status.
//
// A non-nil error that is not an [exec.ExitError] comes from a spawn or IO
// failure, where the child's status was never obtained. That error is returned
// to the caller, and only a real [exec.ExitError] yields a code.
func execExitCode(waitErr error) (int32, error) {
	if waitErr == nil {
		return 0, nil
	}

	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		return 0, waitErr
	}

	return int32(exitErr.ExitCode()), nil
}

// Debug returns instructions for attaching Delve to a running go-exec target.
// The user runs the printed command in their own terminal, so the dlv REPL
// stays interactive and a disconnect never kills the target.
//
// A container-mode target is rejected: attaching to a containerized process
// needs an in-image dlv and a port-forward the daemon does not manage.
//
// The hint's listen port is a suggestion the daemon never binds. It is
// `PID + 20000`, high but unprivileged, so debugging several targets gives a
// unique port per PID with no allocation bookkeeping.
func (s *Server) Debug(_ context.Context, req *anovelv1.DebugRequest) (*anovelv1.DebugResponse, error) {
	id := req.GetTargetId()
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, "debug: target_id required")
	}
	inst, ok := s.runner.Instance(id)
	if !ok {
		return nil, connect.Errorf(connect.CodeNotFound, "debug: target %q not tracked (start it first)", id)
	}
	if inst.Phase != anovelv1.Phase_PHASE_RUNNING {
		return nil, connect.Errorf(connect.CodeFailedPrecondition,
			"debug: target %q is not running (phase=%s)", id, inst.Phase)
	}
	if inst.Mode != anovelv1.Mode_MODE_GO_EXEC {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			"debug: only go-exec targets can be attached to (container-mode dlv requires in-image dlv + port-forward)")
	}
	port := inst.PID + 20000
	return &anovelv1.DebugResponse{Hint: fmt.Sprintf(
		"Attach with:\n  dlv attach %d --listen=:%d --headless --api-version=2\nThen connect from your editor (vscode: 'Connect to server' on port %d).",
		inst.PID, port, port,
	)}, nil
}
