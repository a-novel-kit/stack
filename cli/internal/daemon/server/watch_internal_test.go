package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func TestDescribePhaseEvent(t *testing.T) {
	t.Parallel()

	const id = "default/svc/rest"

	testCases := []struct {
		name   string
		phase  anovelv1.Phase
		reason anovelv1.ExitReason
		want   string
	}{
		{name: "Success/Starting", phase: anovelv1.Phase_PHASE_STARTING, want: "starting"},
		{name: "Success/Running", phase: anovelv1.Phase_PHASE_RUNNING, want: "running"},
		{name: "Success/Stopping", phase: anovelv1.Phase_PHASE_STOPPING, want: "stopping"},
		{name: "Success/Terminated", phase: anovelv1.Phase_PHASE_TERMINATED, want: "terminated"},
		{name: "Success/TerminatedSuccess", phase: anovelv1.Phase_PHASE_TERMINATED, reason: anovelv1.ExitReason_EXIT_REASON_SUCCESS, want: "terminated (success)"},
		{name: "Success/TerminatedError", phase: anovelv1.Phase_PHASE_TERMINATED, reason: anovelv1.ExitReason_EXIT_REASON_ERROR, want: "terminated (error)"},
		{name: "Success/TerminatedKilled", phase: anovelv1.Phase_PHASE_TERMINATED, reason: anovelv1.ExitReason_EXIT_REASON_KILLED, want: "terminated (killed)"},
		{name: "Success/TerminatedCrashed", phase: anovelv1.Phase_PHASE_TERMINATED, reason: anovelv1.ExitReason_EXIT_REASON_CRASHED, want: "terminated (crashed)"},
		// PENDING, like any unhandled phase, falls through to the stringified
		// enum form.
		{name: "Success/UnhandledPhase", phase: anovelv1.Phase_PHASE_PENDING, want: anovelv1.Phase_PHASE_PENDING.String()},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ev := runner.PhaseEvent{TargetID: id, NewPhase: testCase.phase, ExitReason: testCase.reason}
			require.Equal(t, id+" "+testCase.want, describePhaseEvent(ev))
		})
	}
}
