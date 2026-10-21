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

// TestStreamLogs pins how a log follow ends. A client that cancels ends it with
// nil. A daemon stop ends it at once with CodeUnavailable, so the follow never
// holds the graceful shutdown and the client reports the stop.
func TestStreamLogs(t *testing.T) {
	testCases := []struct {
		name string
		// stop ends the follow by stopping the daemon. Otherwise the client
		// cancels it.
		stop bool
		// wantCode is the connect code the follow ends with, 0 for nil.
		wantCode connect.Code
	}{
		{name: "Success/ClientCancel"},
		{name: "Error/DaemonStopping", stop: true, wantCode: connect.CodeUnavailable},
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
			// An open writer gives the follow a live subscription and an
			// empty current.log, so the follow sends nothing to the zero
			// stream.
			w, err := logStore.OpenForWrite(tgt.ID(), tgt.Stack, tgt.Service, tgt.Name)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = w.Close() })

			stopping := make(chan struct{})
			s := &Server{stacks: stacks, logs: logStore, stopping: stopping}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			ended := make(chan error, 1)
			req := &anovelv1.StreamLogsRequest{TargetId: tgt.ID(), Follow: true}
			go func() {
				ended <- s.StreamLogs(ctx, req, anovelv1connect.CoreServiceStreamLogsServerStream{})
			}()
			if testCase.stop {
				close(stopping)
			} else {
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
