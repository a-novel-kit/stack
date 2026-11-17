package runner

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestTranslatePodmanStatus pins the phase adoption reads from the Status column
// `podman ps -a` prints, stopped containers included. A running container marks
// its service's infra session Up, which short-circuits EnsureDepsReady, so the
// phase decides whether a target starts against a database that is serving.
func TestTranslatePodmanStatus(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		status string

		wantPhase  anovelv1.Phase
		wantHealth anovelv1.Health
	}{
		{name: "Success/Up", status: "Up 11 minutes", wantPhase: anovelv1.Phase_PHASE_RUNNING, wantHealth: anovelv1.Health_HEALTH_UNKNOWN},
		{name: "Success/UpHealthy", status: "Up 2 hours (healthy)", wantPhase: anovelv1.Phase_PHASE_RUNNING, wantHealth: anovelv1.Health_HEALTH_HEALTHY},
		{name: "Success/UpStarting", status: "Up 4 seconds (starting)", wantPhase: anovelv1.Phase_PHASE_RUNNING, wantHealth: anovelv1.Health_HEALTH_STARTING},
		{name: "Success/UpUnhealthy", status: "Up 3 minutes (unhealthy)", wantPhase: anovelv1.Phase_PHASE_RUNNING, wantHealth: anovelv1.Health_HEALTH_UNHEALTHY},

		// A container the operator killed, or one that crashed. Adoption sees these on every daemon
		// restart, and treating them as Up leaves the service pointing at nothing.
		{name: "Success/Exited", status: "Exited (0) 5 minutes ago", wantPhase: anovelv1.Phase_PHASE_TERMINATED, wantHealth: anovelv1.Health_HEALTH_UNSPECIFIED},
		{name: "Success/ExitedKilled", status: "Exited (137) 2 days ago", wantPhase: anovelv1.Phase_PHASE_TERMINATED, wantHealth: anovelv1.Health_HEALTH_UNSPECIFIED},
		{name: "Success/Stopped", status: "Stopped", wantPhase: anovelv1.Phase_PHASE_TERMINATED, wantHealth: anovelv1.Health_HEALTH_UNSPECIFIED},

		// Created but never started: the container exists and serves nothing.
		{name: "Success/Created", status: "Created", wantPhase: anovelv1.Phase_PHASE_STARTING, wantHealth: anovelv1.Health_HEALTH_UNKNOWN},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			phase, health := translatePodmanStatus(testCase.status)

			require.Equal(t, testCase.wantPhase, phase)
			require.Equal(t, testCase.wantHealth, health)
		})
	}
}

// TestAdoptEntries applies the predicate adoption uses before marking a session
// Up. Only a running infra container clears it, so a stopped or half-created
// one, or a running target container beside them, leaves EnsureDepsReady to
// bring the infra up. discovery is absent, so only the infra decisions run.
func TestAdoptEntries(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		// statuses are the service's infra containers.
		statuses []string
		// target is the status of a target container beside them, "" for none.
		target string

		wantUp bool
	}{
		{name: "Success/Up", statuses: []string{"Up 11 minutes"}, wantUp: true},
		{name: "Success/UpHealthy", statuses: []string{"Up 2 hours (healthy)"}, wantUp: true},
		// A container the operator killed with `run kill` survives in Exited state so it can be
		// restarted, so adoption meets one on every daemon restart after a kill.
		{name: "Success/Exited", statuses: []string{"Exited (0) 5 minutes ago"}},
		{name: "Success/Stopped", statuses: []string{"Stopped"}},
		{name: "Success/Created", statuses: []string{"Created"}},
		// The whole service is down. EnsureDepsReady has to bring it up rather than short-circuit
		// on a session flagged from the corpses.
		{name: "Success/AllExited", statuses: []string{"Exited (0) 2 minutes ago", "Exited (137) 2 minutes ago"}},
		// A running target container says nothing about its database.
		{name: "Success/UpTargetBesideExitedInfra", statuses: []string{"Exited (137) 2 minutes ago"}, target: "Up 3 minutes"},
		{name: "Success/UpTargetBesideUpInfra", statuses: []string{"Up 11 minutes"}, target: "Up 3 minutes", wantUp: true},
	}

	labels := map[string]string{"anovel.stack": "default", "anovel.service": "service-json-keys"}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

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

			sess, _ := r.InfraSession("default", "service-json-keys")
			require.Equal(t, testCase.wantUp, sess.Up)
		})
	}
}

// TestReseedAllocator covers adoption reseeding the env allocator from the
// adopted infra containers. One podman call inspects them all, and each line
// finds its container by ID, so a container that vanished since the scan skips
// only itself.
//
//nolint:paralleltest // fakePodmanInspect puts a stand-in podman on PATH with t.Setenv.
func TestReseedAllocator(t *testing.T) {
	// inspectLines are the lines podman prints per container ID. A container
	// with no line has vanished.
	inspectLines := map[string]string{
		"cid-a": `cid-a|default_svc-a_postgres-a_1|{"5432/tcp":[{"HostIp":"","HostPort":"38631"}]}` + "\n",
		"cid-b": `cid-b|default_svc-b_valkey-b_1|{"6379/tcp":[{"HostIp":"","HostPort":"38632"}]}` + "\n",
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
			dir := fakePodmanInspect(t)
			fsutiltest.Write(t, dir, inspectLines)

			alloc := env.NewAllocator()
			alloc.SetServices([]string{"svc-a", "svc-b"})
			r := &Runner{stacks: stacks, alloc: alloc}
			infra := make([]psEntry, 0, len(testCase.containers))
			for _, c := range testCase.containers {
				infra = append(infra, psEntry{ID: c[1], Labels: map[string]string{labelStack: "default", labelService: c[0]}})
			}

			r.reseedAllocator(t.Context(), infra)

			require.Equal(t, 1, podmanCalls(t, dir), "podman calls")
			for _, want := range wantPorts {
				port, ok := alloc.Lookup(want.owner, want.localVar)
				require.True(t, ok, "%s/%s has no port", want.owner, want.localVar)
				require.Equal(t, want.port, port, "%s/%s", want.owner, want.localVar)
			}
		})
	}
}
