package logs

import (
	"io"
	"sync"
	"time"
)

// Writer adapts the targetStream into an io.Writer that the runner can
// hand to exec.Cmd.Stdout / .Stderr. Each Write splits on newlines,
// JSON-encodes each line with {ts, stream, line}, writes to the
// per-target file, and fans out to every subscriber.
//
// A single Writer carries one stream tag (Stdout vs Stderr); the runner
// creates two — one for each — pointing at the same targetStream.
type Writer struct {
	ts     *targetStream
	stream Stream
	// partial holds bytes from a prior Write that didn't end in a
	// newline. Concatenated to the next Write so a line split across
	// reads still serializes as one Line entry.
	partial []byte
}

// Stdout / Stderr return a new Writer view bound to this stream tag.
// The two views share the underlying targetStream's file + subscribers.
func (w *Writer) Stdout() io.Writer { return &streamWriter{w: w, stream: StreamStdout} }
func (w *Writer) Stderr() io.Writer { return &streamWriter{w: w, stream: StreamStderr} }

// Close finalizes the underlying stream (closes file + subscribers).
// Idempotent.
func (w *Writer) Close() error {
	w.ts.close()
	return nil
}

// streamWriter is the per-stream io.Writer adapter. One per (Writer, Stream).
type streamWriter struct {
	w      *Writer
	stream Stream
	mu     sync.Mutex
	// partial bytes that haven't yet been terminated by a newline.
	partial []byte
}

func (sw *streamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	now := time.Now()
	sw.mu.Lock()
	defer sw.mu.Unlock()
	// Combine any partial from the previous Write with the new bytes.
	buf := append(sw.partial, p...)
	sw.partial = nil
	// Split on newlines; flush each complete line; carry the tail.
	for {
		i := -1
		for j := 0; j < len(buf); j++ {
			if buf[j] == '\n' {
				i = j
				break
			}
		}
		if i < 0 {
			sw.partial = append(sw.partial[:0], buf...)
			break
		}
		line := string(buf[:i])
		buf = buf[i+1:]
		sw.flush(Line{Ts: now, Stream: sw.stream, Line: line})
	}
	return len(p), nil
}

// flush writes one Line to the file and fans out to subscribers. The
// target's mutex protects the file write + subscriber list snapshot;
// the fanout itself is non-blocking — a subscriber whose buffer is full
// silently drops the line.
func (sw *streamWriter) flush(ln Line) {
	ts := sw.w.ts
	ts.mu.Lock()
	if ts.closed {
		ts.mu.Unlock()
		return
	}
	if ts.encoder != nil {
		_ = ts.encoder.Encode(ln)
	}
	// Snapshot subscribers under the lock so an unsubscribe doesn't
	// race with the fanout.
	subs := append([]chan Line(nil), ts.subscribers...)
	ts.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- ln:
		default:
			// Subscriber is slow; drop. Liveness wins over completeness
			// — the runner never stalls on a stuck client. The archived
			// file always has the full record.
		}
	}
}
