package env

import (
	"fmt"
	"net"
	"sort"
	"sync"
)

// An Allocator picks free host ports for `*_PORT` env variables and refcounts
// them, freeing an allocation once its last consumer terminates. Ports come
// from the kernel-assigned ephemeral range, with no fixed pool.
//
// The (ownerService, localVar) tuple keys a slot. A cross-service reference
// resolves to the owner's allocation and joins its refs without taking a slot
// of its own.
type Allocator struct {
	mu    sync.RWMutex
	slots map[string]*portSlot // key: ownerService + "/" + localVar
	// services holds every known service name, longest-first so resolveOwner
	// picks the most specific match. SetServices populates it at daemon start.
	services []string
}

type portSlot struct {
	owner    string
	localVar string
	port     int
	// refs holds the target IDs consuming this allocation, as a set so a
	// repeated claim collapses.
	refs map[string]struct{}
}

// NewAllocator returns an empty allocator. Call SetServices to register
// the cross-service prefix vocabulary before any allocation.
func NewAllocator() *Allocator {
	return &Allocator{slots: make(map[string]*portSlot)}
}

// SetServices registers the service names the allocator recognizes as
// cross-service prefixes. It sorts them longest-first, so a name that shadows a
// shorter one — `service-template-extra` over `service-template` — wins.
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

// Services returns a snapshot of the registered service names, for callers that
// resolve owners outside the allocator's lock.
func (a *Allocator) Services() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	cp := make([]string, len(a.services))
	copy(cp, a.services)
	return cp
}

// Acquire returns the host port for (owner, localVar), allocating a fresh
// one if no slot exists. `consumer` is the target ID claiming this slot;
// repeated calls with the same consumer are idempotent.
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

// Reserve records (owner, localVar) → port without consulting the kernel.
// Adoption uses it: a daemon restarting onto an already-running infra container
// re-seeds the slot from that container's host mapping, so later Acquire calls
// hand back the port the container is bound to. An existing slot keeps its own
// port, which Reserve then returns.
func (a *Allocator) Reserve(owner, localVar string, port int, consumer string) int {
	if !isAllocatedKind(localVar) {
		return 0
	}
	key := owner + "/" + localVar
	a.mu.Lock()
	defer a.mu.Unlock()
	if slot, ok := a.slots[key]; ok {
		// The existing slot wins; the caller's port is informational.
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

// Lookup returns the current port for (owner, localVar) without allocating, and
// (0, false) when no slot exists. The Builder uses it on the read-only
// ForService path behind `a-novel run env`.
func (a *Allocator) Lookup(owner, localVar string) (int, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	slot, ok := a.slots[owner+"/"+localVar]
	if !ok {
		return 0, false
	}
	return slot.port, true
}

// Release drops the given consumer from every slot it holds, removing a slot
// once its last ref is gone and leaving its port unbound. It is idempotent.
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

// pickFreePort asks the kernel for a free ephemeral port by binding to :0 and
// reading back what was assigned. Another process can claim the same port in
// the window between the close here and the caller's bind, which is acceptable
// for local dev.
func pickFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = ln.Close() }()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("env: unexpected listener type %T", ln.Addr())
	}
	return addr.Port, nil
}
