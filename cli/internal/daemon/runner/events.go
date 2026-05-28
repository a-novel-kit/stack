package runner

import (
	"time"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// PhaseEvent is the minimum metadata Watch subscribers need to render a
// state-change line: who transitioned, from what to what, when. The runner
// emits one for every phase change AND for markTerminated (which carries
// the exit reason in NewPhase=TERMINATED + a derived OldPhase=RUNNING).
//
// Service / Stack are duplicated from the Instance so subscribers don't have
// to look the target up — important because by the time the subscriber
// drains the channel, the daemon may have torn down the instance record.
type PhaseEvent struct {
	TargetID string
	Service  string
	Stack    string
	OldPhase anovelv1.Phase
	NewPhase anovelv1.Phase
	// ExitReason is meaningful only when NewPhase == PHASE_TERMINATED;
	// zero otherwise. Tracked separately from Phase so a subscriber that
	// cares about success-vs-error doesn't have to re-query the runner.
	ExitReason anovelv1.ExitReason
	Ts         time.Time
}

// eventSub is one Watch subscriber. The runner snapshots `subs` under
// `subsMu` whenever it emits, then drops into each channel non-blocking;
// a slow subscriber loses events rather than stalling the runner.
//
// `filter` returns true for events the subscriber wants. Empty filter
// means "all events" — see SubscribePhases below.
type eventSub struct {
	ch     chan PhaseEvent
	filter func(PhaseEvent) bool
}

// SubscribePhases registers a channel-based subscriber for phase events.
// Returns the channel to read from + an unsubscribe func the caller MUST
// invoke on exit (typically deferred). `filter` is optional — pass nil to
// receive every event.
//
// Buffer of 32 picked so a brief consumer stall (rendering a ps table,
// say) doesn't drop events under normal load; sustained slowness drops
// silently (liveness > completeness for the runner).
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

// emitPhase is the runner-internal fanout. Called from transition() and
// markTerminated() so every subscriber sees the same view as Instance
// readers. Snapshots subscribers under the lock, then sends non-blocking
// outside the lock.
func (r *Runner) emitPhase(ev PhaseEvent) {
	ev.Ts = time.Now()
	r.subsMu.RLock()
	subs := append([]*eventSub(nil), r.subs...)
	r.subsMu.RUnlock()
	for _, s := range subs {
		if s.filter != nil && !s.filter(ev) {
			continue
		}
		select {
		case s.ch <- ev:
		default:
			// Drop — see SubscribePhases buffer note.
		}
	}
}
