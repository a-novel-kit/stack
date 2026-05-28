package logs

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Tests for the log store + writer hub. Focus is on the subscriber
// lifecycle (the bug a recent review caught: Subscribe was leaking
// channels into ts.subscribers because there was no unsub API). Each
// test stands up a real Store backed by a tempdir so the file-write
// side-effects don't get in the way of the fanout assertions.

// withStore returns a fresh Store + a configured target ready for
// writes. The targetID is "default/test/x". On teardown, removes the
// XDG_STATE_HOME override so adjacent tests can't pick up our dir.
func withStore(t *testing.T) (*Store, string, *Writer) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)
	s := New()
	const id = "default/test/x"
	w, err := s.OpenForWrite(id, "default", "test", "x")
	if err != nil {
		t.Fatalf("OpenForWrite: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	return s, id, w
}

func TestStore_SubscribeRoundtrip(t *testing.T) {
	s, id, w := withStore(t)
	ch, unsub, ok := s.Subscribe(id)
	if !ok {
		t.Fatal("Subscribe returned ok=false on a live target")
	}
	defer unsub()
	// Write one line to stdout — subscriber should receive it.
	_, err := w.Stdout().Write([]byte("hello\n"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ln := <-ch:
		if ln.Line != "hello" {
			t.Errorf("subscriber line: got %q want hello", ln.Line)
		}
		if ln.Stream != StreamStdout {
			t.Errorf("subscriber stream: got %s want stdout", ln.Stream)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber didn't receive line")
	}
}

func TestStore_UnsubRemovesFromFanout(t *testing.T) {
	// Regression: prior to the review-driven fix, Subscribe had no
	// unsub API, so the subscribers list grew unbounded over the
	// target's lifetime. After unsub, subsequent writes must NOT
	// leave dead channels in the list — the easiest observable
	// proxy is that the underlying list length goes back to zero.
	s, id, w := withStore(t)
	const n = 5
	unsubs := make([]func(), n)
	for i := range n {
		_, u, ok := s.Subscribe(id)
		if !ok {
			t.Fatalf("Subscribe %d returned ok=false", i)
		}
		unsubs[i] = u
	}
	// Confirm n subs are in the list.
	s.mu.RLock()
	ts := s.streams[id]
	s.mu.RUnlock()
	ts.mu.Lock()
	if got := len(ts.subscribers); got != n {
		t.Errorf("subscriber count before unsub: got %d want %d", got, n)
	}
	ts.mu.Unlock()
	for _, u := range unsubs {
		u()
	}
	ts.mu.Lock()
	if got := len(ts.subscribers); got != 0 {
		t.Errorf("subscriber count after all unsub: got %d want 0", got)
	}
	ts.mu.Unlock()
	// Sanity: writing after unsub mustn't crash on the now-dangling
	// channels (the writer holds ts.mu during the fanout, so the
	// unsub happens-before the next write).
	_, _ = w.Stdout().Write([]byte("after-unsub\n"))
}

func TestStore_UnsubDuringWriteNoPanic(t *testing.T) {
	// Race the unsub against an in-flight stream of writes. Pre-fix
	// (snapshot-then-send-outside-lock + close-on-unsub), this could
	// panic with "send on closed channel". Post-fix (hold ts.mu
	// during fanout + don't close on unsub), every iteration is
	// clean.
	s, id, w := withStore(t)
	const rounds = 200
	for range rounds {
		_, unsub, ok := s.Subscribe(id)
		if !ok {
			t.Fatal("Subscribe ok=false")
		}
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
}

func TestStore_SubscribeAfterCloseGetsClosedChannel(t *testing.T) {
	s, id, w := withStore(t)
	_ = w.Close()
	s.CloseTarget(id)
	// After CloseTarget, the stream record is gone — Subscribe
	// returns (nil, nil, false). This is the "subscribe to a
	// vanished target" path; not the "subscribe to a still-alive-
	// but-terminating stream" path which goes through a different
	// branch (closed=true).
	ch, _, ok := s.Subscribe(id)
	if ok {
		t.Errorf("Subscribe to deleted target should return ok=false; got ch=%v", ch)
	}
}

func TestStore_FullBufferDrops(t *testing.T) {
	// Confirm the buffer=32 + select-default contract: spam more lines
	// than the buffer holds, ensure the writer doesn't block.
	s, id, w := withStore(t)
	_, unsub, _ := s.Subscribe(id)
	defer unsub()
	// Don't drain. Each write goes through the fanout; with the
	// subscriber buffer full, select-default fires and drops. The
	// writer must not stall.
	start := time.Now()
	for range 100 {
		_, _ = w.Stdout().Write([]byte("spam\n"))
	}
	if dur := time.Since(start); dur > 500*time.Millisecond {
		t.Errorf("writer stalled with full subscriber buffer: %v", dur)
	}
}

// Smoke: writer pipes to the file even with no subscribers. Ensures
// the no-subscriber path doesn't accidentally short-circuit the file
// write.
func TestStore_NoSubscriberStillWritesFile(t *testing.T) {
	s, _, w := withStore(t)
	_ = s // silence "declared but not used"
	if _, err := w.Stdout().Write([]byte("orphan\n")); err != nil {
		t.Fatal(err)
	}
	// Find the current.log on disk.
	tmp := os.Getenv("XDG_STATE_HOME")
	path := filepath.Join(tmp, "a-novel", "logs", "default", "test", "x", "current.log")
	// Give the encoder a moment.
	time.Sleep(50 * time.Millisecond)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if len(data) == 0 {
		t.Error("log file is empty after a write")
	}
}

// Concurrency probe: many subscribers + concurrent writes. With the
// hold-lock-during-fanout change, this must not deadlock OR
// race-detect (run under `go test -race`).
func TestStore_ConcurrentSubscribersWrites(t *testing.T) {
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
}
