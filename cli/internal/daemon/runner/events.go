package runner

import (
	"time"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// A PhaseEvent carries what a Watch subscriber needs to render a state-change
// line: who transitioned, from which phase to which, and when. The runner emits
// one for every phase change, markTerminated included, where NewPhase is
// TERMINATED and the exit reason rides along.
//
// Service and Stack are copied from the Instance so a subscriber never looks the
// target up: by the time it drains the channel, the daemon may have dropped the
// instance record.
type PhaseEvent struct {
	TargetID string
	Service  string
	Stack    string
	OldPhase anovelv1.Phase
	NewPhase anovelv1.Phase
	// ExitReason is meaningful only when NewPhase is PHASE_TERMINATED, and
	// zero otherwise. It rides alongside the phase so a subscriber telling
	// success from error needs no second query.
	ExitReason anovelv1.ExitReason
	Ts         time.Time
}

// eventSub is one Watch subscriber. emitPhase iterates the live subs under
// subsMu and sends into each channel without blocking, so a slow subscriber
// loses events rather than stalling the runner.
//
// filter returns true for the events the subscriber wants; a nil filter takes
// every event.
type eventSub struct {
	ch     chan PhaseEvent
	filter func(PhaseEvent) bool
}

// SubscribePhases registers a channel-based subscriber for phase events. It
// returns the channel to read from and an unsubscribe function the caller must
// invoke on exit, typically deferred. A nil filter receives every event.
//
// The 32-slot buffer absorbs a brief consumer stall, such as rendering a ps
// table; sustained slowness drops events silently and keeps the runner live.
func (r *Runner) SubscribePhases(filter func(PhaseEvent) bool) (<-chan PhaseEvent, func()) {
	sub := &eventSub{
		ch:     make(chan PhaseEvent, 32),
		filter: filter,
	}
	r.subsMu.Lock()
	r.subs = append(r.subs, sub)
	r.subsMu.Unlock()
	unsub := func() {
		r.subsMu.Lock()
		for i, s := range r.subs {
			if s == sub {
				r.subs = append(r.subs[:i], r.subs[i+1:]...)
				break
			}
		}
		r.subsMu.Unlock()
		close(sub.ch)
	}
	return sub.ch, unsub
}

// emitPhase is the runner-internal fanout, called from transition and
// markTerminated so every subscriber sees the same view as Instance readers.
//
// It holds subsMu.RLock across the whole iteration, so every send lands on a
// channel unsub has not closed yet: unsub removes the sub and closes sub.ch
// under the write lock, which waits for this one. Every send is non-blocking,
// so the read lock costs microseconds per subscriber.
func (r *Runner) emitPhase(ev PhaseEvent) {
	ev.Ts = time.Now()
	r.subsMu.RLock()
	defer r.subsMu.RUnlock()
	for _, s := range r.subs {
		if s.filter != nil && !s.filter(ev) {
			continue
		}
		select {
		case s.ch <- ev:
		default:
			// Dropped; see the buffer note on SubscribePhases.
		}
	}
}
