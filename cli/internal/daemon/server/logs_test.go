package server_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	"github.com/a-novel-kit/stack/cli/internal/daemon/server"
	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// TestStreamLogs pins how a log stream ends. A follow ends with nil when the
// client cancels, and at once with CodeUnavailable when the daemon stops, so it
// never holds the graceful shutdown. A missing log means no run yet: a snapshot
// fails with CodeNotFound and a follow waits for the run. Any other read
// failure fails the stream as it is.
func TestStreamLogs(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			tgt := &discovery.Target{Name: "rest", Service: "svc", Stack: "default"}
			stacks := discovery.Stacks{{
				Name:     "default",
				Default:  true,
				Services: []*discovery.Service{{Name: "svc", Stack: "default", Targets: []*discovery.Target{tgt}}},
			}}
			logStore := logs.New(t.TempDir())
			if testCase.running {
				w, err := logStore.OpenForWrite(tgt.ID(), tgt.Stack, tgt.Service, tgt.Name)
				require.NoError(t, err)
				t.Cleanup(func() { _ = w.Close() })
			}
			if testCase.unreadable {
				require.NoError(t, os.MkdirAll(logStore.CurrentPath(tgt.Stack, tgt.Service, tgt.Name), 0o700))
			}

			stopping := make(chan struct{})
			s := server.New(server.Config{Version: "test", Stacks: stacks, Logs: logStore, Stopping: stopping})
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
				require.Equal(t, testCase.wantCode, codeOf(err), "StreamLogs: %v", err)
			case <-time.After(5 * time.Second):
				require.Fail(t, "StreamLogs is still open 5s after its end")
			}
		})
	}
}

// TestListRuns pins that a request resolves a target shorthand against the
// daemon's default stack, so `run logs --previous` reads the target `run ps`
// shows, whatever $A_NOVEL_STACKS the client's shell holds.
func TestListRuns(t *testing.T) {
	t.Parallel()

	newService := func(stack string) *discovery.Service {
		target := &discovery.Target{Name: "rest", Service: "svc", Stack: stack}
		return &discovery.Service{Name: "svc", Stack: stack, Targets: []*discovery.Target{target}}
	}
	stacks := discovery.Stacks{
		{Name: "main", Default: true, Services: []*discovery.Service{newService("main")}},
		{Name: "fork", Services: []*discovery.Service{newService("fork")}},
	}

	testCases := []struct {
		name     string
		targetID string
		want     []string
		wantCode connect.Code
	}{
		{name: "Success/Shorthand", targetID: "svc/rest", want: []string{"20261117T120000"}},
		{name: "Success/Canonical", targetID: "main/svc/rest", want: []string{"20261117T120000"}},
		{name: "Success/OtherStack", targetID: "fork/svc/rest"},
		{name: "Error/UnknownTarget", targetID: "svc/grpc", wantCode: connect.CodeNotFound},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Only the default stack's target has an archived run.
			logStore := logs.New(t.TempDir())
			archives := filepath.Dir(logStore.CurrentPath("main", "svc", "rest"))
			fsutiltest.Write(t, archives, map[string]string{"run-20261117T120000.log": ""})
			s := server.New(server.Config{Version: "test", Stacks: stacks, Logs: logStore})

			resp, err := s.ListRuns(t.Context(), &anovelv1.ListRunsRequest{TargetId: testCase.targetID})
			require.Equal(t, testCase.wantCode, codeOf(err), "ListRuns: %v", err)
			require.Equal(t, testCase.want, resp.GetRunIds())
		})
	}
}
