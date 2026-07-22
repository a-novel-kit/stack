// Package daemon owns the daemon's process lifecycle: socket listener,
// graceful shutdown, signal handling, and recovery of orphan containers.
// The RPC handlers live in internal/daemon/server.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	"github.com/a-novel-kit/stack/cli/internal/daemon/reinstall"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	"github.com/a-novel-kit/stack/cli/internal/daemon/server"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
	"github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1/anovelv1connect"
)

// Options configures Run.
type Options struct {
	Version    string         // daemon binary version
	SocketPath string         // override; default paths.Socket()
	Stacks     []stacks.Stack // override; default stacks.ParseEnv()
}

// registeredAndDiscovered narrows the configured stacks to those discovery
// kept, preserving the configured order and therefore which entry is default.
// It answers which stacks the daemon manages, where the registration list alone
// over-answers once a vanished stack is skipped.
func registeredAndDiscovered(configured []stacks.Stack, disc []*discovery.Stack) []stacks.Stack {
	kept := make(map[string]bool, len(disc))
	for _, st := range disc {
		kept[st.Name] = true
	}
	out := make([]stacks.Stack, 0, len(disc))
	for _, s := range configured {
		if kept[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

// Run starts the daemon: it binds the unix socket, serves connect-rpc until ctx
// is cancelled or a shutdown signal arrives, then removes the socket file. It
// blocks until the daemon exits, so a caller wanting a background daemon forks
// the process itself.
func Run(ctx context.Context, opts Options) error {
	if opts.SocketPath == "" {
		opts.SocketPath = paths.Socket()
	}
	if opts.Stacks == nil {
		stk, err := stacks.ParseEnv()
		if err != nil {
			return fmt.Errorf("parse %s: %w", stacks.EnvVar, err)
		}
		opts.Stacks = stk
	}

	// A responsive listener means another daemon already owns this socket.
	// Reporting it here names the socket and the command that frees it.
	if live, _ := isLive(opts.SocketPath); live {
		return fmt.Errorf("daemon already running on %s — use `a-novel core kill` to stop it first", opts.SocketPath)
	}

	// A path with no listener is a stale socket file, removed here so the bind
	// can proceed — the recovery path after `kill -9`.
	if _, err := os.Stat(opts.SocketPath); err == nil {
		if err := os.Remove(opts.SocketPath); err != nil {
			return fmt.Errorf("remove stale socket %s: %w", opts.SocketPath, err)
		}
	}

	// The parent is normally an existing /run/user/<uid> or the /tmp fallback;
	// this only creates one when XDG_RUNTIME_DIR points somewhere unusual.
	if err := os.MkdirAll(filepath.Dir(opts.SocketPath), 0o700); err != nil {
		return fmt.Errorf("mkdir socket parent: %w", err)
	}

	ln, err := net.Listen("unix", opts.SocketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", opts.SocketPath, err)
	}
	// Only the owning user may connect. XDG_RUNTIME_DIR is already 0700, and
	// pinning the socket itself keeps a permissive parent directory from
	// exposing the daemon.
	if err := os.Chmod(opts.SocketPath, 0o600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	// Discover every service in every registered stack before listening.
	// Per-service classification errors surface through Status and the startup
	// logs, and a non-default stack whose path has vanished is skipped, so a
	// swept scratch checkout cannot keep the daemon down. Only an unreadable
	// default stack is fatal.
	disc, err := discovery.DiscoverStacks(opts.Stacks)
	if err != nil {
		return fmt.Errorf("discover stacks: %w", err)
	}
	// The server reports the stacks it manages. Handing it the raw config would
	// let ListStacks advertise a skipped stack that every other RPC refuses.
	opts.Stacks = registeredAndDiscovered(opts.Stacks, disc)
	// Surface per-service discovery errors at daemon start. The daemon keeps
	// running: well-formed services still work, and a broken one refuses with
	// this same error.
	for _, st := range disc {
		for _, e := range st.Errors {
			fmt.Fprintf(os.Stderr, "discovery: stack %s: %v\n", st.Name, e)
		}
	}

	// The allocator needs every service name up front so cross-service prefixes
	// (SERVICE_X_VAR) resolve to the owning service.
	alloc := env.NewAllocator()
	allNames := make([]string, 0)
	for _, st := range disc {
		for _, svc := range st.Services {
			allNames = append(allNames, svc.Name)
		}
	}
	alloc.SetServices(allNames)
	builder := env.NewBuilder(alloc)

	// Per-target JSON-line files under $XDG_STATE_HOME/a-novel/logs, with
	// subscriber fan-out for live streaming.
	logStore := logs.New()
	// The supervisor starts empty and fills as RPCs arrive. It shares the
	// discovery snapshot to resolve target IDs without round-tripping through
	// the server, and the allocator to release port refcounts on termination.
	run := runner.New(disc, alloc, builder, logStore)
	srv := server.New(opts.Version, opts.SocketPath, opts.Stacks, disc, run, alloc, builder, logStore)

	// Reconstitute the Instance and InfraSession records of every podman
	// container carrying the adoption labels, so containers that outlived a
	// daemon restart rejoin its view with log streaming and watchers resumed.
	if cN, tN := run.AdoptOrphanContainers(ctx); cN > 0 {
		fmt.Fprintf(os.Stderr, "recovery: adopted %d orphan container(s), %d target(s)\n", cN, tN)
	}

	// Replay the go-exec targets a prior PrepareReinstall checkpointed, then
	// drop the checkpoint. A failed relaunch surfaces through the supervisor as
	// "terminated, exit_reason=crashed" without blocking startup.
	if cp, err := reinstall.Read(); err == nil && cp != nil {
		fmt.Fprintf(os.Stderr, "reinstall: replaying %d go-exec target(s) from %s\n",
			len(cp.GoExec), reinstall.Path())
		for _, gx := range cp.GoExec {
			if _, err := run.StartGoExec(ctx, gx.TargetID, gx.Env, nil); err != nil {
				fmt.Fprintf(os.Stderr, "reinstall: relaunch %s failed: %v\n", gx.TargetID, err)
			}
		}
		_ = reinstall.Delete()
	}
	mux := http.NewServeMux()
	path, handler := anovelv1connect.NewCoreServiceHandler(srv)
	mux.Handle(path, handler)

	httpServer := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Listen for SIGINT/SIGTERM and the server's PrepareReinstall
	// shutdown signal in parallel with ctx cancellation.
	shutdownCtx, cancelShutdown := context.WithCancel(ctx)
	defer cancelShutdown()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			cancelShutdown()
		case <-srv.ShutdownCh():
			// PrepareReinstall fired: shut down gracefully, and the
			// install script restarts the daemon.
			cancelShutdown()
		case <-shutdownCtx.Done():
		}
	}()

	// Serve in a goroutine so we can intercept shutdown signals here.
	serveErr := make(chan error, 1)
	go func() {
		err := httpServer.Serve(ln)
		// http.ErrServerClosed is what Shutdown returns on success.
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		_ = os.Remove(opts.SocketPath)
		return err
	case <-shutdownCtx.Done():
	}

	// Graceful shutdown: 10s for in-flight RPCs to complete. Streaming RPCs
	// observe ctx cancellation and exit.
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutErr := httpServer.Shutdown(shutCtx)
	_ = os.Remove(opts.SocketPath)
	if shutErr != nil {
		return fmt.Errorf("shutdown: %w", shutErr)
	}
	return nil
}

// isLive reports whether the socket at path is bound by a responsive daemon. A
// plain unix dial is enough to tell a stale socket file from a live listener.
func isLive(path string) (bool, error) {
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return false, err
	}
	_ = conn.Close()
	return true, nil
}
