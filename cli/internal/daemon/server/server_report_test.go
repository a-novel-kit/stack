package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func collect(t *testing.T) (func(*anovelv1.LogLine) error, *[]*anovelv1.LogLine) {
	t.Helper()
	var got []*anovelv1.LogLine
	return func(ln *anovelv1.LogLine) error {
		got = append(got, ln)
		return nil
	}, &got
}

func TestStreamLinesReportsACorruptRecord(t *testing.T) {
	// A process killed mid-write leaves exactly this: whole records, then a
	// partial one. Everything after the tear has to still arrive, and the tear
	// itself has to be visible.
	in := strings.Join([]string{
		`{"ts":"2026-07-22T10:00:00Z","stream":"stdout","line":"first"}`,
		`{"ts":"2026-07-22T10:00:01Z","stream":"stdo`,
		`{"ts":"2026-07-22T10:00:02Z","stream":"stdout","line":"third"}`,
		"",
	}, "\n")

	send, got := collect(t)
	if err := streamLines(t.Context(), strings.NewReader(in), send,
		anovelv1.LogStream_LOG_STREAM_UNSPECIFIED, "current.log"); err != nil {
		t.Fatalf("streamLines: %v", err)
	}

	if len(*got) != 3 {
		t.Fatalf("got %d line(s), want 3 (two records and a marker)", len(*got))
	}
	if (*got)[0].GetLine() != "first" {
		t.Errorf("line 1: got %q, want %q", (*got)[0].GetLine(), "first")
	}
	if !strings.Contains((*got)[1].GetLine(), "line 2 is unreadable") {
		t.Errorf("line 2: got %q, want a marker naming line 2", (*got)[1].GetLine())
	}
	if (*got)[2].GetLine() != "third" {
		t.Errorf("line 3: got %q, want %q — the log must not stop at the corrupt record",
			(*got)[2].GetLine(), "third")
	}
}

func TestStreamLinesMarkerSurvivesAStreamFilter(t *testing.T) {
	// A record that does not decode has no stream to match on. Filtering it away
	// would put the viewer back in front of a log that looks complete.
	in := "{\"ts\":\"2026-07-22T10:00:00Z\",\"stream\":\"stderr\",\"line\":\"err\"}\n{ broken\n"

	send, got := collect(t)
	if err := streamLines(t.Context(), strings.NewReader(in), send,
		anovelv1.LogStream_LOG_STREAM_STDOUT, "current.log"); err != nil {
		t.Fatalf("streamLines: %v", err)
	}

	if len(*got) != 1 {
		t.Fatalf("got %d line(s), want 1 — the stderr record filtered out, the marker kept", len(*got))
	}
	if !strings.Contains((*got)[0].GetLine(), "unreadable") {
		t.Errorf("got %q, want the corruption marker", (*got)[0].GetLine())
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

func TestStreamLinesSurfacesAReadFailure(t *testing.T) {
	// EOF is the end of the log; anything else is the reader giving up part-way
	// through one, and the two need different answers.
	send, got := collect(t)
	err := streamLines(t.Context(), &errAfterFirst{}, send,
		anovelv1.LogStream_LOG_STREAM_UNSPECIFIED, "current.log")

	if !errors.Is(err, errRead) {
		t.Fatalf("streamLines: got %v, want the reader's error", err)
	}
	if len(*got) != 1 {
		t.Errorf("got %d line(s), want the one whole record read before the failure", len(*got))
	}
}

func TestStreamLinesEndsCleanlyAtEOF(t *testing.T) {
	// A well-formed file reports no error, which is what the read-failure case
	// above must not cost.
	in := "{\"ts\":\"2026-07-22T10:00:00Z\",\"stream\":\"stdout\",\"line\":\"only\"}\n"

	send, got := collect(t)
	if err := streamLines(t.Context(), strings.NewReader(in), send,
		anovelv1.LogStream_LOG_STREAM_UNSPECIFIED, "current.log"); err != nil {
		t.Fatalf("streamLines: %v", err)
	}
	if len(*got) != 1 || (*got)[0].GetLine() != "only" {
		t.Errorf("got %d line(s), want the single record", len(*got))
	}
}

func TestStreamLinesReadsAFinalRecordWithNoNewline(t *testing.T) {
	// ReadString returns the trailing bytes together with io.EOF, so an
	// unterminated last record is data, not the end of the file.
	in := `{"ts":"2026-07-22T10:00:00Z","stream":"stdout","line":"last"}`

	send, got := collect(t)
	if err := streamLines(t.Context(), strings.NewReader(in), send,
		anovelv1.LogStream_LOG_STREAM_UNSPECIFIED, "current.log"); err != nil {
		t.Fatalf("streamLines: %v", err)
	}
	if len(*got) != 1 || (*got)[0].GetLine() != "last" {
		t.Fatalf("got %d line(s), want the unterminated final record", len(*got))
	}
}

func TestStreamLinesStopsOnACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	send, got := collect(t)
	if err := streamLines(ctx, strings.NewReader("{ broken\n"), send,
		anovelv1.LogStream_LOG_STREAM_UNSPECIFIED, "current.log"); err != nil {
		t.Fatalf("streamLines: got %v, want nil for a client that went away", err)
	}
	if len(*got) != 0 {
		t.Errorf("got %d line(s), want none", len(*got))
	}
}

var errKill = errors.New("container stop timed out")

func TestTearDownCountsOnlyWhatItStopped(t *testing.T) {
	sessions := []runner.InfraSessionRef{
		{Stack: "default", Service: "auth"},
		{Stack: "default", Service: "keys"},
	}

	out := tearDown(
		[]string{"a", "b", "c"},
		sessions,
		func(id string) error {
			if id == "b" {
				return errKill
			}
			return nil
		},
		func(ref runner.InfraSessionRef) error {
			if ref.Service == "keys" {
				return errKill
			}
			return nil
		},
	)

	// Three targets attempted, two stopped; the count reports the second number.
	if out.goExecKilled != 2 {
		t.Errorf("goExecKilled: got %d, want 2 of 3 attempted", out.goExecKilled)
	}
	if out.infraTornDown != 1 {
		t.Errorf("infraTornDown: got %d, want 1 of 2 attempted", out.infraTornDown)
	}
	if len(out.failures) != 2 {
		t.Fatalf("failures: got %d, want 2", len(out.failures))
	}
	// Sorted, so the assertion does not depend on goroutine completion order.
	if !strings.Contains(out.failures[0], "go-exec target b") {
		t.Errorf("failures[0]: got %q, want the failed go-exec target named", out.failures[0])
	}
	if !strings.Contains(out.failures[1], "default/keys") {
		t.Errorf("failures[1]: got %q, want the failed infra session named", out.failures[1])
	}
}

func TestTearDownSkipsInfraWhenNotForced(t *testing.T) {
	// Without --force the caller passes no sessions, and nothing may reach
	// podman.
	called := false
	out := tearDown([]string{"a"}, nil,
		func(string) error { return nil },
		func(runner.InfraSessionRef) error {
			called = true
			return nil
		})

	if called {
		t.Error("killInfra ran with no sessions to tear down")
	}
	if out.goExecKilled != 1 || len(out.failures) != 0 {
		t.Errorf("got killed=%d failures=%d, want 1 and 0", out.goExecKilled, len(out.failures))
	}
}

func TestTearDownOnAnEmptyEnvironment(t *testing.T) {
	// Nothing running is a clean shutdown, not a failed one.
	out := tearDown(nil, nil,
		func(string) error { return errKill },
		func(runner.InfraSessionRef) error { return errKill })

	if out.goExecKilled != 0 || out.infraTornDown != 0 || len(out.failures) != 0 {
		t.Errorf("got killed=%d torn=%d failures=%d, want all zero",
			out.goExecKilled, out.infraTornDown, len(out.failures))
	}
}
