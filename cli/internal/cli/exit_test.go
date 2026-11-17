package cli

import (
	"errors"
	"os/exec"
	"testing"
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
			if err := exec.Command("sh", "-c", testCase.script).Run(); !errors.As(err, &exitErr) {
				t.Fatalf("run %q: got %v, want an exit error", testCase.script, err)
			}
			if got := exitStatus(exitErr); got != testCase.want {
				t.Errorf("exitStatus = %d, want %d", got, testCase.want)
			}
		})
	}
}
