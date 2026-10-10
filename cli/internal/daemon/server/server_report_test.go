package server

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	"github.com/a-novel-kit/stack/cli/internal/daemon/reinstall"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// Tests for the places the daemon reports an outcome to an operator who has no
// other way to check it: what a shutdown or a reinstall handoff managed to
// stop, and whether a log snapshot reached the end of the file.

// TestPrepareReinstallStopsGoExecTargets covers the reinstall handoff. A
// go-exec process outlives its daemon, so a target left running keeps the
// ports the next daemon relaunches it on.
func TestPrepareReinstallStopsGoExecTargets(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// The runner spawns `go run`. A stand-in go that only sleeps keeps the
	// toolchain out of the test.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o700); err != nil { //nolint:gosec // an executable fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	tgt := &discovery.Target{Name: "rest", Service: "svc", Stack: "default", CmdDir: filepath.Join(t.TempDir(), "cmd", "rest")}
	stacks := discovery.Stacks{{
		Name:     "default",
		Default:  true,
		Services: []*discovery.Service{{Name: "svc", Stack: "default", Targets: []*discovery.Target{tgt}}},
	}}
	alloc := env.NewAllocator()
	builder := env.NewBuilder(alloc)
	logStore := logs.New()
	run := runner.New(stacks, alloc, builder, logStore)
	stopped := make(chan struct{})
	srv := New("test", "", stacks, run, builder, logStore, func() { close(stopped) })

	if err := run.Relaunch(t.Context(), tgt.ID(), os.Environ()); err != nil {
		t.Fatalf("Relaunch: %v", err)
	}
	t.Cleanup(func() { _ = run.Kill(context.Background(), tgt.ID(), 0) })

	resp, err := srv.PrepareReinstall(t.Context(), &anovelv1.PrepareReinstallRequest{})
	if err != nil {
		t.Fatalf("PrepareReinstall: %v", err)
	}

	if resp.GetGoExecTargetCount() != 1 {
		t.Errorf("checkpointed %d target(s), want 1", resp.GetGoExecTargetCount())
	}
	if cp, err := reinstall.Read(); err != nil || cp == nil || len(cp.GoExec) != 1 || cp.GoExec[0].TargetID != tgt.ID() {
		t.Errorf("checkpoint = %+v (err %v), want %s listed", cp, err, tgt.ID())
	}
	if inst, _ := run.Instance(tgt.ID()); inst.Phase != anovelv1.Phase_PHASE_TERMINATED {
		t.Errorf("target phase after the handoff: got %v, want TERMINATED", inst.Phase)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Error("PrepareReinstall never stopped the daemon")
	}
}

// errAfterFirst yields one whole record and then fails, standing in for a log
// file on failing storage.
type errAfterFirst struct{ done bool }

var errRead = errors.New("input/output error")

func (r *errAfterFirst) Read(p []byte) (int, error) {
	if r.done {
		return 0, errRead
	}
	r.done = true
	return copy(p, `{"ts":"2026-07-22T10:00:00Z","stream":"stdout","line":"first"}`+"\n"), nil
}

func TestStreamLines(t *testing.T) {
	record := func(stream, line string) string {
		return `{"ts":"2026-07-22T10:00:00Z","stream":"` + stream + `","line":"` + line + `"}` + "\n"
	}
	cases := []struct {
		name      string
		in        io.Reader
		filter    anovelv1.LogStream
		cancelled bool
		want      []string // a substring of each line sent, in order
		wantErr   error
	}{
		{
			// A process killed mid-write leaves exactly this: whole records, then a
			// partial one. Everything after the tear has to still arrive, and the
			// tear itself has to be visible.
			name: "ReportsACorruptRecord",
			in:   strings.NewReader(record("stdout", "first") + `{"ts":"2026-07-22T10:00:01Z","stream":"stdo` + "\n" + record("stdout", "third")),
			want: []string{"first", "line 2 is unreadable", "third"},
		},
		{
			// A record that does not decode has no stream to match on. Filtering it
			// away would put the viewer back in front of a log that looks complete.
			name:   "MarkerSurvivesAStreamFilter",
			in:     strings.NewReader(record("stderr", "err") + "{ broken\n"),
			filter: anovelv1.LogStream_LOG_STREAM_STDOUT,
			want:   []string{"unreadable"},
		},
		{
			// EOF is the end of the log; anything else is the reader giving up
			// part-way through one, and the two need different answers.
			name: "SurfacesAReadFailure", in: &errAfterFirst{}, want: []string{"first"}, wantErr: errRead,
		},
		{name: "EndsCleanlyAtEOF", in: strings.NewReader(record("stdout", "only")), want: []string{"only"}},
		{
			// ReadString returns the trailing bytes together with io.EOF, so an
			// unterminated last record is data, not the end of the file.
			name: "ReadsAFinalRecordWithNoNewline",
			in:   strings.NewReader(strings.TrimSuffix(record("stdout", "last"), "\n")),
			want: []string{"last"},
		},
		// A client that went away is not a failure.
		{name: "StopsOnACancelledContext", in: strings.NewReader("{ broken\n"), cancelled: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if c.cancelled {
				cancel()
			}
			var got []string
			err := streamLines(ctx, c.in, func(ln *anovelv1.LogLine) error {
				got = append(got, ln.GetLine())
				return nil
			}, c.filter, "current.log")
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("streamLines: got %v, want %v", err, c.wantErr)
			}
			if len(got) != len(c.want) {
				t.Fatalf("got lines %q, want %d matching %q", got, len(c.want), c.want)
			}
			for i, want := range c.want {
				if !strings.Contains(got[i], want) {
					t.Errorf("line %d: got %q, want it to contain %q", i+1, got[i], want)
				}
			}
		})
	}
}

var errKill = errors.New("container stop timed out")

func TestTearDown(t *testing.T) {
	cases := []struct {
		name             string
		targets          []string
		sessions         []runner.InfraSessionRef
		killed, tornDown int
		failures         []string // a substring of each failure, sorted
	}{
		{
			// Three targets attempted, two stopped; the count reports the second
			// number. Failures are sorted, so the assertion does not depend on
			// goroutine completion order.
			name:     "CountsOnlyWhatItStopped",
			targets:  []string{"a", "b", "c"},
			sessions: []runner.InfraSessionRef{{Stack: "default", Service: "auth"}, {Stack: "default", Service: "keys"}},
			killed:   2, tornDown: 1,
			failures: []string{"go-exec target b", "default/keys"},
		},
		// Without --force the caller passes no sessions, and nothing may reach
		// podman.
		{name: "SkipsInfraWhenNotForced", targets: []string{"a"}, killed: 1},
		// Nothing running is a clean shutdown, not a failed one.
		{name: "OnAnEmptyEnvironment"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var infraCalls atomic.Int32
			out := tearDown(c.targets, c.sessions,
				func(id string) error {
					if id == "b" {
						return errKill
					}
					return nil
				},
				func(ref runner.InfraSessionRef) error {
					infraCalls.Add(1)
					if ref.Service == "keys" {
						return errKill
					}
					return nil
				})
			if out.goExecKilled != c.killed || out.infraTornDown != c.tornDown || int(infraCalls.Load()) != len(c.sessions) {
				t.Errorf("killed=%d torn=%d infra calls=%d, want %d, %d, %d",
					out.goExecKilled, out.infraTornDown, infraCalls.Load(), c.killed, c.tornDown, len(c.sessions))
			}
			if len(out.failures) != len(c.failures) {
				t.Fatalf("failures = %q, want %d", out.failures, len(c.failures))
			}
			for i, want := range c.failures {
				if !strings.Contains(out.failures[i], want) {
					t.Errorf("failures[%d]: got %q, want it to name %q", i, out.failures[i], want)
				}
			}
		})
	}
}
