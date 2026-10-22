package server

import (
	"errors"
	"os/exec"
	"slices"
	"strconv"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func TestParseInfraLogID(t *testing.T) {
	testCases := []struct {
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
	for _, testCase := range testCases {
		s, v, n, ok := parseInfraLogID(testCase.in)
		if s != testCase.stack || v != testCase.svc || n != testCase.nm || ok != testCase.ok {
			t.Errorf("parseInfraLogID(%q): got (%q, %q, %q, %v) want (%q, %q, %q, %v)",
				testCase.in, s, v, n, ok, testCase.stack, testCase.svc, testCase.nm, testCase.ok)
		}
	}
}

func TestDescribePhaseEvent(t *testing.T) {
	const id = "default/svc/rest"
	testCases := []struct {
		phase  anovelv1.Phase
		reason anovelv1.ExitReason
		want   string
	}{
		{anovelv1.Phase_PHASE_STARTING, 0, "starting"},
		{anovelv1.Phase_PHASE_RUNNING, 0, "running"},
		{anovelv1.Phase_PHASE_STOPPING, 0, "stopping"},
		{anovelv1.Phase_PHASE_TERMINATED, 0, "terminated"},
		{anovelv1.Phase_PHASE_TERMINATED, anovelv1.ExitReason_EXIT_REASON_SUCCESS, "terminated (success)"},
		{anovelv1.Phase_PHASE_TERMINATED, anovelv1.ExitReason_EXIT_REASON_ERROR, "terminated (error)"},
		{anovelv1.Phase_PHASE_TERMINATED, anovelv1.ExitReason_EXIT_REASON_KILLED, "terminated (killed)"},
		{anovelv1.Phase_PHASE_TERMINATED, anovelv1.ExitReason_EXIT_REASON_CRASHED, "terminated (crashed)"},
		// PENDING, like any unhandled phase, falls through to the stringified
		// enum form.
		{anovelv1.Phase_PHASE_PENDING, 0, anovelv1.Phase_PHASE_PENDING.String()},
	}
	for _, testCase := range testCases {
		ev := runner.PhaseEvent{TargetID: id, NewPhase: testCase.phase, ExitReason: testCase.reason}
		if got := describePhaseEvent(ev); got != id+" "+testCase.want {
			t.Errorf("describePhaseEvent(%v): got %q want %q", ev, got, id+" "+testCase.want)
		}
	}
}

// TestListStacksReportsDiscoveredStacks pins that ListStacks and Status
// advertise the stacks discovery kept, in registration order, so they match
// what every other RPC accepts. Each stack carries its discovery problems in
// the text the daemon log prints.
func TestListStacksReportsDiscoveredStacks(t *testing.T) {
	t.Parallel()

	problems := []discovery.DiscoveryError{
		{Service: "service-a", Reason: "compose service has no profile"},
		{Service: "service-b", Path: "/w/app/service-b/cmd/rest", Reason: "cmd/rest/ has no compose service"},
	}
	srv := &Server{stacks: discovery.Stacks{
		{Name: "default", Path: "/w", Default: true, Errors: problems},
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

	wantProblems := []string{problems[0].Error(), problems[1].Error()}
	if sent := got[0].GetDiscoveryErrors(); !slices.Equal(sent, wantProblems) {
		t.Errorf("default stack problems = %q, want %q", sent, wantProblems)
	}
	if sent := got[1].GetDiscoveryErrors(); len(sent) != 0 {
		t.Errorf("alive stack problems = %q, want none", sent)
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

	testCases := []struct {
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
	for _, testCase := range testCases {
		got, err := execExitCode(testCase.in)
		if (err != nil) != testCase.wantErr {
			t.Errorf("execExitCode(%s): got err %v want wantErr=%v", testCase.name, err, testCase.wantErr)
			continue
		}
		if got != testCase.want {
			t.Errorf("execExitCode(%s): got %d want %d", testCase.name, got, testCase.want)
		}
	}
}
