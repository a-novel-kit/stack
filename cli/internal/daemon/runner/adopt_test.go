package runner

import (
	"testing"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Adoption scans `podman ps -a`, which lists stopped containers alongside running ones, and marks a
// service's infra session Up from what it finds. An Up session short-circuits EnsureDepsReady, so
// the phase this returns decides whether a target starts against a database that is serving.
//
// The strings are what podman prints in the Status column.

func TestTranslatePodmanStatus(t *testing.T) {
	cases := []struct {
		status string
		phase  anovelv1.Phase
		health anovelv1.Health
	}{
		{"Up 11 minutes", anovelv1.Phase_PHASE_RUNNING, anovelv1.Health_HEALTH_UNKNOWN},
		{"Up 2 hours (healthy)", anovelv1.Phase_PHASE_RUNNING, anovelv1.Health_HEALTH_HEALTHY},
		{"Up 4 seconds (starting)", anovelv1.Phase_PHASE_RUNNING, anovelv1.Health_HEALTH_STARTING},
		{"Up 3 minutes (unhealthy)", anovelv1.Phase_PHASE_RUNNING, anovelv1.Health_HEALTH_UNHEALTHY},

		// A container the operator killed, or one that crashed. Adoption sees these on every daemon
		// restart, and treating them as Up leaves the service pointing at nothing.
		{"Exited (0) 5 minutes ago", anovelv1.Phase_PHASE_TERMINATED, anovelv1.Health_HEALTH_UNSPECIFIED},
		{"Exited (137) 2 days ago", anovelv1.Phase_PHASE_TERMINATED, anovelv1.Health_HEALTH_UNSPECIFIED},
		{"Stopped", anovelv1.Phase_PHASE_TERMINATED, anovelv1.Health_HEALTH_UNSPECIFIED},

		// Created but never started: the container exists and serves nothing.
		{"Created", anovelv1.Phase_PHASE_STARTING, anovelv1.Health_HEALTH_UNKNOWN},
	}

	for _, c := range cases {
		phase, health := translatePodmanStatus(c.status)
		if phase != c.phase || health != c.health {
			t.Errorf("translatePodmanStatus(%q): got (%v, %v) want (%v, %v)",
				c.status, phase, health, c.phase, c.health)
		}
	}
}

func TestTranslatePodmanStatusGatesTheInfraSession(t *testing.T) {
	// The predicate adoption applies before marking a session Up. Only a running container clears it,
	// so a stopped or half-created one leaves EnsureDepsReady to bring the infra up.
	up := []string{"Up 11 minutes", "Up 2 hours (healthy)", "Up 3 minutes (unhealthy)"}
	notUp := []string{"Exited (0) 5 minutes ago", "Exited (137) 2 days ago", "Stopped", "Created"}

	for _, status := range up {
		if phase, _ := translatePodmanStatus(status); phase != anovelv1.Phase_PHASE_RUNNING {
			t.Errorf("%q should mark the infra session up, got %v", status, phase)
		}
	}

	for _, status := range notUp {
		if phase, _ := translatePodmanStatus(status); phase == anovelv1.Phase_PHASE_RUNNING {
			t.Errorf("%q should leave the infra session down, got %v", status, phase)
		}
	}
}

// newRunnerForAdopt builds the minimum an adoption scan touches: the instance map and the infra
// session store. discovery is absent, so target entries are left orphaned and only the infra
// decisions run.
func newRunnerForAdopt() *Runner {
	return &Runner{
		instances:     map[string]*Instance{},
		infraSessions: map[string]*infraSession{},
	}
}

func TestAdoptEntriesMarksTheSessionUpFromRunningContainersOnly(t *testing.T) {
	infra := func(status string) podmanEntry {
		return podmanEntry{
			ID:     "cid-" + status,
			Status: status,
			Labels: map[string]string{
				"anovel.stack":   "default",
				"anovel.service": "service-json-keys",
			},
		}
	}

	cases := []struct {
		status string
		up     bool
	}{
		{"Up 11 minutes", true},
		{"Up 2 hours (healthy)", true},
		// A container the operator killed with `run kill` survives in Exited state so it can be
		// restarted, so adoption meets one on every daemon restart after a kill.
		{"Exited (0) 5 minutes ago", false},
		{"Stopped", false},
		{"Created", false},
	}

	for _, c := range cases {
		r := newRunnerForAdopt()
		r.adoptEntries(t.Context(), []podmanEntry{infra(c.status)})

		sess, _ := r.InfraSession("default", "service-json-keys")
		got := sess.Up
		if got != c.up {
			t.Errorf("infra %q: session up = %v, want %v", c.status, got, c.up)
		}
	}
}

func TestAdoptEntriesLeavesTheSessionDownWhenEveryContainerIsStopped(t *testing.T) {
	// The whole service is down. EnsureDepsReady has to bring it up rather than short-circuit on a
	// session flagged from the corpses.
	r := newRunnerForAdopt()

	labels := map[string]string{"anovel.stack": "default", "anovel.service": "service-json-keys"}
	r.adoptEntries(t.Context(), []podmanEntry{
		{ID: "cid-pg", Status: "Exited (0) 2 minutes ago", Labels: labels},
		{ID: "cid-mail", Status: "Exited (137) 2 minutes ago", Labels: labels},
	})

	if sess, _ := r.InfraSession("default", "service-json-keys"); sess.Up {
		t.Error("every container is stopped, but the infra session reads Up")
	}
}
