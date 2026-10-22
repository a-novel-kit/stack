package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestStartContainer covers a kill that arrives during `compose up`. The kill
// finds no container yet and leaves the target STOPPING. When compose returns,
// the start stops the container it brought up and fails, so the killed target
// never serves.
func TestStartContainer(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// A stand-in podman: compose up waits for the test's release file, ps
	// names one container, and stop records its arguments.
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
compose) : > %[1]s/composing; while [ ! -e %[1]s/release ]; do sleep 0.05; done ;;
ps) echo cid123 ;;
stop) echo "$@" >> %[1]s/stops ;;
*) exit 1 ;;
esac
`, dir)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "podman"), []byte(script), 0o700); err != nil { //nolint:gosec // an executable fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	r, _, tgt := newRunnerForLaunch()

	started := make(chan error, 1)
	go func() {
		_, err := r.launch(t.Context(), tgt, anovelv1.Mode_MODE_CONTAINER)
		started <- err
	}()
	waitForFile(t, filepath.Join(dir, "composing"))

	if err := r.Kill(t.Context(), tgt.ID(), 0); err != nil {
		t.Fatalf("Kill during compose up: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-started:
		if !errors.Is(err, errKilledWhileStarting) {
			t.Fatalf("launch: got %v, want %v", err, errKilledWhileStarting)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("launch still running 10s after compose up returned")
	}
	if inst, _ := r.Instance(tgt.ID()); inst.Phase != anovelv1.Phase_PHASE_TERMINATED || inst.ExitReason != anovelv1.ExitReason_EXIT_REASON_KILLED {
		t.Errorf("record = (%v, %v), want TERMINATED with KILLED", inst.Phase, inst.ExitReason)
	}
	if stops, _ := os.ReadFile(filepath.Join(dir, "stops")); !strings.Contains(string(stops), "cid123") {
		t.Errorf("podman stop calls = %q, want a stop of cid123", stops)
	}
}

// waitForFile polls until path exists, failing the test after 10s.
func waitForFile(t *testing.T, path string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
	t.Fatalf("%s never appeared", path)
}

// TestUpdateContainerState pins that a container phase podman reports emits its
// PhaseEvent, as every other phase change does, so `run watch` and
// `ps --watch` follow container targets. A terminated instance never revives,
// and a stopping one never goes back to running.
func TestUpdateContainerState(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		phase anovelv1.Phase
		// state is the container state podman reports.
		state string

		wantPhase anovelv1.Phase
		wantEvent bool
	}{
		{name: "Success/Running", phase: anovelv1.Phase_PHASE_STARTING, state: "running", wantPhase: anovelv1.Phase_PHASE_RUNNING, wantEvent: true},
		{name: "Success/Stopping", phase: anovelv1.Phase_PHASE_RUNNING, state: "stopping", wantPhase: anovelv1.Phase_PHASE_STOPPING, wantEvent: true},
		{name: "Success/Restarting", phase: anovelv1.Phase_PHASE_RUNNING, state: "created", wantPhase: anovelv1.Phase_PHASE_STARTING, wantEvent: true},
		{name: "Success/Unchanged", phase: anovelv1.Phase_PHASE_RUNNING, state: "running", wantPhase: anovelv1.Phase_PHASE_RUNNING},
		{name: "Success/StoppingStays", phase: anovelv1.Phase_PHASE_STOPPING, state: "running", wantPhase: anovelv1.Phase_PHASE_STOPPING},
		{name: "Success/TerminatedStays", phase: anovelv1.Phase_PHASE_TERMINATED, state: "running", wantPhase: anovelv1.Phase_PHASE_TERMINATED},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const id = "default/svc/rest"
			inst := &Instance{ID: id, Service: "svc", Stack: "default", Mode: anovelv1.Mode_MODE_CONTAINER, Phase: testCase.phase}
			r := &Runner{instances: map[string]*Instance{id: inst}}
			events, unsubscribe := r.SubscribePhases(nil)
			defer unsubscribe()

			r.updateContainerState(id, testCase.state, "")

			if inst.Phase != testCase.wantPhase {
				t.Errorf("phase = %v, want %v", inst.Phase, testCase.wantPhase)
			}
			// emitPhase sends before updateContainerState returns, so an event
			// that is not buffered now was never emitted.
			select {
			case ev := <-events:
				if !testCase.wantEvent || ev.OldPhase != testCase.phase || ev.NewPhase != testCase.wantPhase {
					t.Errorf("event %v→%v, want event %v for %v→%v", ev.OldPhase, ev.NewPhase, testCase.wantEvent, testCase.phase, testCase.wantPhase)
				}
			default:
				if testCase.wantEvent {
					t.Errorf("no event, want %v→%v", testCase.phase, testCase.wantPhase)
				}
			}
		})
	}
}
