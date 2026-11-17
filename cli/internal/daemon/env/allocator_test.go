package env_test

import (
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
)

// TestAllocator exercises the refcounted (owner, localVar) → port store behind
// every daemon port allocation. A defect here breaks port reuse, letting two
// services bind one slot, or leaks slots that never free after a target dies.
func TestAllocator(t *testing.T) {
	t.Parallel()

	t.Run("Error/NotAllocatedKind", func(t *testing.T) {
		t.Parallel()

		// Only *_PORT is allocatable, through Acquire or Reserve.
		a := env.NewAllocator()
		_, err := a.Acquire("svc", "HOST", "consumer")
		require.Error(t, err)
		require.Zero(t, a.Reserve("svc", "HOST", 1234, "consumer"))
	})

	t.Run("Success/AcquireRefcounts", func(t *testing.T) {
		t.Parallel()

		// A repeated Acquire from one consumer returns the same port and holds a
		// single ref. In the cross-service case service-A allocates
		// SERVICE_B_GRPC_PORT, then service-B's own grpc target must land on that
		// same port, and the slot survives until each consumer releases.
		a := env.NewAllocator()
		consumers := []string{"consumer-a", "consumer-a", "consumer-b"}
		ports := make([]int, len(consumers))
		for i, consumer := range consumers {
			port, err := a.Acquire("svc-b", "GRPC_PORT", consumer)
			require.NoError(t, err)
			ports[i] = port
		}
		require.Equal(t, slices.Repeat(ports[:1], len(consumers)), ports)
		require.Equal(t, []env.Allocation{
			{Owner: "svc-b", LocalVar: "GRPC_PORT", Port: ports[0], Refs: []string{"consumer-a", "consumer-b"}},
		}, a.Snapshot())

		a.Release("consumer-a")
		port, ok := a.Lookup("svc-b", "GRPC_PORT")
		require.True(t, ok, "the slot must survive a partial Release")
		require.Equal(t, ports[0], port)

		a.Release("consumer-b")
		_, ok = a.Lookup("svc-b", "GRPC_PORT")
		require.False(t, ok, "the slot must go with its last consumer")
	})

	t.Run("Success/Reserve", func(t *testing.T) {
		t.Parallel()

		// Reserve is the adoption path: a restarting daemon finds an already-bound
		// container and re-seeds the slot with its port, with no kernel call. Over
		// an existing slot it takes a ref and returns the port already recorded,
		// ignoring the one passed, which keeps a reseed robust when an Acquire
		// fires first.
		a := env.NewAllocator()
		require.Equal(t, 41699, a.Reserve("svc", "POSTGRES_PORT", 41699, "infra-consumer"))
		port, ok := a.Lookup("svc", "POSTGRES_PORT")
		require.True(t, ok)
		require.Equal(t, 41699, port)

		first, err := a.Acquire("svc", "REST_PORT", "owner-a")
		require.NoError(t, err)
		require.Equal(t, first, a.Reserve("svc", "REST_PORT", first+999, "owner-b"))
		require.Equal(t, []env.Allocation{
			{Owner: "svc", LocalVar: "POSTGRES_PORT", Port: 41699, Refs: []string{"infra-consumer"}},
			{Owner: "svc", LocalVar: "REST_PORT", Port: first, Refs: []string{"owner-a", "owner-b"}},
		}, a.Snapshot())
	})

	t.Run("Success/UnknownSlot", func(t *testing.T) {
		t.Parallel()

		a := env.NewAllocator()
		port, ok := a.Lookup("nobody", "REST_PORT")
		require.False(t, ok)
		require.Zero(t, port)
		// Releasing a consumer that holds nothing is a no-op, every time.
		a.Release("never-acquired")
		a.Release("never-acquired")
	})

	t.Run("Success/SnapshotSorted", func(t *testing.T) {
		t.Parallel()

		a := env.NewAllocator()
		_, _ = a.Acquire("svc-b", "REST_PORT", "c1")
		_, _ = a.Acquire("svc-a", "GRPC_PORT", "c2")
		_, _ = a.Acquire("svc-a", "REST_PORT", "c3")
		snap := a.Snapshot()
		got := make([]string, len(snap))
		for i, s := range snap {
			got[i] = s.Owner + "/" + s.LocalVar
		}
		// Sorted by (owner, localVar).
		require.Equal(t, []string{"svc-a/GRPC_PORT", "svc-a/REST_PORT", "svc-b/REST_PORT"}, got)
	})

	t.Run("Success/ServicesLongestFirst", func(t *testing.T) {
		t.Parallel()

		a := env.NewAllocator()
		a.SetServices([]string{"svc-a", "svc-aaa", "svc-ab", "svc-aa"})
		// Sorted by length desc, ties by lex asc.
		require.Equal(t, []string{"svc-aaa", "svc-aa", "svc-ab", "svc-a"}, a.Services())
	})

	t.Run("Success/ConcurrentAcquireSameKey", func(t *testing.T) {
		t.Parallel()

		// Race one (owner, localVar) across many goroutines: each gets the same
		// port, nothing panics, and the refcount matches the goroutine count.
		a := env.NewAllocator()
		const n = 64
		ports := make([]int, n)
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() {
				ports[i], errs[i] = a.Acquire("svc", "REST_PORT", "consumer-"+strconv.Itoa(i))
			})
		}
		wg.Wait()

		require.NoError(t, errors.Join(errs...))
		require.NotZero(t, ports[0])
		require.Equal(t, slices.Repeat(ports[:1], n), ports)
		snap := a.Snapshot()
		require.Len(t, snap, 1)
		require.Len(t, snap[0].Refs, n)
	})
}
