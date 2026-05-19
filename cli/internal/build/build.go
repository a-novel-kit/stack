// Package build executes a detected target as a subprocess and reports a
// structured pass/fail [Result] with captured output — the raw material the UI
// turns into a build report.
package build

import (
	"bytes"
	"context"
	"fmt"
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

	// One buffer for both streams so the report preserves the interleaving the
	// user would have seen in a terminal. compose up/down output is captured
	// into the same buffer, fenced with ── headers, so a failure to stand up
	// the environment reads as part of the same story.
	var buf bytes.Buffer

	// Inner closure so the deferred env-down runs (and appends its output)
	// before we snapshot buf into res.Output below.
	res := func() Result {
		if t.Env != nil {
			fmt.Fprintf(&buf, "── env up: %s ──\n", t.Env.ID)
			if err := compose(ctx, &buf, t.Env, "up", "-d", "--build", "--wait"); err != nil {
				// A half-up env left running is worse than a clean failure.
				fmt.Fprint(&buf, "── env down ──\n")
				_ = compose(context.WithoutCancel(ctx), &buf, t.Env, "down", "--volumes")
				return Result{Target: t, ExitErr: fmt.Errorf(
					"environment %s failed to start: %w", t.Env.ID, err)}
			}
			// down always runs, even on ctx cancellation mid-test — detach the
			// context so cleanup itself is never cancelled.
			defer func() {
				fmt.Fprint(&buf, "── env down ──\n")
				_ = compose(context.WithoutCancel(ctx), &buf, t.Env, "down", "--volumes")
			}()
			fmt.Fprintf(&buf, "── %s ──\n", t.Name)
		}

		cmd := exec.CommandContext(ctx, t.Cmd, t.Args...)
		cmd.Dir = t.Dir
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		err := cmd.Run()
		return Result{Target: t, Success: err == nil, ExitErr: err}
	}()

	res.Output = strings.TrimRight(buf.String(), "\n")
	res.Duration = time.Since(start)
	return res
}

// compose runs `podman compose -p <project> -f <file> <args...>`, streaming
// combined output into buf. It returns the process error (nil on success).
func compose(ctx context.Context, buf *bytes.Buffer, env *detect.ComposeEnv, args ...string) error {
	full := append([]string{"compose", "-p", env.Project, "-f", env.File}, args...)
	cmd := exec.CommandContext(ctx, "podman", full...)
	cmd.Stdout = buf
	cmd.Stderr = buf
	return cmd.Run()
}

// Summary aggregates a set of results for the report screen.
type Summary struct {
	Total  int
	Passed int
	Failed int

	// CumulativeDuration is the sum of every target's own build time. Under
	// the interactive parallel runner this exceeds wall-clock time (overlapping
	// builds are counted in full each), so it is NOT what the report shows as
	// "took" — that is real elapsed time, tracked by the runner and passed in
	// separately. CumulativeDuration is kept as the total work performed.
	CumulativeDuration time.Duration
}

// Summarize folds results into counts and cumulative per-target build time.
// Wall-clock elapsed is the runner's responsibility, not derivable here.
func Summarize(results []Result) Summary {
	s := Summary{Total: len(results)}
	for _, r := range results {
		s.CumulativeDuration += r.Duration
		if r.Success {
			s.Passed++
		} else {
			s.Failed++
		}
	}
	return s
}
