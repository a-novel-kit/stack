package workspace

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/seamtest"
)

// TestSSHMux pins the check window of up: a success vouches for the connection
// during sshUpWindow, and a failure leaves the next call to check again.
//
//nolint:paralleltest // swaps the package-level connectSSH.
func TestSSHMux(t *testing.T) {
	var checks int
	var connected bool
	seamtest.Swap(t, &connectSSH, func([]string) bool {
		checks++
		return connected
	})

	// The steps run in order on one sshMux. connected is what a check finds.
	steps := []struct {
		name       string
		at         time.Duration
		connected  bool
		wantUp     bool
		wantChecks int
	}{
		{name: "FirstCallChecks", connected: true, wantUp: true, wantChecks: 1},
		{name: "InsideWindowSkips", at: sshUpWindow - time.Second, wantUp: true, wantChecks: 1},
		{name: "WindowEndChecks", at: sshUpWindow, wantUp: false, wantChecks: 2},
		{name: "FailureChecksAgain", at: sshUpWindow + time.Second, connected: true, wantUp: true, wantChecks: 3},
		{name: "NewWindowSkips", at: 2 * sshUpWindow, wantUp: true, wantChecks: 3},
	}

	start := time.Now()
	var m sshMux
	for _, step := range steps {
		connected = step.connected
		require.Equal(t, step.wantUp, m.up(start.Add(step.at)), "%s: up", step.name)
		require.Equal(t, step.wantChecks, checks, "%s: checks", step.name)
	}
}
