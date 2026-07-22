package env

import (
	"sync"
	"testing"
)

// Allocator tests exercise the refcounted (owner, localVar) → port store behind
// every daemon port allocation. A defect here breaks port reuse, letting two
// services bind one slot, or leaks slots that never free after a target dies.

func TestAllocator_RejectsNonAllocatedKind(t *testing.T) {
	a := NewAllocator()
	_, err := a.Acquire("svc", "HOST", "consumer")
	if err == nil {
		t.Fatal("Acquire(HOST) should reject — only *_PORT is allocatable")
	}
}

func TestAllocator_AcquireIdempotentSameConsumer(t *testing.T) {
	// A repeated Acquire from one consumer returns the same port and holds a
	// single ref, so one Release still frees the slot.
	a := NewAllocator()
	p1, err := a.Acquire("svc", "REST_PORT", "consumer-1")
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	p2, err := a.Acquire("svc", "REST_PORT", "consumer-1")
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if p1 != p2 {
		t.Errorf("same consumer should reuse port: got %d then %d", p1, p2)
	}
	snap := a.Snapshot()
	if len(snap) != 1 || len(snap[0].Refs) != 1 {
		t.Errorf("expected 1 slot with 1 ref, got %+v", snap)
	}
}

func TestAllocator_TwoConsumersShareSlot(t *testing.T) {
	// In the cross-service case service-A allocates SERVICE_B_GRPC_PORT, then
	// service-B starts its own grpc target and must land on that same port.
	// Both refs are recorded, so the slot survives until each releases.
	a := NewAllocator()
	p1, err := a.Acquire("svc-b", "GRPC_PORT", "consumer-a")
	if err != nil {
		t.Fatalf("Acquire #1: %v", err)
	}
	p2, err := a.Acquire("svc-b", "GRPC_PORT", "consumer-b")
	if err != nil {
		t.Fatalf("Acquire #2: %v", err)
	}
	if p1 != p2 {
		t.Errorf("cross-consumer Acquire on same key: got %d then %d (must match)", p1, p2)
	}
	// Release consumer-a — slot should survive.
	a.Release("consumer-a")
	if p, ok := a.Lookup("svc-b", "GRPC_PORT"); !ok || p != p1 {
		t.Errorf("after partial Release, slot should still exist: lookup got (%d, %v)", p, ok)
	}
	// Release consumer-b — slot should disappear.
	a.Release("consumer-b")
	if _, ok := a.Lookup("svc-b", "GRPC_PORT"); ok {
		t.Error("after full Release, slot should be gone")
	}
}

func TestAllocator_ReserveSkipsKernel(t *testing.T) {
	// Reserve is the adoption path: a restarting daemon finds an already-bound
	// container and re-seeds the slot with its port. No kernel call happens,
	// and whatever port the caller passes is what gets recorded.
	a := NewAllocator()
	port := a.Reserve("svc", "POSTGRES_PORT", 41699, "infra-consumer")
	if port != 41699 {
		t.Errorf("Reserve should return the passed port: got %d want 41699", port)
	}
	if p, ok := a.Lookup("svc", "POSTGRES_PORT"); !ok || p != 41699 {
		t.Errorf("Reserve should populate Lookup: got (%d, %v)", p, ok)
	}
}

func TestAllocator_ReserveExistingWins(t *testing.T) {
	// Over an existing slot, Reserve takes a ref and returns the port already
	// recorded, ignoring the one passed. That is what makes a reseed followed
	// by an Acquire robust when the Acquire fires first.
	a := NewAllocator()
	first, err := a.Acquire("svc", "POSTGRES_PORT", "owner-a")
	if err != nil {
		t.Fatalf("initial Acquire: %v", err)
	}
	got := a.Reserve("svc", "POSTGRES_PORT", first+999, "owner-b")
	if got != first {
		t.Errorf("Reserve over existing slot: got %d want %d (existing port)", got, first)
	}
	snap := a.Snapshot()
	if len(snap) != 1 || len(snap[0].Refs) != 2 {
		t.Errorf("expected 1 slot with 2 refs, got %+v", snap)
	}
}

func TestAllocator_ReserveRejectsNonAllocatedKind(t *testing.T) {
	a := NewAllocator()
	got := a.Reserve("svc", "HOST", 1234, "consumer")
	if got != 0 {
		t.Errorf("Reserve on non-PORT kind: got %d want 0 (rejected)", got)
	}
}

func TestAllocator_LookupMissing(t *testing.T) {
	a := NewAllocator()
	p, ok := a.Lookup("nobody", "REST_PORT")
	if ok || p != 0 {
		t.Errorf("Lookup of missing slot: got (%d, %v) want (0, false)", p, ok)
	}
}

func TestAllocator_ReleaseIdempotentAndUnknownNoop(t *testing.T) {
	a := NewAllocator()
	a.Release("never-acquired") // shouldn't panic, shouldn't error
	a.Release("never-acquired") // still no-op
}

func TestAllocator_SnapshotDeterministic(t *testing.T) {
	a := NewAllocator()
	_, _ = a.Acquire("svc-b", "REST_PORT", "c1")
	_, _ = a.Acquire("svc-a", "GRPC_PORT", "c2")
	_, _ = a.Acquire("svc-a", "REST_PORT", "c3")
	snap := a.Snapshot()
	// Sorted by (owner, localVar): svc-a/GRPC_PORT, svc-a/REST_PORT,
	// svc-b/REST_PORT.
	want := []struct{ owner, local string }{
		{"svc-a", "GRPC_PORT"},
		{"svc-a", "REST_PORT"},
		{"svc-b", "REST_PORT"},
	}
	if len(snap) != len(want) {
		t.Fatalf("snapshot len: got %d want %d", len(snap), len(want))
	}
	for i, w := range want {
		if snap[i].Owner != w.owner || snap[i].LocalVar != w.local {
			t.Errorf("snap[%d]: got (%q, %q) want (%q, %q)",
				i, snap[i].Owner, snap[i].LocalVar, w.owner, w.local)
		}
	}
}

func TestAllocator_SetServicesLongestFirst(t *testing.T) {
	a := NewAllocator()
	a.SetServices([]string{"svc-a", "svc-aaa", "svc-aa"})
	got := a.Services()
	// Sorted by length desc, ties by lex asc.
	want := []string{"svc-aaa", "svc-aa", "svc-a"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("Services()[%d]: got %q want %q", i, got[i], w)
		}
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
			p, err := a.Acquire("svc", "REST_PORT", "consumer-"+itoa(idx))
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
