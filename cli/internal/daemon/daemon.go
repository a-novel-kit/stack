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
	ln, err := listen(socketPath)
	if err != nil {
		return err
	}

	// Discover every service of every registered stack before serving. A
	// non-default stack whose path is gone is skipped, so a swept scratch
	// checkout cannot keep the daemon down. Only an unreadable default stack is
	// fatal. The stacks discovery keeps are the ones the daemon manages.
	disc, err := discovery.DiscoverStacks(registered)
	if err != nil {
		return fmt.Errorf("discover stacks: %w", err)
	}
	// Print each non-fatal discovery problem to the daemon log. Status and
	// ListStacks also report them with their stack. A service or target that
	// failed discovery is left out, and the rest work.
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
	srv := server.New(opts.Version, socketPath, disc, run, builder, logStore, stop, ctx.Done())

	// Reconstitute the Instance and InfraSession records of every podman
	// container carrying the adoption labels, so containers that outlived a
	// daemon restart rejoin its view with log streaming and polling resumed.
	if cN, tN := run.AdoptOrphanContainers(ctx); cN > 0 {
		fmt.Fprintf(os.Stderr, "recovery: adopted %d orphan container(s), %d target(s)\n", cN, tN)
	}

	replayCheckpoint(ctx, run)
	return serve(ctx, ln, srv)
}

// listen binds the daemon's unix socket at socketPath, refusing when another
// daemon answers there and removing a stale socket file, the recovery path
// after `kill -9`.
func listen(socketPath string) (net.Listener, error) {
	// A responsive listener means another daemon already owns this socket.
	// Reporting it here names the socket and the command that frees it.
	if isLive(socketPath) {
		return nil, fmt.Errorf("daemon already running on %s — use `a-novel core kill` to stop it first", socketPath)
	}
	if _, err := os.Stat(socketPath); err == nil {
		if err := os.Remove(socketPath); err != nil {
			return nil, fmt.Errorf("remove stale socket %s: %w", socketPath, err)
		}
	}
	// The parent is normally an existing /run/user/<uid> or the /tmp fallback;
	// this only creates one when XDG_RUNTIME_DIR points somewhere unusual.
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir socket parent: %w", err)
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", socketPath, err)
	}
	// Only the owning user may connect. XDG_RUNTIME_DIR is already 0700, and
	// pinning the socket itself keeps a permissive parent directory from
	// exposing the daemon.
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	return ln, nil
}

// replayCheckpoint relaunches the go-exec targets a prior PrepareReinstall
// checkpointed, then drops the checkpoint. A checkpoint is used once: an
// unusable one is reported and dropped too, so it never refuses a later
// install. A relaunch that fails is reported and skipped, so it never blocks
// startup.
func replayCheckpoint(ctx context.Context, run *runner.Runner) {
	cp, err := reinstall.Read()
	defer func() { _ = reinstall.Delete() }()
	if err != nil {
		fmt.Fprintf(os.Stderr, "reinstall: discarding the checkpoint: %v\n", err)
		return
	}
	if cp == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "reinstall: replaying %d go-exec target(s) from %s\n", len(cp.GoExec), reinstall.Path())
	for _, gx := range cp.GoExec {
		if err := run.Relaunch(ctx, gx.TargetID, gx.Env); err != nil {
			fmt.Fprintf(os.Stderr, "reinstall: relaunch %s failed: %v\n", gx.TargetID, err)
		}
	}
}

// serve answers connect-rpc on ln until ctx ends, then shuts down gracefully.
// Closing ln removes the socket file. The next daemon may bind the path while
// this one still waits on open RPCs, so serve leaves the path alone.
func serve(ctx context.Context, ln net.Listener, srv *server.Server) error {
	rpcServer := connect.NewServer()
	anovelv1connect.RegisterCoreServiceHandler(rpcServer, srv)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, rpcServer)

	httpServer := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Serve runs in a goroutine, so the shutdown signal is handled here. Serve
	// returns before Shutdown only on a failure.
	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(ln) }()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	// Graceful shutdown gives unary RPCs and Exec streams up to 10s. Log follows
	// and watches end as soon as ctx does.
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
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
