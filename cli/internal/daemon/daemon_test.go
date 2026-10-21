package daemon

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/server"
)

// rebindingListener wraps the daemon's listener. Its Close binds a fresh
// listener at the socket path right after the old one unlinks it, as the next
// daemon does while this one finishes shutting down.
type rebindingListener struct {
	net.Listener

	path string
	// acceptErr, when set, fails Serve on its first Accept.
	acceptErr error
	// accepting closes on the first Accept, once Serve tracks the listener.
	accepting chan struct{}
	once      sync.Once
	// next is the listener Close bound, and rebindErr its failure.
	next      net.Listener
	rebindErr error
}

func (l *rebindingListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.accepting) })
	if l.acceptErr != nil {
		return nil, l.acceptErr
	}
	return l.Listener.Accept()
}

func (l *rebindingListener) Close() error {
	err := l.Listener.Close()
	l.next, l.rebindErr = net.Listen("unix", l.path)
	return err
}

// TestServe pins that serve leaves the socket path to its listener on both
// exits. Closing the listener removes the socket file, and a socket the next
// daemon binds before serve returns stays in place.
func TestServe(t *testing.T) {
	t.Parallel()

	errAccept := errors.New("accept failed")

	testCases := []struct {
		name string
		// acceptErr fails Serve. A case without one ends serve through its ctx.
		acceptErr error
	}{
		{name: "Success/Shutdown"},
		{name: "Error/Serve", acceptErr: errAccept},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "daemon.sock")
			ln, err := listen(path)
			if err != nil {
				t.Fatal(err)
			}
			wrapped := &rebindingListener{Listener: ln, path: path, acceptErr: testCase.acceptErr, accepting: make(chan struct{})}

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			srv := server.New("test", path, nil, nil, nil, nil, cancel, ctx.Done())
			serveErr := make(chan error, 1)
			go func() { serveErr <- serve(ctx, wrapped, srv) }()

			// A failing Serve is the error case's only exit, so only the shutdown
			// case cancels, and only once Serve runs.
			<-wrapped.accepting
			if testCase.acceptErr == nil {
				cancel()
			}
			if err := <-serveErr; !errors.Is(err, testCase.acceptErr) {
				t.Fatalf("serve: got %v, want %v", err, testCase.acceptErr)
			}

			// The rebind succeeds only when closing the old listener removed its
			// socket file.
			if wrapped.rebindErr != nil {
				t.Fatalf("rebind after close: %v", wrapped.rebindErr)
			}
			t.Cleanup(func() { _ = wrapped.next.Close() })
			if !isLive(path) {
				t.Fatal("serve removed the socket the next daemon bound")
			}
		})
	}
}
