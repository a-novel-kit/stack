package server

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// TestStreamFileToClient pins the snapshot a tail sends: the last lines that
// pass the stream filter, in file order.
func TestStreamFileToClient(t *testing.T) {
	t.Parallel()

	record := func(stream, line string) string {
		return `{"ts":"2026-07-22T10:00:00Z","stream":"` + stream + `","line":"` + line + `"}` + "\n"
	}
	path := filepath.Join(t.TempDir(), "current.log")
	content := record("stdout", "one") + record("stderr", "two") + record("stdout", "three") + record("stdout", "four")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	testCases := []struct {
		name   string
		filter anovelv1.LogStream
		tail   int
		want   []string
	}{
		{name: "Success/NoTail", want: []string{"one", "two", "three", "four"}},
		{name: "Success/Tail", tail: 2, want: []string{"three", "four"}},
		{name: "Success/TailAfterFilter", filter: anovelv1.LogStream_LOG_STREAM_STDERR, tail: 2, want: []string{"two"}},
		{name: "Success/TailAboveLength", tail: 10, want: []string{"one", "two", "three", "four"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			send := func(ln *anovelv1.StreamLogsResponse) error {
				got = append(got, ln.GetLine())
				return nil
			}
			if err := streamFileToClient(t.Context(), path, send, testCase.filter, testCase.tail); err != nil {
				t.Fatalf("streamFileToClient: %v", err)
			}
			if !slices.Equal(got, testCase.want) {
				t.Errorf("sent %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestPodmanLogsArgs pins that an infra log honors --tail and --follow.
func TestPodmanLogsArgs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		follow bool
		tail   int32
		want   string
	}{
		{name: "Success/Snapshot", want: "logs cid"},
		{name: "Success/Follow", follow: true, want: "logs -f cid"},
		{name: "Success/TailAndFollow", follow: true, tail: 100, want: "logs -f --tail 100 cid"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.Join(podmanLogsArgs("cid", testCase.follow, testCase.tail), " "); got != testCase.want {
				t.Errorf("podmanLogsArgs = %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestStreamLogs pins how a log stream ends. A follow ends with nil when the
// client cancels, and at once with CodeUnavailable when the daemon stops, so it
// never holds the graceful shutdown. A missing log means no run yet: a snapshot
// fails with CodeNotFound and a follow waits for the run. Any other read
// failure fails the stream as it is.
func TestStreamLogs(t *testing.T) {
	testCases := []struct {
		name string
		// follow asks for the lines written after the snapshot.
		follow bool
		// running opens the target's log writer, as a running target does.
		// Its follow sends nothing to the zero stream and lasts until the test
		// ends it.
		running bool
		// stop ends a running follow by stopping the daemon. Otherwise the
		// client cancels it.
		stop bool
		// unreadable puts a directory where current.log belongs, so the read
		// fails.
		unreadable bool
		// wantCode is the connect code the stream ends with, 0 for nil.
		wantCode connect.Code
	}{
		{name: "Success/ClientCancel", follow: true, running: true},
		{name: "Success/NoRunFollow", follow: true},
		{name: "Error/DaemonStopping", follow: true, running: true, stop: true, wantCode: connect.CodeUnavailable},
		{name: "Error/NoRun", wantCode: connect.CodeNotFound},
		{name: "Error/UnreadableLog", unreadable: true, wantCode: connect.CodeUnknown},
		{name: "Error/UnreadableLogFollow", follow: true, unreadable: true, wantCode: connect.CodeUnknown},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// The log store writes under XDG_STATE_HOME.
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			tgt := &discovery.Target{Name: "rest", Service: "svc", Stack: "default"}
			stacks := discovery.Stacks{{
				Name:     "default",
				Default:  true,
				Services: []*discovery.Service{{Name: "svc", Stack: "default", Targets: []*discovery.Target{tgt}}},
			}}
			logStore := logs.New()
			if testCase.running {
				w, err := logStore.OpenForWrite(tgt.ID(), tgt.Stack, tgt.Service, tgt.Name)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = w.Close() })
			}
			if testCase.unreadable {
				if err := os.MkdirAll(logStore.CurrentPath(tgt.Stack, tgt.Service, tgt.Name), 0o700); err != nil {
					t.Fatal(err)
				}
			}

			stopping := make(chan struct{})
			s := &Server{stacks: stacks, logs: logStore, stopping: stopping}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			ended := make(chan error, 1)
			req := &anovelv1.StreamLogsRequest{TargetId: tgt.ID(), Follow: testCase.follow}
			go func() {
				ended <- s.StreamLogs(ctx, req, anovelv1connect.CoreServiceStreamLogsServerStream{})
			}()
			switch {
			case testCase.running && testCase.stop:
				close(stopping)
			case testCase.running:
				cancel()
			}

			select {
			case err := <-ended:
				if code := codeOf(err); code != testCase.wantCode {
					t.Errorf("StreamLogs: got %v, want code %v", err, testCase.wantCode)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("StreamLogs is still open 5s after its end")
			}
		})
	}
}

// TestLineScanner pins that a line over 1 MiB never stops a scan. It comes out
// in 1 MiB pieces and the lines after it still come through, so an Exec child
// writing it never blocks on a pipe nobody reads.
func TestLineScanner(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 2*maxLineBytes+maxLineBytes/2)
	piece, rest := strings.Repeat("x", maxLineBytes), strings.Repeat("x", maxLineBytes/2)

	testCases := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "Success/ShortLines", input: "one\ntwo\n", want: []string{"one", "two"}},
		{name: "Success/LongLine", input: "before\n" + long + "\nafter\n", want: []string{"before", piece, piece, rest, "after"}},
		{name: "Success/LongLineAtEOF", input: long, want: []string{piece, piece, rest}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var got []string
			sc := lineScanner(strings.NewReader(testCase.input))
			for sc.Scan() {
				got = append(got, sc.Text())
			}
			if err := sc.Err(); err != nil {
				t.Fatalf("scan: %v", err)
			}
			// The lines run to MiBs, so a mismatch reports their lengths.
			if !slices.Equal(got, testCase.want) {
				t.Errorf("line lengths = %v, want %v", lengths(got), lengths(testCase.want))
			}
		})
	}
}

// lengths returns the length of each line.
func lengths(lines []string) []int {
	out := make([]int, len(lines))
	for i, line := range lines {
		out[i] = len(line)
	}
	return out
}
