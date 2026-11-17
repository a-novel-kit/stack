package server

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
)

// TestSiblingCommand pins that a cancelled Exec ends the command's whole
// process tree. A shell's background job inherits the output pipe, so killing
// the shell alone leaves the pipe, and the Exec handler reading it, open until
// the job ends.
func TestSiblingCommand(t *testing.T) {
	t.Parallel()

	cmdDir := filepath.Join(t.TempDir(), "service-svc", "cmd", "rest")
	require.NoError(t, os.MkdirAll(cmdDir, 0o700))
	tgt := &discovery.Target{Name: "rest", Service: "svc", Stack: "default", CmdDir: cmdDir}
	stacks := discovery.Stacks{{
		Name:     "default",
		Default:  true,
		Services: []*discovery.Service{{Name: "svc", Stack: "default", Targets: []*discovery.Target{tgt}}},
	}}
	alloc := env.NewAllocator()
	alloc.SetServices([]string{"svc"})
	builder := env.NewBuilder(alloc)
	s := &Server{runner: runner.New(stacks, alloc, builder, logs.New()), envBuilder: builder}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd, err := s.siblingCommand(ctx, tgt, []string{"sh", "-c", "sleep 30 & echo started; wait"})
	require.NoError(t, err)
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Wait() })
	reader := bufio.NewReader(out)
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "started\n", line)

	cancel()

	closed := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, reader)
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		require.Fail(t, "the output pipe is still open 5s after the cancel: a background job outlived the Exec")
	}
}

// TestExecExitCode covers the wait-error to exit-status mapping the Exec
// stream's terminal message carries. A wait failure yields no status at all,
// so the mapping returns the error.
func TestExecExitCode(t *testing.T) {
	t.Parallel()

	// Real *exec.ExitError values, since ExitCode reads the platform
	// ProcessState.
	runExit := func(code int) error {
		return exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run() //nolint:gosec
	}
	errPipe := errors.New("pipe closed")

	testCases := []struct {
		name    string
		in      error
		want    int32
		wantErr error
	}{
		{name: "Success/CleanExit", in: nil, want: 0},
		{name: "Success/NonZeroExit", in: runExit(3), want: 3},
		// A signal ends the child as a shell reports it: 128 + SIGTERM.
		{name: "Success/Signal", in: exec.Command("sh", "-c", "kill -TERM $$").Run(), want: 143},
		// Not an *exec.ExitError: the child's status was never obtained.
		{name: "Error/WaitFailure", in: errPipe, want: 0, wantErr: errPipe},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := execExitCode(testCase.in)
			require.ErrorIs(t, err, testCase.wantErr)
			require.Equal(t, testCase.want, got)
		})
	}
}
