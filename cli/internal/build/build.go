// Package build executes a detected target as a subprocess and reports a
// structured pass/fail [Result] with captured output — the raw material the UI
// turns into a build report.
package build

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/detect"
	"github.com/a-novel-kit/stack/cli/internal/secrets"
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

// LiveLog holds the most recent output line per target, written concurrently
// by the parallel runners and read by the TUI each spinner tick so devs get a
// brief idea of progress and can spot a stalled command early. A nil *LiveLog
// is a valid no-op (non-interactive path).
type LiveLog struct {
	mu sync.Mutex
	m  map[string]string
}

// NewLiveLog returns an empty LiveLog ready for concurrent writes and reads.
func NewLiveLog() *LiveLog { return &LiveLog{m: map[string]string{}} }

func (l *LiveLog) set(id, line string) {
	if l == nil || line == "" {
		return
	}
	l.mu.Lock()
	l.m[id] = line
	l.mu.Unlock()
}

// Line returns the latest captured output line for target id ("" if none).
func (l *LiveLog) Line(id string) string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.m[id]
}

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// cleanLine reduces a raw output chunk to one readable line: text after the
// last carriage return (collapses \r progress redraws), ANSI stripped, trimmed.
func cleanLine(s string) string {
	if i := strings.LastIndexByte(s, '\r'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(ansiSeq.ReplaceAllString(s, ""))
}

// tailWriter records the most recent (completed or in-progress) output line
// into a LiveLog under the target's id. Composed via io.MultiWriter alongside
// the capture buffer, so it never swallows output.
type tailWriter struct {
	id   string
	live *LiveLog
	rest []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.rest = append(w.rest, p...)
	for {
		i := bytes.IndexByte(w.rest, '\n')
		if i < 0 {
			break
		}
		w.live.set(w.id, cleanLine(string(w.rest[:i])))
		w.rest = w.rest[i+1:]
	}
	w.live.set(w.id, cleanLine(string(w.rest)))
	return len(p), nil
}

// PrepareEnv builds the process environment for a target: os.Environ() plus,
// when the target has a compose env, Go-allocated host ports, their derived
// URLs/DSN, standard local test creds for referenced vars, and finally
// whatever scripts/setup-env.sh defines (service constants). It is the
// single source of env truth shared by `build`/`test` (Run) and `run`. The
// `── env ──` / `── setup-env ──` progress is written to out.
func PrepareEnv(ctx context.Context, t detect.Target, out io.Writer) ([]string, []string, error) {
	// Snapshot the inherited env so we can compute the CLI's full delta at
	// the end — ports/URLs/DSN AND whatever setup-env.sh subsequently
	// exported. The delta is what global-mode cross-shares between services.
	baseline := make(map[string]string, len(os.Environ()))
	for _, e := range os.Environ() {
		k, v, _ := strings.Cut(e, "=")
		baseline[k] = v
	}

	runEnv := os.Environ()
	if t.Env != nil {
		var added []string
		if len(t.Env.Ports) > 0 {
			portEnv, err := allocPorts(t.Env.Ports)
			if err != nil {
				return nil, nil, fmt.Errorf("port allocation failed: %w", err)
			}
			runEnv = append(runEnv, portEnv...)
			derived := derivedURLs(t.Env.Ports, runEnv)
			runEnv = append(runEnv, derived...)
			added = append(added, portEnv...)
			added = append(added, derived...)
		}
		// Standard local test values for known vars the compose file
		// references (e.g. an internal-only postgres' POSTGRES_PASSWORD/USER/
		// DB) that are not produced by a host port. Local-only, so a fixed
		// generic credential set is fine.
		def := testDefaults(t.Env.Refs, runEnv)
		runEnv = append(runEnv, def...)
		added = append(added, def...)
		if len(added) > 0 {
			_, _ = io.WriteString(out, formatEnvBlock(added))
		}
	}

	// Source scripts/setup-env.sh only for whatever else it defines
	// (service-specific constants like APP_MASTER_KEY). It sees the ports +
	// URLs we already set, so its `${VAR:=…}` lines are inert (no node) and
	// cannot override them. If there is no setup-env.sh, the ports + derived
	// vars above are already complete.
	if root := repoRoot(t); root != "" {
		if se := filepath.Join(root, "scripts", "setup-env.sh"); isFile(se) {
			_, _ = fmt.Fprintf(out, "── setup-env: %s ──\n", rel(root, se))
			env, err := sourceEnv(ctx, se, root, runEnv, out)
			if err != nil {
				return nil, nil, fmt.Errorf("setup-env.sh failed: %w", err)
			}
			runEnv = env
		}
	}

	// Un-prefix the operator's `<SERVICE_X>_*` vars into this service's own
	// env. From the X service's OWN perspective the value is the local name
	// (`APP_MASTER_KEY`), not the cross-service alias (`SERVICE_X_APP_MASTER_KEY`):
	// if the operator pre-exports `SERVICE_JSON_KEYS_APP_MASTER_KEY=hex` in
	// their shell, the json-keys process should read it as `APP_MASTER_KEY=hex`.
	// Applied AFTER setup-env so the operator's value wins over any default
	// the script provided.
	if prefix := servicePrefix(t.Service); prefix != "" {
		runEnv = unprefixForOwner(runEnv, prefix)
	}

	// Inject the repo's locally-stored secrets (decrypted) into the child env.
	// Driven by the value-free .a-novel/secrets.yaml manifest at the repo root;
	// an absent manifest is a no-op (most repos have none), while an absent
	// key/store reports every declared secret as missing. Appended last
	// so a developer-provisioned secret wins over any default, and folded into
	// the delta so global mode cross-shares it like any other CLI-set var.
	// Values are never printed — they are deliberately excluded from the
	// `── env ──` block above. A declared-but-unset secret is skipped and
	// surfaced as a descriptive (value-free) warning so the developer knows
	// what to set, without blocking the tests that don't need it.
	if root := repoRoot(t); root != "" {
		res, err := secrets.InjectForRepo(root)
		if err != nil {
			return nil, nil, fmt.Errorf("secrets injection failed: %w", err)
		}
		for name, value := range res.Env {
			runEnv = append(runEnv, name+"="+value)
		}
		for _, w := range res.Warnings() {
			_, _ = fmt.Fprintln(out, w)
		}
	}

	// Compute the delta: any var that did not exist in the inherited env or
	// whose value the CLI/setup-env changed. This is the set global mode
	// re-exports to other services with an `<SERVICE>_` prefix.
	delta := envDelta(runEnv, baseline)
	return runEnv, delta, nil
}

// servicePrefix is the producer-namespace prefix under the AAA_XX
// cross-service convention: "service-json-keys" → "SERVICE_JSON_KEYS_". Empty
// service → empty prefix (no un-prefix work).
func servicePrefix(svc string) string {
	if svc == "" {
		return ""
	}
	return strings.ToUpper(strings.ReplaceAll(svc, "-", "_")) + "_"
}

// unprefixForOwner mirrors a `<PREFIX><KEY>=value` entry as `<KEY>=value` —
// the OWNER-side perspective. Operator-set values therefore reach the owning
// service under its native variable name. Latest-value wins on duplicate
// inputs; the un-prefixed entry replaces any earlier same-key entry so
// owner-side reads honour the operator's intent.
func unprefixForOwner(env []string, prefix string) []string {
	val := make(map[string]string, len(env))
	order := make([]string, 0, len(env))
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		if _, ok := val[k]; !ok {
			order = append(order, k)
		}
		val[k] = v
	}
	var added []string
	for _, k := range order {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		unp := k[len(prefix):]
		if unp == "" {
			continue
		}
		if _, ok := val[unp]; !ok {
			added = append(added, unp)
		}
		val[unp] = val[k]
	}
	out := make([]string, 0, len(order)+len(added))
	for _, k := range order {
		out = append(out, k+"="+val[k])
	}
	for _, k := range added {
		out = append(out, k+"="+val[k])
	}
	return out
}

// envDelta returns the env entries in final that differ from baseline. Order
// follows final (so the most recent value of any duplicate key wins, matching
// shell-export semantics).
func envDelta(final []string, baseline map[string]string) []string {
	seen := make(map[string]string, len(final))
	for _, e := range final {
		k, v, _ := strings.Cut(e, "=")
		seen[k] = v
	}
	out := make([]string, 0, len(seen))
	for k, v := range seen {
		if bv, ok := baseline[k]; !ok || bv != v {
			out = append(out, k+"="+v)
		}
	}
	sort.Strings(out)
	return out
}

// ANSI SGR used by formatEnvBlock. build cannot import the ui/lipgloss layer
// (import cycle: ui → build), so the palette is inlined as raw 256-color
// escapes. They survive runner.SanitizeLine (SGR is kept) and are rendered by
// the viewport; on a non-TTY they are harmless and match the rest of the
// report, which is already colored.
const (
	envHdr = "\x1b[1;38;5;172m" // gold, bold — the header
	envKey = "\x1b[38;5;37m"    // accent — variable names
	envEq  = "\x1b[38;5;66m"    // muted — the " = " separator
	envRst = "\x1b[0m"
)

// formatEnvBlock renders the env vars the CLI injected as an aligned,
// key-sorted, lightly-colored list — one `KEY = value` per line under a
// header — instead of one unreadable space-joined blob. Local-only generic
// creds, so values are shown verbatim (you need the real DSN/URL to curl
// against the run).
func formatEnvBlock(kv []string) string {
	type pair struct{ k, v string }
	pairs := make([]pair, 0, len(kv))
	maxK := 0
	for _, e := range kv {
		k, v, _ := strings.Cut(e, "=")
		pairs = append(pairs, pair{k, v})
		if len(k) > maxK {
			maxK = len(k)
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].k < pairs[j].k })
	var b strings.Builder
	b.WriteString(envHdr + "── env ──" + envRst + "\n")
	for _, p := range pairs {
		pad := strings.Repeat(" ", maxK-len(p.k))
		_, _ = fmt.Fprintf(&b, "   %s%s%s %s=%s %s\n",
			envKey, p.k, envRst, envEq+pad, envRst, p.v)
	}
	return b.String()
}

// Run executes t.Cmd with t.Args in t.Dir, capturing combined output. It
// blocks until the process exits or ctx is cancelled. timeout > 0 bounds the
// whole target (env up + command); env teardown still runs on a detached
// context so a timed-out target is always cleaned up. live (may be nil)
// receives the most recent output line for the live TUI tail. It never panics
// and never returns a partially-zero Result — every field is meaningful.
func Run(ctx context.Context, t detect.Target, timeout time.Duration, live *LiveLog) Result {
	start := time.Now()

	// Per-target deadline. Cancellation propagates into exec.CommandContext
	// (kills the test/compose-up process); the deferred env-down below uses
	// context.WithoutCancel(ctx) so it survives this deadline.
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// One buffer for both streams so the report preserves the interleaving the
	// user would have seen in a terminal. compose up/down output is captured
	// into the same buffer, fenced with ── headers, so a failure to stand up
	// the environment reads as part of the same story.
	var buf bytes.Buffer
	// Everything that flows to the report buffer also tees to the live tail,
	// so env-up progress, setup-env, and the command's own output all feed
	// the "most recent line" the TUI shows under the running target.
	out := io.MultiWriter(&buf, &tailWriter{id: t.ID(), live: live})

	runEnv, _, perr := PrepareEnv(ctx, t, out)
	if perr != nil {
		return Result{
			Target:   t,
			ExitErr:  perr,
			Output:   strings.TrimRight(buf.String(), "\n"),
			Duration: time.Since(start),
		}
	}

	// Inner closure so the deferred env-down runs (and appends its output)
	// before we snapshot buf into res.Output below.
	res := func() Result {
		if t.Env != nil {
			_, _ = fmt.Fprintf(out, "── env up: %s ──\n", t.Env.ID)
			// down always runs — even on a mid-bring-up failure (wave 1 up,
			// wave 2 fails) or ctx cancellation mid-test — so register it before
			// the up. Detach the context so cleanup itself is never cancelled.
			// --volume (singular) is the form the external podman-compose
			// provider accepts.
			defer func() {
				_, _ = fmt.Fprint(out, "── env down ──\n")
				_ = compose(context.WithoutCancel(ctx), out, runEnv, t.Env, nil, "down", "--volume")
			}()
			// Bring the env up in dependency waves and wait for each to become
			// healthy before running the tests — ordering never relies on the
			// provider's depends_on wait.
			if err := composeUpPhased(ctx, out, runEnv, t.Env, 120*time.Second); err != nil {
				return Result{Target: t, ExitErr: fmt.Errorf(
					"environment %s failed to start: %w", t.Env.ID, err)}
			}
			_, _ = fmt.Fprintf(out, "── %s ──\n", t.Name)
		}

		cmd := exec.Command(t.Cmd, t.Args...)
		cmd.Dir = t.Dir
		cmd.Env = runEnv
		cmd.Stdout = out
		cmd.Stderr = out
		// Own process group so a timeout/abort kills the whole tree
		// (pnpm → sh → node, go test → compiled bins), not just the direct
		// child — otherwise Wait blocks on a grandchild's inherited pipe and
		// the deadline is reported but never enforced.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			return Result{Target: t, ExitErr: err}
		}
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			case <-done:
			}
		}()
		err := cmd.Wait()
		close(done)
		return Result{Target: t, Success: err == nil, ExitErr: err}
	}()

	// A timed-out target is a failure presented like any other: the command
	// is logged into the captured output and the error states the timeout, so
	// the report's failure panel shows "$ cmd …" + error + whatever output
	// was produced before the deadline.
	if timeout > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		_, _ = fmt.Fprintf(out, "\n── TIMED OUT after %s ──\n$ %s %s\n",
			timeout, t.Cmd, strings.Join(t.Args, " "))
		res.Success = false
		res.ExitErr = fmt.Errorf("timed out after %s (override with --timeout)", timeout)
	}

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
// returns "NAME=port" entries. There is a small close-then-reuse race between
// releasing the port and the container claiming it, accepted as unavoidable
// for local runs. Distinct binds within one call never alias because each
// listener holds a different port until closed.
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

// pgStd is the standard local postgres user, password, and database name for
// tests.
const pgStd = "postgres"

// stdTestEnv holds the standard local test values for known compose vars.
// Being local-only, a fixed generic credential set is fine. A value is applied
// for any of these the compose file references that no host port produced —
// this is what lets an internal-only postgres (no host port) still get
// credentials.
var stdTestEnv = map[string]string{
	"POSTGRES_HOST":     "localhost",
	"POSTGRES_USER":     pgStd,
	"POSTGRES_PASSWORD": pgStd,
	"POSTGRES_DB":       pgStd,
	// 32-byte hex-encoded master key for at-rest encryption. Matches the
	// fixed value every service's CI workflow uses (main.yaml /
	// release.yaml) so locally-encrypted blobs interchange with CI. Local
	// only — never a production secret. Without this, container-mode
	// rotate-keys panics with "expected 32 bytes, got 0 bytes" because
	// the compose file's `${APP_MASTER_KEY}` substitution resolves empty.
	"APP_MASTER_KEY": "fec0681a2f57242211c559ca347721766f8a3acd8ed2e63b36b3768051c702ca",
}

// has reports whether env already defines key.
func has(env []string, key string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return true
		}
	}
	return false
}

// envVal returns env's value for key, or "".
func envVal(env []string, key string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

// testDefaults fills stdTestEnv values for every known var the compose file
// references that is not already set (by a port, a derived URL, or the
// inherited environment) — credential provisioning driven by what the compose
// needs, independent of host-port exposure.
func testDefaults(refs, env []string) []string {
	var out []string
	for _, r := range refs {
		if v, known := stdTestEnv[r]; known && !has(env, r) {
			out = append(out, r+"="+v)
		}
	}
	return out
}

// derivedURLs builds the host connection vars from the allocated ports by the
// fixed `<X>_PORT` rule: POSTGRES_PORT→POSTGRES_DSN (standard local creds +
// the allocated port), GRPC_PORT→GRPC_URL (host:port), any other
// X_PORT→X_URL (http://localhost:port). Credentials themselves come from
// testDefaults, not here, so internal-only services are covered too.
func derivedURLs(names []string, env []string) []string {
	var out []string
	for _, n := range names {
		base, ok := strings.CutSuffix(n, "_PORT")
		if !ok {
			continue
		}
		p := envVal(env, n)
		switch base {
		case "POSTGRES":
			out = append(out, "POSTGRES_DSN=postgres://"+pgStd+":"+pgStd+"@localhost:"+p+"/"+pgStd+"?sslmode=disable")
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
func sourceEnv(ctx context.Context, script, dir string, baseEnv []string, log io.Writer) ([]string, error) {
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
func compose(ctx context.Context, buf io.Writer, env []string, e *detect.ComposeEnv, global []string, args ...string) error {
	full := append([]string{"compose"}, global...)
	full = append(full, "-p", e.Project, "-f", e.File)
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, "podman", full...)
	cmd.Env = env
	cmd.Stdout = buf
	cmd.Stderr = buf
	return cmd.Run()
}

// podmanOut runs `podman <args...>` and returns trimmed stdout, folding
// stderr into the error — used for the inspect/ps polling that backs
// waitHealthy (parsed, so it must not share the report buffer).
func podmanOut(ctx context.Context, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.Env = env
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// Conflict is a compose project that already has containers on the host —
// almost always the leftover of a previously aborted run that would collide
// with a target's env (deterministic project names mean the same env reuses
// the same project).
type Conflict struct {
	Env        detect.ComposeEnv
	Containers []string
}

// EnvConflicts returns the distinct env projects among targets that already
// have containers (running or not). It is a fail-safe preflight: running on
// top of a half-up env from an aborted command is the bug we want to catch.
func EnvConflicts(ctx context.Context, targets []detect.Target) []Conflict {
	seen := map[string]bool{}
	var envs []detect.ComposeEnv
	for _, t := range targets {
		if t.Env == nil || seen[t.Env.Project] {
			continue
		}
		seen[t.Env.Project] = true
		envs = append(envs, *t.Env)
	}
	if len(envs) == 0 {
		return nil
	}
	out, err := podmanOut(ctx, os.Environ(), "ps", "-a", "--format", "{{.Names}}")
	if err != nil {
		return nil
	}
	names := strings.Fields(out)
	var conflicts []Conflict
	for _, e := range envs {
		prefix := e.Project + "_"
		var hit []string
		for _, n := range names {
			if strings.HasPrefix(n, prefix) {
				hit = append(hit, n)
			}
		}
		if len(hit) > 0 {
			conflicts = append(conflicts, Conflict{Env: e, Containers: hit})
		}
	}
	return conflicts
}

// TearDown removes ONE compose project's containers, volumes AND orphans —
// scoped to that env via `-p <project> -f <file>`, never a global podman
// wipe. --remove-orphans cleans up containers the current compose no longer
// declares (e.g. after a profile change), which is what otherwise triggers
// `container name … already in use / use --replace`.
//
// Two-pass teardown:
//
//  1. Graceful: `compose down -t 10`. 10s is generous for postgres'
//     shutdown checkpoint (which can stretch past 2s once the DB has dirty
//     buffers from migrations + key rotation), but still snappy on quit.
//  2. Force fallback if pass 1 errored: podman pod rm -f + network rm -f
//     scoped to this env. Order matters — removing the pod first frees
//     its containers, which de-references the network so its rm can
//     succeed. podman-compose 1.5.0 occasionally returns success while
//     leaving a running container — that container holds its pod, which
//     holds the network — and the next run then fails to claim the same
//     project name. Errors at this stage are intentionally swallowed:
//     the resources may already be gone (first pass partially succeeded),
//     or may never have come up.
//
// Pod / network naming follows podman-compose's conventions:
//   - Pod:     "pod_<project>"  (podman-compose's resolve_pod_name convention)
//   - Network: "<project>_api"  (matches every current service's compose
//     `networks: { api: }` declaration; if a future service uses a
//     different network name we'll need to read e.Networks instead).
func TearDown(ctx context.Context, e detect.ComposeEnv) error {
	var buf bytes.Buffer
	err := compose(ctx, &buf, os.Environ(), &e, nil,
		"down", "--volume", "--remove-orphans", "-t", "10")
	if err == nil {
		return nil
	}
	// Force-reclaim the pod and network so the env name is fully free
	// before any subsequent run reuses it. Best-effort.
	_ = exec.CommandContext(ctx, "podman", "pod", "rm", "-f", "pod_"+e.Project).Run()
	_ = exec.CommandContext(ctx, "podman", "network", "rm", "-f", e.Project+"_api").Run()
	return err
}

// composeUpPhased brings a compose env up WITHOUT relying on the external
// provider's `depends_on` wait — which hangs on podman-compose ≤1.5.x and
// silently no-ops on setups without systemd (health stays "starting"). It
// starts services in two waves: dependency-free services first, then the ones
// that declare `depends_on:` (e.Dependents), waiting for each wave to become
// healthy (waitHealthy) before starting the next. --no-deps stops the provider
// from doing any ordering of its own. This mirrors the `run` daemon path
// (infra-up → healthy → target). The test composes are flat (infra + one
// standalone SUT), so two waves suffice; a deeper chain would need more.
//
// services, when non-empty, restricts the bring-up to that subset (global-mode
// duplicate skipping) and is partitioned into the same two waves. When the
// service list is unknown (empty scan), it falls back to a single whole-env
// `up` — the pre-existing behavior — rather than bringing nothing up.
//
// --remove-orphans is intentionally omitted: podman-compose's orphan handling
// relative to a positional `up <svc>` is inconsistent across versions and can
// sweep services it just created. TearDown owns orphan removal.
func composeUpPhased(ctx context.Context, out io.Writer, env []string, e *detect.ComposeEnv, healthTimeout time.Duration, services ...string) error {
	want := services
	if len(want) == 0 {
		want = e.Services
	}
	if len(want) == 0 {
		// Unknown service list — bring the whole env up at once (legacy path).
		if err := compose(ctx, out, env, e,
			[]string{"--podman-build-args=--format docker -q"}, "up", "-d", "--build"); err != nil {
			return err
		}
		return waitHealthy(ctx, out, env, e, healthTimeout)
	}

	dependent := make(map[string]bool, len(e.Dependents))
	for _, s := range e.Dependents {
		dependent[s] = true
	}
	var infra, dependents []string
	for _, s := range want {
		if dependent[s] {
			dependents = append(dependents, s)
		} else {
			infra = append(infra, s)
		}
	}

	// Label waves by execution order, not slice index — an empty infra wave (a
	// subset of only dependents) must not make the first bring-up read "wave 2".
	waveNum := 0
	for _, wave := range [][]string{infra, dependents} {
		if len(wave) == 0 {
			continue
		}
		waveNum++
		_, _ = fmt.Fprintf(out, "── env up (wave %d: %s) ──\n", waveNum, strings.Join(wave, " "))
		args := append([]string{"up", "-d", "--build", "--no-deps"}, wave...)
		if err := compose(ctx, out, env, e,
			[]string{"--podman-build-args=--format docker -q"}, args...); err != nil {
			return err
		}
		_, _ = fmt.Fprint(out, "── env wait ──\n")
		if err := waitHealthy(ctx, out, env, e, healthTimeout); err != nil {
			return err
		}
	}
	return nil
}

// EnvUp brings a compose env up (build, detached) in dependency waves, then
// waits for it to become healthy — the `run` counterpart of the env handling
// Run does inline. Progress streams to out.
//
// services, when non-empty, is a positional list of compose service names to
// bring up (instead of every profile-less service). The runner uses it in
// global mode to skip services that duplicate a sibling already running from
// its own repo.
func EnvUp(ctx context.Context, env []string, e *detect.ComposeEnv, out io.Writer, services ...string) error {
	// 180s: a fresh `--build` plus first-run postgres `initdb` can exceed the
	// 120s budget on slower machines / cold caches.
	if err := composeUpPhased(ctx, out, env, e, 180*time.Second, services...); err != nil {
		return fmt.Errorf("environment %s failed to start: %w", e.ID, err)
	}
	return nil
}

// waitHealthy polls the env's containers until every one is ready, replacing
// `compose up --wait` (unsupported by the external podman-compose provider).
// A container is ready when it is running with a healthy (or absent)
// healthcheck, or has exited 0 (a one-shot init/migration). A non-zero exit
// fails fast; otherwise it polls until timeout — on timeout the offending
// containers' tails are appended to the error so the user sees why they
// never came up.
func waitHealthy(ctx context.Context, log io.Writer, env []string, e *detect.ComposeEnv, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	prefix := e.Project + "_"
	var last string
	var lastUnhealthy []string // ids of containers not yet ready at last check
	for {
		ids, err := podmanOut(ctx, env, "ps", "-a", "--filter", "name="+prefix, "--format", "{{.ID}}")
		if err != nil {
			return fmt.Errorf("listing containers: %w", err)
		}
		idList := strings.Fields(ids)
		ready := len(idList) > 0
		var states []string
		var unhealthy []string
		for _, id := range idList {
			out, ierr := podmanOut(ctx, env, "inspect", id, "--format",
				"{{.Name}}|{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}-{{end}}|{{.State.ExitCode}}")
			parts := strings.Split(out, "|")
			if ierr != nil || len(parts) < 4 {
				ready = false
				unhealthy = append(unhealthy, id)
				continue
			}
			name, st, health, code := parts[0], parts[1], parts[2], parts[3]
			states = append(states, fmt.Sprintf("%s=%s/%s", name, st, health))
			switch {
			case st == "exited" && code == "0":
				// one-shot completed successfully
			case st == "running" && (health == "-" || health == "healthy"):
				// long-lived service ready
			case st == "exited":
				return fmt.Errorf("container %s exited with code %s:\n%s",
					name, code, containerLogTail(ctx, env, id, 30))
			default:
				ready = false
				unhealthy = append(unhealthy, id)
			}
		}
		last = strings.Join(states, "  ")
		lastUnhealthy = unhealthy
		if ready {
			_, _ = fmt.Fprintf(log, "ready: %s\n", last)
			return nil
		}
		if time.Now().After(deadline) {
			var b strings.Builder
			fmt.Fprintf(&b, "timed out after %s; last state: %s", timeout, last)
			for _, id := range lastUnhealthy {
				tail := containerLogTail(ctx, env, id, 20)
				if tail != "" {
					fmt.Fprintf(&b, "\n── tail %s ──\n%s", id[:12], tail)
				}
			}
			return errors.New(b.String())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}
}

// containerLogTail returns the last `lines` lines of a container's combined
// stdout+stderr — small helper for waitHealthy's failure path so the user
// sees postgres's own startup errors instead of just "timed out".
func containerLogTail(ctx context.Context, env []string, id string, lines int) string {
	out, err := podmanOut(ctx, env, "logs", "--tail", strconv.Itoa(lines), id)
	if err != nil {
		return ""
	}
	return strings.TrimRight(out, "\n")
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
