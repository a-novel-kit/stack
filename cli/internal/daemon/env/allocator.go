package env

import (
	"fmt"
	"net"
	"sort"
	"sync"
)

// Allocator picks free host ports for `*_PORT` env variables and tracks
// refcounts so an allocation is freed only when its last consumer
// terminates. Local-dev convention: ephemeral range, kernel-assigned, no
// fixed pool — see spec §6.1.
//
// The (ownerService, localVar) tuple is the unique key. Cross-service
// references resolve by looking up the owner's allocation; the consumer
// is added to the refs list without taking a separate slot.
type Allocator struct {
	mu     sync.RWMutex
	slots  map[string]*portSlot // key: ownerService + "/" + localVar
	// services is the list of every known service name, longest-first
	// (so resolveOwner picks the most specific match). Populated by
	// SetServices at daemon-start.
	services []string
}

type portSlot struct {
	owner    string
	localVar string
	port     int
	// Set of target IDs currently consuming this allocation. Map for O(1)
	// add/remove and natural dedup if a target somehow lands twice.
	refs map[string]struct{}
}

// NewAllocator returns an empty allocator. Call SetServices to register
// the cross-service prefix vocabulary before any allocation.
func NewAllocator() *Allocator {
	return &Allocator{slots: make(map[string]*portSlot)}
}

// SetServices registers the universe of service names the allocator
// should recognize as cross-service prefixes. Sorts longest-first so
// shadowing (e.g., `service-template-extra` vs `service-template`)
// resolves to the longer name.
func (a *Allocator) SetServices(names []string) {
	cp := append([]string(nil), names...)
	sort.Slice(cp, func(i, j int) bool {
		if len(cp[i]) != len(cp[j]) {
			return len(cp[i]) > len(cp[j])
		}
		return cp[i] < cp[j]
	})
	a.mu.Lock()
	a.services = cp
	a.mu.Unlock()
}

// Services returns a snapshot of the registered service names. Read-only
// helper for callers that need to resolve owners without grabbing the
// internal lock.
func (a *Allocator) Services() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	cp := make([]string, len(a.services))
	copy(cp, a.services)
	return cp
}

// Acquire returns the host port for (owner, localVar), allocating a fresh
// one if no slot exists. `consumer` is the target ID claiming this slot;
// repeated calls with the same consumer are idempotent. Spec §6.2.
func (a *Allocator) Acquire(owner, localVar, consumer string) (int, error) {
	if !isAllocatedKind(localVar) {
		return 0, fmt.Errorf("env: %s is not an allocated kind (only *_PORT)", localVar)
	}
	key := owner + "/" + localVar
	a.mu.Lock()
	defer a.mu.Unlock()
	if slot, ok := a.slots[key]; ok {
		slot.refs[consumer] = struct{}{}
		return slot.port, nil
	}
	port, err := pickFreePort()
	if err != nil {
		return 0, fmt.Errorf("env: pick free port for %s/%s: %w", owner, localVar, err)
	}
	a.slots[key] = &portSlot{
		owner:    owner,
		localVar: localVar,
		port:     port,
		refs:     map[string]struct{}{consumer: {}},
	}
	return port, nil
}

// Reserve records (owner, localVar) → port WITHOUT consulting the
// kernel. Used by adoption: when the daemon restarts and finds an
// already-running infra container with port mapping host:N → container:5432,
// it calls Reserve to re-seed the slot so subsequent Acquire calls (e.g.,
// for the one-shot migrations target) return the same host port the
// running container is bound to. Idempotent for matching (owner,
// localVar, port); refuses to overwrite a conflicting port already
// recorded (returns the existing port instead).
func (a *Allocator) Reserve(owner, localVar string, port int, consumer string) int {
	if !isAllocatedKind(localVar) {
		return 0
	}
	key := owner + "/" + localVar
	a.mu.Lock()
	defer a.mu.Unlock()
	if slot, ok := a.slots[key]; ok {
		// Existing slot wins — caller's port is informational. Add the
		// consumer ref.
		slot.refs[consumer] = struct{}{}
		return slot.port
	}
	a.slots[key] = &portSlot{
		owner:    owner,
		localVar: localVar,
		port:     port,
		refs:     map[string]struct{}{consumer: {}},
	}
	return port
}

// Lookup returns the current port for (owner, localVar) WITHOUT
// allocating. Returns (0, false) if no slot exists. Used by the Builder
// for the read-only ForService path (`a-novel run env`).
func (a *Allocator) Lookup(owner, localVar string) (int, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	slot, ok := a.slots[owner+"/"+localVar]
	if !ok {
		return 0, false
	}
	return slot.port, true
}

// Release decrements every slot the given consumer was holding. A slot
// whose refcount hits zero is removed (its port returned to the
// ephemeral pool simply by virtue of nobody binding it). Idempotent.
func (a *Allocator) Release(consumer string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, slot := range a.slots {
		if _, held := slot.refs[consumer]; held {
			delete(slot.refs, consumer)
			if len(slot.refs) == 0 {
				delete(a.slots, key)
			}
		}
	}
}

// Snapshot returns every current allocation. Stable order keyed by
// (owner, localVar) so output is deterministic.
func (a *Allocator) Snapshot() []Allocation {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]Allocation, 0, len(a.slots))
	for _, s := range a.slots {
		refs := make([]string, 0, len(s.refs))
		for r := range s.refs {
			refs = append(refs, r)
		}
		sort.Strings(refs)
		out = append(out, Allocation{
			Owner:    s.owner,
			LocalVar: s.localVar,
			Port:     s.port,
			Refs:     refs,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].LocalVar < out[j].LocalVar
	})
	return out
}

// Allocation is one snapshot row.
type Allocation struct {
	Owner    string
	LocalVar string
	Port     int
	Refs     []string
}

// pickFreePort asks the kernel for a free ephemeral port by binding to
// :0 and reading back what was assigned. There's a small race window
// between Close and the caller's bind — acceptable for local dev and
// well-understood in the get-port-please ecosystem.
func pickFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = ln.Close() }()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.Port, nil
}
