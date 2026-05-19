// Package runner orchestrates long-lived `run` targets: it brings each unique
// compose env up once (shared by the targets that need it), launches every
// target as a long-lived process, captures full per-process output, and tears
// EVERYTHING down on exit or first failure — no phantom runners. The UI polls
// a thread-safe snapshot each tick; the runner owns no rendering.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/build"
	"github.com/a-novel-kit/stack/cli/internal/detect"
)

// Status is a process lifecycle state.
type Status int

const (
	Pending Status = iota // queued, env not up yet
	EnvUp                 // its compose env is being brought up
	Running               // process started, still alive
	Exited                // exited 0 (a server exiting is unusual but not an error by itself)
	Failed                // exited non-zero / failed to start / env failed
)

func (s Status) String() string {
	switch s {
	case Pending:
		return "pending"
	case EnvUp:
		return "env-up"
	case Running:
		return "running"
	case Exited:
		return "exited"
	case Failed:
		return "failed"
	default:
		return "?"
	}
}

// Proc is one run target's live state. All fields are read under mu.
type Proc struct {
	Target detect.Target

	mu      sync.Mutex
	status  Status
	started time.Time
	ended   time.Time
	exitErr error

	lines   []string // bounded ring of sanitized, completed lines
	partial []byte   // bytes after the last '\n' — not yet a line
	last    string   // most recent non-empty line (dashboard tail)
	seq     uint64   // bumps on every content change → render-on-change

	// term is closed exactly once when the process reaches a terminal state
	// (or failed to start). It lets the runner gate dependent targets on an
	// init step (migrations) finishing.
	term chan struct{}
}

// ProcView is an immutable snapshot for the UI.
type ProcView struct {
	Target  detect.Target
	Status  Status
	Elapsed time.Duration
	ExitErr error
	Last    string
	Output  string
	// Seq changes iff Output changed since the last snapshot, so the UI can
	// skip re-feeding (and re-wrapping) the viewport when nothing is new.
	Seq uint64
}

func (p *Proc) view() ProcView {
	p.mu.Lock()
	defer p.mu.Unlock()
	end := p.ended
	if end.IsZero() {
		end = time.Now()
	}
	var el time.Duration
	if !p.started.IsZero() {
		el = end.Sub(p.started)
	}
	out := strings.Join(p.lines, "\n")
	if len(p.partial) > 0 {
		ps := SanitizeLine(string(p.partial))
		if out != "" {
			out += "\n"
		}
		out += ps
	}
	return ProcView{
		Target: p.Target, Status: p.status, Elapsed: el,
		ExitErr: p.exitErr, Last: p.last, Output: out, Seq: p.seq,
	}
}

func (p *Proc) set(s Status) { p.mu.Lock(); p.status = s; p.mu.Unlock() }

// Write implements io.Writer: splits incoming bytes into newline-terminated
// lines, sanitizes each (SGR kept, everything else stripped — see output.go),
// and appends to a bounded ring. The most recent non-empty line is tracked
// for the dashboard tail, and seq is bumped so the UI re-renders only on
// actual change.
func (p *Proc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(b)
	p.partial = append(p.partial, b...)
	for {
		i := bytes.IndexByte(p.partial, '\n')
		if i < 0 {
			// No newline yet. Flush an over-long tail so a stream that never
			// emits '\n' can't grow memory without bound.
			if len(p.partial) > maxPartial {
				p.appendLine(string(p.partial))
				p.partial = p.partial[:0]
			}
			break
		}
		line := string(p.partial[:i])
		p.partial = append(p.partial[:0], p.partial[i+1:]...)
		p.appendLine(line)
	}
	p.seq++
	return n, nil
}

// appendLine sanitizes one raw line and pushes it onto the bounded ring,
// dropping the oldest line past maxLines. Caller holds p.mu.
func (p *Proc) appendLine(raw string) {
	ln := SanitizeLine(raw)
	p.lines = append(p.lines, ln)
	if len(p.lines) > maxLines {
		p.lines = p.lines[len(p.lines)-maxLines:]
	}
	if t := strings.TrimSpace(ln); t != "" {
		p.last = t
	}
}

// Runner drives a set of run targets.
type Runner struct {
	procs []*Proc
	done  chan struct{}
	once  sync.Once
	// recreate forces a scoped teardown of any pre-existing env before up
	// (vs. reusing it).
	recreate bool

	// Console context: the working dir + env a quick interactive command
	// (curl, etc.) should see, so it talks to the SAME ports/URLs the run
	// allocated. Captured once from the first prepared group.
	consoleMu   sync.Mutex
	consoleOnce sync.Once
	consoleDir  string
	consoleEnv  []string
}

// setConsole records the dir/env a console command should run with. Called
// once, with the first group that has a prepared env (so curl sees the
// allocated REST_URL/ports); a no-env target falls back to its own dir.
func (r *Runner) setConsole(dir string, env []string) {
	r.consoleOnce.Do(func() {
		r.consoleMu.Lock()
		r.consoleDir, r.consoleEnv = dir, env
		r.consoleMu.Unlock()
	})
}

// Console returns the dir and env for an interactive command (curl, …). The
// env may be nil (inherit the process environment); the dir may be "" (inherit
// the current working directory).
func (r *Runner) Console() (string, []string) {
	r.consoleMu.Lock()
	defer r.consoleMu.Unlock()
	return r.consoleDir, r.consoleEnv
}

// New builds a Runner over the selected targets. recreate=true tears any
// existing env down and rebuilds it; false reuses an already-up env.
func New(targets []detect.Target, recreate bool) *Runner {
	r := &Runner{done: make(chan struct{}), recreate: recreate}
	for _, t := range targets {
		r.procs = append(r.procs, &Proc{
			Target: t, status: Pending, term: make(chan struct{}),
		})
	}
	return r
}

// Snapshot returns the current state of every proc, in selection order.
func (r *Runner) Snapshot() []ProcView {
	out := make([]ProcView, len(r.procs))
	for i, p := range r.procs {
		out[i] = p.view()
	}
	return out
}

// Done is closed once every process has terminated and teardown has finished.
func (r *Runner) Done() <-chan struct{} { return r.done }

// Run brings envs up, launches all targets, and blocks until ctx is cancelled
// or a target fails — then tears everything down. It is meant to run in a
// goroutine; the UI watches Snapshot()/Done().
func (r *Runner) Run(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	// Group targets by their compose env (project) so a shared env is brought
	// up exactly once and its allocated ports/creds are reused by every
	// process that needs it.
	type envGroup struct {
		env  *detect.ComposeEnv
		prep []string // PrepareEnv result, shared across the group
		ps   []*Proc
	}
	groups := map[string]*envGroup{}
	var order []string
	var noEnv []*Proc
	for _, p := range r.procs {
		if p.Target.Env == nil {
			noEnv = append(noEnv, p)
			continue
		}
		key := p.Target.Env.Project
		g := groups[key]
		if g == nil {
			g = &envGroup{env: p.Target.Env}
			groups[key] = g
			order = append(order, key)
		}
		g.ps = append(g.ps, p)
	}

	var ups []detect.ComposeEnv // envs we brought up → teardown list

	fail := func(p *Proc, err error) {
		p.mu.Lock()
		p.status = Failed
		p.exitErr = err
		p.ended = time.Now()
		p.mu.Unlock()
		cancel() // first failure tears everything down
	}

	// Bring each unique env up once, then launch its processes.
	for _, key := range order {
		g := groups[key]
		if ctx.Err() != nil {
			break
		}
		for _, p := range g.ps {
			p.set(EnvUp)
		}
		writer := groupWriter(g.ps)

		prep, err := build.PrepareEnv(ctx, g.ps[0].Target, writer)
		if err != nil {
			for _, p := range g.ps {
				fail(p, err)
			}
			continue
		}
		g.prep = prep

		// Reuse vs recreate: a pre-existing env is reused by default; recreate
		// tears it down first so code/image changes take effect.
		if r.recreate {
			_, _ = fmt.Fprintf(writer, "── env recreate: %s ──\n", g.env.ID)
			_ = build.TearDown(context.WithoutCancel(ctx), *g.env)
		}
		_, _ = fmt.Fprintf(writer, "── env up: %s ──\n", g.env.ID)
		if err := build.EnvUp(ctx, prep, g.env, writer); err != nil {
			for _, p := range g.ps {
				fail(p, err)
			}
			continue
		}
		ups = append(ups, *g.env)
		// First prepared env wins the console context: a curl typed in the
		// console then sees this group's allocated REST_URL/ports.
		r.setConsole(g.ps[0].Target.Dir, g.prep)
		r.launchGroup(ctx, g.ps, g.prep, fail)
	}
	if len(noEnv) > 0 && ctx.Err() == nil {
		// Fallback only if no env group set it: inherit the process env,
		// run from the target's dir.
		r.setConsole(noEnv[0].Target.Dir, nil)
		r.launchGroup(ctx, noEnv, nil, fail)
	}

	<-ctx.Done() // user quit (parent cancelled) or a target failed

	// Teardown: kill every still-running process group, then scoped-down
	// every env we brought up. Detached context so cleanup always completes.
	clean := context.WithoutCancel(parent)
	for _, e := range ups {
		_ = build.TearDown(clean, e)
	}
	r.once.Do(func() { close(r.done) })
}

// isInit reports whether a target is a schema/init step that dependent
// targets must wait for. By a-novel convention that is the `migrations`
// Go entrypoint (cmd/migrations): rotate-keys / rest / grpc all need the
// schema it creates, so launching them in parallel raced it and failed with
// `relation "active_keys" does not exist`.
func isInit(t detect.Target) bool {
	return t.Kind == detect.KindGo && t.Name == "migrations"
}

// launchGroup launches a set of co-located procs with an init barrier: every
// init target (migrations) is run to completion FIRST, in order, then the
// rest start concurrently. If an init fails it cancels the run (via fail), so
// the loop stops before launching the dependents.
func (r *Runner) launchGroup(ctx context.Context, ps []*Proc, env []string, fail func(*Proc, error)) {
	var inits, rest []*Proc
	for _, p := range ps {
		if isInit(p.Target) {
			inits = append(inits, p)
		} else {
			rest = append(rest, p)
		}
	}
	for _, p := range inits {
		if ctx.Err() != nil {
			return
		}
		r.launch(ctx, p, env, fail)
		select {
		case <-p.term: // migrations finished (Exited) or failed (ctx cancelled)
		case <-ctx.Done():
			return
		}
	}
	for _, p := range rest {
		if ctx.Err() != nil {
			return
		}
		r.launch(ctx, p, env, fail)
	}
}

// launch starts one process in its own group and watches it. A non-zero exit
// (or spawn failure) fails the whole run.
func (r *Runner) launch(ctx context.Context, p *Proc, env []string, fail func(*Proc, error)) {
	cmd := exec.Command(p.Target.Cmd, p.Target.Args...)
	cmd.Dir = p.Target.Dir
	if env != nil {
		cmd.Env = env
	}
	cmd.Stdout = p
	cmd.Stderr = p
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	p.mu.Lock()
	p.started = time.Now()
	p.mu.Unlock()

	if err := cmd.Start(); err != nil {
		fail(p, fmt.Errorf("failed to start: %w", err))
		close(p.term) // never started → unblock any init barrier waiting on it
		return
	}
	p.set(Running)

	exited := make(chan struct{})
	// Own process group killed on teardown (ctx cancel) — like build.Run, so
	// the whole tree dies, not just the direct child.
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		case <-exited:
		}
	}()
	go func() {
		err := cmd.Wait()
		close(exited)
		p.mu.Lock()
		p.ended = time.Now()
		p.mu.Unlock()
		defer close(p.term) // terminal state reached → release init barrier
		switch {
		case ctx.Err() != nil:
			// We killed it during teardown — expected, not a failure.
			p.set(Exited)
		case err != nil:
			// Non-zero / abnormal exit is a real failure: tear the run down.
			fail(p, err)
		default:
			// Clean exit 0. One-shot targets (migrations, rotate-keys) do
			// this by design — it is a success, NOT an error, and must not
			// cascade-teardown the still-running servers. Long-lived servers
			// rarely exit 0 on their own, but when they do it is still a
			// clean stop, not a failure to surface.
			p.set(Exited)
		}
	}()
}

// groupWriter fans env-up/setup output to every proc in the group so it shows
// in each one's log.
func groupWriter(ps []*Proc) io.Writer {
	ws := make([]io.Writer, len(ps))
	for i, p := range ps {
		ws[i] = p
	}
	return io.MultiWriter(ws...)
}
