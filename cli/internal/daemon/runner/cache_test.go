package runner

import (
	"testing"
	"time"
)

// Tests for the infra-state cache + generation-counter invalidation.
// We can't actually exercise InfraStatesOf without podman, but the
// cache-management primitives (InvalidateInfraStateCache + the
// generation counter the long scan checks before writing) are pure
// in-memory state we CAN test in isolation.

func newRunnerForCache() *Runner {
	r := &Runner{
		infraStateCache: make(map[string]infraStateCacheEntry),
	}
	return r
}

func TestCache_InvalidateBumpsGeneration(t *testing.T) {
	r := newRunnerForCache()
	gen0 := r.infraStateGen
	r.InvalidateInfraStateCache()
	if r.infraStateGen != gen0+1 {
		t.Errorf("InvalidateInfraStateCache should bump generation: got %d want %d",
			r.infraStateGen, gen0+1)
	}
	r.InvalidateInfraStateCache()
	if r.infraStateGen != gen0+2 {
		t.Errorf("two Invalidates: got %d want %d", r.infraStateGen, gen0+2)
	}
}

func TestCache_InvalidateClearsEntries(t *testing.T) {
	r := newRunnerForCache()
	r.infraStateCache["default"] = infraStateCacheEntry{
		at:     time.Now(),
		states: map[string]InfraState{"x/y": {ContainerID: "abc"}},
	}
	r.InvalidateInfraStateCache()
	if _, ok := r.infraStateCache["default"]; ok {
		t.Error("InvalidateInfraStateCache should drop existing entries")
	}
}

// TestCache_GenCheckSkipsStaleWrite simulates the race the generation
// counter fixes: the cache write at the end of a long InfraStatesOf
// scan must be skipped if InvalidateInfraStateCache fired during the
// scan. We can't run the real podman scan, but we can simulate the
// "I snapshotted gen=X, did work, now compare-and-write" logic.
func TestCache_GenCheckSkipsStaleWrite(t *testing.T) {
	r := newRunnerForCache()
	// Pretend we're about to do a scan — snapshot the generation.
	r.infraStateMu.Lock()
	startGen := r.infraStateGen
	r.infraStateMu.Unlock()
	// Concurrent invalidation during our "scan".
	r.InvalidateInfraStateCache()
	// Now we'd write the cache. Mirror the runtime check.
	scanResult := map[string]InfraState{"x/y": {ContainerID: "stale"}}
	r.infraStateMu.Lock()
	if r.infraStateGen == startGen {
		r.infraStateCache["default"] = infraStateCacheEntry{at: time.Now(), states: scanResult}
	}
	r.infraStateMu.Unlock()
	if _, ok := r.infraStateCache["default"]; ok {
		t.Errorf("post-Invalidate write should be skipped, but cache has %v", r.infraStateCache["default"])
	}
}

func TestCache_GenCheckAllowsCleanWrite(t *testing.T) {
	// Inverse: with no concurrent Invalidate, the gen match holds and
	// the write proceeds.
	r := newRunnerForCache()
	r.infraStateMu.Lock()
	startGen := r.infraStateGen
	r.infraStateMu.Unlock()
	scanResult := map[string]InfraState{"x/y": {ContainerID: "fresh"}}
	r.infraStateMu.Lock()
	if r.infraStateGen == startGen {
		r.infraStateCache["default"] = infraStateCacheEntry{at: time.Now(), states: scanResult}
	}
	r.infraStateMu.Unlock()
	if got, ok := r.infraStateCache["default"]; !ok || got.states["x/y"].ContainerID != "fresh" {
		t.Errorf("clean write should have populated cache; got %v ok=%v", got, ok)
	}
}
