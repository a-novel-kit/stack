package cli

import (
	"errors"
	"testing"
)

// TestExecResult pins how `a-novel run exec` propagates a child's exit status,
// so a failing exec stops a `&&` chain.
func TestExecResult(t *testing.T) {
	code := func(v int32) *int32 { return &v }

	testCases := []struct {
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

	for _, testCase := range testCases {
		err := execResult(testCase.exitCode)
		var exitErr *ExitError
		gotExit := 0
		if errors.As(err, &exitErr) {
			gotExit = exitErr.Code
		}
		if gotExit != testCase.wantExit || (err != nil && gotExit == 0) != testCase.wantErr {
			t.Errorf("execResult(%s) = %v, want exit code %d (non-exit error %v)", testCase.name, err, testCase.wantExit, testCase.wantErr)
		}
	}
}
