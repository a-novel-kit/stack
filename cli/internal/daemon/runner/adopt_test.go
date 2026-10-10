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

// TestAdoptEntriesMarksTheSessionUpFromRunningContainersOnly applies the
// predicate adoption uses before marking a session Up. Only a running container
// clears it, so a stopped or half-created one leaves EnsureDepsReady to bring
// the infra up. discovery is absent, so only the infra decisions run.
func TestAdoptEntriesMarksTheSessionUpFromRunningContainersOnly(t *testing.T) {
	cases := []struct {
		statuses []string
		up       bool
	}{
		{[]string{"Up 11 minutes"}, true},
		{[]string{"Up 2 hours (healthy)"}, true},
		// A container the operator killed with `run kill` survives in Exited state so it can be
		// restarted, so adoption meets one on every daemon restart after a kill.
		{[]string{"Exited (0) 5 minutes ago"}, false},
		{[]string{"Stopped"}, false},
		{[]string{"Created"}, false},
		// The whole service is down. EnsureDepsReady has to bring it up rather than short-circuit
		// on a session flagged from the corpses.
		{[]string{"Exited (0) 2 minutes ago", "Exited (137) 2 minutes ago"}, false},
	}

	labels := map[string]string{"anovel.stack": "default", "anovel.service": "service-json-keys"}
	for _, c := range cases {
		r := &Runner{instances: map[string]*Instance{}, infraSessions: map[string]*infraSession{}}
		entries := make([]psEntry, len(c.statuses))
		for i, status := range c.statuses {
			entries[i] = psEntry{ID: "cid-" + status, Status: status, Labels: labels}
		}
		r.adoptEntries(t.Context(), entries)

		if sess, _ := r.InfraSession("default", "service-json-keys"); sess.Up != c.up {
			t.Errorf("infra %q: session up = %v, want %v", c.statuses, sess.Up, c.up)
		}
	}
}
