package runner

import "testing"

// TestInfraHealthy pins that an empty container query is ready only for a
// service that declares no infra. A compose version that swallows the labels,
// or containers that have not appeared yet, also yield an empty query, and the
// dependents must not run against a database still starting.
func TestInfraHealthy(t *testing.T) {
	running := infraContainerState{phase: pmPhaseRunning, health: pmHealthHealthy}
	// A container with no healthcheck reports health "-", which is healthy enough to run.
	noHealthcheck := infraContainerState{phase: pmPhaseRunning, health: "-"}
	oneShotDone := infraContainerState{phase: pmPhaseExited, exitCode: 0}
	starting := infraContainerState{phase: "created", health: "starting"}
	oneShotFailed := infraContainerState{phase: pmPhaseExited, exitCode: 1}
	inspectFailed := infraContainerState{inspectErr: true}

	testCases := []struct {
		name          string
		declaredInfra int
		states        []infraContainerState
		want          bool
	}{
		{"no infra declared is ready", 0, nil, true},
		{"no infra declared ignores stray containers", 0, []infraContainerState{starting}, true},

		// Infra is declared but no container resolved yet, so keep waiting.
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

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := infraHealthy(testCase.declaredInfra, testCase.states)
			if got != testCase.want {
				t.Errorf("infraHealthy(%d, %v) = %v, want %v", testCase.declaredInfra, testCase.states, got, testCase.want)
			}
		})
	}
}
