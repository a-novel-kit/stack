package logs

import (
	"bytes"
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
// carries the actual stream tag. Per-stream partials live on streamWriter.
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
	// Flush every complete line of the previous partial plus p, then keep the
	// unterminated tail for the next Write.
	sw.partial = append(sw.partial, p...)
	buf := sw.partial
	for i := bytes.IndexByte(buf, '\n'); i >= 0; i = bytes.IndexByte(buf, '\n') {
		sw.flush(Line{Ts: now, Stream: sw.stream, Line: string(buf[:i])})
		buf = buf[i+1:]
	}
	sw.partial = append(sw.partial[:0], buf...)
	return len(p), nil
}

// flush writes one Line to the file and fans it out to subscribers. The
// target's mutex covers both the write and the fanout, so every send lands on a
// channel still in the subscriber list. Every send is non-blocking, so holding
// the lock across the fanout stays bounded.
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
			// A slow subscriber loses the line, so the runner never
			// stalls; the archived file keeps the full record.
		}
	}
}
