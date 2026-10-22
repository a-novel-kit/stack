package cli

import (
	"testing"
	"time"
)

// TestSSHMuxUp pins the check window: a success vouches for the connection
// during sshUpWindow, and a failure leaves the next call to check again.
//
//nolint:paralleltest // swaps the package-level connectSSH.
func TestSSHMuxUp(t *testing.T) {
	var checks int
	var connected bool
	swap(t, &connectSSH, func([]string) bool {
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
		if up := m.up(start.Add(step.at)); up != step.wantUp {
			t.Errorf("%s: up = %v, want %v", step.name, up, step.wantUp)
		}
		if checks != step.wantChecks {
			t.Errorf("%s: %d checks, want %d", step.name, checks, step.wantChecks)
		}
	}
}
