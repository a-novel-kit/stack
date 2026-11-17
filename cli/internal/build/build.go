package build

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
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
	"github.com/a-novel-kit/stack/cli/internal/shared/hostenv"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	"github.com/a-novel-kit/stack/cli/internal/shared/podman"
	"github.com/a-novel-kit/stack/cli/internal/shared/poll"
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

// PrepareEnv builds the process environment for a target. It starts from
// os.Environ(). A target with a compose env adds its allocated host ports, the
// URLs derived from them and the local test credentials. The repo's declared
// secrets come last, so a provisioned value wins over any default. Progress
// goes to out, and secret values never do.
func PrepareEnv(ctx context.Context, t detect.Target, out io.Writer) ([]string, error) {
	runEnv := os.Environ()
	if t.Env != nil {
		added, err := composeEnv(ctx, t.Env, runEnv)
		if err != nil {
			return nil, err
		}
		runEnv = append(runEnv, added...)
		if len(added) > 0 {
			_, _ = io.WriteString(out, formatEnvBlock(added))
		}
	}
	root := repoRoot(t)
	if root == "" {
		return runEnv, nil
	}

	// An unset secret only raises a value-free warning, so tests that do not
	// need it still run.
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
	return runEnv, nil
}

// composeEnv lists the vars a compose env adds to base: allocated host ports,
// standard local test values for the vars the compose file references that
// nothing else set, such as an internal-only Postgres' credentials, and the
// URLs and connection settings derived from both.
func composeEnv(ctx context.Context, e *detect.ComposeEnv, base []string) ([]string, error) {
	ports, err := allocPorts(ctx, e.Ports)
	if err != nil {
		return nil, fmt.Errorf("port allocation failed: %w", err)
	}
	added := slices.Concat(ports, testDefaults(e.Refs, slices.Concat(base, ports)))
	return append(added, derivedURLs(e.Ports, slices.Concat(base, added))...), nil
}

// ANSI SGR codes used by formatEnvBlock. The block lands in captured output, so
// its colors travel as raw 256-color escapes rather than lipgloss styles.
const (
	envHdr = "\x1b[1;38;5;172m" // gold, bold: the header
	envKey = "\x1b[38;5;37m"    // accent: variable names
	envEq  = "\x1b[38;5;66m"    // muted: the " = " separator
	envRst = "\x1b[0m"
)

// formatEnvBlock renders the env vars the CLI injected as an aligned,
// key-sorted list, one `KEY = value` per line under a header. Values appear
// verbatim: they are local-only credentials, and the real DSN or URL is what a
// developer needs to reach the run.
func formatEnvBlock(kv []string) string {
	type pair struct{ k, v string }
	pairs := make([]pair, 0, len(kv))
	var maxK int
	for _, e := range kv {
		k, v, _ := strings.Cut(e, "=")
		pairs = append(pairs, pair{k, v})
		maxK = max(maxK, len(k))
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

// envHealthTimeout bounds each bring-up wave's wait for healthy containers.
const envHealthTimeout = 120 * time.Second

// runInEnv brings t's compose env up, when it has one, runs the command, and
// tears the env down unless keep is set.
func runInEnv(ctx context.Context, t detect.Target, keep bool, runEnv []string, out io.Writer) error {
	if t.Env != nil {
		_, _ = fmt.Fprintf(out, "── env up: %s ──\n", t.Env.ID)
		if keep {
			_, _ = fmt.Fprint(out, "── env kept up (--keep) ──\n")
		} else {
			// Registered before the up, so a half-started env is torn down too.
			defer envDown(ctx, t.Env, runEnv, out)
		}
		if err := composeUpPhased(ctx, out, runEnv, t.Env, envHealthTimeout); err != nil {
			return fmt.Errorf("environment %s failed to start: %w", t.Env.ID, err)
		}
		_, _ = fmt.Fprintf(out, "── %s ──\n", t.Name)
	}
	return runCommand(ctx, t, runEnv, out)
}

// envDown tears e down on a detached context, so an aborted run still cleans
// up. The podman-compose provider spells the volume flag in the singular.
func envDown(ctx context.Context, e *detect.ComposeEnv, runEnv []string, out io.Writer) {
	ctx = context.WithoutCancel(ctx)
	_, _ = fmt.Fprint(out, "── env down ──\n")
	_ = compose(ctx, out, runEnv, e, nil, "down", "--volume")
	untagImages(ctx, e)
}

// untagImages removes the tags of the images e built. A removed checkout then
// leaves no tag behind, and `podman image prune` reclaims its images. The
// untagged images stay in the store, and the next run's build reuses them as
// cache.
//
// Each tag goes by its exact name. A bare `podman untag <image>` strips every
// tag of the image, other checkouts' tags included.
func untagImages(ctx context.Context, e *detect.ComposeEnv) {
	for _, image := range e.Images {
		_ = podman.Run(ctx, nil, "untag", image, image)
	}
}

// runCommand runs t's command in its own process group, writing its output to
// out, and kills the group when ctx ends.
func runCommand(ctx context.Context, t detect.Target, runEnv []string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, t.Cmd, t.Args...)
	cmd.Dir = t.Dir
	cmd.Env = runEnv
	cmd.Stdout = out
	cmd.Stderr = out
	// Own process group so a timeout or abort kills the whole tree (pnpm → sh →
	// node, go test → compiled bins). Killing only the direct child leaves Wait
	// blocked on a grandchild's inherited pipe, and the deadline is never
	// enforced. WaitDelay bounds the wait for a descendant that left the group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	return cmd.Run()
}

// repoRoot is the repository root of a target, which holds its secrets
// manifest: the compose file's grandparent (builds/<file> → repo) when there is
// an env, else the target's own directory (kit libs, root pnpm).
func repoRoot(t detect.Target) string {
	if t.Env != nil {
		return filepath.Dir(filepath.Dir(t.Env.File))
	}
	return t.Dir
}

// allocPorts binds a free host port for each var name and returns "NAME=port"
// entries. Every listener holds its port until allocPorts returns, so binds
// within one call never alias.
func allocPorts(ctx context.Context, names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, n := range names {
		listener, port, err := hostenv.Listen(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = listener.Close() }()
		out = append(out, n+"="+strconv.Itoa(port))
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
	// APP_MASTER_KEY is the 32-byte hex master key for at-rest encryption. It
	// equals the fixed value the services' CI workflows use, so blobs encrypted
	// locally and in CI interchange. It is a local-only value. Container-mode
	// rotate-keys needs it: an empty `${APP_MASTER_KEY}` makes it panic with
	// "expected 32 bytes, got 0 bytes".
	"APP_MASTER_KEY": "fec0681a2f57242211c559ca347721766f8a3acd8ed2e63b36b3768051c702ca",
}

// has reports whether env already defines key.
func has(env []string, key string) bool {
	return slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, key+"=") })
}

// envVal returns env's value for key, or "". The last entry for key wins, as
// it does for a child process's [exec.Cmd.Env].
func envVal(env []string, key string) string {
	for _, kv := range slices.Backward(env) {
		if v, ok := strings.CutPrefix(kv, key+"="); ok {
			return v
		}
	}
	return ""
}

// testDefaults fills in stdTestEnv values for every var the compose file
// references that no port or inherited variable already set.
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
// fixed `<X>_PORT` rule: POSTGRES_PORT gives the Postgres connection settings,
// any other X_PORT gives X_URL (see hostenv.URL).
func derivedURLs(names []string, env []string) []string {
	var out []string
	for _, n := range names {
		base, ok := strings.CutSuffix(n, "_PORT")
		if !ok {
			continue
		}
		// allocPorts wrote the value, so it always parses.
		port, _ := strconv.Atoi(envVal(env, n))
		if base == "POSTGRES" {
			out = append(out, postgresEnv(env, port)...)
		} else {
			out = append(out, base+"_URL="+hostenv.URL(base, port))
		}
	}
	return out
}

// postgresEnv lists the settings that point a test process at the env's
// Postgres on port (see hostenv.Postgres), logging in with the user, password
// and database the compose file hands the container. It forces
// POSTGRES_TLS_ENABLED off, since the test database serves no TLS, and leaves
// out a setting env already holds, so the env block lists each once.
func postgresEnv(env []string, port int) []string {
	settings := hostenv.Postgres(
		envVal(env, "POSTGRES_USER"),
		envVal(env, "POSTGRES_PASSWORD"),
		// The compose file hands the container its database as POSTGRES_DB,
		// while a POSTGRES_DATABASE the shell inherited names another one.
		envVal(env, "POSTGRES_DB"),
		port,
	)
	settings["POSTGRES_TLS_ENABLED"] = "false"

	out := make([]string, 0, len(settings))
	for _, key := range slices.Sorted(maps.Keys(settings)) {
		if envVal(env, key) != settings[key] {
			out = append(out, key+"="+settings[key])
		}
	}
	return out
}

// compose runs `podman compose [global...] -p <project> -f <file> <args...>`
// with the given environment, streaming combined output to out.
//
// The command runs in its own process group, and a cancelled ctx kills the
// whole group. The compose provider's children hold the output pipe, and
// killing podman alone leaves Run waiting on them. WaitDelay bounds the wait
// for a descendant that left the group.
func compose(ctx context.Context, out io.Writer, env []string, e *detect.ComposeEnv, global []string, args ...string) error {
	full := slices.Concat([]string{"compose"}, global, []string{"-p", e.Project, "-f", e.File}, args)
	cmd := exec.CommandContext(ctx, "podman", full...)
	cmd.Env = env
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	return cmd.Run()
}

// distinctEnvs returns the envs of targets, each project once.
func distinctEnvs(targets []detect.Target) []detect.ComposeEnv {
	seen := map[string]bool{}
	var envs []detect.ComposeEnv
	for _, t := range targets {
		if t.Env == nil || seen[t.Env.Project] {
			continue
		}
		seen[t.Env.Project] = true
		envs = append(envs, *t.Env)
	}
	return envs
}

// ClaimEnvs locks the env of every target until the returned release runs, so
// a second run of the same checkout stops instead of sharing or tearing down an
// env in use. Each lock is a flock on a file named after the env's project, in
// a directory of the shared temp dir that must belong to this user (see
// paths.Lock). The kernel drops a lock when its process ends, however it ends,
// so an env left by a run that died holds none.
func ClaimEnvs(targets []detect.Target) (func(), error) {
	var held []func()
	release := func() {
		for _, unlock := range held {
			unlock()
		}
	}

	envs := distinctEnvs(targets)
	if len(envs) == 0 {
		return release, nil
	}
	dir := filepath.Join(os.TempDir(), "a-novel-test-envs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create env lock directory: %w", err)
	}
	if err := paths.CheckOwner(dir); err != nil {
		return nil, fmt.Errorf("env lock directory: %w", err)
	}

	for _, e := range envs {
		unlock, err := paths.Lock(filepath.Join(dir, e.Project+".lock"), false)
		if errors.Is(err, paths.ErrLocked) {
			err = fmt.Errorf("env %s is in use by another run of this checkout; retry once it ends", e.ID)
		}
		if err != nil {
			release()
			return nil, err
		}
		held = append(held, unlock)
	}
	return release, nil
}

// A Conflict is a compose project that already has containers on the host,
// left behind by an earlier run of the same checkout. Project names are fixed
// per checkout, so the same env always reclaims the same project.
type Conflict struct {
	Env        detect.ComposeEnv
	Containers []string
}

// EnvConflicts returns the distinct env projects among targets that already
// have containers, running or not. It is the preflight that catches a half-up
// env left by an aborted command.
func EnvConflicts(ctx context.Context, targets []detect.Target) []Conflict {
	envs := distinctEnvs(targets)
	if len(envs) == 0 {
		return nil
	}
	out, err := podman.Output(ctx, os.Environ(), "ps", "-a", "--format", "{{.Names}}")
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

// TearDown removes one compose project's containers, volumes and orphans, and
// untags its images. `-p <project> -f <file>` scopes it to that env.
// --remove-orphans also clears containers the compose file no longer declares,
// such as after a profile change, which a later `up` would collide with.
// `-t 10` gives Postgres ten seconds for its shutdown checkpoint, which can
// pass 2s once migrations and key rotation dirtied its buffers.
//
// A failed `down` can leave a container that keeps the env's pod and network
// alive, and the next run then cannot claim the project name. TearDown then
// force-removes the pod `pod_<project>` and the network `<project>_api`, and
// ignores their errors because either may be gone. Both names follow
// podman-compose's naming for the `networks: { api: }` block every service
// compose file declares.
func TearDown(ctx context.Context, e detect.ComposeEnv) error {
	var buf bytes.Buffer
	err := compose(ctx, &buf, os.Environ(), &e, nil,
		"down", "--volume", "--remove-orphans", "-t", "10")
	untagImages(ctx, &e)
	if err == nil {
		return nil
	}
	_ = podman.Run(ctx, nil, "pod", "rm", "-f", "pod_"+e.Project)
	_ = podman.Run(ctx, nil, "network", "rm", "-f", e.Project+"_api")
	return err
}

// composeUpPhased brings a compose env up in two waves and waits for each wave
// to become healthy. The first wave holds the dependency-free services, the
// second the ones declaring `depends_on:` (e.Dependents). Two waves cover the
// flat test composes: infra plus one standalone subject. An env with an
// unknown service list comes up in one whole-env `up`.
//
// --no-deps stops the provider from ordering services itself. The
// podman-compose `depends_on` wait hangs on versions up to 1.5.x, and without
// systemd it does nothing and leaves health at "starting".
//
// --remove-orphans stays out: podman-compose handles orphans around a
// positional `up <svc>` differently across versions, and can remove services
// it just created. TearDown owns orphan removal.
func composeUpPhased(ctx context.Context, out io.Writer, env []string, e *detect.ComposeEnv, healthTimeout time.Duration) error {
	if len(e.Services) == 0 {
		if err := compose(ctx, out, env, e, []string{podman.DockerFormatBuildArgs}, "up", "-d", "--build"); err != nil {
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

	var waveNum int
	for _, wave := range [][]string{infra, dependents} {
		if len(wave) == 0 {
			continue
		}
		waveNum++
		_, _ = fmt.Fprintf(out, "── env up (wave %d: %s) ──\n", waveNum, strings.Join(wave, " "))
		args := append([]string{"up", "-d", "--build", "--no-deps"}, wave...)
		if err := compose(ctx, out, env, e, []string{podman.DockerFormatBuildArgs}, args...); err != nil {
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
// for the `compose up --wait` the podman-compose provider lacks. A container
// that exits non-zero fails the wait at once; otherwise it lasts until the
// timeout, whose error carries the log tails of the containers still pending,
// so the user sees why they never came up.
func waitHealthy(ctx context.Context, out io.Writer, env []string, e *detect.ComposeEnv, timeout time.Duration) error {
	var last envPoll
	err := poll.Until(ctx, 1500*time.Millisecond, timeout, func(ctx context.Context) (bool, error) {
		var err error
		last, err = pollEnv(ctx, env, e.Project+"_")
		return last.ready, err
	})

	states := strings.Join(last.states, "  ")
	switch {
	case errors.Is(err, poll.ErrTimeout):
		return healthTimeoutError(ctx, env, last.pending, states, timeout)
	case err != nil:
		return err
	}
	_, _ = fmt.Fprintf(out, "ready: %s\n", states)
	return nil
}

// healthTimeoutError reports the env's last state, with the log tail of each
// container still pending.
func healthTimeoutError(ctx context.Context, env, pending []string, states string, timeout time.Duration) error {
	var b strings.Builder
	fmt.Fprintf(&b, "timed out after %s; last state: %s", timeout, states)
	for _, id := range pending {
		if tail := containerLogTail(ctx, env, id, 20); tail != "" {
			fmt.Fprintf(&b, "\n── tail %s ──\n%s", id[:12], tail)
		}
	}
	return errors.New(b.String())
}

// envPoll is one inspection of a compose env's containers.
type envPoll struct {
	ready   bool
	states  []string // "<name>=<status>/<health>" per inspected container
	pending []string // ids of the containers not ready yet
}

// pollEnv inspects every container whose name starts with prefix. It fails on
// a container that exited non-zero, quoting its log tail. A container the
// inspect call missed counts as pending.
func pollEnv(ctx context.Context, env []string, prefix string) (envPoll, error) {
	ids, err := podman.IDs(ctx, env, "--filter", "name="+prefix)
	if err != nil {
		return envPoll{}, fmt.Errorf("listing containers: %w", err)
	}

	result := envPoll{ready: len(ids) > 0}
	containers, _ := podman.Inspect(ctx, env, ids)
	for _, id := range ids {
		c, ok := podman.Find(containers, id)
		if !ok {
			result.ready = false
			result.pending = append(result.pending, id)
			continue
		}

		result.states = append(result.states, fmt.Sprintf("%s=%s/%s", c.Name, c.State.Status, c.HealthStatus()))
		if c.Failed() {
			return envPoll{}, fmt.Errorf("container %s exited with code %d:\n%s",
				c.Name, c.State.ExitCode, containerLogTail(ctx, env, id, 30))
		}
		if !c.Ready() {
			result.ready = false
			result.pending = append(result.pending, id)
		}
	}
	return result, nil
}

// containerLogTail returns the last `lines` lines of a container's combined
// stdout+stderr, so waitHealthy's failure path can show a container's own
// startup errors.
func containerLogTail(ctx context.Context, env []string, id string, lines int) string {
	out, err := podman.Output(ctx, env, "logs", "--tail", strconv.Itoa(lines), id)
	if err != nil {
		return ""
	}
	return strings.TrimRight(out, "\n")
}
