package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestEnsureDepsReadyGatesALongRunnerOnItsOneShots pins the gate the compose file leaves to the
// daemon: a long-runner depends only on its infra there, so a plain `podman compose` up runs no
// one-shot twice, and the daemon refuses it while a one-shot of its service has failed. The session
// is Up and the results recorded, so nothing reaches podman.
func TestEnsureDepsReadyGatesALongRunnerOnItsOneShots(t *testing.T) {
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

	testCases := []struct {
		name    string
		target  *discovery.Target
		result  anovelv1.ExitReason
		wantErr bool
	}{
		{name: "LongRunnerAfterSucceededOneShot", target: grpc, result: anovelv1.ExitReason_EXIT_REASON_SUCCESS},
		{name: "LongRunnerAfterFailedOneShot", target: grpc, result: anovelv1.ExitReason_EXIT_REASON_ERROR, wantErr: true},
		// A one-shot answers to its own edges only: migrations does not wait on itself.
		{name: "OneShotIgnoresTheOthers", target: migrations, result: anovelv1.ExitReason_EXIT_REASON_ERROR},
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
			if (err != nil) != testCase.wantErr {
				t.Fatalf("EnsureDepsReady(%s) = %v, want error %v", testCase.target.Name, err, testCase.wantErr)
			}
		})
	}
}

// TestEnsureDepsReadyStartsInfraForAOneShotDepListedFirst covers a target whose
// one-shot dependency sorts before its infra dependency. The one-shot brings
// the infra up instead of refusing the start. A stand-in podman records the
// bring-up and fails it, which ends the start there.
func TestEnsureDepsReadyStartsInfraForAOneShotDepListedFirst(t *testing.T) {
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %s/calls\nexit 1\n", dir)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "podman"), []byte(script), 0o700); err != nil { //nolint:gosec // an executable fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

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
	r := New(discovery.Stacks{{Name: "default", Default: true, Services: []*discovery.Service{svc}}}, alloc, env.NewBuilder(alloc), logs.New())

	err := r.EnsureDepsReady(t.Context(), rest, svc, anovelv1.Mode_MODE_GO_EXEC)

	if err == nil || strings.Contains(err.Error(), "requires infra up") {
		t.Fatalf("EnsureDepsReady: got %v, want the failed infra bring-up", err)
	}
	if calls, _ := os.ReadFile(filepath.Join(dir, "calls")); !strings.Contains(string(calls), "compose") {
		t.Errorf("podman calls = %q, want a compose up", calls)
	}
}
