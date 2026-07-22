package logs

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Tests for the log store and its writer hub, centered on the subscriber
// lifecycle. Each test stands up a real Store backed by a tempdir, so the
// file-write side effects stay out of the way of the fanout assertions.

// withStore returns a fresh Store and a target ready for writes, under the
// targetID "default/test/x". Teardown drops the XDG_STATE_HOME override so an
// adjacent test cannot pick up this directory.
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
	// The subscribers list returns to zero length once every subscriber
	// unsubscribes, so a long-lived target sheds the ones that leave.
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
	// A write after unsub must not crash on the dangling channels: the writer
	// holds ts.mu during the fanout, so the unsub happens before it.
	_, _ = w.Stdout().Write([]byte("after-unsub\n"))
}

func TestStore_UnsubDuringWriteNoPanic(t *testing.T) {
	// Race the unsub against an in-flight stream of writes. Holding ts.mu
	// through the fanout, and leaving the channel open on unsub, is what keeps
	// every iteration clear of a "send on closed channel" panic.
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
	// CloseTarget drops the stream record, so Subscribe reports
	// (nil, nil, false). A still-alive but terminating stream takes the other
	// branch, where closed is true.
	ch, _, ok := s.Subscribe(id)
	if ok {
		t.Errorf("Subscribe to deleted target should return ok=false; got ch=%v", ch)
	}
}

func TestStore_FullBufferDrops(t *testing.T) {
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
	if dur := time.Since(start); dur > 500*time.Millisecond {
		t.Errorf("writer stalled with full subscriber buffer: %v", dur)
	}
}

// TestStore_NoSubscriberStillWritesFile pins that the no-subscriber path still
// reaches the file.
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

// TestStore_ConcurrentSubscribersWrites drives many subscribers against
// concurrent writes: holding the lock through the fanout must neither deadlock
// nor trip the race detector.
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
