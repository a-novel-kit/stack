package cli

import (
	"errors"
	"testing"
)

// Tests for execResult, the mapping `a-novel run exec` relies on to propagate a child's
// exit status. Before the stream carried a terminal exit code, this command always exited
// 0 no matter what the child did, so a failing exec chained straight into the next step of
// a `&&` list — while the help text promised exit-code fidelity.

func TestExecResult(t *testing.T) {
	code := func(v int32) *int32 { return &v }

	cases := []struct {
		name     string
		exitCode *int32
		// wantExit is the code carried by an *ExitError; 0 means "expect no error".
		wantExit int
		// wantErr covers the other failure shape: the daemon never reported at all.
		wantErr bool
	}{
		{"success", code(0), 0, false},
		{"failure", code(3), 3, false},
		// A signal-killed child reports -1, and the code stays non-zero so it
		// never reads as success.
		{"signalled", code(-1), -1, false},
		// The case the terminal message exists for: no report is not success.
		{"no terminal message", nil, 0, true},
	}

	for _, c := range cases {
		err := execResult(c.exitCode)
		var exitErr *ExitError
		gotExit := 0
		if errors.As(err, &exitErr) {
			gotExit = exitErr.Code
		}
		if gotExit != c.wantExit || (err != nil && gotExit == 0) != c.wantErr {
			t.Errorf("execResult(%s) = %v, want exit code %d (non-exit error %v)", c.name, err, c.wantExit, c.wantErr)
		}
	}
}

// TestParseEntityID pins the ID shapes kill, restart, logs and start accept. A
// shorthand resolves against the first $A_NOVEL_STACKS entry, not a stack
// literally named "default".
func TestParseEntityID(t *testing.T) {
	// Not parallel: t.Setenv redirects the stack registry.
	t.Setenv("A_NOVEL_STACKS", "main:/srv/main,fork:/srv/fork")

	cases := []struct {
		arg, stack string
		want       entityRef
	}{
		{arg: "svc/rest", want: entityRef{Stack: "main", Service: "svc", Target: "rest", ID: "main/svc/rest"}},
		{arg: "svc/rest", stack: "fork", want: entityRef{Stack: "fork", Service: "svc", Target: "rest", ID: "fork/svc/rest"}},
		{arg: "fork/svc/rest", want: entityRef{Stack: "fork", Service: "svc", Target: "rest", ID: "fork/svc/rest"}},
		{arg: "svc/infra/pg", want: entityRef{IsInfra: true, Stack: "main", Service: "svc", Infra: "pg", ID: "main/svc/infra/pg"}},
		{arg: "fork/svc/infra/pg", want: entityRef{IsInfra: true, Stack: "fork", Service: "svc", Infra: "pg", ID: "fork/svc/infra/pg"}},
		// "infra" anywhere but second-to-last is an ordinary target name.
		{arg: "svc/infra", want: entityRef{Stack: "main", Service: "svc", Target: "infra", ID: "main/svc/infra"}},
	}
	for _, c := range cases {
		if got := parseEntityID(c.arg, c.stack); got != c.want {
			t.Errorf("parseEntityID(%q, %q) = %+v, want %+v", c.arg, c.stack, got, c.want)
		}
	}
}
