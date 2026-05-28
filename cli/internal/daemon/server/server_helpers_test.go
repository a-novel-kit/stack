package server

import (
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Tests for the small pure helpers in server.go — parseInfraLogID,
// describePhaseEvent, findInfra, convertModeFromProto. Cheap, but their
// output is wire-visible (StateEvent.Description, log-streaming routing)
// so silent regressions are user-facing.

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
			// PENDING (or unhandled phase): falls through to the
			// stringified enum form. Documents the catch-all so a
			// future "improve string" tweak doesn't break it silently.
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

func TestFindInfra(t *testing.T) {
	svc := &discovery.Service{
		Infra: []*discovery.Infra{
			{Name: "postgres"},
			{Name: "mailserver"},
		},
	}
	if got := findInfra(svc, "postgres"); got == nil || got.Name != "postgres" {
		t.Errorf("findInfra(postgres): got %v", got)
	}
	if got := findInfra(svc, "missing"); got != nil {
		t.Errorf("findInfra(missing): got %v want nil", got)
	}
	// Note: findInfra does not nil-check its svc — callers in this
	// package always pass a discovered svc, so adding the guard would
	// be defensive overhead. Don't probe `findInfra(nil, ...)`.
}

func TestConvertModeFromProto(t *testing.T) {
	cases := []struct {
		in   anovelv1.Mode
		want runner.Mode
	}{
		{anovelv1.Mode_MODE_GO_EXEC, runner.ModeGoExec},
		{anovelv1.Mode_MODE_CONTAINER, runner.ModeContainer},
		// Unspecified defaults to go-exec — covers the CLI's bare
		// `a-novel run start` case where no mode flag was passed.
		{anovelv1.Mode_MODE_UNSPECIFIED, runner.ModeGoExec},
	}
	for _, c := range cases {
		if got := convertModeFromProto(c.in); got != c.want {
			t.Errorf("convertModeFromProto(%v): got %v want %v", c.in, got, c.want)
		}
	}
}

func TestUnimplementedCarriesPhaseLabel(t *testing.T) {
	// The standard stub message includes the phase reference so users
	// know when to expect the feature. Make sure the format doesn't
	// silently change.
	err := unimplemented("Foo", "phase 99")
	if err == nil {
		t.Fatal("nil error")
	}
	if !strings.Contains(err.Error(), "Foo") || !strings.Contains(err.Error(), "phase 99") {
		t.Errorf("unimplemented label missing pieces: %q", err.Error())
	}
}
