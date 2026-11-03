package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	"github.com/a-novel-kit/stack/cli/internal/daemon/reinstall"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	"github.com/a-novel-kit/stack/cli/internal/daemon/server"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
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

// TestListen pins that daemons starting at once bind the socket once: every
// other start finds it taken and refuses, and the socket stays the winner's.
func TestListen(t *testing.T) {
	t.Parallel()

	const starts = 8
	path := filepath.Join(t.TempDir(), "daemon.sock")
	listeners := make([]net.Listener, starts)
	errs := make([]error, starts)
	var wg sync.WaitGroup
	for i := range starts {
		wg.Go(func() { listeners[i], errs[i] = listen(path) })
	}
	wg.Wait()

	var bound int
	for i, ln := range listeners {
		if errs[i] == nil {
			bound++
			t.Cleanup(func() { _ = ln.Close() })
		}
	}
	if bound != 1 {
		t.Fatalf("%d starts bound the socket, want 1: %v", bound, errs)
	}
	if !isLive(path) {
		t.Fatal("the bound socket is gone")
	}
}

// TestReplayCheckpoint pins that startup consumes the reinstall checkpoint
// whatever it holds. An unusable checkpoint left on disk refuses every later
// install with "checkpoint already present".
func TestReplayCheckpoint(t *testing.T) {
	testCases := []struct {
		name string
		// content is the checkpoint file, "" for none.
		content string
	}{
		{name: "Success/None"},
		{name: "Success/Valid", content: fmt.Sprintf(`{"schema": %d, "go_exec": []}`, reinstall.SchemaVersion)},
		{name: "Error/Malformed", content: "{"},
		{name: "Error/OtherSchema", content: fmt.Sprintf(`{"schema": %d}`, reinstall.SchemaVersion+1)},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			if testCase.content != "" {
				if err := os.MkdirAll(filepath.Dir(paths.ReinstallCheckpoint()), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(paths.ReinstallCheckpoint(), []byte(testCase.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			replayCheckpoint(t.Context(), runner.New(nil, nil, nil, logs.New()))

			if reinstall.Exists() {
				t.Error("the checkpoint is still on disk after startup")
			}
		})
	}
}

// TestLockFile pins that the lock beside the socket never follows a symlink: on
// the /tmp fallback, another user could plant one there.
func TestLockFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "daemon.sock.lock")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if unlock, err := lockFile(link); err == nil {
		unlock()
		t.Fatal("lockFile followed a symlink")
	}

	unlock, err := lockFile(filepath.Join(dir, "own.lock"))
	if err != nil {
		t.Fatalf("lockFile on a fresh path: %v", err)
	}
	unlock()
}
