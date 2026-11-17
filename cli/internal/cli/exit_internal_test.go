package cli

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExitStatus pins the status a finished child hands the CLI: its own exit
// code, or 128 plus the signal that ended it, as a shell reports it.
func TestExitStatus(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		script string
		want   int
	}{
		{name: "Success/ExitCode", script: "exit 3", want: 3},
		{name: "Success/Signal", script: "kill -TERM $$", want: 143},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var exitErr *exec.ExitError
			require.ErrorAs(t, exec.CommandContext(t.Context(), "sh", "-c", testCase.script).Run(), &exitErr)
			require.Equal(t, testCase.want, exitStatus(exitErr))
		})
	}
}
