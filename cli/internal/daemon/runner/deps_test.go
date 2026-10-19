package runner

import (
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
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
