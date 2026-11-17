package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestStartContainer covers a kill that arrives during `compose up`. The kill
// finds no container yet and leaves the target STOPPING. When compose returns,
// the start stops the container it brought up and fails, so the killed target
// never serves.
//
//nolint:paralleltest // stubOnPath puts a stand-in podman on PATH with t.Setenv.
func TestStartContainer(t *testing.T) {
	// A stand-in podman: compose up waits for the test's release file, ps
	// names one container, and stop records its arguments.
	dir := t.TempDir()
	stubOnPath(t, "podman", fmt.Sprintf(`#!/bin/sh
case "$1" in
compose) : > %[1]s/composing; while [ ! -e %[1]s/release ]; do sleep 0.05; done ;;
ps) echo cid123 ;;
stop) echo "$@" >> %[1]s/stops ;;
*) exit 1 ;;
esac
`, dir))
	r, _, tgt := newRunnerForLaunch(t, t.TempDir())

	started := make(chan error, 1)
	go func() {
		_, err := r.launch(t.Context(), tgt, anovelv1.Mode_MODE_CONTAINER)
		started <- err
	}()
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(dir, "composing"))
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "compose up never started")

	require.NoError(t, r.Kill(t.Context(), tgt.ID(), 0), "Kill during compose up")
	fsutiltest.Write(t, dir, map[string]string{"release": ""})

	select {
	case err := <-started:
		require.ErrorIs(t, err, errKilledWhileStarting)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "launch still running 10s after compose up returned")
	}
	inst, _ := r.Instance(tgt.ID())
	require.Equal(t, anovelv1.Phase_PHASE_TERMINATED, inst.Phase)
	require.Equal(t, anovelv1.ExitReason_EXIT_REASON_KILLED, inst.ExitReason)
	stops, _ := os.ReadFile(filepath.Join(dir, "stops"))
	require.Contains(t, string(stops), "cid123", "want a podman stop of cid123")
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

			require.Equal(t, testCase.wantPhase, inst.Phase)
			// emitPhase sends before updateContainerState returns, so an event
			// that is not buffered now was never emitted.
			select {
			case ev := <-events:
				require.True(t, testCase.wantEvent, "unexpected event %v→%v", ev.OldPhase, ev.NewPhase)
				require.Equal(t, testCase.phase, ev.OldPhase)
				require.Equal(t, testCase.wantPhase, ev.NewPhase)
			default:
				require.False(t, testCase.wantEvent, "no event, want %v→%v", testCase.phase, testCase.wantPhase)
			}
		})
	}
}

// TestCheckContainers covers one tick of the poller. A single podman call
// inspects every container, and each result reaches its own instance. A
// vanished container, which the call misses, ends as crashed. A result that comes back after a restart leaves the
// target's new instance alone.
//
//nolint:paralleltest // fakePodmanInspect puts a stand-in podman on PATH with t.Setenv.
func TestCheckContainers(t *testing.T) {
	type container struct {
		// phase is the instance's phase before the check.
		phase anovelv1.Phase
		// inspect is the container's state for inspectState, "" once it
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
			wantCalls: 1,
		},
		{
			name: "Success/Restarted",
			containers: map[string]container{
				"exited": {phase: anovelv1.Phase_PHASE_PENDING, inspect: "exited|-|1", restarted: true, wantPhase: anovelv1.Phase_PHASE_PENDING},
				"gone":   {phase: anovelv1.Phase_PHASE_PENDING, restarted: true, wantPhase: anovelv1.Phase_PHASE_PENDING},
			},
			wantCalls: 1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := fakePodmanInspect(t)
			r := &Runner{instances: map[string]*Instance{}}
			inspects := make(map[string]string, len(testCase.containers))
			watches := make([]containerWatch, 0, len(testCase.containers))
			for cid, c := range testCase.containers {
				if c.inspect != "" {
					inspects[cid] = inspectState(cid, c.inspect)
				}
				id := "default/svc/" + cid
				inst := &Instance{ID: id, Mode: anovelv1.Mode_MODE_CONTAINER, Phase: c.phase, ContainerID: cid}
				if c.restarted {
					inst.ContainerID = ""
				}
				r.instances[id] = inst
				watches = append(watches, containerWatch{id: id, cid: cid})
			}
			fsutiltest.Write(t, dir, inspects)

			r.checkContainers(watches)

			require.Equal(t, testCase.wantCalls, podmanCalls(t, dir), "podman calls")
			for cid, c := range testCase.containers {
				inst := r.instances["default/svc/"+cid]
				require.Equal(t, c.wantPhase, inst.Phase, cid)
				require.Equal(t, c.wantReason, inst.ExitReason, cid)
			}
		})
	}
}

// TestCheckContainersPodmanFailure pins that a podman call failing before it
// reports any container ends nothing: no container is known gone, and the next
// tick retries.
//
//nolint:paralleltest // stubOnPath puts a stand-in podman on PATH with t.Setenv.
func TestCheckContainersPodmanFailure(t *testing.T) {
	stubOnPath(t, "podman", "#!/bin/sh\necho 'Error: database is locked' >&2\nexit 125\n")
	r := &Runner{instances: map[string]*Instance{}}
	watches := make([]containerWatch, 0, 2)
	for _, cid := range []string{"cid-a", "cid-b"} {
		id := "default/svc/" + cid
		r.instances[id] = &Instance{ID: id, Mode: anovelv1.Mode_MODE_CONTAINER, Phase: anovelv1.Phase_PHASE_RUNNING, ContainerID: cid}
		watches = append(watches, containerWatch{id: id, cid: cid})
	}

	r.checkContainers(watches)

	for _, w := range watches {
		inst, _ := r.Instance(w.id)
		require.Equal(t, anovelv1.Phase_PHASE_RUNNING, inst.Phase, w.id)
	}
}

// TestWatchContainer pins that a new container is checked at once, so one that
// exits right away ends its instance without waiting a tick.
//
//nolint:paralleltest // fakePodmanInspect puts a stand-in podman on PATH with t.Setenv.
func TestWatchContainer(t *testing.T) {
	dir := fakePodmanInspect(t)
	fsutiltest.Write(t, dir, map[string]string{"cid123": inspectState("cid123", "exited|-|0")})
	const id = "default/svc/rest"
	r := &Runner{instances: map[string]*Instance{
		id: {ID: id, Mode: anovelv1.Mode_MODE_CONTAINER, Phase: anovelv1.Phase_PHASE_RUNNING, ContainerID: "cid123"},
	}}

	r.watchContainer(id, "cid123")

	inst, _ := r.Instance(id)
	require.Equal(t, anovelv1.Phase_PHASE_TERMINATED, inst.Phase)
	require.Equal(t, anovelv1.ExitReason_EXIT_REASON_SUCCESS, inst.ExitReason)
	require.Equal(t, 1, podmanCalls(t, dir), "podman calls")
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

			require.Equal(t, testCase.want, got)
			require.Equal(t, len(testCase.want) > 0, r.polling, "polling")
		})
	}
}

// TestKillContainer pins that a stop that fails reaches the caller.
func TestKillContainer(t *testing.T) {
	t.Parallel()

	const id = "default/svc/rest"
	r := &Runner{instances: map[string]*Instance{
		id: {
			ID:          id,
			Service:     "svc",
			Stack:       "default",
			Phase:       anovelv1.Phase_PHASE_STOPPING,
			Mode:        anovelv1.Mode_MODE_CONTAINER,
			ContainerID: "a-novel-test-container-that-does-not-exist",
		},
	}}

	// The stop fails whether or not podman is installed: the binary is missing, or it rejects the
	// unknown container.
	err := r.killContainer(t.Context(), id, time.Second)

	require.Error(t, err, "killContainer: got nil for a container that cannot be stopped")
	// The instance keeps its STOPPING phase. Marking it terminated would report a container gone
	// while it still holds its name and host ports, and the next start collides with it.
	require.Equal(t, anovelv1.Phase_PHASE_STOPPING, r.instances[id].Phase)
}
