// Package build executes a detected target as a subprocess and reports a
// structured pass/fail [Result] with captured output — the raw material the UI
// turns into a build report.
package build

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

	// Resolve scripts/setup-env.sh for this target and, if present, source it
	// once: it assigns RANDOM host ports (get-port-please) and derives the
	// URLs/DSN from them. The captured environment is reused for BOTH compose
	// and the test process, so the env binds and the test connects to the
	// same ports — and because each Run sources it independently, parallel
	// targets get disjoint ports for free. No per-repo special-casing: the
	// json-keys master key is just another captured var.
	runEnv := os.Environ()
	if root := repoRoot(t); root != "" {
		if se := filepath.Join(root, "scripts", "setup-env.sh"); isFile(se) {
			fmt.Fprintf(&buf, "── setup-env: %s ──\n", rel(root, se))
			env, err := sourceEnv(ctx, se, root, &buf)
			if err != nil {
				return Result{
					Target:   t,
					ExitErr:  fmt.Errorf("setup-env.sh failed: %w", err),
					Output:   strings.TrimRight(buf.String(), "\n"),
					Duration: time.Since(start),
				}
			}
			runEnv = env
		}
	}

	// Inner closure so the deferred env-down runs (and appends its output)
	// before we snapshot buf into res.Output below.
	res := func() Result {
		if t.Env != nil {
			fmt.Fprintf(&buf, "── env up: %s ──\n", t.Env.ID)
			// --podman-build-args mirrors scripts/test.sh: docker manifest
			// format + quiet, required for podman to build the env images.
			if err := compose(ctx, &buf, runEnv, t.Env,
				[]string{"--podman-build-args=--format docker -q"},
				"up", "-d", "--build", "--wait"); err != nil {
				// A half-up env left running is worse than a clean failure.
				fmt.Fprint(&buf, "── env down ──\n")
				_ = compose(context.WithoutCancel(ctx), &buf, runEnv, t.Env, nil, "down", "--volumes")
				return Result{Target: t, ExitErr: fmt.Errorf(
					"environment %s failed to start: %w", t.Env.ID, err)}
			}
			// down always runs, even on ctx cancellation mid-test — detach the
			// context so cleanup itself is never cancelled.
			defer func() {
				fmt.Fprint(&buf, "── env down ──\n")
				_ = compose(context.WithoutCancel(ctx), &buf, runEnv, t.Env, nil, "down", "--volumes")
			}()
			fmt.Fprintf(&buf, "── %s ──\n", t.Name)
		}

		cmd := exec.CommandContext(ctx, t.Cmd, t.Args...)
		cmd.Dir = t.Dir
		cmd.Env = runEnv
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		err := cmd.Run()
		return Result{Target: t, Success: err == nil, ExitErr: err}
	}()

	res.Output = strings.TrimRight(buf.String(), "\n")
	res.Duration = time.Since(start)
	return res
}

// repoRoot is the directory holding scripts/ and builds/ for a target: the
// compose file's grandparent (builds/<file> → repo) when there is an env, else
// the target's own directory (kit libs, root pnpm).
func repoRoot(t detect.Target) string {
	if t.Env != nil {
		return filepath.Dir(filepath.Dir(t.Env.File))
	}
	return t.Dir
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func rel(base, p string) string {
	if r, err := filepath.Rel(base, p); err == nil {
		return r
	}
	return p
}

// sourceEnv runs setup-env.sh in bash and returns the resulting environment.
// `set -a` exports every assignment; setup-env's own prints are redirected to
// stderr (captured into log) so only the NUL-delimited `env` reaches stdout.
func sourceEnv(ctx context.Context, script, dir string, log *bytes.Buffer) ([]string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", `set -a; . "$1" 1>&2; env -0`, "bash", script)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = log
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	var env []string
	for _, kv := range strings.Split(out.String(), "\x00") {
		if kv != "" {
			env = append(env, kv)
		}
	}
	return env, nil
}

// compose runs `podman compose [global...] -p <project> -f <file> <args...>`
// with the given environment, streaming combined output into buf.
func compose(ctx context.Context, buf *bytes.Buffer, env []string, e *detect.ComposeEnv, global []string, args ...string) error {
	full := append([]string{"compose"}, global...)
	full = append(full, "-p", e.Project, "-f", e.File)
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "podman", full...)
	cmd.Env = env
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
