package runner

import (
	"testing"
	"time"
)

// Tests for the infra-state cache and its generation-counter invalidation.
// InfraStatesOf needs podman, but the cache primitives it relies on are pure
// in-memory state and stand on their own here.

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

// TestCache_GenCheckSkipsStaleWrite covers the race the generation counter
// closes: the cache write ending a long InfraStatesOf scan is skipped when
// InvalidateInfraStateCache fired mid-scan. Standing in for the podman scan, it
// replays the snapshot-work-compare-and-write sequence.
func TestCache_GenCheckSkipsStaleWrite(t *testing.T) {
	r := newRunnerForCache()
	// Snapshot the generation, as a scan about to start would.
	r.infraStateMu.Lock()
	startGen := r.infraStateGen
	r.infraStateMu.Unlock()
	// An invalidation lands during the scan.
	r.InvalidateInfraStateCache()
	// The cache write then mirrors the runtime check.
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
	// Without a concurrent invalidation the generation still matches, so the
	// write proceeds.
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
