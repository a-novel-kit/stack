package runner

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestInvalidateInfraStateCache covers the in-memory cache InfraStatesOf relies
// on, which needs no podman. An invalidation drops every entry and bumps the
// generation. The generation closes a race: the cache write ending a long
// InfraStatesOf scan is skipped when an invalidation fired mid-scan, and
// proceeds otherwise. The cases drive the scan's own cache read and write
// around the podman call.
func TestInvalidateInfraStateCache(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		r := &Runner{infraStateCache: map[string]infraStateCacheEntry{
			"default": {at: time.Now(), states: map[string]InfraState{"x/y": {ContainerID: "abc"}}},
		}}

		r.InvalidateInfraStateCache()

		require.Empty(t, r.infraStateCache)
		require.Equal(t, uint64(1), r.infraStateGen)

		r.InvalidateInfraStateCache()

		require.Equal(t, uint64(2), r.infraStateGen)
	})

	testCases := []struct {
		name string
		// invalidated fires an invalidation while the scan runs.
		invalidated bool

		// wantStates is the cached scan once it ends, nil for none.
		wantStates map[string]InfraState
	}{
		{name: "Success/ScanStored", wantStates: map[string]InfraState{"x/y": {ContainerID: "scanned"}}},
		{name: "Success/ScanOvertaken", invalidated: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			r := &Runner{infraStateCache: make(map[string]infraStateCacheEntry)}
			_, startGen, _ := r.cachedInfraStates("default")
			if testCase.invalidated {
				r.InvalidateInfraStateCache()
			}

			r.storeInfraStates("default", startGen, map[string]InfraState{"x/y": {ContainerID: "scanned"}})

			require.Equal(t, testCase.wantStates, r.infraStateCache["default"].states)
		})
	}
}

// TestLaunch pins how a start treats the target's ports. A start that meets a
// live instance leaves its ports alone: they are refcounted under the target
// ID, the consumer every start of that target claims for, so a refused start
// that released them handed a running process's ports back to the allocator. A
// spawn that fails after the env build claimed the target's ports gives them
// back, and leaves the target terminated with the error, so a caller waiting on
// its claim sees the failure. Concurrent starts of one target, as a TUI double
// press or two long-runners waiting on one one-shot make, spawn it once.
//
//nolint:paralleltest // sub-tests set XDG_STATE_HOME and a stand-in go on PATH with t.Setenv.
func TestLaunch(t *testing.T) {
	testCases := []struct {
		name string
		// phase and mode are the live instance's.
		phase anovelv1.Phase
		mode  anovelv1.Mode

		wantErr bool
	}{
		{name: "Success/RunningInSameMode", phase: anovelv1.Phase_PHASE_RUNNING, mode: anovelv1.Mode_MODE_GO_EXEC},
		{name: "Error/RunningInOtherMode", phase: anovelv1.Phase_PHASE_RUNNING, mode: anovelv1.Mode_MODE_CONTAINER, wantErr: true},
		{name: "Error/Stopping", phase: anovelv1.Phase_PHASE_STOPPING, mode: anovelv1.Mode_MODE_GO_EXEC, wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			r, alloc, tgt := newRunnerForLaunch()
			port, err := alloc.Acquire("svc", "REST_PORT", tgt.ID())
			require.NoError(t, err)
			r.instances[tgt.ID()] = &Instance{ID: tgt.ID(), Target: tgt.Name, Service: "svc", Stack: "default", Phase: testCase.phase, Mode: testCase.mode}

			_, err = r.launch(t.Context(), tgt, anovelv1.Mode_MODE_GO_EXEC)

			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			got, ok := alloc.Lookup("svc", "REST_PORT")
			require.True(t, ok, "the live instance's port is gone")
			require.Equal(t, port, got)
		})
	}

	t.Run("Error/SpawnFails", func(t *testing.T) {
		// A state dir that is a file makes the log directory, and so the spawn,
		// fail.
		state := t.TempDir()
		fsutiltest.Write(t, state, map[string]string{"not-a-dir": ""})
		t.Setenv("XDG_STATE_HOME", filepath.Join(state, "not-a-dir"))
		r, alloc, tgt := newRunnerForLaunch()

		_, err := r.launch(t.Context(), tgt, anovelv1.Mode_MODE_GO_EXEC)

		require.Error(t, err, "want the log directory error")
		require.Empty(t, alloc.Snapshot(), "a failed spawn kept its claims")
		inst, ok := r.Instance(tgt.ID())
		require.True(t, ok)
		require.Equal(t, anovelv1.Phase_PHASE_TERMINATED, inst.Phase)
		require.Equal(t, anovelv1.ExitReason_EXIT_REASON_ERROR, inst.ExitReason)
	})

	t.Run("Success/ConcurrentStarts", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		// A stand-in go that only sleeps keeps the toolchain out of the test.
		stubOnPath(t, "go", "#!/bin/sh\nexec sleep 30\n")
		r, _, tgt := newRunnerForLaunch()
		tgt.CmdDir = filepath.Join(t.TempDir(), "cmd", "rest")
		t.Cleanup(func() { _ = r.Kill(context.Background(), tgt.ID(), 0) })

		const starts = 8
		runs := make([]*Instance, starts)
		errs := make([]error, starts)
		ready := make(chan struct{})
		var wg sync.WaitGroup
		for i := range starts {
			wg.Go(func() {
				<-ready
				runs[i], errs[i] = r.launch(t.Context(), tgt, anovelv1.Mode_MODE_GO_EXEC)
			})
		}
		close(ready)
		wg.Wait()

		for i := range starts {
			require.NoError(t, errs[i], "start %d", i)
			require.True(t, runs[i].StartedAt.Equal(runs[0].StartedAt),
				"start %d returned a run started at %v, start 0 one at %v: the target spawned twice",
				i, runs[i].StartedAt, runs[0].StartedAt)
		}
	})
}

// TestRelaunch covers the reinstall replay. The relaunched process binds the
// ports the previous daemon gave it, so the new daemon must hold them for it: a
// replay that skipped the allocator let the next consumer of REST_PORT draw a
// fresh port the process never bound.
func TestRelaunch(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// The runner spawns `go run`; a stand-in go that only sleeps keeps the
	// toolchain out of the test.
	stubOnPath(t, "go", "#!/bin/sh\nexec sleep 30\n")
	r, alloc, tgt := newRunnerForLaunch()
	tgt.CmdDir = filepath.Join(t.TempDir(), "cmd", "rest")

	require.NoError(t, r.Relaunch(t.Context(), tgt.ID(), []string{"REST_PORT=41234"}))
	t.Cleanup(func() { _ = r.Kill(context.Background(), tgt.ID(), 0) })

	require.Equal(t, []env.Allocation{{Owner: "svc", LocalVar: "REST_PORT", Port: 41234, Refs: []string{tgt.ID()}}}, alloc.Snapshot())
}

// TestBegin pins the gate between a claim and its spawn. A kill that reaches a
// claimed instance before its process exists leaves it STOPPING, and begin
// then marks it killed, so the start spawns nothing.
func TestBegin(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		phase anovelv1.Phase

		wantErr    error
		wantPhase  anovelv1.Phase
		wantReason anovelv1.ExitReason
	}{
		{name: "Success", phase: anovelv1.Phase_PHASE_PENDING, wantPhase: anovelv1.Phase_PHASE_STARTING},
		{
			name:       "Error/Killed",
			phase:      anovelv1.Phase_PHASE_STOPPING,
			wantErr:    errKilledWhileStarting,
			wantPhase:  anovelv1.Phase_PHASE_TERMINATED,
			wantReason: anovelv1.ExitReason_EXIT_REASON_KILLED,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			const id = "default/svc/rest"
			inst := &Instance{ID: id, Service: "svc", Stack: "default", Phase: testCase.phase}
			r := &Runner{instances: map[string]*Instance{id: inst}}
			procCtx, cancel := context.WithCancel(t.Context())
			defer cancel()

			err := r.begin(inst, nil, cancel)

			require.ErrorIs(t, err, testCase.wantErr)
			require.Equal(t, testCase.wantPhase, inst.Phase)
			require.Equal(t, testCase.wantReason, inst.ExitReason)
			// A killed instance releases the process context its start built.
			require.Equal(t, testCase.wantErr != nil, procCtx.Err() != nil, "process context ended")
		})
	}
}

// TestMarkTerminated pins that a terminated instance stops the goroutines
// watching it. A container-mode instance runs a watch loop and a log stream on
// one context, and a container that outlives its instance would otherwise keep
// both going for the life of the daemon. A go-exec instance that never started
// carries no cancel.
func TestMarkTerminated(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		// started gives the instance the cancel of its process context.
		started bool
	}{
		{name: "Success", started: true},
		{name: "Success/NeverStarted"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			const id = "default/svc/rest"
			inst := &Instance{ID: id, Service: "svc", Stack: "default", Phase: anovelv1.Phase_PHASE_RUNNING}
			if testCase.started {
				inst.cancel = cancel
			}
			r := &Runner{instances: map[string]*Instance{id: inst}}

			r.markTerminated(id, anovelv1.ExitReason_EXIT_REASON_KILLED, "")

			require.Equal(t, anovelv1.Phase_PHASE_TERMINATED, r.instances[id].Phase)
			if !testCase.started {
				return
			}
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
				require.FailNow(t, "markTerminated left the instance context open; the watch and log goroutines run on it")
			}
		})
	}
}
