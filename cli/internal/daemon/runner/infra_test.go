package runner

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestInfraSession pins that a session copy shares no state with the live
// session. ensureOneShotSatisfied reads the copy's results without sessMu while
// StartInfra records results under it. A shared map turns that into a
// concurrent map read and write, which ends the daemon.
func TestInfraSession(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		// results is the live session's one-shot record. A nil map records no
		// session.
		results map[string]anovelv1.ExitReason
		wantOK  bool
	}{
		{name: "Success/Empty", results: map[string]anovelv1.ExitReason{}, wantOK: true},
		{
			name:    "Success/Recorded",
			results: map[string]anovelv1.ExitReason{"migrations": anovelv1.ExitReason_EXIT_REASON_SUCCESS},
			wantOK:  true,
		},
		{name: "Error/NotFound"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			key := sessionKey("default", "service-x")
			r := &Runner{infraSessions: map[string]*infraSession{}}
			if testCase.results != nil {
				sess := newInfraSession("default", "service-x")
				sess.Up = true
				maps.Copy(sess.OneShotResults, testCase.results)
				r.infraSessions[key] = sess
			}

			snapshot, ok := r.InfraSession("default", "service-x")
			if ok != testCase.wantOK {
				t.Fatalf("InfraSession: got ok %v, want %v", ok, testCase.wantOK)
			}

			// Record a one-shot under sessMu, as StartInfra does, while this
			// goroutine reads the copy unlocked. The race detector flags a
			// shared map here.
			var wg sync.WaitGroup
			wg.Go(func() {
				r.sessMu.Lock()
				defer r.sessMu.Unlock()
				if live, ok := r.infraSessions[key]; ok {
					live.OneShotResults["seed"] = anovelv1.ExitReason_EXIT_REASON_SUCCESS
				}
			})
			_, seeded := snapshot.OneShotResults["seed"]
			wg.Wait()

			if seeded || !maps.Equal(snapshot.OneShotResults, testCase.results) {
				t.Errorf("copy results: got %v, want %v, unchanged by the live session", snapshot.OneShotResults, testCase.results)
			}
		})
	}
}

// TestLockInfra pins the per-service infra lock. StartInfra and KillInfra of
// one service wait for the call in flight, and a caller whose ctx ends while it
// waits gives up and leaves the session alone. A call on another service holds
// neither.
func TestLockInfra(t *testing.T) {
	t.Parallel()

	start := func(ctx context.Context, r *Runner) error {
		return r.StartInfra(ctx, "default", "service-x", anovelv1.Mode_MODE_GO_EXEC)
	}
	kill := func(ctx context.Context, r *Runner) error {
		return r.KillInfra(ctx, "default", "service-x", false)
	}

	testCases := []struct {
		name string
		call func(ctx context.Context, r *Runner) error
		// held is the service whose call is in flight, "" for none.
		held string
		// wantErr is the call's error. A case that wants one cancels the
		// call's ctx first, so giving up is the only way out of the wait.
		wantErr error
	}{
		{name: "StartInfra/Success/NoCallInFlight", call: start},
		{name: "StartInfra/Success/OtherServiceInFlight", call: start, held: "service-y"},
		{name: "StartInfra/Error/CallInFlight", call: start, held: "service-x", wantErr: context.Canceled},
		{name: "KillInfra/Error/CallInFlight", call: kill, held: "service-x", wantErr: context.Canceled},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// The session is Up, so a call that gets the lock returns at the
			// idempotent check without reaching podman.
			sess := newInfraSession("default", "service-x")
			sess.Up = true
			key := sessionKey("default", "service-x")
			r := &Runner{
				stacks: discovery.Stacks{{
					Name:     "default",
					Default:  true,
					Services: []*discovery.Service{{Name: "service-x", Stack: "default"}, {Name: "service-y", Stack: "default"}},
				}},
				infraSessions: map[string]*infraSession{key: sess},
				infraLocks:    map[string]chan struct{}{},
			}
			if testCase.held != "" {
				unlock, err := r.lockInfra(t.Context(), "default", testCase.held)
				if err != nil {
					t.Fatal(err)
				}
				defer unlock()
			}

			// The timeout bounds a call that waits on the wrong lock.
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if testCase.wantErr != nil {
				cancel()
			}
			if err := testCase.call(ctx, r); !errors.Is(err, testCase.wantErr) {
				t.Fatalf("got %v, want %v", err, testCase.wantErr)
			}
			if r.infraSessions[key] != sess || !sess.Up {
				t.Error("the call changed the session")
			}
		})
	}
}

// TestKillInfra pins that a forced teardown reports a target it failed to
// stop. The teardown still runs and resets the session, so the user can re-run
// it, but the caller learns which target survived.
func TestKillInfra(t *testing.T) {
	testCases := []struct {
		name string
		// stopExit is the exit status of the stand-in `podman stop`.
		stopExit int
		wantErr  bool
	}{
		{name: "Success", stopExit: 0},
		{name: "Error/TargetSurvives", stopExit: 1, wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// A stand-in podman: compose down succeeds, and stop exits as the
			// case says.
			script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\ncompose) exit 0 ;;\nstop) exit %d ;;\n*) exit 1 ;;\nesac\n", testCase.stopExit)
			bin := t.TempDir()
			if err := os.WriteFile(filepath.Join(bin, "podman"), []byte(script), 0o700); err != nil { //nolint:gosec // an executable fixture
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

			const id = "default/svc/rest"
			svc := &discovery.Service{Name: "svc", Stack: "default"}
			r := New(discovery.Stacks{{Name: "default", Default: true, Services: []*discovery.Service{svc}}}, env.NewAllocator(), nil, logs.New())
			r.instances[id] = &Instance{
				ID: id, Target: "rest", Service: "svc", Stack: "default",
				Mode: anovelv1.Mode_MODE_CONTAINER, Phase: anovelv1.Phase_PHASE_RUNNING, ContainerID: "cid1",
			}
			sess := newInfraSession("default", "svc")
			sess.Up = true
			r.infraSessions[sessionKey("default", "svc")] = sess

			err := r.KillInfra(t.Context(), "default", "svc", true)

			if (err != nil) != testCase.wantErr || (err != nil && !strings.Contains(err.Error(), "kill "+id)) {
				t.Fatalf("KillInfra: got %v, want an error naming %s: %v", err, id, testCase.wantErr)
			}
			if _, ok := r.InfraSession("default", "svc"); ok {
				t.Error("the teardown left the session in place")
			}
		})
	}
}
