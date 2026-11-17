package env_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
)

// newBuilderWith returns a Builder over a fresh Allocator that knows services,
// and an empty secrets store.
func newBuilderWith(t *testing.T, services []string) (*env.Builder, *env.Allocator) {
	t.Helper()

	alloc := env.NewAllocator()
	alloc.SetServices(services)
	return env.NewBuilder(alloc, t.TempDir()), alloc
}

// TestBuilderForService pins the env block `a-novel run env` shows and
// infra-up hands to compose.
func TestBuilderForService(t *testing.T) {
	t.Parallel()

	t.Run("Success/LookupOnlyLeavesUnknownEmpty", func(t *testing.T) {
		t.Parallel()

		// ForService is the read-only path (`a-novel run env`). The allocator
		// is never mutated, and a var with no existing allocation comes back
		// empty.
		b, alloc := newBuilderWith(t, []string{"service-a", "service-b"})
		svc := &discovery.Service{
			Name:  "service-a",
			Stack: "default",
			Targets: []*discovery.Target{
				{
					Name: "rest", Service: "service-a", Stack: "default",
					Environment: map[string]string{
						"DEP_PORT": "${SERVICE_B_GRPC_PORT}",
					},
				},
			},
		}
		entries, err := b.ForService(svc, "")
		require.NoError(t, err)
		// The substitute pass writes "" for a ref that does not resolve, matching
		// compose's behavior.
		require.Contains(t, entries, env.Entry{Key: "DEP_PORT", Value: ""})
		require.Empty(t, alloc.Snapshot())
	})

	t.Run("Success/OverlappingServiceNamesResolveLongestFirst", func(t *testing.T) {
		t.Parallel()

		// Service names register in discovery order, which is alphabetical, so
		// service-template precedes the service-template-extra it shadows. Every
		// builder path must still route SERVICE_TEMPLATE_EXTRA_* to
		// service-template-extra.
		b, alloc := newBuilderWith(t, []string{"service-consumer", "service-template", "service-template-extra"})
		tgt := &discovery.Target{
			Name:        "rest",
			Service:     "service-consumer",
			Stack:       "default",
			Environment: map[string]string{"DEP_PORT": "${SERVICE_TEMPLATE_EXTRA_GRPC_PORT}"},
		}

		entries, _, err := b.ForTarget(tgt)
		require.NoError(t, err)
		port, ok := alloc.Lookup("service-template-extra", "GRPC_PORT")
		require.True(t, ok, "ForTarget allocated %+v, want service-template-extra/GRPC_PORT", alloc.Snapshot())
		require.Contains(t, entries, env.Entry{Key: "DEP_PORT", Value: strconv.Itoa(port)})

		svc := &discovery.Service{Name: "service-consumer", Stack: "default", Targets: []*discovery.Target{tgt}}
		viewed, err := b.ForService(svc, "")
		require.NoError(t, err)
		require.Contains(t, viewed, env.Entry{Key: "DEP_PORT", Value: strconv.Itoa(port)})
	})

	t.Run("Success/ConsumerAllocatesInfraPorts", func(t *testing.T) {
		t.Parallel()

		// The infra-up path: with a consumer, the ports the infra references are
		// acquired for it, and Release frees them again. Compose publishes the
		// infra on what the block holds, so every allocated port appears with its
		// number, whether a target names it or not. A target's own POSTGRES_PORT,
		// the port inside the compose network, yields to the allocation.
		b, alloc := newBuilderWith(t, []string{"svc"})
		svc := &discovery.Service{
			Name:  "svc",
			Stack: "default",
			Targets: []*discovery.Target{
				{Name: "migrations", Service: "svc", Stack: "default", Environment: map[string]string{
					"POSTGRES_HOST": "postgres-svc",
					"POSTGRES_PORT": "5432",
				}},
				{Name: "rest", Service: "svc", Stack: "default", Environment: map[string]string{"MAIL_ADDR": "${MAIL_HOST}:${MAIL_PORT}"}},
			},
			Infra: []*discovery.Infra{
				{
					Name:        "postgres-svc",
					Service:     "svc",
					Stack:       "default",
					Ports:       []string{"${POSTGRES_PORT}:5432"},
					Environment: map[string]string{"POSTGRES_USER": "postgres"},
				},
				{Name: "mail-svc", Service: "svc", Stack: "default", Ports: []string{"${MAIL_PORT}:1025"}},
			},
		}

		entries, err := b.ForService(svc, "default/svc-infra")
		require.NoError(t, err)
		port, ok := alloc.Lookup("svc", "POSTGRES_PORT")
		require.True(t, ok, "infra-up ForService did not allocate POSTGRES_PORT")
		mailPort, ok := alloc.Lookup("svc", "MAIL_PORT")
		require.True(t, ok, "infra-up ForService did not allocate MAIL_PORT")
		require.Subset(t, entries, []env.Entry{
			{Key: "POSTGRES_USER", Value: "postgres"},
			{Key: "POSTGRES_PORT", Value: strconv.Itoa(port)},
			{Key: "POSTGRES_HOST", Value: "localhost"},
			{Key: "MAIL_PORT", Value: strconv.Itoa(mailPort)},
			{Key: "MAIL_ADDR", Value: "localhost:" + strconv.Itoa(mailPort)},
		})

		alloc.Release("default/svc-infra")
		_, ok = alloc.Lookup("svc", "POSTGRES_PORT")
		require.False(t, ok, "port %d still allocated after releasing the infra consumer", port)
	})
}

// TestBuilderKeepPorts covers the reinstall relaunch: the ports the previous
// daemon recorded are reserved for the target, so its env and every other
// consumer resolve to what the relaunched process binds.
func TestBuilderKeepPorts(t *testing.T) {
	t.Parallel()

	target := func() *discovery.Target {
		return &discovery.Target{
			Name: "rest", Service: "svc", Stack: "default",
			Ports: []string{"${REST_PORT}:8080"},
			Environment: map[string]string{
				"DEP_PORT": "${SERVICE_OTHER_GRPC_PORT}",
				// A constant named like a port is no reference, so it claims nothing.
				"ALIAS_PORT": "1234",
			},
		}
	}
	held := []string{"REST_PORT=41001", "SERVICE_OTHER_GRPC_PORT=41002", "ALIAS_PORT=41003", "STRAY_PORT=41004", "PATH=/bin"}

	testCases := []struct {
		name string
		// seed reserves a slot before KeepPorts runs, as adopting a container does.
		seed     func(a *env.Allocator)
		wantRest int
	}{
		{name: "Success/KeepsReferencedPorts", seed: func(*env.Allocator) {}, wantRest: 41001},
		{name: "Success/AllocatedSlotKeepsItsPort", seed: func(a *env.Allocator) { a.Reserve("svc", "REST_PORT", 40000, "default/svc-infra") }, wantRest: 40000},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			b, alloc := newBuilderWith(t, []string{"svc", "service-other"})
			testCase.seed(alloc)
			tgt := target()
			b.KeepPorts(tgt, held)

			entries, _, err := b.ForTarget(tgt)
			require.NoError(t, err)
			require.Subset(t, entries, []env.Entry{
				{Key: "REST_PORT", Value: strconv.Itoa(testCase.wantRest)},
				{Key: "DEP_PORT", Value: "41002"},
				{Key: "ALIAS_PORT", Value: "1234"},
			})
			snap := alloc.Snapshot()
			claimed := make([]string, len(snap))
			for i, slot := range snap {
				claimed[i] = slot.Owner + "/" + slot.LocalVar
			}
			require.Equal(t, []string{"service-other/GRPC_PORT", "svc/REST_PORT"}, claimed)

			// A sibling consumer resolves the kept port, not a fresh one.
			sibling := &discovery.Target{
				Name: "web", Service: "service-other", Stack: "default",
				Environment: map[string]string{"API_PORT": "${SVC_REST_PORT}"},
			}
			siblingEnv, _, err := b.ForTarget(sibling)
			require.NoError(t, err)
			require.Contains(t, siblingEnv, env.Entry{Key: "API_PORT", Value: strconv.Itoa(testCase.wantRest)})

			// Releasing the relaunched target drops its claims like any other.
			alloc.Release(tgt.ID())
			alloc.Release(sibling.ID())
			_, ok := alloc.Lookup("service-other", "GRPC_PORT")
			require.False(t, ok, "service-other/GRPC_PORT outlived its consumers")
		})
	}
}

// TestBuilderHeldPorts pins the checkpoint side: the ports a running target
// holds, named as its env references them, and nothing else, so the next
// daemon's KeepPorts hands them straight back.
func TestBuilderHeldPorts(t *testing.T) {
	t.Parallel()

	b, alloc := newBuilderWith(t, []string{"svc", "service-other"})
	tgt := &discovery.Target{
		Name: "rest", Service: "svc", Stack: "default",
		Ports:       []string{"${REST_PORT}:8080"},
		Environment: map[string]string{"DEP_PORT": "${SERVICE_OTHER_GRPC_PORT}", "TOKEN": "secret-value"},
	}
	_, _, err := b.ForTarget(tgt)
	require.NoError(t, err)
	rest, _ := alloc.Lookup("svc", "REST_PORT")
	grpc, _ := alloc.Lookup("service-other", "GRPC_PORT")

	held := b.HeldPorts(tgt)
	require.Equal(t, []string{"REST_PORT=" + strconv.Itoa(rest), "SERVICE_OTHER_GRPC_PORT=" + strconv.Itoa(grpc)}, held)

	next, nextAlloc := newBuilderWith(t, []string{"svc", "service-other"})
	next.KeepPorts(tgt, held)
	nextRest, _ := nextAlloc.Lookup("svc", "REST_PORT")
	require.Equal(t, rest, nextRest)
	nextGRPC, _ := nextAlloc.Lookup("service-other", "GRPC_PORT")
	require.Equal(t, grpc, nextGRPC)
}
