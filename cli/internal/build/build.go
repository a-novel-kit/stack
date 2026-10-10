// Package build turns a detected target into a [jobs.Job]: it assembles the
// target's environment, brings its compose env up and down around the command,
// and enforces the per-target deadline.
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
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/detect"
	"github.com/a-novel-kit/stack/cli/internal/jobs"
	"github.com/a-novel-kit/stack/cli/internal/secrets"
)

// Job wraps t for the job runner. A timeout above zero bounds the whole target,
// env bring-up included; teardown then runs on a detached context, so a
// timed-out target is still cleaned up.
//
// maxProcs above zero caps GOMAXPROCS for the spawned process, so concurrent
// `go test` targets share the machine's cores. keep leaves the target's compose
// env up after the run; the next run's preflight adopts it, schema included.
func Job(t detect.Target, timeout time.Duration, maxProcs int, keep bool) jobs.Job {
	return jobs.Job{
		Name:   t.Name,
		Group:  string(t.Kind),
		Detail: t.RelDir,
		Run: func(ctx context.Context, out io.Writer) (string, error) {
			return "", run(ctx, t, timeout, maxProcs, keep, out)
		},
	}
}

// PrepareEnv builds the process environment for a target: os.Environ() plus,
// when the target has a compose env, allocated host ports, the URLs derived
// from them, standard local test credentials, and whatever
// scripts/setup-env.sh defines, then the repo's declared secrets. It streams
// its progress to out.
func PrepareEnv(ctx context.Context, t detect.Target, out io.Writer) ([]string, error) {
	runEnv := os.Environ()
	if t.Env != nil {
		var added []string
		if len(t.Env.Ports) > 0 {
			portEnv, err := allocPorts(t.Env.Ports)
			if err != nil {
				return nil, fmt.Errorf("port allocation failed: %w", err)
			}
			runEnv = append(runEnv, portEnv...)
			derived := derivedURLs(t.Env.Ports, runEnv)
			runEnv = append(runEnv, derived...)
			added = append(added, portEnv...)
			added = append(added, derived...)
		}
		// Standard local test values for the vars the compose file references
		// that no host port produced, such as an internal-only Postgres'
		// credentials.
		def := testDefaults(t.Env.Refs, runEnv)
		runEnv = append(runEnv, def...)
		added = append(added, def...)
		if len(added) > 0 {
			_, _ = io.WriteString(out, formatEnvBlock(added))
		}
	}

	// Source scripts/setup-env.sh for the service constants it defines, such as
	// APP_MASTER_KEY. It already sees the ports and URLs set above, so its
	// `${VAR:=…}` defaults stay inert and cannot override them.
	if root := repoRoot(t); root != "" {
		if se := filepath.Join(root, "scripts", "setup-env.sh"); detect.IsFile(se) {
			_, _ = fmt.Fprintf(out, "── setup-env: %s ──\n", rel(root, se))
			env, err := sourceEnv(ctx, se, root, runEnv, out)
			if err != nil {
				return nil, fmt.Errorf("setup-env.sh failed: %w", err)
			}
			runEnv = env
		}
	}

	// Secrets come last so a provisioned value wins over any default. Values
	// never reach the `── env ──` block above, and a declared-but-unset secret
	// only raises a value-free warning, so tests that don't need it still run.
	if root := repoRoot(t); root != "" {
		res, err := secrets.InjectForRepo(root)
		if err != nil {
			return nil, fmt.Errorf("secrets injection failed: %w", err)
		}
		for name, value := range res.Env {
			runEnv = append(runEnv, name+"="+value)
		}
		for _, w := range res.Warnings() {
			_, _ = fmt.Fprintln(out, w)
		}
	}

	return runEnv, nil
}

// ANSI SGR codes used by formatEnvBlock. The block lands in captured output, so
// its colors travel as raw 256-color escapes rather than lipgloss styles.
const (
	envHdr = "\x1b[1;38;5;172m" // gold, bold — the header
	envKey = "\x1b[38;5;37m"    // accent — variable names
	envEq  = "\x1b[38;5;66m"    // muted — the " = " separator
	envRst = "\x1b[0m"
)

// formatEnvBlock renders the env vars the CLI injected as an aligned,
// key-sorted list, one `KEY = value` per line under a header. Values appear
// verbatim: they are local-only credentials, and the real DSN or URL is what a
// developer needs to reach the run.
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
	slices.SortFunc(pairs, func(a, b pair) int { return strings.Compare(a.k, b.k) })
	var b strings.Builder
	b.WriteString(envHdr + "── env ──" + envRst + "\n")
	for _, p := range pairs {
		pad := strings.Repeat(" ", maxK-len(p.k))
		_, _ = fmt.Fprintf(&b, "   %s%s%s %s=%s %s\n",
			envKey, p.k, envRst, envEq+pad, envRst, p.v)
	}
	return b.String()
}

// run executes t.Cmd with t.Args in t.Dir, writing the command line, the env
// assembly, compose output and the command's own output to out.
func run(ctx context.Context, t detect.Target, timeout time.Duration, maxProcs int, keep bool, out io.Writer) error {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	_, _ = fmt.Fprintf(out, "$ %s %s\n", t.Cmd, strings.Join(t.Args, " "))
	runEnv, err := PrepareEnv(ctx, t, out)
	if err != nil {
		return err
	}

	// GOMAXPROCS also bounds `go test`'s compile parallelism, since -p defaults
	// to it, so the cap throttles the build and the run alike. A target that
	// pins GOMAXPROCS itself keeps its own value.
	if maxProcs > 0 && !has(runEnv, "GOMAXPROCS") {
		runEnv = append(runEnv, "GOMAXPROCS="+strconv.Itoa(maxProcs))
	}

	err = runInEnv(ctx, t, keep, runEnv, out)
	if timeout > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		_, _ = fmt.Fprintf(out, "\n── TIMED OUT after %s ──\n", timeout)
		return fmt.Errorf("timed out after %s (override with --timeout)", timeout)
	}
	return err
}

// runInEnv brings t's compose env up, when it has one, runs the command, and
// tears the env down unless keep is set.
func runInEnv(ctx context.Context, t detect.Target, keep bool, runEnv []string, out io.Writer) error {
	if t.Env != nil {
		_, _ = fmt.Fprintf(out, "── env up: %s ──\n", t.Env.ID)
		// The down is registered before the up so it runs even when bring-up
		// fails midway, on a detached context so cleanup is never cancelled.
		// The podman-compose provider accepts --volume in the singular.
		if keep {
			_, _ = fmt.Fprint(out, "── env kept up (--keep) ──\n")
		} else {
			defer func() {
				_, _ = fmt.Fprint(out, "── env down ──\n")
				_ = compose(context.WithoutCancel(ctx), out, runEnv, t.Env, nil, "down", "--volume")
			}()
		}
		if err := composeUpPhased(ctx, out, runEnv, t.Env, 120*time.Second); err != nil {
			return fmt.Errorf("environment %s failed to start: %w", t.Env.ID, err)
		}
		_, _ = fmt.Fprintf(out, "── %s ──\n", t.Name)
	}

	cmd := exec.Command(t.Cmd, t.Args...)
	cmd.Dir = t.Dir
	cmd.Env = runEnv
	cmd.Stdout = out
	cmd.Stderr = out
	// Own process group so a timeout or abort kills the whole tree (pnpm → sh →
	// node, go test → compiled bins). Killing only the direct child leaves Wait
	// blocked on a grandchild's inherited pipe, and the deadline is never
	// enforced.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) })
	defer stop()
	return cmd.Wait()
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

func rel(base, p string) string {
	if r, err := filepath.Rel(base, p); err == nil {
		return r
	}
	return p
}

// allocPorts binds an ephemeral TCP port for each var name, closes it, and
// returns "NAME=port" entries. A small race remains between releasing a port
// and the container claiming it, accepted as unavoidable for local runs. Binds
// within one call never alias, since each listener holds its port until closed.
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

// stdTestEnv holds the standard local test values for known compose vars. They
// are local-only, so a fixed generic credential set is fine. A value applies to
// any var the compose file references that no host port produced, which is how
// an internal-only Postgres still gets credentials.
var stdTestEnv = map[string]string{
	"POSTGRES_HOST":     "localhost",
	"POSTGRES_USER":     pgStd,
	"POSTGRES_PASSWORD": pgStd,
	"POSTGRES_DB":       pgStd,
	// 32-byte hex-encoded master key for at-rest encryption, matching the
	// fixed value every service's CI workflow uses so locally-encrypted blobs
	// interchange with CI. Local only, never a production secret. Without it
	// the compose file's `${APP_MASTER_KEY}` resolves empty and container-mode
	// rotate-keys panics with "expected 32 bytes, got 0 bytes".
	"APP_MASTER_KEY": "fec0681a2f57242211c559ca347721766f8a3acd8ed2e63b36b3768051c702ca",
}

// has reports whether env already defines key.
func has(env []string, key string) bool {
	return slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, key+"=") })
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

// testDefaults fills in stdTestEnv values for every var the compose file
// references that no port, derived URL, or inherited variable already set.
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
// fixed `<X>_PORT` rule: POSTGRES_PORT→POSTGRES_DSN, GRPC_PORT→GRPC_URL
// (host:port), and any other X_PORT→X_URL (http://localhost:port). Credentials
// come from testDefaults.
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

// podmanOut runs `podman <args...>` and returns trimmed stdout, folding stderr
// into the error. It backs the inspect/ps polling in waitHealthy, which parses
// the output, so it stays out of the report buffer.
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

// A Conflict is a compose project that already has containers on the host,
// almost always left behind by an aborted run. Project names are deterministic,
// so the same env always reclaims the same project.
type Conflict struct {
	Env        detect.ComposeEnv
	Containers []string
}

// EnvConflicts returns the distinct env projects among targets that already
// have containers, running or not. It is the preflight that catches a half-up
// env left by an aborted command.
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

// TearDown removes one compose project's containers, volumes, and orphans,
// scoped to that env via `-p <project> -f <file>`. --remove-orphans clears
// containers the current compose no longer declares, such as after a profile
// change, which otherwise trigger `container name … already in use`.
//
// Teardown runs in two passes:
//
//  1. `compose down -t 10`. 10s covers postgres' shutdown checkpoint, which can
//     stretch past 2s once migrations and key rotation have dirtied its
//     buffers, and still quits promptly.
//  2. On an error from the first pass, `podman pod rm -f` then `network rm -f`
//     for this env. podman-compose 1.5.0 sometimes reports success while
//     leaving a container running; that container holds its pod, which holds
//     the network, and the next run cannot claim the project name. Removing the
//     pod first frees its containers and de-references the network, so the
//     network rm succeeds. Errors here are swallowed, since the resources may
//     already be gone or may never have come up.
//
// The names follow podman-compose's conventions: the pod is "pod_<project>",
// and the network "<project>_api" — matching every service compose file's
// `networks: { api: }` declaration.
func TearDown(ctx context.Context, e detect.ComposeEnv) error {
	var buf bytes.Buffer
	err := compose(ctx, &buf, os.Environ(), &e, nil,
		"down", "--volume", "--remove-orphans", "-t", "10")
	if err == nil {
		return nil
	}
	// Force-reclaim the pod and network so the project name is free again.
	_ = exec.CommandContext(ctx, "podman", "pod", "rm", "-f", "pod_"+e.Project).Run()
	_ = exec.CommandContext(ctx, "podman", "network", "rm", "-f", e.Project+"_api").Run()
	return err
}

// composeUpPhased brings a compose env up in two waves: dependency-free
// services first, then the ones declaring `depends_on:` (e.Dependents), waiting
// for each wave to become healthy before starting the next. --no-deps keeps the
// provider from ordering anything itself. Its `depends_on` wait hangs on
// podman-compose ≤1.5.x, and without systemd it silently no-ops and leaves
// health at "starting". Two waves cover the flat test composes: infra plus one
// standalone subject.
//
// An env whose service list is unknown comes up in a single whole-env `up`.
//
// --remove-orphans stays out: podman-compose's orphan handling around a
// positional `up <svc>` varies by version and can sweep services it just
// created. TearDown owns orphan removal.
func composeUpPhased(ctx context.Context, out io.Writer, env []string, e *detect.ComposeEnv, healthTimeout time.Duration) error {
	if len(e.Services) == 0 {
		// Unknown service list: bring the whole env up at once.
		if err := compose(ctx, out, env, e,
			[]string{"--podman-build-args=--format docker -q"}, "up", "-d", "--build"); err != nil {
			return err
		}
		return waitHealthy(ctx, out, env, e, healthTimeout)
	}

	var infra, dependents []string
	for _, s := range e.Services {
		if slices.Contains(e.Dependents, s) {
			dependents = append(dependents, s)
		} else {
			infra = append(infra, s)
		}
	}

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

// waitHealthy polls the env's containers until every one is ready, standing in
// for the `compose up --wait` the podman-compose provider lacks. A container is
// ready once it runs with a healthy or absent healthcheck, or has exited 0 as a
// one-shot init or migration. A non-zero exit fails fast; otherwise polling
// continues to the timeout, whose error carries the offending containers' log
// tails so the user sees why they never came up.
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
// stdout+stderr, so waitHealthy's failure path can show a container's own
// startup errors.
func containerLogTail(ctx context.Context, env []string, id string, lines int) string {
	out, err := podmanOut(ctx, env, "logs", "--tail", strconv.Itoa(lines), id)
	if err != nil {
		return ""
	}
	return strings.TrimRight(out, "\n")
}
