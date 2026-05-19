// Package build executes a detected target as a subprocess and reports a
// structured pass/fail [Result] with captured output — the raw material the UI
// turns into a build report.
package build

import (
	"bytes"
	"context"
	"fmt"
	"net"
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

	// Allocate a free TCP port (Go, no node) for every host-side port var the
	// compose file declares — exactly the ports the host test talks to.
	// Pre-exporting them means setup-env.sh's `${PORT:=$(node …)}` skips the
	// node call (assign-if-unset) while still deriving the URLs/DSN; each Run
	// allocates independently, so parallel targets never collide.
	runEnv := os.Environ()
	if t.Env != nil && len(t.Env.Ports) > 0 {
		portEnv, err := allocPorts(t.Env.Ports)
		if err != nil {
			return Result{
				Target:   t,
				ExitErr:  fmt.Errorf("port allocation failed: %w", err),
				Output:   strings.TrimRight(buf.String(), "\n"),
				Duration: time.Since(start),
			}
		}
		fmt.Fprintf(&buf, "── ports ── %s\n", strings.Join(portEnv, "  "))
		runEnv = append(runEnv, portEnv...)
	}

	// Resolve scripts/setup-env.sh and, if present, source it for the
	// remaining (service-specific / derived) vars. It sees the ports we just
	// set, so node is never invoked. The captured environment is reused for
	// BOTH compose and the test process.
	if root := repoRoot(t); root != "" {
		if se := filepath.Join(root, "scripts", "setup-env.sh"); isFile(se) {
			fmt.Fprintf(&buf, "── setup-env: %s ──\n", rel(root, se))
			env, err := sourceEnv(ctx, se, root, runEnv, &buf)
			if err != nil {
				return Result{
					Target:   t,
					ExitErr:  fmt.Errorf("setup-env.sh failed: %w", err),
					Output:   strings.TrimRight(buf.String(), "\n"),
					Duration: time.Since(start),
				}
			}
			runEnv = env
		} else if t.Env != nil {
			// No setup-env.sh: derive the connection vars ourselves from the
			// allocated ports by the fixed *_PORT naming rule.
			runEnv = append(runEnv, derivedURLs(t.Env.Ports, runEnv)...)
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

// allocPorts binds an ephemeral TCP port for each var name, closes it, and
// returns "NAME=port" entries. Same close-then-reuse window as
// get-port-please, but pure Go (no node). Distinct binds within one call
// never alias because each listener holds a different port until closed.
func allocPorts(names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, n := range names {
		l, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			return nil, err
		}
		addr, ok := l.Addr().(*net.TCPAddr)
		_ = l.Close()
		if !ok {
			return nil, fmt.Errorf("unexpected listener address type %T", l.Addr())
		}
		out = append(out, fmt.Sprintf("%s=%d", n, addr.Port))
	}
	return out, nil
}

// derivedURLs builds the host connection vars from the allocated ports by the
// fixed `<X>_PORT` rule — used only when there is no setup-env.sh to do it:
// POSTGRES_PORT→POSTGRES_DSN, GRPC_PORT→GRPC_URL (host:port),
// any other X_PORT→X_URL (http://localhost:port).
func derivedURLs(names []string, env []string) []string {
	get := func(k string) string {
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, k+"="); ok {
				return v
			}
		}
		return ""
	}
	var out []string
	for _, n := range names {
		base, ok := strings.CutSuffix(n, "_PORT")
		if !ok {
			continue
		}
		p := get(n)
		switch base {
		case "POSTGRES":
			out = append(out, "POSTGRES_DSN=postgres://postgres:postgres@localhost:"+p+"/postgres?sslmode=disable")
		case "GRPC":
			out = append(out, "GRPC_URL=localhost:"+p)
		default:
			out = append(out, base+"_URL=http://localhost:"+p)
		}
	}
	return out
}

// sourceEnv runs setup-env.sh in bash and returns the resulting environment.
// `set -a` exports every assignment; setup-env's own prints are redirected to
// stderr (captured into log) so only the NUL-delimited `env` reaches stdout.
// baseEnv (including the pre-allocated ports) is the script's starting env, so
// its `${PORT:=$(node …)}` assignments are already satisfied.
func sourceEnv(ctx context.Context, script, dir string, baseEnv []string, log *bytes.Buffer) ([]string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", `set -a; . "$1" 1>&2; env -0`, "bash", script)
	cmd.Dir = dir
	cmd.Env = baseEnv
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
