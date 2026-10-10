package env

import (
	"slices"
	"strconv"
	"sync"
	"testing"
)

// Allocator tests exercise the refcounted (owner, localVar) → port store behind
// every daemon port allocation. A defect here breaks port reuse, letting two
// services bind one slot, or leaks slots that never free after a target dies.

// TestAllocator_RejectsNonAllocatedKind: only *_PORT is allocatable, through
// Acquire or Reserve.
func TestAllocator_RejectsNonAllocatedKind(t *testing.T) {
	a := NewAllocator()
	if _, err := a.Acquire("svc", "HOST", "consumer"); err == nil {
		t.Error("Acquire(HOST) should reject")
	}
	if got := a.Reserve("svc", "HOST", 1234, "consumer"); got != 0 {
		t.Errorf("Reserve(HOST): got %d want 0 (rejected)", got)
	}
}

func TestAllocator_AcquireRefcounts(t *testing.T) {
	// A repeated Acquire from one consumer returns the same port and holds a
	// single ref. In the cross-service case service-A allocates
	// SERVICE_B_GRPC_PORT, then service-B's own grpc target must land on that
	// same port, and the slot survives until each consumer releases.
	a := NewAllocator()
	consumers := []string{"consumer-a", "consumer-a", "consumer-b"}
	ports := make([]int, len(consumers))
	for i, consumer := range consumers {
		p, err := a.Acquire("svc-b", "GRPC_PORT", consumer)
		if err != nil {
			t.Fatalf("Acquire(%s): %v", consumer, err)
		}
		ports[i] = p
	}
	if ports[0] != ports[1] || ports[0] != ports[2] {
		t.Errorf("Acquire on one key: got ports %v, want one", ports)
	}
	if snap := a.Snapshot(); len(snap) != 1 || !slices.Equal(snap[0].Refs, []string{"consumer-a", "consumer-b"}) {
		t.Errorf("expected 1 slot with one ref per consumer, got %+v", snap)
	}
	a.Release("consumer-a")
	if p, ok := a.Lookup("svc-b", "GRPC_PORT"); !ok || p != ports[0] {
		t.Errorf("after partial Release, slot should still exist: lookup got (%d, %v)", p, ok)
	}
	a.Release("consumer-b")
	if _, ok := a.Lookup("svc-b", "GRPC_PORT"); ok {
		t.Error("after full Release, slot should be gone")
	}
}

func TestAllocator_Reserve(t *testing.T) {
	// Reserve is the adoption path: a restarting daemon finds an already-bound
	// container and re-seeds the slot with its port, with no kernel call. Over
	// an existing slot it takes a ref and returns the port already recorded,
	// ignoring the one passed, which keeps a reseed robust when an Acquire
	// fires first.
	a := NewAllocator()
	if port := a.Reserve("svc", "POSTGRES_PORT", 41699, "infra-consumer"); port != 41699 {
		t.Errorf("Reserve should return the passed port: got %d want 41699", port)
	}
	if p, ok := a.Lookup("svc", "POSTGRES_PORT"); !ok || p != 41699 {
		t.Errorf("Reserve should populate Lookup: got (%d, %v)", p, ok)
	}
	first, err := a.Acquire("svc", "REST_PORT", "owner-a")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if got := a.Reserve("svc", "REST_PORT", first+999, "owner-b"); got != first {
		t.Errorf("Reserve over existing slot: got %d want %d (existing port)", got, first)
	}
	if snap := a.Snapshot(); len(snap) != 2 || len(snap[1].Refs) != 2 {
		t.Errorf("expected the REST_PORT slot with 2 refs, got %+v", snap)
	}
}

func TestAllocator_UnknownSlot(t *testing.T) {
	a := NewAllocator()
	if p, ok := a.Lookup("nobody", "REST_PORT"); ok || p != 0 {
		t.Errorf("Lookup of missing slot: got (%d, %v) want (0, false)", p, ok)
	}
	a.Release("never-acquired") // shouldn't panic, shouldn't error
	a.Release("never-acquired") // still no-op
}

func TestAllocator_SnapshotDeterministic(t *testing.T) {
	a := NewAllocator()
	_, _ = a.Acquire("svc-b", "REST_PORT", "c1")
	_, _ = a.Acquire("svc-a", "GRPC_PORT", "c2")
	_, _ = a.Acquire("svc-a", "REST_PORT", "c3")
	snap := a.Snapshot()
	got := make([]string, len(snap))
	for i, s := range snap {
		got[i] = s.Owner + "/" + s.LocalVar
	}
	// Sorted by (owner, localVar).
	if want := []string{"svc-a/GRPC_PORT", "svc-a/REST_PORT", "svc-b/REST_PORT"}; !slices.Equal(got, want) {
		t.Errorf("snapshot: got %v want %v", got, want)
	}
}

func TestAllocator_SetServicesLongestFirst(t *testing.T) {
	a := NewAllocator()
	a.SetServices([]string{"svc-a", "svc-aaa", "svc-ab", "svc-aa"})
	// Sorted by length desc, ties by lex asc.
	if got, want := a.Services(), []string{"svc-aaa", "svc-aa", "svc-ab", "svc-a"}; !slices.Equal(got, want) {
		t.Errorf("Services(): got %v want %v", got, want)
	}
}

func TestAllocator_ConcurrentAcquireSameKey(t *testing.T) {
	// Race one (owner, localVar) across many goroutines: each gets the same
	// port, nothing panics, and the refcount matches the goroutine count.
	a := NewAllocator()
	const N = 64
	var wg sync.WaitGroup
	wg.Add(N)
	got := make([]int, N)
	for i := range N {
		go func(idx int) {
			defer wg.Done()
			p, err := a.Acquire("svc", "REST_PORT", "consumer-"+strconv.Itoa(idx))
			if err != nil {
				t.Errorf("goroutine %d Acquire: %v", idx, err)
				return
			}
			got[idx] = p
		}(i)
	}
	wg.Wait()
	first := got[0]
	if first == 0 {
		t.Fatal("first acquire returned port 0")
	}
	for i, p := range got {
		if p != first {
			t.Errorf("concurrent Acquire goroutine %d: got %d want %d", i, p, first)
		}
	}
	snap := a.Snapshot()
	if len(snap) != 1 || len(snap[0].Refs) != N {
		t.Errorf("expected 1 slot with %d refs, got %d slot(s) refs=%v",
			N, len(snap), snap[0].Refs)
	}
}
