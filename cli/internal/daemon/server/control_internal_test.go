package server

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
)

var errKill = errors.New("container stop timed out")

// TestTearDown pins what a shutdown reports to an operator who has no other way
// to check what it managed to stop.
func TestTearDown(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		targets          []string
		sessions         []runner.InfraSessionRef
		killed, tornDown int
		failures         []string // a substring of each failure, sorted
	}{
		{
			// Three targets attempted, two stopped; the count reports the second
			// number. Failures are sorted, so the assertion does not depend on
			// goroutine completion order.
			name:     "Success/CountsOnlyWhatItStopped",
			targets:  []string{"a", "b", "c"},
			sessions: []runner.InfraSessionRef{{Stack: "default", Service: "auth"}, {Stack: "default", Service: "keys"}},
			killed:   2, tornDown: 1,
			failures: []string{"go-exec target b", "default/keys"},
		},
		// Without --force the caller passes no sessions, and nothing may reach
		// podman.
		{name: "Success/SkipsInfraWhenNotForced", targets: []string{"a"}, killed: 1},
		// Nothing running is a clean shutdown, not a failed one.
		{name: "Success/EmptyEnvironment"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var infraCalls atomic.Int32
			out := tearDown(testCase.targets, testCase.sessions,
				func(id string) error {
					if id == "b" {
						return errKill
					}
					return nil
				},
				func(ref runner.InfraSessionRef) error {
					infraCalls.Add(1)
					if ref.Service == "keys" {
						return errKill
					}
					return nil
				})
			require.Equal(t, testCase.killed, out.goExecKilled)
			require.Equal(t, testCase.tornDown, out.infraTornDown)
			require.Equal(t, int32(len(testCase.sessions)), infraCalls.Load())
			require.Len(t, out.failures, len(testCase.failures))
			for i, want := range testCase.failures {
				require.Contains(t, out.failures[i], want)
			}
		})
	}
}
