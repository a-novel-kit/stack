package runner

import "testing"

// waitInfraHealthy used to read an empty container query as "ready", so a service whose
// labels were swallowed by the compose version — or whose containers had not appeared
// yet — was declared healthy and its dependents ran against a database still starting.
// The discriminator is whether the service DECLARES infra, which infraHealthy takes as
// declaredInfra.

func TestInfraHealthy(t *testing.T) {
	running := infraContainerState{phase: pmPhaseRunning, health: pmHealthHealthy}
	// A container with no healthcheck reports health "-", which is healthy enough to run.
	noHealthcheck := infraContainerState{phase: pmPhaseRunning, health: "-"}
	oneShotDone := infraContainerState{phase: pmPhaseExited, exitCode: 0}
	starting := infraContainerState{phase: "created", health: "starting"}
	oneShotFailed := infraContainerState{phase: pmPhaseExited, exitCode: 1}
	inspectFailed := infraContainerState{inspectErr: true}

	cases := []struct {
		name          string
		declaredInfra int
		states        []infraContainerState
		want          bool
	}{
		{"no infra declared is ready", 0, nil, true},
		{"no infra declared ignores stray containers", 0, []infraContainerState{starting}, true},

		// The bug: infra is declared, but no container resolved. Not ready — keep waiting.
		{"declared but none resolved is not ready", 1, nil, false},

		{"single running container is ready", 1, []infraContainerState{running}, true},
		{"container with no healthcheck is ready", 1, []infraContainerState{noHealthcheck}, true},
		{"completed one-shot is ready", 1, []infraContainerState{oneShotDone}, true},
		{"mix of running and completed one-shot is ready", 2, []infraContainerState{running, oneShotDone}, true},

		{"a still-starting container is not ready", 1, []infraContainerState{starting}, false},
		{"a failed one-shot is not ready", 1, []infraContainerState{oneShotFailed}, false},
		{"one healthy and one starting is not ready", 2, []infraContainerState{running, starting}, false},
		{"an inspect failure is not ready", 1, []infraContainerState{inspectFailed}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := infraHealthy(c.declaredInfra, c.states)
			if got != c.want {
				t.Errorf("infraHealthy(%d, %v) = %v, want %v", c.declaredInfra, c.states, got, c.want)
			}
		})
	}
}
