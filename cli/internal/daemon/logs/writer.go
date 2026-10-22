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

// Stdout returns a view of the Writer that tags its lines stdout. Both views
// share the target's file and subscribers.
func (w *Writer) Stdout() io.Writer { return &streamWriter{w: w, stream: StreamStdout} }

// Stderr returns a view of the Writer that tags its lines stderr.
func (w *Writer) Stderr() io.Writer { return &streamWriter{w: w, stream: StreamStderr} }

// Close finalizes the underlying stream (closes file + subscribers).
// Idempotent.
func (w *Writer) Close() error {
	w.ts.close()
	return nil
}

// maxLineBytes is the longest line a view writes to the log in one piece.
const maxLineBytes = 1024 * 1024

// streamWriter is the per-stream io.Writer adapter. One per (Writer, Stream).
type streamWriter struct {
	w      *Writer
	stream Stream
	mu     sync.Mutex
	// partial holds the output not yet flushed as a line, at most
	// maxLineBytes.
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
	line, rest, ok := cutLine(sw.partial)
	for ok {
		sw.flush(Line{Ts: now, Stream: sw.stream, Line: string(line)})
		line, rest, ok = cutLine(rest)
	}
	sw.partial = append(sw.partial[:0], rest...)
	return len(p), nil
}

// cutLine cuts the first line off buf. It returns the line without its newline,
// the bytes after it, and true. A line longer than maxLineBytes comes out in
// maxLineBytes pieces, so output that never ends a line, such as a
// carriage-return progress bar or binary data, still reaches the log. While buf
// holds no full line and no full piece, cutLine returns nil, buf and false.
func cutLine(buf []byte) ([]byte, []byte, bool) {
	head := buf[:min(len(buf), maxLineBytes+1)]
	if i := bytes.IndexByte(head, '\n'); i >= 0 {
		return buf[:i], buf[i+1:], true
	}
	if len(head) > maxLineBytes {
		return buf[:maxLineBytes], buf[maxLineBytes:], true
	}
	return nil, buf, false
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
