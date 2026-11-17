package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestEnsureDepsReady pins the dependency walk before a start.
//
// A long-runner waits on its one-shots, the gate the compose file leaves to the
// daemon: a long-runner depends only on its infra there, so a plain
// `podman compose` up runs no one-shot twice, and the daemon refuses it while a
// one-shot of its service has failed. A one-shot dependency that sorts before
// the infra dependency brings the infra up instead of refusing the start. A
// one-shot that fails while a target starts is recorded, so the next start
// refuses with the restart hint instead of running it again.
//
//nolint:paralleltest // sub-tests put a stand-in podman or go on PATH with t.Setenv.
func TestEnsureDepsReady(t *testing.T) {
	migrations := &discovery.Target{
		Name: "migrations", ComposeName: "service-x-migrations", Service: "service-x", Stack: "default",
		Kind: discovery.TargetKindOneShot, DependsOn: []string{"postgres-x"},
	}
	grpc := &discovery.Target{
		Name: "grpc", ComposeName: "service-x-grpc", Service: "service-x", Stack: "default",
		Kind: discovery.TargetKindLongRunner, DependsOn: []string{"postgres-x"},
	}
	svc := &discovery.Service{
		Name: "service-x", Stack: "default",
		Infra:   []*discovery.Infra{{Name: "postgres-x", Service: "service-x", Stack: "default"}},
		Targets: []*discovery.Target{grpc, migrations},
	}

	// The session is Up and the results recorded, so nothing reaches podman.
	testCases := []struct {
		name   string
		target *discovery.Target
		// result is the one-shot's recorded outcome.
		result  anovelv1.ExitReason
		wantErr bool
	}{
		{name: "Success/LongRunnerAfterSucceededOneShot", target: grpc, result: anovelv1.ExitReason_EXIT_REASON_SUCCESS},
		{name: "Error/LongRunnerAfterFailedOneShot", target: grpc, result: anovelv1.ExitReason_EXIT_REASON_ERROR, wantErr: true},
		// A one-shot answers to its own edges only: migrations does not wait on itself.
		{name: "Success/OneShotIgnoresTheOthers", target: migrations, result: anovelv1.ExitReason_EXIT_REASON_ERROR},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			sess := newInfraSession("default", "service-x")
			sess.Up = true
			sess.OneShotResults["migrations"] = testCase.result
			r := &Runner{
				instances:     map[string]*Instance{},
				infraSessions: map[string]*infraSession{sessionKey("default", "service-x"): sess},
			}

			err := r.EnsureDepsReady(t.Context(), testCase.target, svc, anovelv1.Mode_MODE_GO_EXEC)

			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}

	// A stand-in podman records the bring-up and fails it, which ends the start
	// there.
	t.Run("Error/InfraUpForAOneShotDepListedFirst", func(t *testing.T) {
		dir := t.TempDir()
		stubOnPath(t, "podman", fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %s/calls\nexit 1\n", dir))

		migrations := &discovery.Target{
			Name: "migrations", ComposeName: "service-x-migrations", Service: "service-x", Stack: "default",
			Kind: discovery.TargetKindOneShot,
		}
		// Discovery sorts DependsOn by name, and service-x-migrations sorts
		// before valkey-x.
		rest := &discovery.Target{
			Name: "rest", ComposeName: "service-x-rest", Service: "service-x", Stack: "default",
			Kind: discovery.TargetKindLongRunner, DependsOn: []string{"service-x-migrations", "valkey-x"},
		}
		svc := &discovery.Service{
			Name: "service-x", Stack: "default",
			Infra:   []*discovery.Infra{{Name: "valkey-x", Service: "service-x", Stack: "default"}},
			Targets: []*discovery.Target{migrations, rest},
		}
		alloc := env.NewAllocator()
		alloc.SetServices([]string{"service-x"})
		r := New(discovery.Stacks{{Name: "default", Default: true, Services: []*discovery.Service{svc}}}, alloc, env.NewBuilder(alloc, t.TempDir()), logs.New(t.TempDir()))

		err := r.EnsureDepsReady(t.Context(), rest, svc, anovelv1.Mode_MODE_GO_EXEC)

		require.Error(t, err, "want the failed infra bring-up")
		require.NotContains(t, err.Error(), "requires infra up")
		calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
		require.Contains(t, string(calls), "compose", "want a compose up")
	})

	t.Run("Error/FailedOneShotRecorded", func(t *testing.T) {
		// A stand-in go that records each run and fails keeps the toolchain out
		// of the test.
		dir := t.TempDir()
		stubOnPath(t, "go", fmt.Sprintf("#!/bin/sh\necho run >> %s/runs\nexit 1\n", dir))

		cmdDir := filepath.Join(t.TempDir(), "service-x", "cmd", "migrations")
		require.NoError(t, os.MkdirAll(cmdDir, 0o700))
		migrations := &discovery.Target{
			Name: "migrations", ComposeName: "service-x-migrations", Service: "service-x", Stack: "default",
			Kind: discovery.TargetKindOneShot, CmdDir: cmdDir,
		}
		rest := &discovery.Target{
			Name: "rest", ComposeName: "service-x-rest", Service: "service-x", Stack: "default",
			Kind: discovery.TargetKindLongRunner,
		}
		svc := &discovery.Service{Name: "service-x", Stack: "default", Targets: []*discovery.Target{migrations, rest}}
		alloc := env.NewAllocator()
		alloc.SetServices([]string{"service-x"})
		r := New(discovery.Stacks{{Name: "default", Default: true, Services: []*discovery.Service{svc}}}, alloc, env.NewBuilder(alloc, t.TempDir()), logs.New(t.TempDir()))
		// The infra is up and no one-shot has run, as after a restart of the daemon.
		sess := newInfraSession("default", "service-x")
		sess.Up = true
		r.infraSessions[sessionKey("default", "service-x")] = sess

		require.Error(t, r.EnsureDepsReady(t.Context(), rest, svc, anovelv1.Mode_MODE_GO_EXEC), "first start: want the failed one-shot")
		err := r.EnsureDepsReady(t.Context(), rest, svc, anovelv1.Mode_MODE_GO_EXEC)

		require.ErrorContains(t, err, "restart infra to retry", "second start: want the restart hint")
		runs, _ := os.ReadFile(filepath.Join(dir, "runs"))
		require.Equal(t, 1, strings.Count(string(runs), "run"), "one-shot runs")
	})
}
