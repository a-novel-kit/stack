package runner

import (
	"maps"
	"testing"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestTranslatePodmanStatus pins the phase adoption reads from the Status column
// `podman ps -a` prints, stopped containers included. A running container marks
// its service's infra session Up, which short-circuits EnsureDepsReady, so the
// phase decides whether a target starts against a database that is serving.
func TestTranslatePodmanStatus(t *testing.T) {
	testCases := []struct {
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

	for _, testCase := range testCases {
		phase, health := translatePodmanStatus(testCase.status)
		if phase != testCase.phase || health != testCase.health {
			t.Errorf("translatePodmanStatus(%q): got (%v, %v) want (%v, %v)",
				testCase.status, phase, health, testCase.phase, testCase.health)
		}
	}
}

// TestAdoptEntriesMarksTheSessionUpFromRunningContainersOnly applies the
// predicate adoption uses before marking a session Up. Only a running infra
// container clears it, so a stopped or half-created one, or a running target
// container beside them, leaves EnsureDepsReady to bring the infra up.
// discovery is absent, so only the infra decisions run.
func TestAdoptEntriesMarksTheSessionUpFromRunningContainersOnly(t *testing.T) {
	testCases := []struct {
		// statuses are the service's infra containers.
		statuses []string
		// target is the status of a target container beside them, "" for none.
		target string
		up     bool
	}{
		{[]string{"Up 11 minutes"}, "", true},
		{[]string{"Up 2 hours (healthy)"}, "", true},
		// A container the operator killed with `run kill` survives in Exited state so it can be
		// restarted, so adoption meets one on every daemon restart after a kill.
		{[]string{"Exited (0) 5 minutes ago"}, "", false},
		{[]string{"Stopped"}, "", false},
		{[]string{"Created"}, "", false},
		// The whole service is down. EnsureDepsReady has to bring it up rather than short-circuit
		// on a session flagged from the corpses.
		{[]string{"Exited (0) 2 minutes ago", "Exited (137) 2 minutes ago"}, "", false},
		// A running target container says nothing about its database.
		{[]string{"Exited (137) 2 minutes ago"}, "Up 3 minutes", false},
		{[]string{"Up 11 minutes"}, "Up 3 minutes", true},
	}

	labels := map[string]string{"anovel.stack": "default", "anovel.service": "service-json-keys"}
	for _, testCase := range testCases {
		r := &Runner{instances: map[string]*Instance{}, infraSessions: map[string]*infraSession{}}
		entries := make([]psEntry, 0, len(testCase.statuses)+1)
		for _, status := range testCase.statuses {
			entries = append(entries, psEntry{ID: "cid-" + status, Status: status, Labels: labels})
		}
		if testCase.target != "" {
			targetLabels := map[string]string{"anovel.target": "rest"}
			maps.Copy(targetLabels, labels)
			entries = append(entries, psEntry{ID: "cid-target", Status: testCase.target, Labels: targetLabels})
		}
		r.adoptEntries(t.Context(), entries)

		if sess, _ := r.InfraSession("default", "service-json-keys"); sess.Up != testCase.up {
			t.Errorf("infra %q, target %q: session up = %v, want %v", testCase.statuses, testCase.target, sess.Up, testCase.up)
		}
	}
}
