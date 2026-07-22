package runner

import (
	"sync"
	"testing"
	"time"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Tests for the runner's phase-event broadcaster, SubscribePhases and
// emitPhase. The fanout iterates its subscribers under the read lock and sends
// without blocking, so these cover delivery, filtering, the drop on a full
// buffer, and unsubscribe.

func newRunnerForEvents() *Runner {
	// New requires non-nil deps for normal use, but the events surface needs
	// nothing beyond a zero-value runner and its subs slice.
	return &Runner{}
}

func TestEvents_BasicDelivery(t *testing.T) {
	r := newRunnerForEvents()
	ch, unsub := r.SubscribePhases(nil)
	defer unsub()
	r.emitPhase(PhaseEvent{
		TargetID: "default/svc/rest",
		Service:  "svc",
		Stack:    "default",
		NewPhase: anovelv1.Phase_PHASE_RUNNING,
	})
	select {
	case ev := <-ch:
		if ev.TargetID != "default/svc/rest" {
			t.Errorf("event delivered with wrong id: %q", ev.TargetID)
		}
		if ev.Ts.IsZero() {
			t.Error("emitPhase should stamp Ts before fanout")
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber didn't receive event")
	}
}

func TestEvents_FilterDropsNonMatching(t *testing.T) {
	r := newRunnerForEvents()
	// Filter: only events for svc=alpha get through.
	ch, unsub := r.SubscribePhases(func(ev PhaseEvent) bool {
		return ev.Service == "alpha"
	})
	defer unsub()
	r.emitPhase(PhaseEvent{Service: "beta", TargetID: "x"})
	r.emitPhase(PhaseEvent{Service: "alpha", TargetID: "y"})
	select {
	case ev := <-ch:
		if ev.Service != "alpha" || ev.TargetID != "y" {
			t.Errorf("filter let wrong event through: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("filtered subscriber received no events")
	}
	// No further events should be queued.
	select {
	case ev := <-ch:
		t.Errorf("filter let a non-matching event through later: %+v", ev)
	case <-time.After(50 * time.Millisecond):
		// good — silence confirms the beta event was dropped.
	}
}

func TestEvents_Unsubscribe(t *testing.T) {
	r := newRunnerForEvents()
	ch, unsub := r.SubscribePhases(nil)
	unsub()
	// After unsub, the channel is closed; emit should fanout-to-empty.
	r.emitPhase(PhaseEvent{TargetID: "after-unsub"})
	// Drain channel — closed chan reads zero values immediately.
	select {
	case ev, ok := <-ch:
		if ok {
			t.Errorf("post-unsub delivery: got %+v", ev)
		}
		// closed-chan zero read is fine.
	case <-time.After(50 * time.Millisecond):
		t.Fatal("post-unsub channel should be closed (read should return immediately)")
	}
}

func TestEvents_FullBufferDrops(t *testing.T) {
	// A slow subscriber loses events but never stalls the runner, so emitting
	// 100 events into a 32-slot buffer nobody reads must still return
	// promptly.
	r := newRunnerForEvents()
	_, unsub := r.SubscribePhases(nil)
	defer unsub()
	start := time.Now()
	for range 100 {
		r.emitPhase(PhaseEvent{TargetID: "spam"})
	}
	if dur := time.Since(start); dur > 100*time.Millisecond {
		t.Errorf("emitPhase stalled on slow subscriber: %v (should be near-instant)", dur)
	}
}

// TestEvents_UnsubDuringEmitNoPanic guards the send-on-closed-channel race: an
// unsub landing while emitPhase iterates must never close a channel the fanout
// is still sending into. Surfacing that race takes thousands of iterations
// under -race, so this loops tightly, subscribing, emitting concurrently, and
// unsubscribing mid-emit.
func TestEvents_UnsubDuringEmitNoPanic(t *testing.T) {
	r := newRunnerForEvents()
	const rounds = 500
	for range rounds {
		_, unsub := r.SubscribePhases(nil)
		done := make(chan struct{})
		go func() {
			for range 32 {
				r.emitPhase(PhaseEvent{TargetID: "race"})
			}
			close(done)
		}()
		unsub() // may interleave anywhere in the emit loop
		<-done
	}
}

func TestEvents_MultipleSubscribers(t *testing.T) {
	r := newRunnerForEvents()
	const N = 8
	chans := make([]<-chan PhaseEvent, N)
	unsubs := make([]func(), N)
	for i := range N {
		chans[i], unsubs[i] = r.SubscribePhases(nil)
	}
	defer func() {
		for _, u := range unsubs {
			u()
		}
	}()
	r.emitPhase(PhaseEvent{TargetID: "broadcast"})
	var wg sync.WaitGroup
	wg.Add(N)
	for i := range N {
		go func(i int) {
			defer wg.Done()
			select {
			case ev := <-chans[i]:
				if ev.TargetID != "broadcast" {
					t.Errorf("sub %d: wrong id %q", i, ev.TargetID)
				}
			case <-time.After(time.Second):
				t.Errorf("sub %d: didn't receive event", i)
			}
		}(i)
	}
	wg.Wait()
}
