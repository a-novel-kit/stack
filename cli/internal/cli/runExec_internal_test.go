package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExecResult pins how `a-novel run exec` propagates a child's exit status,
// so a failing exec stops a `&&` chain.
func TestExecResult(t *testing.T) {
	t.Parallel()

	code := func(v int32) *int32 { return &v }

	testCases := []struct {
		name     string
		exitCode *int32
		// wantExit is the code carried by an *ExitError; 0 means "expect no ExitError".
		wantExit int
		// wantErr covers the other failure shape: the daemon never reported at all.
		wantErr bool
	}{
		{name: "Success", exitCode: code(0)},
		{name: "Error/ExitCode", exitCode: code(3), wantExit: 3},
		// A signal-killed child reports -1, and the code stays non-zero so it
		// never reads as success.
		{name: "Error/Signalled", exitCode: code(-1), wantExit: -1},
		// The case the terminal message exists for: no report is not success.
		{name: "Error/NoTerminalMessage", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := execResult(testCase.exitCode)
			switch {
			case testCase.wantExit != 0:
				var exitErr *ExitError
				require.ErrorAs(t, err, &exitErr)
				require.Equal(t, testCase.wantExit, exitErr.Code)
			case testCase.wantErr:
				require.Error(t, err)
				require.NotErrorAs(t, err, new(*ExitError))
			default:
				require.NoError(t, err)
			}
		})
	}
}
