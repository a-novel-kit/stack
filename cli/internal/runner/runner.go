// Package runner orchestrates long-lived `run` targets: it brings each unique
// compose env up once (shared by the targets that need it), launches every
// target as a long-lived process, captures full per-process output, and tears
// EVERYTHING down on exit or first failure — no phantom runners. The UI polls
// a thread-safe snapshot each tick; the runner owns no rendering.
package runner

import (
	"bytes"
	"context"
	"errors"
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
	buf     bytes.Buffer
	last    string
}

// ProcView is an immutable snapshot for the UI.
type ProcView struct {
	Target  detect.Target
	Status  Status
	Elapsed time.Duration
	ExitErr error
	Last    string
	Output  string
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
	return ProcView{
		Target: p.Target, Status: p.status, Elapsed: el,
		ExitErr: p.exitErr, Last: p.last, Output: p.buf.String(),
	}
}

func (p *Proc) set(s Status) { p.mu.Lock(); p.status = s; p.mu.Unlock() }

// Write implements io.Writer: appends to the full buffer and tracks the most
// recent line for the dashboard.
func (p *Proc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf.Write(b)
	if ln := lastLine(p.buf.Bytes()); ln != "" {
		p.last = ln
	}
	return len(b), nil
}

// Runner drives a set of run targets.
type Runner struct {
	procs []*Proc
	done  chan struct{}
	once  sync.Once
	// recreate forces a scoped teardown of any pre-existing env before up
	// (vs. reusing it).
	recreate bool
}

// New builds a Runner over the selected targets. recreate=true tears any
// existing env down and rebuilds it; false reuses an already-up env.
func New(targets []detect.Target, recreate bool) *Runner {
	r := &Runner{done: make(chan struct{}), recreate: recreate}
	for _, t := range targets {
		r.procs = append(r.procs, &Proc{Target: t, status: Pending})
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
		for _, p := range g.ps {
			r.launch(ctx, p, g.prep, fail)
		}
	}
	for _, p := range noEnv {
		if ctx.Err() != nil {
			break
		}
		r.launch(ctx, p, nil, fail)
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
		switch {
		case ctx.Err() != nil:
			// We killed it during teardown — expected, not a failure.
			p.set(Exited)
		case err != nil:
			fail(p, err)
		default:
			// A long-lived target exiting 0 on its own is still unexpected
			// for `run`; treat it as a failure so the whole run tears down.
			fail(p, errors.New("process exited unexpectedly"))
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

// lastLine returns the last non-empty line in b, trimmed.
func lastLine(b []byte) string {
	s := strings.TrimRight(string(b), "\n")
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}
