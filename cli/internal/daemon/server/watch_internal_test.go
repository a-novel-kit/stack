package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
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
		{name: "Success/Pending", phase: anovelv1.Phase_PHASE_PENDING, want: "pending"},
		// An exit reason qualifies a terminated phase only.
		{name: "Success/RunningIgnoresExitReason", phase: anovelv1.Phase_PHASE_RUNNING, reason: anovelv1.ExitReason_EXIT_REASON_KILLED, want: "running"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ev := runner.PhaseEvent{TargetID: id, NewPhase: testCase.phase, ExitReason: testCase.reason}
			require.Equal(t, id+" "+testCase.want, describePhaseEvent(ev))
		})
	}
}

// TestWatchFilter pins that a watch on a target shorthand follows the canonical
// target: events carry canonical IDs, so a raw shorthand would match nothing.
func TestWatchFilter(t *testing.T) {
	t.Parallel()

	s := New(Config{Stacks: discovery.Stacks{{Name: "fork"}, {Name: "main", Default: true}}})
	event := runner.PhaseEvent{Stack: "main", Service: "svc", TargetID: "main/svc/rest"}

	testCases := []struct {
		name     string
		targetID string
		want     bool
	}{
		{name: "Success/Shorthand", targetID: "svc/rest", want: true},
		{name: "Success/Canonical", targetID: "main/svc/rest", want: true},
		{name: "Success/AnyTarget", want: true},
		{name: "Success/OtherStack", targetID: "fork/svc/rest"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			filter := s.watchFilter(&anovelv1.WatchRequest{TargetId: testCase.targetID})
			require.Equal(t, testCase.want, watches(filter, event))
		})
	}
}
