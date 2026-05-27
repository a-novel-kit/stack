// Package daemon owns the daemon's process lifecycle: socket listener,
// graceful shutdown, signal handling, and (later) recovery of orphan
// containers. The actual RPC handlers live in internal/daemon/server.
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

// Run starts the daemon: binds the unix socket, serves connect-rpc until ctx
// is cancelled or a shutdown signal arrives, then cleans up the socket file.
// Blocks until the daemon exits — callers wanting a background daemon must
// fork the process themselves (see cli/core/start).
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

	// Reject a stale-but-live socket. PingExisting succeeds if another daemon
	// is responsive — surface that as an error so the caller exits with a
	// "daemon already running" message rather than silently failing on
	// listen().
	if live, _ := isLive(opts.SocketPath); live {
		return fmt.Errorf("daemon already running on %s — use `a-novel core kill` to stop it first", opts.SocketPath)
	}

	// Stale socket file (path exists but no listener): remove so we can bind.
	// This is the recovery path after `kill -9` left the socket behind.
	if _, err := os.Stat(opts.SocketPath); err == nil {
		if err := os.Remove(opts.SocketPath); err != nil {
			return fmt.Errorf("remove stale socket %s: %w", opts.SocketPath, err)
		}
	}

	// Ensure the parent directory exists. Most cases this is /run/user/<uid>
	// and exists; the fallback /tmp also always exists; the only case we
	// might create is XDG_RUNTIME_DIR pointing somewhere unusual.
	if err := os.MkdirAll(filepath.Dir(opts.SocketPath), 0o700); err != nil {
		return fmt.Errorf("mkdir socket parent: %w", err)
	}

	ln, err := net.Listen("unix", opts.SocketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", opts.SocketPath, err)
	}
	// Restrict permissions: only the owning user can connect. Defense in
	// depth — XDG_RUNTIME_DIR is already 0700, but pinning the socket
	// itself means a permissive parent dir doesn't expose the daemon.
	if err := os.Chmod(opts.SocketPath, 0o600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	// Discover every service in every registered stack before we start
	// listening. Per-service classification errors are non-fatal — they
	// surface via Status / startup logs — but a truly inaccessible stack
	// path is fatal (recovery, §3.4).
	disc, err := discovery.DiscoverStacks(opts.Stacks)
	if err != nil {
		return fmt.Errorf("discover stacks: %w", err)
	}
	// Log any per-service discovery errors to stderr so the user sees
	// them at daemon-start. The daemon keeps running — operations on
	// well-formed services still work; operations on broken services
	// will refuse with the same error message.
	for _, st := range disc {
		for _, e := range st.Errors {
			fmt.Fprintf(os.Stderr, "discovery: stack %s: %v\n", st.Name, e)
		}
	}

	// Phase 4: port allocator + env builder. Allocator knows every
	// service name so cross-service prefixes (SERVICE_X_VAR) resolve
	// correctly to the owning service.
	alloc := env.NewAllocator()
	allNames := make([]string, 0)
	for _, st := range disc {
		for _, svc := range st.Services {
			allNames = append(allNames, svc.Name)
		}
	}
	alloc.SetServices(allNames)
	builder := env.NewBuilder(alloc)

	// Phase 3: the process / container supervisor. Empty at start; takes
	// instances as RPCs come in. Shares the discovery snapshot so it can
	// resolve target IDs without round-tripping through the server, and
	// the env allocator so it can release refcounts on termination.
	// Phase 5: log store. Per-target JSON-line files in $XDG_STATE_HOME/
	// a-novel/logs/ with subscriber fan-out for live streaming.
	logStore := logs.New()
	run := runner.New(disc, alloc, builder, logStore)
	srv := server.New(opts.Version, opts.SocketPath, opts.Stacks, disc, run, alloc, builder, logStore)

	// Orphan adoption (spec §3.4 step 3): scan podman for containers
	// labeled with our adoption labels and reconstitute Instance +
	// InfraSession records. Containers that survived a daemon
	// restart (or kill -9) are picked up so the daemon's view matches
	// reality. Logs streaming + watchers resume on adopted containers.
	if cN, tN := run.AdoptOrphanContainers(ctx); cN > 0 {
		fmt.Fprintf(os.Stderr, "recovery: adopted %d orphan container(s), %d target(s)\n", cN, tN)
	}

	// Phase 8: reinstall handoff. If a checkpoint exists from a prior
	// PrepareReinstall, replay the recorded go-exec targets and delete
	// the checkpoint. Per-target failures don't block startup — they
	// surface as "terminated, exit_reason=crashed" via the supervisor.
	if cp, err := reinstall.Read(); err == nil && cp != nil {
		fmt.Fprintf(os.Stderr, "reinstall: replaying %d go-exec target(s) from %s\n",
			len(cp.GoExec), reinstall.Path())
		for _, gx := range cp.GoExec {
			if _, err := run.StartGoExec(ctx, gx.TargetID, gx.Env); err != nil {
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

	// Listen for SIGINT/SIGTERM AND the server's PrepareReinstall
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
			// PrepareReinstall fired — fall through to graceful
			// shutdown, then the install script restarts us.
			cancelShutdown()
		case <-shutdownCtx.Done():
		}
	}()

	// Serve in a goroutine so we can intercept shutdown signals here.
	serveErr := make(chan error, 1)
	go func() {
		err := httpServer.Serve(ln)
		// http.ErrServerClosed is the expected error from Shutdown; treat
		// as success.
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

// isLive returns whether the socket at path is bound by a responsive daemon.
// A connect-rpc handshake would be more thorough but a simple unix dial is
// enough to distinguish "stale socket file" from "live listener".
func isLive(path string) (bool, error) {
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return false, err
	}
	_ = conn.Close()
	return true, nil
}
