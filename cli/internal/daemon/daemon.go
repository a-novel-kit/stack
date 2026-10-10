// Package daemon owns the daemon's process lifecycle: socket listener,
// graceful shutdown, signal handling, and recovery of orphan containers.
// The RPC handlers live in internal/daemon/server.
package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

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
	Version string // daemon binary version
}

// Run starts the daemon: it binds the unix socket, serves connect-rpc until ctx
// is cancelled or a shutdown signal arrives, then removes the socket file. It
// blocks until the daemon exits, so a caller wanting a background daemon forks
// the process itself.
func Run(ctx context.Context, opts Options) error {
	socketPath := paths.Socket()
	registered, err := stacks.ParseEnv()
	if err != nil {
		return fmt.Errorf("parse %s: %w", stacks.EnvVar, err)
	}

	// A responsive listener means another daemon already owns this socket.
	// Reporting it here names the socket and the command that frees it.
	if isLive(socketPath) {
		return fmt.Errorf("daemon already running on %s — use `a-novel core kill` to stop it first", socketPath)
	}

	// A path with no listener is a stale socket file, removed here so the bind
	// can proceed — the recovery path after `kill -9`.
	if _, err := os.Stat(socketPath); err == nil {
		if err := os.Remove(socketPath); err != nil {
			return fmt.Errorf("remove stale socket %s: %w", socketPath, err)
		}
	}

	// The parent is normally an existing /run/user/<uid> or the /tmp fallback;
	// this only creates one when XDG_RUNTIME_DIR points somewhere unusual.
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return fmt.Errorf("mkdir socket parent: %w", err)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", socketPath, err)
	}
	// Only the owning user may connect. XDG_RUNTIME_DIR is already 0700, and
	// pinning the socket itself keeps a permissive parent directory from
	// exposing the daemon.
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	// Discover every service in every registered stack before serving.
	// Per-service classification errors surface through Status and the startup
	// logs, and a non-default stack whose path has vanished is skipped, so a
	// swept scratch checkout cannot keep the daemon down. Only an unreadable
	// default stack is fatal. The stacks discovery keeps are the ones the
	// daemon manages and reports.
	disc, err := discovery.DiscoverStacks(registered)
	if err != nil {
		return fmt.Errorf("discover stacks: %w", err)
	}
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
	alloc.SetServices(disc.ServiceNames())
	builder := env.NewBuilder(alloc)

	// Per-target JSON-line files under $XDG_STATE_HOME/a-novel/logs, with
	// subscriber fan-out for live streaming.
	logStore := logs.New()
	// The supervisor starts empty and fills as RPCs arrive. It shares the
	// discovery snapshot to resolve target IDs without round-tripping through
	// the server, and the allocator to release port refcounts on termination.
	run := runner.New(disc, alloc, builder, logStore)

	// The daemon stops on SIGINT or SIGTERM, when ctx ends, or when an RPC
	// calls stop, as Shutdown and PrepareReinstall do. After PrepareReinstall,
	// `a-novel install` or `core restart` starts the next daemon.
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	srv := server.New(opts.Version, socketPath, disc, run, builder, logStore, stop)

	// Reconstitute the Instance and InfraSession records of every podman
	// container carrying the adoption labels, so containers that outlived a
	// daemon restart rejoin its view with log streaming and watchers resumed.
	if cN, tN := run.AdoptOrphanContainers(ctx); cN > 0 {
		fmt.Fprintf(os.Stderr, "recovery: adopted %d orphan container(s), %d target(s)\n", cN, tN)
	}

	// Replay the go-exec targets a prior PrepareReinstall checkpointed, then
	// drop the checkpoint. A relaunch that fails is reported and skipped, so it
	// never blocks startup.
	if cp, err := reinstall.Read(); err == nil && cp != nil {
		fmt.Fprintf(os.Stderr, "reinstall: replaying %d go-exec target(s) from %s\n",
			len(cp.GoExec), reinstall.Path())
		for _, gx := range cp.GoExec {
			if err := run.Relaunch(ctx, gx.TargetID, gx.Env); err != nil {
				fmt.Fprintf(os.Stderr, "reinstall: relaunch %s failed: %v\n", gx.TargetID, err)
			}
		}
		_ = reinstall.Delete()
	}
	rpcServer := connect.NewServer()
	anovelv1connect.RegisterCoreServiceHandler(rpcServer, srv)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, rpcServer)

	httpServer := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Serve in a goroutine so we can intercept shutdown signals here. Serve
	// returns before Shutdown only on a failure.
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(ln) }()

	select {
	case err := <-serveErr:
		_ = os.Remove(socketPath)
		return err
	case <-ctx.Done():
	}

	// Graceful shutdown: 10s for in-flight RPCs to complete. Streaming RPCs
	// observe ctx cancellation and exit.
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	shutErr := httpServer.Shutdown(shutCtx)
	_ = os.Remove(socketPath)
	if shutErr != nil {
		return fmt.Errorf("shutdown: %w", shutErr)
	}
	return nil
}

// isLive reports whether the socket at path is bound by a responsive daemon. A
// plain unix dial is enough to tell a stale socket file from a live listener.
func isLive(path string) bool {
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
