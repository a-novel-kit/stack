package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	"github.com/a-novel-kit/stack/cli/internal/shared/podman"
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
			require.Equal(t, testCase.wantOK, ok)

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

			require.False(t, seeded, "the copy saw a result the live session recorded")
			require.Equal(t, testCase.results, snapshot.OneShotResults, "the copy changed with the live session")
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
				require.NoError(t, err)
				defer unlock()
			}

			// The timeout bounds a call that waits on the wrong lock.
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if testCase.wantErr != nil {
				cancel()
			}
			require.ErrorIs(t, testCase.call(ctx, r), testCase.wantErr)
			require.Same(t, sess, r.infraSessions[key], "the call replaced the session")
			require.True(t, sess.Up, "the call took the session down")
		})
	}
}

// TestKillInfra pins that a forced teardown reports a target it failed to
// stop. The teardown still runs and resets the session, so the user can re-run
// it, but the caller learns which target survived.
//
//nolint:paralleltest // stubOnPath puts a stand-in podman on PATH with t.Setenv.
func TestKillInfra(t *testing.T) {
	const id = "default/svc/rest"

	testCases := []struct {
		name string
		// stopExit is the exit status of the stand-in `podman stop`.
		stopExit int
		// wantErr is a part of the error, "" for none.
		wantErr string
	}{
		{name: "Success", stopExit: 0},
		{name: "Error/TargetSurvives", stopExit: 1, wantErr: "kill " + id},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// A stand-in podman: compose down succeeds, and stop exits as the
			// case says.
			stubOnPath(t, "podman", fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\ncompose) exit 0 ;;\nstop) exit %d ;;\n*) exit 1 ;;\nesac\n", testCase.stopExit))

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

			if testCase.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, testCase.wantErr)
			}
			_, ok := r.InfraSession("default", "svc")
			require.False(t, ok, "the teardown left the session in place")
		})
	}
}

// TestRecordOneShot pins how a one-shot's outcome lands in its infra session.
// A failed run is recorded, so later starts refuse it until the infra restarts.
// A wait the caller cancelled records nothing, since the one-shot may still
// succeed.
func TestRecordOneShot(t *testing.T) {
	t.Parallel()

	errFoo := errors.New("foo")

	testCases := []struct {
		name string
		err  error

		wantResult   anovelv1.ExitReason
		wantRecorded bool
	}{
		{name: "Success", wantResult: anovelv1.ExitReason_EXIT_REASON_SUCCESS, wantRecorded: true},
		{name: "Error/Failed", err: errFoo, wantResult: anovelv1.ExitReason_EXIT_REASON_ERROR, wantRecorded: true},
		{name: "Error/Cancelled", err: fmt.Errorf("wait: %w", context.Canceled)},
		{name: "Error/DeadlineExceeded", err: fmt.Errorf("wait: %w", context.DeadlineExceeded)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sess := newInfraSession("default", "service-x")
			r := &Runner{infraSessions: map[string]*infraSession{sessionKey("default", "service-x"): sess}}

			r.recordOneShot("default", "service-x", "migrations", testCase.err)

			result, recorded := sess.OneShotResults["migrations"]
			require.Equal(t, testCase.wantRecorded, recorded)
			require.Equal(t, testCase.wantResult, result)
		})
	}
}

// TestInfraReady pins that an empty container query is not ready: a compose
// version that swallows the labels, or containers that have not appeared yet,
// also yield one, and the dependents must not run against a database still
// starting.
func TestInfraReady(t *testing.T) {
	t.Parallel()

	container := func(state string) podman.Container {
		var c podman.Container
		if err := json.Unmarshal([]byte(inspectState("cid", state)), &c); err != nil {
			panic(err)
		}
		return c
	}
	running := container("running|healthy|0")
	noHealthcheck := container("running|-|0")
	oneShotDone := container("exited|-|0")
	starting := container("created|starting|0")
	oneShotFailed := container("exited|-|1")

	testCases := []struct {
		name       string
		containers []podman.Container

		want bool
	}{
		{name: "Success/Running", containers: []podman.Container{running}, want: true},
		{name: "Success/NoHealthcheck", containers: []podman.Container{noHealthcheck}, want: true},
		{name: "Success/OneShotDone", containers: []podman.Container{oneShotDone}, want: true},
		{name: "Success/RunningAndOneShotDone", containers: []podman.Container{running, oneShotDone}, want: true},

		// No container resolved yet, so keep waiting.
		{name: "Error/NoneResolved"},
		{name: "Error/Starting", containers: []podman.Container{starting}},
		{name: "Error/OneShotFailed", containers: []podman.Container{oneShotFailed}},
		{name: "Error/RunningAndStarting", containers: []podman.Container{running, starting}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, infraReady(testCase.containers))
		})
	}
}

// TestWaitInfraHealthy pins the waits around infraReady: a service that
// declares no infra is ready at once, and an inspect that keeps failing reads
// as not ready until the timeout.
//
//nolint:paralleltest // stubOnPath puts a stand-in podman on PATH with t.Setenv.
func TestWaitInfraHealthy(t *testing.T) {
	t.Run("Success/NoInfraDeclared", func(t *testing.T) {
		require.NoError(t, (&Runner{}).waitInfraHealthy(t.Context(), &discovery.Service{}, time.Millisecond))
	})

	t.Run("Error/InspectFailed", func(t *testing.T) {
		// The label query finds a container that every inspect then misses.
		stubOnPath(t, "podman", "#!/bin/sh\ncase \"$1\" in\nps) echo cid-gone ;;\n*) echo '[]'; exit 125 ;;\nesac\n")

		svc := &discovery.Service{Name: "svc", Stack: "default", Infra: []*discovery.Infra{{Name: "postgres"}}}
		err := (&Runner{}).waitInfraHealthy(t.Context(), svc, 50*time.Millisecond)
		require.ErrorContains(t, err, "infra not healthy within")
	})
}
