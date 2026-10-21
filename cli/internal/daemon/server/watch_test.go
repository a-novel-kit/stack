package server

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect/v2"

	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// TestWatch pins how a watch ends. A client that cancels ends it with nil. A
// daemon stop ends it at once with CodeUnavailable, so the watch never holds
// the graceful shutdown and the client reports the stop.
func TestWatch(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		// stop ends the watch by stopping the daemon. Otherwise the client
		// cancels it.
		stop bool
		// wantCode is the connect code the watch ends with, 0 for nil.
		wantCode connect.Code
	}{
		{name: "Success/ClientCancel"},
		{name: "Error/DaemonStopping", stop: true, wantCode: connect.CodeUnavailable},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			stopping := make(chan struct{})
			s := &Server{runner: runner.New(nil, nil, nil, logs.New()), stopping: stopping}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			// No event fires, so the zero stream is never sent to.
			ended := make(chan error, 1)
			go func() {
				ended <- s.Watch(ctx, &anovelv1.WatchRequest{}, anovelv1connect.CoreServiceWatchServerStream{})
			}()
			if testCase.stop {
				close(stopping)
			} else {
				cancel()
			}

			select {
			case err := <-ended:
				if code := codeOf(err); code != testCase.wantCode {
					t.Errorf("Watch: got %v, want code %v", err, testCase.wantCode)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Watch is still open 5s after its end")
			}
		})
	}
}
