package server

import (
	"errors"
	"os/exec"
	"strconv"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Tests for the pure helpers in server.go. Their output is wire-visible, in
// StateEvent.Description and in log-streaming routing, so a regression here
// reaches the user.

func TestParseInfraLogID(t *testing.T) {
	cases := []struct {
		in             string
		stack, svc, nm string
		ok             bool
	}{
		{"default/svc-x/infra/postgres-x", "default", "svc-x", "postgres-x", true},
		// Wrong segment count.
		{"default/svc/infra", "", "", "", false},
		{"default/svc/infra/x/y", "", "", "", false},
		// Missing the "infra" sentinel — addresses a regular target.
		{"default/svc/rest/x", "", "", "", false},
		// Empty.
		{"", "", "", "", false},
	}
	for _, c := range cases {
		s, v, n, ok := parseInfraLogID(c.in)
		if s != c.stack || v != c.svc || n != c.nm || ok != c.ok {
			t.Errorf("parseInfraLogID(%q): got (%q, %q, %q, %v) want (%q, %q, %q, %v)",
				c.in, s, v, n, ok, c.stack, c.svc, c.nm, c.ok)
		}
	}
}

func TestDescribePhaseEvent(t *testing.T) {
	const id = "default/svc/rest"
	cases := []struct {
		ev   runner.PhaseEvent
		want string
	}{
		{
			runner.PhaseEvent{TargetID: id, NewPhase: anovelv1.Phase_PHASE_STARTING},
			id + " starting",
		},
		{
			runner.PhaseEvent{TargetID: id, NewPhase: anovelv1.Phase_PHASE_RUNNING},
			id + " running",
		},
		{
			runner.PhaseEvent{TargetID: id, NewPhase: anovelv1.Phase_PHASE_STOPPING},
			id + " stopping",
		},
		{
			runner.PhaseEvent{
				TargetID:   id,
				NewPhase:   anovelv1.Phase_PHASE_TERMINATED,
				ExitReason: anovelv1.ExitReason_EXIT_REASON_SUCCESS,
			},
			id + " terminated (success)",
		},
		{
			runner.PhaseEvent{
				TargetID:   id,
				NewPhase:   anovelv1.Phase_PHASE_TERMINATED,
				ExitReason: anovelv1.ExitReason_EXIT_REASON_ERROR,
			},
			id + " terminated (error)",
		},
		{
			runner.PhaseEvent{
				TargetID:   id,
				NewPhase:   anovelv1.Phase_PHASE_TERMINATED,
				ExitReason: anovelv1.ExitReason_EXIT_REASON_KILLED,
			},
			id + " terminated (killed)",
		},
		{
			runner.PhaseEvent{
				TargetID:   id,
				NewPhase:   anovelv1.Phase_PHASE_TERMINATED,
				ExitReason: anovelv1.ExitReason_EXIT_REASON_CRASHED,
			},
			id + " terminated (crashed)",
		},
		{
			// PENDING, like any unhandled phase, falls through to the
			// stringified enum form.
			runner.PhaseEvent{TargetID: id, NewPhase: anovelv1.Phase_PHASE_PENDING},
			id + " " + anovelv1.Phase_PHASE_PENDING.String(),
		},
	}
	for _, c := range cases {
		got := describePhaseEvent(c.ev)
		if got != c.want {
			t.Errorf("describePhaseEvent(%v): got %q want %q", c.ev, got, c.want)
		}
	}
}

// TestListStacksReportsDiscoveredStacks pins that ListStacks and Status
// advertise the stacks discovery kept, in registration order, so they match
// what every other RPC accepts.
func TestListStacksReportsDiscoveredStacks(t *testing.T) {
	t.Parallel()

	srv := &Server{stacks: discovery.Stacks{
		{Name: "default", Path: "/w", Default: true},
		{Name: "alive", Path: "/tmp/here"},
	}}

	resp, err := srv.ListStacks(t.Context(), &anovelv1.ListStacksRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := resp.GetStacks()
	if len(got) != 2 {
		t.Fatalf("listed %d stacks, want 2", len(got))
	}
	if got[0].GetName() != "default" || !got[0].GetIsDefault() || got[0].GetPath() != "/w" {
		t.Errorf("first stack = %v, want the default", got[0])
	}
	if got[1].GetName() != "alive" || got[1].GetIsDefault() {
		t.Errorf("second stack = %v, want alive", got[1])
	}
}

// TestExecExitCode covers the wait-error to exit-status mapping the Exec
// stream's terminal message carries. A wait failure yields no status at all,
// so the mapping returns the error.
func TestExecExitCode(t *testing.T) {
	// Real *exec.ExitError values, since ExitCode reads the platform
	// ProcessState.
	runExit := func(code int) error {
		return exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run() //nolint:gosec
	}

	cases := []struct {
		name    string
		in      error
		want    int32
		wantErr bool
	}{
		{"clean exit", nil, 0, false},
		{"non-zero exit", runExit(3), 3, false},
		// Not an *exec.ExitError: the child's status was never obtained.
		{"wait failure", errors.New("pipe closed"), 0, true},
	}
	for _, c := range cases {
		got, err := execExitCode(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("execExitCode(%s): got err %v want wantErr=%v", c.name, err, c.wantErr)
			continue
		}
		if got != c.want {
			t.Errorf("execExitCode(%s): got %d want %d", c.name, got, c.want)
		}
	}
}
