package runner

import (
	"testing"
	"time"
)

// Tests for the infra-state cache and its generation-counter invalidation.
// InfraStatesOf needs podman, but the cache primitives it relies on are pure
// in-memory state and stand on their own here.

func TestCache_InvalidateBumpsGenerationAndClearsEntries(t *testing.T) {
	r := &Runner{infraStateCache: map[string]infraStateCacheEntry{
		"default": {at: time.Now(), states: map[string]InfraState{"x/y": {ContainerID: "abc"}}},
	}}
	gen0 := r.infraStateGen
	r.InvalidateInfraStateCache()
	if _, ok := r.infraStateCache["default"]; ok || r.infraStateGen != gen0+1 {
		t.Errorf("after Invalidate: generation %d (want %d), entry kept = %v", r.infraStateGen, gen0+1, ok)
	}
	r.InvalidateInfraStateCache()
	if r.infraStateGen != gen0+2 {
		t.Errorf("two Invalidates: got %d want %d", r.infraStateGen, gen0+2)
	}
}

// TestCache_GenCheck covers the race the generation counter closes: the cache
// write ending a long InfraStatesOf scan is skipped when
// InvalidateInfraStateCache fired mid-scan, and proceeds otherwise. Standing in
// for the podman scan, it replays the snapshot-work-compare-and-write sequence.
func TestCache_GenCheck(t *testing.T) {
	for _, invalidated := range []bool{true, false} {
		r := &Runner{infraStateCache: make(map[string]infraStateCacheEntry)}
		// Snapshot the generation, the way a scan does as it starts.
		r.infraStateMu.Lock()
		startGen := r.infraStateGen
		r.infraStateMu.Unlock()
		if invalidated {
			r.InvalidateInfraStateCache()
		}
		// The cache write then mirrors the runtime check.
		r.infraStateMu.Lock()
		if r.infraStateGen == startGen {
			r.infraStateCache["default"] = infraStateCacheEntry{
				at: time.Now(), states: map[string]InfraState{"x/y": {ContainerID: "scanned"}},
			}
		}
		r.infraStateMu.Unlock()
		if got, ok := r.infraStateCache["default"]; ok == invalidated || (ok && got.states["x/y"].ContainerID != "scanned") {
			t.Errorf("invalidated mid-scan = %v: cache = %v (written %v)", invalidated, got, ok)
		}
	}
}
