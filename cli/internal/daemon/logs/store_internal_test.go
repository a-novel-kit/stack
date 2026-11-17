package logs

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
)

// Tests for the log store and its writer hub, centered on the subscriber
// lifecycle. Each test stands up a real Store backed by a tempdir, so the
// file-write side effects stay out of the way of the fanout assertions.

// withStore returns a fresh Store and a target ready for writes, under the
// targetID "default/test/x". Teardown drops the XDG_STATE_HOME override so an
// adjacent test cannot pick up this directory.
func withStore(t *testing.T) (*Store, string, *Writer) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	s := New()
	const id = "default/test/x"
	w, err := s.OpenForWrite(id, "default", "test", "x")
	if err != nil {
		panic(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return s, id, w
}

//nolint:paralleltest // every sub-test sets XDG_STATE_HOME through withStore
func TestStore(t *testing.T) {
	t.Run("Success/SubscribeRoundtrip", func(t *testing.T) {
		s, id, w := withStore(t)
		ch, unsub, ok := s.Subscribe(id)
		require.True(t, ok, "Subscribe on a live target")
		defer unsub()
		// Write one line to stdout: the subscriber should receive it.
		_, err := w.Stdout().Write([]byte("hello\n"))
		require.NoError(t, err)
		select {
		case ln := <-ch:
			require.Equal(t, "hello", ln.Line)
			require.Equal(t, StreamStdout, ln.Stream)
		case <-time.After(time.Second):
			require.Fail(t, "subscriber didn't receive line")
		}
	})

	t.Run("Success/UnsubscribeRemovesFromFanout", func(t *testing.T) {
		// The subscribers list returns to zero length once every subscriber
		// unsubscribes, so a long-lived target sheds the ones that leave.
		s, id, w := withStore(t)
		const n = 5
		unsubs := make([]func(), n)
		for i := range n {
			_, u, ok := s.Subscribe(id)
			require.True(t, ok, "Subscribe %d", i)
			unsubs[i] = u
		}
		s.mu.RLock()
		ts := s.streams[id]
		s.mu.RUnlock()
		// Read under the lock, assert after it: a failed assertion must not
		// leave ts.mu held for the cleanup's Close.
		subscribers := func() int {
			ts.mu.Lock()
			defer ts.mu.Unlock()
			return len(ts.subscribers)
		}
		require.Equal(t, n, subscribers(), "subscriber count before unsubscribe")
		for _, u := range unsubs {
			u()
		}
		require.Zero(t, subscribers(), "subscriber count after every unsubscribe")
		// A write after unsub must not crash on the dangling channels: the writer
		// holds ts.mu during the fanout, so the unsub happens before it.
		_, _ = w.Stdout().Write([]byte("after-unsub\n"))
	})

	t.Run("Success/UnsubscribeDuringWrite", func(t *testing.T) {
		// Race the unsub against an in-flight stream of writes. Holding ts.mu
		// through the fanout, and leaving the channel open on unsub, is what keeps
		// every iteration clear of a "send on closed channel" panic.
		s, id, w := withStore(t)
		const rounds = 200
		for range rounds {
			_, unsub, ok := s.Subscribe(id)
			require.True(t, ok, "Subscribe on a live target")
			done := make(chan struct{})
			go func() {
				for range 64 {
					_, _ = w.Stdout().Write([]byte("spam\n"))
				}
				close(done)
			}()
			unsub() // may interleave anywhere in the write loop
			<-done
		}
	})

	t.Run("Error/SubscribeAfterClose", func(t *testing.T) {
		s, id, w := withStore(t)
		_ = w.Close()
		s.CloseTarget(id)
		// CloseTarget drops the stream record, so Subscribe reports
		// (nil, nil, false). A still-alive but terminating stream takes the other
		// branch, where closed is true.
		_, _, ok := s.Subscribe(id)
		require.False(t, ok, "Subscribe to a deleted target")
	})

	t.Run("Success/FullBufferDrops", func(t *testing.T) {
		// The 32-slot buffer plus a non-blocking send means more lines than the
		// buffer holds still never block the writer.
		s, id, w := withStore(t)
		_, unsub, _ := s.Subscribe(id)
		defer unsub()
		// Nothing drains the channel, so once the subscriber's buffer fills the
		// fanout drops each line, and the writer must not stall.
		start := time.Now()
		for range 100 {
			_, _ = w.Stdout().Write([]byte("spam\n"))
		}
		require.LessOrEqual(t, time.Since(start), 500*time.Millisecond, "writer stalled with full subscriber buffer")
	})

	// The no-subscriber path still reaches the file.
	t.Run("Success/NoSubscriberStillWritesFile", func(t *testing.T) {
		_, _, w := withStore(t)
		_, err := w.Stdout().Write([]byte("orphan\n"))
		require.NoError(t, err)
		// Give the encoder a moment.
		time.Sleep(50 * time.Millisecond)
		data, err := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "a-novel", "logs", "default", "test", "x", "current.log"))
		require.NoError(t, err)
		require.NotEmpty(t, data, "log file after a write")
	})

	// Archive names outside the run-<stamp>.log shape, such as a stray run-x,
	// stay out of the listing.
	t.Run("Success/ListRunsSkipsMalformedNames", func(t *testing.T) {
		s, _, _ := withStore(t)
		dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "a-novel", "logs", "default", "test", "x")
		fsutiltest.Write(t, dir, map[string]string{
			"run-x":              "",
			"run-":               "",
			"run-2026-07-01.log": "",
			"run-2026-07-02.log": "",
			"notes.log":          "",
		})

		require.Equal(t, []string{"2026-07-02", "2026-07-01"}, s.ListRuns("default", "test", "x"))
	})

	// Many subscribers against concurrent writes: holding the lock through the
	// fanout must neither deadlock nor trip the race detector.
	t.Run("Success/ConcurrentSubscribersWrites", func(t *testing.T) {
		s, id, w := withStore(t)
		const subs = 8
		var wg sync.WaitGroup
		wg.Add(subs)
		for range subs {
			go func() {
				defer wg.Done()
				ch, unsub, ok := s.Subscribe(id)
				if !ok {
					return
				}
				defer unsub()
				// Drain whatever arrives for 200ms.
				deadline := time.After(200 * time.Millisecond)
				for {
					select {
					case <-ch:
					case <-deadline:
						return
					}
				}
			}()
		}
		for range 1000 {
			_, _ = w.Stdout().Write([]byte("burst\n"))
		}
		wg.Wait()
	})
}
