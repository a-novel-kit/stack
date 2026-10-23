package build

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
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

// PrepareEnv builds the process environment for a target. It starts from
// os.Environ(). A target with a compose env adds its allocated host ports, the
// URLs derived from them and the local test credentials. The repo's declared
// secrets come last, so a provisioned value wins over any default. Progress
// goes to out, and secret values never do.
func PrepareEnv(t detect.Target, out io.Writer) ([]string, error) {
	runEnv := os.Environ()
	if t.Env != nil {
		added, err := composeEnv(t.Env, runEnv)
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
func composeEnv(e *detect.ComposeEnv, base []string) ([]string, error) {
	ports, err := allocPorts(e.Ports)
	if err != nil {
		return nil, fmt.Errorf("port allocation failed: %w", err)
	}
	added := slices.Concat(ports, testDefaults(e.Refs, slices.Concat(base, ports)))
	return append(added, derivedURLs(e.Ports, slices.Concat(base, added))...), nil
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
	runEnv, err := PrepareEnv(t, out)
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
	_, _ = fmt.Fprint(out, "── env down ──\n")
	_ = compose(context.WithoutCancel(ctx), out, runEnv, e, nil, "down", "--volume")
}

// runCommand runs t's command in its own process group, writing its output to
// out, and kills the group when ctx ends.
func runCommand(ctx context.Context, t detect.Target, runEnv []string, out io.Writer) error {
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

// repoRoot is the repository root of a target, which holds its secrets
// manifest: the compose file's grandparent (builds/<file> → repo) when there is
// an env, else the target's own directory (kit libs, root pnpm).
func repoRoot(t detect.Target) string {
	if t.Env != nil {
		return filepath.Dir(filepath.Dir(t.Env.File))
	}
	return t.Dir
}

// allocPorts binds an ephemeral TCP port for each var name and returns
// "NAME=port" entries. Every listener holds its port until allocPorts returns,
// so binds within one call never alias. A small race remains between releasing
// a port and the container claiming it, accepted as unavoidable for local runs.
func allocPorts(names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, n := range names {
		l, err := net.Listen("tcp", "localhost:0")
		if err != nil {
			return nil, err
		}
		defer func() { _ = l.Close() }()
		addr, ok := l.Addr().(*net.TCPAddr)
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
// fixed `<X>_PORT` rule: POSTGRES_PORT→the Postgres connection settings,
// GRPC_PORT→GRPC_URL (host:port), and any other X_PORT→X_URL
// (http://localhost:port).
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
			out = append(out, postgresEnv(env, p)...)
		case "GRPC":
			out = append(out, "GRPC_URL=localhost:"+p)
		default:
			out = append(out, base+"_URL=http://localhost:"+p)
		}
	}
	return out
}

// postgresEnv lists the settings that point a test process at the env's
// Postgres on port, in both forms services read: the POSTGRES_DSN URL, and the
// discrete POSTGRES_* settings a service prefers once POSTGRES_HOST is set,
// beside the allocated POSTGRES_PORT. It leaves out a setting env already
// holds, so the env block lists each once.
//
// Both forms log in with env's POSTGRES_USER, POSTGRES_PASSWORD and
// POSTGRES_DB, the values the compose file hands the container, or pgStd for an
// unset one. The test database serves no TLS.
func postgresEnv(env []string, port string) []string {
	user := cmp.Or(envVal(env, "POSTGRES_USER"), pgStd)
	password := cmp.Or(envVal(env, "POSTGRES_PASSWORD"), pgStd)
	db := cmp.Or(envVal(env, "POSTGRES_DB"), pgStd)

	dsn := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     "localhost:" + port,
		Path:     "/" + db,
		RawQuery: "sslmode=disable",
	}
	settings := []string{
		"POSTGRES_DSN=" + dsn.String(),
		"POSTGRES_HOST=localhost",
		"POSTGRES_USER=" + user,
		"POSTGRES_PASSWORD=" + password,
		"POSTGRES_DATABASE=" + db,
		"POSTGRES_TLS_ENABLED=false",
	}
	return slices.DeleteFunc(settings, func(kv string) bool {
		key, value, _ := strings.Cut(kv, "=")
		return envVal(env, key) == value
	})
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

// podmanOut runs `podman <args...>` and returns its trimmed stdout, with stderr
// folded into the error. Its output is parsed, so it stays out of the job's
// log.
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

// TearDown removes one compose project's containers, volumes and orphans.
// `-p <project> -f <file>` scopes it to that env. --remove-orphans also clears
// containers the compose file no longer declares, such as after a profile
// change, which a later `up` would collide with. `-t 10` gives Postgres ten
// seconds for its shutdown checkpoint, which can pass 2s once migrations and
// key rotation dirtied its buffers.
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
	if err == nil {
		return nil
	}
	_ = exec.CommandContext(ctx, "podman", "pod", "rm", "-f", "pod_"+e.Project).Run()
	_ = exec.CommandContext(ctx, "podman", "network", "rm", "-f", e.Project+"_api").Run()
	return err
}

// dockerFormatBuildArgs builds images in the docker format. Podman's default
// OCI format drops a Dockerfile's HEALTHCHECK, which waitHealthy follows.
const dockerFormatBuildArgs = "--podman-build-args=--format docker -q"

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
		if err := compose(ctx, out, env, e, []string{dockerFormatBuildArgs}, "up", "-d", "--build"); err != nil {
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
		if err := compose(ctx, out, env, e, []string{dockerFormatBuildArgs}, args...); err != nil {
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
func waitHealthy(ctx context.Context, out io.Writer, env []string, e *detect.ComposeEnv, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		poll, err := pollEnv(ctx, env, e.Project+"_")
		if err != nil {
			return err
		}
		states := strings.Join(poll.states, "  ")
		if poll.ready {
			_, _ = fmt.Fprintf(out, "ready: %s\n", states)
			return nil
		}
		if time.Now().After(deadline) {
			return healthTimeoutError(ctx, env, poll.pending, states, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}
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
// a container that exited non-zero, quoting its log tail.
func pollEnv(ctx context.Context, env []string, prefix string) (envPoll, error) {
	ids, err := podmanOut(ctx, env, "ps", "-a", "--filter", "name="+prefix, "--format", "{{.ID}}")
	if err != nil {
		return envPoll{}, fmt.Errorf("listing containers: %w", err)
	}
	idList := strings.Fields(ids)
	poll := envPoll{ready: len(idList) > 0}
	inspected := inspectContainers(ctx, env, idList)
	for _, id := range idList {
		parts, ok := inspected[id]
		if !ok {
			poll.ready = false
			poll.pending = append(poll.pending, id)
			continue
		}
		name, st, health, code := parts[0], parts[1], parts[2], parts[3]
		poll.states = append(poll.states, fmt.Sprintf("%s=%s/%s", name, st, health))
		switch {
		case st == "exited" && code == "0":
			// one-shot completed successfully
		case st == "running" && (health == "-" || health == "healthy"):
			// long-lived service ready
		case st == "exited":
			return envPoll{}, fmt.Errorf("container %s exited with code %s:\n%s",
				name, code, containerLogTail(ctx, env, id, 30))
		default:
			poll.ready = false
			poll.pending = append(poll.pending, id)
		}
	}
	return poll, nil
}

// pollInspectFormat prints a container's full id, name, status, health and exit
// code on one line.
const pollInspectFormat = "{{.Id}}|{{.Name}}|{{.State.Status}}|" +
	"{{if .State.Health}}{{.State.Health.Status}}{{else}}-{{end}}|{{.State.ExitCode}}"

// inspectContainers inspects every container of ids in one podman call. It maps
// each id to its name, status, health and exit code. A failed call maps none,
// and the poll then counts every container as pending.
func inspectContainers(ctx context.Context, env, ids []string) map[string][]string {
	if len(ids) == 0 {
		return nil
	}
	out, err := podmanOut(ctx, env, slices.Concat([]string{"inspect", "--format", pollInspectFormat}, ids)...)
	if err != nil {
		return nil
	}
	fields := make(map[string][]string, len(ids))
	for line := range strings.Lines(out) {
		parts := strings.Split(strings.TrimRight(line, "\n"), "|")
		if len(parts) < 5 {
			continue
		}
		// podman ps prints short ids, and the inspect line starts with the full one.
		for _, id := range ids {
			if strings.HasPrefix(parts[0], id) {
				fields[id] = parts[1:]
			}
		}
	}
	return fields
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
