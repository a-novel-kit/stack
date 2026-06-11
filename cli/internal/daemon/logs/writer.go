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
// A single Writer is shared by both the Stdout and Stderr views; each view
// carries the actual stream tag. Per-stream partials live on streamWriter
// (see below).
type Writer struct {
	ts *targetStream
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
	// Assign back to sw.partial first (appendAssign lint), then transfer
	// ownership to buf and clear sw.partial — the for-loop below either
	// flushes complete lines or stashes a new tail back onto sw.partial.
	sw.partial = append(sw.partial, p...)
	buf := sw.partial
	sw.partial = nil
	// Split on newlines; flush each complete line; carry the tail.
	for {
		i := -1
		for j := range buf {
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
// target's mutex protects the file write AND the fanout. Holding it
// during the fanout serializes against Subscribe/unsub: sending outside
// the lock would race with unsubscribe — a goroutine could remove its sub
// from the list, GC the receiver, and the writer would still try to send
// to a channel nobody reads (it falls harmlessly through select-default,
// but it's a smell). Holding the lock keeps the send window tied to
// "still in the list."
//
// Cost: each fanout serializes Subscribe + unsub for ~O(N_subs ×
// microseconds). Sends are non-blocking (select-default), so it's
// bounded.
func (sw *streamWriter) flush(ln Line) {
	ts := sw.w.ts
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.closed {
		return
	}
	if ts.encoder != nil {
		_ = ts.encoder.Encode(ln)
	}
	for _, ch := range ts.subscribers {
		select {
		case ch <- ln:
		default:
			// Subscriber is slow; drop. Liveness wins over completeness
			// — the runner never stalls on a stuck client. The archived
			// file always has the full record.
		}
	}
}
