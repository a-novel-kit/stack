package runner

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// newRunnerForLaunch builds a runner over one discovered target whose ports
// block makes every env build claim REST_PORT.
func newRunnerForLaunch() (*Runner, *env.Allocator, *discovery.Target) {
	tgt := &discovery.Target{Name: "rest", Service: "svc", Stack: "default", Ports: []string{"${REST_PORT}:8080"}}
	stacks := discovery.Stacks{{
		Name:     "default",
		Default:  true,
		Services: []*discovery.Service{{Name: "svc", Stack: "default", Targets: []*discovery.Target{tgt}}},
	}}
	alloc := env.NewAllocator()
	alloc.SetServices([]string{"svc"})
	return New(stacks, alloc, env.NewBuilder(alloc), logs.New()), alloc, tgt
}

// TestLaunchKeepsALiveInstancesPorts covers a start that meets a live
// instance. Its ports are refcounted under the target ID, the consumer every
// start of that target claims for, so a refused start that released them
// handed a running process's ports back to the allocator.
func TestLaunchKeepsALiveInstancesPorts(t *testing.T) {
	testCases := []struct {
		name    string
		phase   anovelv1.Phase
		mode    anovelv1.Mode
		wantErr bool
	}{
		{"running in the other mode is refused", anovelv1.Phase_PHASE_RUNNING, anovelv1.Mode_MODE_CONTAINER, true},
		{"stopping is refused", anovelv1.Phase_PHASE_STOPPING, anovelv1.Mode_MODE_GO_EXEC, true},
		{"running in the same mode is a no-op", anovelv1.Phase_PHASE_RUNNING, anovelv1.Mode_MODE_GO_EXEC, false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			r, alloc, tgt := newRunnerForLaunch()
			port, err := alloc.Acquire("svc", "REST_PORT", tgt.ID())
			if err != nil {
				t.Fatal(err)
			}
			r.instances[tgt.ID()] = &Instance{ID: tgt.ID(), Target: tgt.Name, Service: "svc", Stack: "default", Phase: testCase.phase, Mode: testCase.mode}

			_, err = r.launch(t.Context(), tgt, anovelv1.Mode_MODE_GO_EXEC)

			if (err != nil) != testCase.wantErr {
				t.Fatalf("launch: got err %v, want error %v", err, testCase.wantErr)
			}
			if got, ok := alloc.Lookup("svc", "REST_PORT"); !ok || got != port {
				t.Errorf("live instance's port: lookup = (%d, %v), want (%d, true)", got, ok, port)
			}
		})
	}
}

// TestLaunchReleasesTheClaimsOfAFailedSpawn covers the other half: a spawn that
// fails after the env build claimed the target's ports gives them back. The
// target is left terminated with the error, so a caller waiting on its claim
// sees the failure.
func TestLaunchReleasesTheClaimsOfAFailedSpawn(t *testing.T) {
	// A state dir that is a file makes the log directory, and so the spawn,
	// fail.
	state := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(state, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", state)
	r, alloc, tgt := newRunnerForLaunch()

	if _, err := r.launch(t.Context(), tgt, anovelv1.Mode_MODE_GO_EXEC); err == nil {
		t.Fatal("launch: got nil, want the log directory error")
	}
	if snap := alloc.Snapshot(); len(snap) != 0 {
		t.Errorf("a failed spawn kept its claims: %+v", snap)
	}
	inst, ok := r.Instance(tgt.ID())
	if !ok || inst.Phase != anovelv1.Phase_PHASE_TERMINATED || inst.ExitReason != anovelv1.ExitReason_EXIT_REASON_ERROR {
		t.Errorf("record after a failed spawn = (%v, %v, %v), want TERMINATED with ERROR", inst.Phase, inst.ExitReason, ok)
	}
}

// TestRelaunchClaimsTheCheckpointedPorts covers the reinstall replay. The
// relaunched process binds the ports the previous daemon gave it, so the new
// daemon must hold them for it: a replay that skipped the allocator let the
// next consumer of REST_PORT draw a fresh port the process never bound.
func TestRelaunchClaimsTheCheckpointedPorts(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// The runner spawns `go run`; a stand-in go that only sleeps keeps the
	// toolchain out of the test.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil { //nolint:gosec // an executable fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	r, alloc, tgt := newRunnerForLaunch()
	tgt.CmdDir = filepath.Join(t.TempDir(), "cmd", "rest")

	if err := r.Relaunch(t.Context(), tgt.ID(), []string{"REST_PORT=41234"}); err != nil {
		t.Fatalf("Relaunch: %v", err)
	}
	t.Cleanup(func() { _ = r.Kill(context.Background(), tgt.ID(), 0) })

	snap := alloc.Snapshot()
	if len(snap) != 1 || snap[0].Port != 41234 || len(snap[0].Refs) != 1 || snap[0].Refs[0] != tgt.ID() {
		t.Errorf("allocations after relaunch = %+v, want svc/REST_PORT=41234 held by %s", snap, tgt.ID())
	}
}

// TestLaunchSpawnsOnceForConcurrentStarts races several starts of one target,
// as a TUI double press or two long-runners waiting on one one-shot do. One
// start spawns the target, and every start returns that run.
func TestLaunchSpawnsOnceForConcurrentStarts(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// A stand-in go that only sleeps keeps the toolchain out of the test.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil { //nolint:gosec // an executable fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
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
		if errs[i] != nil {
			t.Fatalf("start %d: %v", i, errs[i])
		}
		if !runs[i].StartedAt.Equal(runs[0].StartedAt) {
			t.Fatalf("start %d returned a run started at %v, start 0 one at %v: the target spawned twice",
				i, runs[i].StartedAt, runs[0].StartedAt)
		}
	}
}

// TestBegin pins the gate between a claim and its spawn. A kill that reaches a
// claimed instance before its process exists leaves it STOPPING, and begin
// then marks it killed, so the start spawns nothing.
func TestBegin(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		phase anovelv1.Phase

		wantErr    bool
		wantPhase  anovelv1.Phase
		wantReason anovelv1.ExitReason
	}{
		{name: "Success", phase: anovelv1.Phase_PHASE_PENDING, wantPhase: anovelv1.Phase_PHASE_STARTING},
		{
			name:       "Error/Killed",
			phase:      anovelv1.Phase_PHASE_STOPPING,
			wantErr:    true,
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

			if (err != nil) != testCase.wantErr {
				t.Fatalf("begin: got %v, want error %v", err, testCase.wantErr)
			}
			if inst.Phase != testCase.wantPhase || inst.ExitReason != testCase.wantReason {
				t.Errorf("instance = (%v, %v), want (%v, %v)", inst.Phase, inst.ExitReason, testCase.wantPhase, testCase.wantReason)
			}
			// A killed instance releases the process context its start built.
			if (procCtx.Err() != nil) != testCase.wantErr {
				t.Errorf("process context ended = %v, want %v", procCtx.Err() != nil, testCase.wantErr)
			}
		})
	}
}
