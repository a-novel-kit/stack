package runner

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestRunning pins the gate after a go-exec spawn. A started process moves to
// RUNNING. A kill that reached the instance before its PID existed found
// nothing to signal, so running ends the process and fails the start.
func TestRunning(t *testing.T) {
	testCases := []struct {
		name string
		// phase is the instance's phase when its process has started:
		// STOPPING after a kill that found no PID.
		phase anovelv1.Phase

		wantErr   error
		wantPhase anovelv1.Phase
	}{
		{name: "Success", phase: anovelv1.Phase_PHASE_STARTING, wantPhase: anovelv1.Phase_PHASE_RUNNING},
		{
			name:      "Error/KilledWhileStarting",
			phase:     anovelv1.Phase_PHASE_STOPPING,
			wantErr:   errKilledWhileStarting,
			wantPhase: anovelv1.Phase_PHASE_TERMINATED,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// The watcher closes a log writer, which lives under XDG_STATE_HOME.
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			const id = "default/svc/rest"
			cmd := exec.CommandContext(t.Context(), "sleep", "30")
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			logWriter, err := logs.New().OpenForWrite(id, "default", "svc", "rest")
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			inst := &Instance{ID: id, Service: "svc", Stack: "default", Mode: anovelv1.Mode_MODE_GO_EXEC, Phase: testCase.phase, cmd: cmd}
			r := &Runner{instances: map[string]*Instance{id: inst}}
			go r.watchGoExec(inst, cmd, logWriter)
			t.Cleanup(func() { _ = r.Kill(context.Background(), id, 0) })

			err = r.running(t.Context(), inst, cmd.Process.Pid)

			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("running: got %v, want %v", err, testCase.wantErr)
			}
			if got, _ := r.Instance(id); got.Phase != testCase.wantPhase {
				t.Errorf("phase = %v, want %v", got.Phase, testCase.wantPhase)
			}
		})
	}
}
