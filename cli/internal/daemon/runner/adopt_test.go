package runner

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
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

// TestReseedAllocator covers adoption reseeding the env allocator from the
// adopted infra containers. One podman call inspects them all, and each line
// finds its container by ID, so a container that vanished since the scan skips
// only itself.
func TestReseedAllocator(t *testing.T) {
	// inspectLines are the lines podman prints per container ID. A container
	// with no line has vanished.
	inspectLines := map[string]string{
		"cid-a": `cid-a|default_svc-a_postgres-a_1|{"5432/tcp":[{"HostIp":"","HostPort":"38631"}]}`,
		"cid-b": `cid-b|default_svc-b_valkey-b_1|{"6379/tcp":[{"HostIp":"","HostPort":"38632"}]}`,
	}
	stacks := discovery.Stacks{{Name: "default", Services: []*discovery.Service{
		{Name: "svc-a", Stack: "default", Infra: []*discovery.Infra{{Name: "postgres-a", Ports: []string{"${POSTGRES_PORT}:5432"}}}},
		{Name: "svc-b", Stack: "default", Infra: []*discovery.Infra{{Name: "valkey-b", Ports: []string{"${VALKEY_PORT}:6379"}}}},
	}}}
	wantPorts := []struct {
		owner, localVar string
		port            int
	}{
		{"svc-a", "POSTGRES_PORT", 38631},
		{"svc-b", "VALKEY_PORT", 38632},
	}

	testCases := []struct {
		name string
		// containers are the adopted infra containers, as service and ID.
		containers [][2]string
	}{
		{name: "Success", containers: [][2]string{{"svc-a", "cid-a"}, {"svc-b", "cid-b"}}},
		// The vanished container sits between the others, which shifts the
		// line positions.
		{name: "Error/Vanished", containers: [][2]string{{"svc-a", "cid-a"}, {"svc-a", "cid-gone"}, {"svc-b", "cid-b"}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// A stand-in podman prints the line of each known ID, then fails
			// with exit 125 when one was unknown, as podman does.
			dir := t.TempDir()
			script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %[1]s/calls
shift 2
status=0
for cid in "$@"; do
	cat %[1]s/"$cid" 2>/dev/null || status=125
done
exit $status
`, dir)
			bin := t.TempDir()
			if err := os.WriteFile(filepath.Join(bin, "podman"), []byte(script), 0o700); err != nil { //nolint:gosec // an executable fixture
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			for id, line := range inspectLines {
				if err := os.WriteFile(filepath.Join(dir, id), []byte(line+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			alloc := env.NewAllocator()
			alloc.SetServices([]string{"svc-a", "svc-b"})
			r := &Runner{stacks: stacks, alloc: alloc}
			infra := make([]psEntry, 0, len(testCase.containers))
			for _, c := range testCase.containers {
				infra = append(infra, psEntry{ID: c[1], Labels: map[string]string{labelStack: "default", labelService: c[0]}})
			}

			r.reseedAllocator(t.Context(), infra)

			if calls, _ := os.ReadFile(filepath.Join(dir, "calls")); strings.Count(string(calls), "\n") != 1 {
				t.Errorf("podman calls = %q, want one", calls)
			}
			for _, want := range wantPorts {
				if port, ok := alloc.Lookup(want.owner, want.localVar); !ok || port != want.port {
					t.Errorf("%s/%s = (%d, %v), want %d", want.owner, want.localVar, port, ok, want.port)
				}
			}
		})
	}
}
