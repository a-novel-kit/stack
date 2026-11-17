package runner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestSubscribePhases pins the phase-event fanout: a filter drops the events it
// rejects, an unsubscribed channel closes, a slow subscriber loses events but
// never stalls the runner, and every subscriber receives a broadcast stamped
// before the fanout.
func TestSubscribePhases(t *testing.T) {
	t.Parallel()

	t.Run("Success/Filter", func(t *testing.T) {
		t.Parallel()

		r := &Runner{}
		// Only events for svc=alpha get through.
		ch, unsub := r.SubscribePhases(func(ev PhaseEvent) bool {
			return ev.Service == "alpha"
		})
		defer unsub()

		r.emitPhase(PhaseEvent{Service: "beta", TargetID: "x"})
		r.emitPhase(PhaseEvent{Service: "alpha", TargetID: "y"})

		select {
		case ev := <-ch:
			require.Equal(t, "alpha", ev.Service)
			require.Equal(t, "y", ev.TargetID)
		case <-time.After(time.Second):
			require.FailNow(t, "filtered subscriber received no events")
		}
		// No further events should be queued.
		select {
		case ev := <-ch:
			require.Failf(t, "filter let a non-matching event through later", "%+v", ev)
		case <-time.After(50 * time.Millisecond):
			// Silence confirms the beta event was dropped.
		}
	})

	t.Run("Success/Unsubscribe", func(t *testing.T) {
		t.Parallel()

		r := &Runner{}
		ch, unsub := r.SubscribePhases(nil)
		unsub()

		// After unsub, the channel is closed; emit should fanout-to-empty.
		r.emitPhase(PhaseEvent{TargetID: "after-unsub"})

		// A closed channel reads a zero value immediately.
		select {
		case ev, ok := <-ch:
			require.False(t, ok, "post-unsub delivery: got %+v", ev)
		case <-time.After(50 * time.Millisecond):
			require.FailNow(t, "post-unsub channel should be closed (read should return immediately)")
		}
	})

	t.Run("Success/FullBufferDrops", func(t *testing.T) {
		t.Parallel()

		// A slow subscriber loses events but never stalls the runner, so emitting
		// 100 events into a 32-slot buffer nobody reads must still return
		// promptly.
		r := &Runner{}
		_, unsub := r.SubscribePhases(nil)
		defer unsub()

		start := time.Now()
		for range 100 {
			r.emitPhase(PhaseEvent{TargetID: "spam"})
		}

		require.Less(t, time.Since(start), 100*time.Millisecond, "emitPhase stalled on slow subscriber (should be near-instant)")
	})

	// UnsubscribeDuringEmit guards the send-on-closed-channel race: an unsub
	// landing while emitPhase iterates must never close a channel the fanout is
	// still sending into. Surfacing that race takes thousands of iterations
	// under -race, so this loops tightly, subscribing, emitting concurrently,
	// and unsubscribing mid-emit.
	t.Run("Success/UnsubscribeDuringEmit", func(t *testing.T) {
		t.Parallel()

		r := &Runner{}
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
	})

	t.Run("Success/MultipleSubscribers", func(t *testing.T) {
		t.Parallel()

		r := &Runner{}
		chans := make([]<-chan PhaseEvent, 8)
		for i := range chans {
			var unsub func()
			chans[i], unsub = r.SubscribePhases(nil)
			defer unsub()
		}

		r.emitPhase(PhaseEvent{TargetID: "broadcast", NewPhase: anovelv1.Phase_PHASE_RUNNING})

		for i, ch := range chans {
			select {
			case ev := <-ch:
				require.Equal(t, "broadcast", ev.TargetID, "sub %d", i)
				require.False(t, ev.Ts.IsZero(), "sub %d: want the broadcast stamped with Ts before fanout", i)
			case <-time.After(time.Second):
				require.FailNow(t, "didn't receive event", "sub %d", i)
			}
		}
	})
}
