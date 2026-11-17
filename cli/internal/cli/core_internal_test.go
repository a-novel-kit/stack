package cli

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/stretchr/testify/require"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// fakeDaemon answers the RPCs stopDaemon issues. It runs until a Shutdown or a
// PrepareReinstall, then answers Ping as a missing socket does, unless it
// lingers.
type fakeDaemon struct {
	anovelv1connect.CoreServiceClient

	// down makes the daemon absent from the start.
	down bool
	// pingErr fails every Ping with an error other than a missing socket.
	pingErr error
	// failures are the targets Shutdown reports it could not stop.
	failures []string
	// lingers keeps the daemon answering after it agreed to stop.
	lingers bool

	stopped atomic.Bool
}

func (d *fakeDaemon) Ping(context.Context, *anovelv1.PingRequest) (*anovelv1.PingResponse, error) {
	switch {
	case d.pingErr != nil:
		return nil, d.pingErr
	case d.down, d.stopped.Load() && !d.lingers:
		return nil, connect.NewError(connect.CodeUnavailable, "no such file or directory")
	}
	return &anovelv1.PingResponse{}, nil
}

func (d *fakeDaemon) Shutdown(context.Context, *anovelv1.ShutdownRequest) (*anovelv1.ShutdownResponse, error) {
	d.stopped.Store(true)
	return &anovelv1.ShutdownResponse{GoExecKilled: 2, Failures: d.failures}, nil
}

func (d *fakeDaemon) PrepareReinstall(context.Context, *anovelv1.PrepareReinstallRequest) (*anovelv1.PrepareReinstallResponse, error) {
	d.stopped.Store(true)
	return &anovelv1.PrepareReinstallResponse{CheckpointPath: "/state/reinstall.json", GoExecTargetCount: 1}, nil
}

// TestStopDaemon pins the one failure policy of kill, restart,
// prepare-reinstall and install: a daemon that does not answer is no error,
// and a ping failure, a target the shutdown left running or a daemon that
// stays up fail the stop.
func TestStopDaemon(t *testing.T) {
	t.Parallel()

	errPing := connect.NewError(connect.CodeInternal, "handler panicked")
	testCases := []struct {
		name       string
		daemon     *fakeDaemon
		checkpoint bool

		wantStopped bool
		wantOut     []string
		wantErr     string
	}{
		{name: "Success/NotRunning", daemon: &fakeDaemon{down: true}},
		{
			name: "Success/Shutdown", daemon: &fakeDaemon{},
			wantStopped: true, wantOut: []string{"shutdown: 2 go-exec target(s) killed", "daemon stopped."},
		},
		{
			name: "Success/Checkpoint", daemon: &fakeDaemon{}, checkpoint: true,
			wantStopped: true, wantOut: []string{"checkpoint written: /state/reinstall.json", "daemon stopped."},
		},
		{name: "Error/Ping", daemon: &fakeDaemon{pingErr: errPing}, wantErr: "handler panicked"},
		{
			name: "Error/TargetsLeft", daemon: &fakeDaemon{failures: []string{"default/svc/rest"}},
			wantStopped: true, wantOut: []string{"daemon stopped."},
			wantErr: "shutdown could not stop 1 target(s): default/svc/rest",
		},
		// The wait ends with the command's context, which stands in for
		// daemonExitTimeout.
		{
			name: "Error/DaemonStaysUp", daemon: &fakeDaemon{lingers: true}, checkpoint: true,
			wantStopped: true, wantErr: context.DeadlineExceeded.Error(),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			var out strings.Builder
			stopped, err := stopDaemon(ctx, testCase.daemon, &out, testCase.checkpoint, false)
			require.Equal(t, testCase.wantStopped, stopped)
			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
			} else {
				require.NoError(t, err)
			}
			for _, line := range testCase.wantOut {
				require.Contains(t, out.String(), line)
			}
		})
	}
}
