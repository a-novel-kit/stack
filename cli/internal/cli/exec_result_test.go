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
		// A signal-killed child reports -1. It must stay non-zero rather than
		// collapsing into something that reads as success.
		{"signalled", code(-1), -1, false},
		// The case the terminal message exists for: no report is not success.
		{"no terminal message", nil, 0, true},
	}

	for _, c := range cases {
		err := execResult(c.exitCode)

		var exitErr *ExitError

		isExit := errors.As(err, &exitErr)

		switch {
		case c.wantErr:
			if err == nil || isExit {
				t.Errorf("execResult(%s): got (%v, isExitError=%v) want a non-ExitError error",
					c.name, err, isExit)
			}
		case c.wantExit == 0:
			if err != nil {
				t.Errorf("execResult(%s): got %v want nil", c.name, err)
			}
		case !isExit:
			t.Errorf("execResult(%s): got %v want an *ExitError", c.name, err)
		case exitErr.Code != c.wantExit:
			t.Errorf("execResult(%s): got exit code %d want %d", c.name, exitErr.Code, c.wantExit)
		}
	}
}
