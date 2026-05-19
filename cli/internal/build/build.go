// Package build executes a detected target as a subprocess and reports a
// structured pass/fail [Result] with captured output — the raw material the UI
// turns into a build report.
package build

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/detect"
)

// Result is the outcome of running one [detect.Target].
type Result struct {
	Target detect.Target

	// Success is true iff the process exited zero.
	Success bool

	// ExitErr is the non-nil error from a failed/aborted process (non-zero
	// exit, context cancellation, or a binary that could not be spawned).
	ExitErr error

	// Output is the combined stdout+stderr, trimmed. On success it is usually
	// noise; on failure it is the thing the user actually needs to see, so it
	// is always captured rather than streamed-and-dropped.
	Output string

	// Duration is wall-clock time spent in the subprocess.
	Duration time.Duration
}

// Run executes t.Cmd with t.Args in t.Dir, capturing combined output. It
// blocks until the process exits or ctx is cancelled. It never panics and
// never returns a partially-zero Result — every field is meaningful.
func Run(ctx context.Context, t detect.Target) Result {
	start := time.Now()

	cmd := exec.CommandContext(ctx, t.Cmd, t.Args...)
	cmd.Dir = t.Dir

	// One buffer for both streams so the report preserves the interleaving the
	// user would have seen in a terminal — splitting them reorders errors
	// relative to the output that explains them.
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()

	return Result{
		Target:   t,
		Success:  err == nil,
		ExitErr:  err,
		Output:   strings.TrimRight(buf.String(), "\n"),
		Duration: time.Since(start),
	}
}

// Summary aggregates a set of results for the report screen.
type Summary struct {
	Total    int
	Passed   int
	Failed   int
	Duration time.Duration
}

// Summarize folds results into counts and total wall-clock-ish time (the sum
// of per-target durations, since the runner is sequential).
func Summarize(results []Result) Summary {
	s := Summary{Total: len(results)}
	for _, r := range results {
		s.Duration += r.Duration
		if r.Success {
			s.Passed++
		} else {
			s.Failed++
		}
	}
	return s
}
