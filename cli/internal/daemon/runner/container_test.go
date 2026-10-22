package runner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// fakePodmanInspect puts on PATH a stand-in podman whose inspect prints, for
// each container ID, the line held in the file of that name under the returned
// directory. An ID with no file fails the call with exit 125 after the other
// lines, as podman does. Every call appends its arguments to the directory's
// calls file.
func fakePodmanInspect(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %[1]s/calls
shift 2
status=0
for cid in "$@"; do
	cat %[1]s/"$cid" 2>/dev/null || status=125
done
exit $status
`, dir)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "podman"), []byte(script), 0o700); err != nil { //nolint:gosec // an executable fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// podmanCalls counts the calls a fakePodmanInspect directory recorded.
func podmanCalls(t *testing.T, dir string) int {
	t.Helper()
	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	return strings.Count(string(calls), "\n")
}

// TestCheckContainers covers one tick of the poller. A single podman call
// inspects every container, and each line reaches its own instance. A vanished
// container fails that call, and one inspect per container then ends only the
// vanished one, as crashed. A result that comes back after a restart leaves the
// target's new instance alone.
func TestCheckContainers(t *testing.T) {
	type container struct {
		// phase is the instance's phase before the check.
		phase anovelv1.Phase
		// inspect is the line podman prints for the container, "" once it
		// vanished.
		inspect string
		// restarted marks an instance that a kill and a restart replaced
		// while the check ran. The new instance has no container yet.
		restarted bool

		wantPhase  anovelv1.Phase
		wantReason anovelv1.ExitReason
	}

	testCases := []struct {
		name string
		// containers is keyed by container ID.
		containers map[string]container

		wantCalls int
	}{
		{
			name: "Success",
			containers: map[string]container{
				"starting": {phase: anovelv1.Phase_PHASE_STARTING, inspect: "running|healthy|0", wantPhase: anovelv1.Phase_PHASE_RUNNING},
				"done": {
					phase: anovelv1.Phase_PHASE_RUNNING, inspect: "exited|-|0",
					wantPhase: anovelv1.Phase_PHASE_TERMINATED, wantReason: anovelv1.ExitReason_EXIT_REASON_SUCCESS,
				},
				"failed": {
					phase: anovelv1.Phase_PHASE_RUNNING, inspect: "exited|-|1",
					wantPhase: anovelv1.Phase_PHASE_TERMINATED, wantReason: anovelv1.ExitReason_EXIT_REASON_ERROR,
				},
				"killed": {
					phase: anovelv1.Phase_PHASE_STOPPING, inspect: "exited|-|137",
					wantPhase: anovelv1.Phase_PHASE_TERMINATED, wantReason: anovelv1.ExitReason_EXIT_REASON_KILLED,
				},
			},
			wantCalls: 1,
		},
		{
			name: "Error/Vanished",
			containers: map[string]container{
				"running": {phase: anovelv1.Phase_PHASE_RUNNING, inspect: "running|healthy|0", wantPhase: anovelv1.Phase_PHASE_RUNNING},
				"gone": {
					phase:     anovelv1.Phase_PHASE_RUNNING,
					wantPhase: anovelv1.Phase_PHASE_TERMINATED, wantReason: anovelv1.ExitReason_EXIT_REASON_CRASHED,
				},
			},
			// The batch, then one inspect per container.
			wantCalls: 3,
		},
		{
			name: "Success/Restarted",
			containers: map[string]container{
				"exited": {phase: anovelv1.Phase_PHASE_PENDING, inspect: "exited|-|1", restarted: true, wantPhase: anovelv1.Phase_PHASE_PENDING},
				"gone":   {phase: anovelv1.Phase_PHASE_PENDING, restarted: true, wantPhase: anovelv1.Phase_PHASE_PENDING},
			},
			wantCalls: 3,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := fakePodmanInspect(t)
			r := &Runner{instances: map[string]*Instance{}}
			watches := make([]containerWatch, 0, len(testCase.containers))
			for cid, c := range testCase.containers {
				if c.inspect != "" {
					if err := os.WriteFile(filepath.Join(dir, cid), []byte(c.inspect+"\n"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				id := "default/svc/" + cid
				inst := &Instance{ID: id, Mode: anovelv1.Mode_MODE_CONTAINER, Phase: c.phase, ContainerID: cid}
				if c.restarted {
					inst.ContainerID = ""
				}
				r.instances[id] = inst
				watches = append(watches, containerWatch{id: id, cid: cid})
			}

			r.checkContainers(watches)

			if calls := podmanCalls(t, dir); calls != testCase.wantCalls {
				t.Errorf("podman calls = %d, want %d", calls, testCase.wantCalls)
			}
			for cid, c := range testCase.containers {
				inst := r.instances["default/svc/"+cid]
				if inst.Phase != c.wantPhase || inst.ExitReason != c.wantReason {
					t.Errorf("%s: record = (%v, %v), want (%v, %v)", cid, inst.Phase, inst.ExitReason, c.wantPhase, c.wantReason)
				}
			}
		})
	}
}

// TestWatchContainer pins that a new container is checked at once, so one that
// exits right away ends its instance without waiting a tick.
func TestWatchContainer(t *testing.T) {
	dir := fakePodmanInspect(t)
	if err := os.WriteFile(filepath.Join(dir, "cid123"), []byte("exited|-|0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const id = "default/svc/rest"
	r := &Runner{instances: map[string]*Instance{
		id: {ID: id, Mode: anovelv1.Mode_MODE_CONTAINER, Phase: anovelv1.Phase_PHASE_RUNNING, ContainerID: "cid123"},
	}}

	r.watchContainer(id, "cid123")

	if inst, _ := r.Instance(id); inst.Phase != anovelv1.Phase_PHASE_TERMINATED || inst.ExitReason != anovelv1.ExitReason_EXIT_REASON_SUCCESS {
		t.Errorf("record = (%v, %v), want TERMINATED with SUCCESS", inst.Phase, inst.ExitReason)
	}
	if calls := podmanCalls(t, dir); calls != 1 {
		t.Errorf("podman calls = %d, want 1", calls)
	}
}

// TestLiveContainers pins the set the poller follows: the container of every
// container instance not yet terminated. With none left, the poller stops.
func TestLiveContainers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		instances []*Instance

		want []containerWatch
	}{
		{
			name: "Success",
			instances: []*Instance{
				{ID: "running", Mode: anovelv1.Mode_MODE_CONTAINER, Phase: anovelv1.Phase_PHASE_RUNNING, ContainerID: "c-running"},
				{ID: "stopping", Mode: anovelv1.Mode_MODE_CONTAINER, Phase: anovelv1.Phase_PHASE_STOPPING, ContainerID: "c-stopping"},
				{ID: "terminated", Mode: anovelv1.Mode_MODE_CONTAINER, Phase: anovelv1.Phase_PHASE_TERMINATED, ContainerID: "c-terminated"},
				// compose up has not returned yet, so the container has no ID.
				{ID: "pending", Mode: anovelv1.Mode_MODE_CONTAINER, Phase: anovelv1.Phase_PHASE_PENDING},
				{ID: "go-exec", Mode: anovelv1.Mode_MODE_GO_EXEC, Phase: anovelv1.Phase_PHASE_RUNNING},
			},
			want: []containerWatch{{id: "running", cid: "c-running"}, {id: "stopping", cid: "c-stopping"}},
		},
		{
			name: "Success/NoneLeft",
			instances: []*Instance{
				{ID: "terminated", Mode: anovelv1.Mode_MODE_CONTAINER, Phase: anovelv1.Phase_PHASE_TERMINATED, ContainerID: "c-terminated"},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			r := &Runner{instances: map[string]*Instance{}, polling: true}
			for _, inst := range testCase.instances {
				r.instances[inst.ID] = inst
			}

			got := r.liveContainers()
			slices.SortFunc(got, func(a, b containerWatch) int { return strings.Compare(a.id, b.id) })

			if !slices.Equal(got, testCase.want) {
				t.Errorf("followed %v, want %v", got, testCase.want)
			}
			if wantPolling := len(testCase.want) > 0; r.polling != wantPolling {
				t.Errorf("polling = %v, want %v", r.polling, wantPolling)
			}
		})
	}
}
