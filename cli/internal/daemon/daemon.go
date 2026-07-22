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

// Run starts the daemon: binds the unix socket, serves connect-rpc until ctx
// is cancelled or a shutdown signal arrives, then cleans up the socket file.
// Blocks until the daemon exits — callers wanting a background daemon must
// fork the process themselves (see cli/core/start).
// registeredAndDiscovered narrows the configured stacks to those discovery
// actually kept, preserving the configured order (and therefore which entry is
// default). It is the daemon's answer to "which stacks do I manage?" — the
// registration list alone over-answers it once a vanished stack is skipped.
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
	// surface via Status and startup logs — and a non-default stack whose
	// path has vanished is skipped, so a swept scratch checkout cannot keep
	// the daemon down. Only an unreadable DEFAULT stack is fatal.
	disc, err := discovery.DiscoverStacks(opts.Stacks)
	if err != nil {
		return fmt.Errorf("discover stacks: %w", err)
	}
	// The server reports the stacks it manages, which after the skip above is
	// no longer every registered entry. Handing it the raw config would let
	// ListStacks advertise a stack that every other RPC then refuses.
	opts.Stacks = registeredAndDiscovered(opts.Stacks, disc)
	// Log any per-service discovery errors to stderr so the user sees
	// them at daemon-start. The daemon keeps running — operations on
	// well-formed services still work; operations on broken services
	// will refuse with the same error message.
	for _, st := range disc {
		for _, e := range st.Errors {
			fmt.Fprintf(os.Stderr, "discovery: stack %s: %v\n", st.Name, e)
		}
	}

	// Port allocator and env builder. The allocator needs every service
	// name up front so cross-service prefixes (SERVICE_X_VAR) resolve to
	// the owning service.
	alloc := env.NewAllocator()
	allNames := make([]string, 0)
	for _, st := range disc {
		for _, svc := range st.Services {
			allNames = append(allNames, svc.Name)
		}
	}
	alloc.SetServices(allNames)
	builder := env.NewBuilder(alloc)

	// Log store: per-target JSON-line files under $XDG_STATE_HOME/a-novel/logs
	// with subscriber fan-out for live streaming.
	logStore := logs.New()
	// The process/container supervisor, empty at start and populated as RPCs
	// arrive. It shares the discovery snapshot to resolve target IDs without
	// round-tripping through the server, and the env allocator to release
	// port refcounts on termination.
	run := runner.New(disc, alloc, builder, logStore)
	srv := server.New(opts.Version, opts.SocketPath, opts.Stacks, disc, run, alloc, builder, logStore)

	// Adopt orphan containers: scan podman for containers carrying our
	// adoption labels and reconstitute their Instance and InfraSession
	// records, so containers that outlived a daemon restart (or kill -9)
	// rejoin the daemon's view. Log streaming and watchers resume on them.
	if cN, tN := run.AdoptOrphanContainers(ctx); cN > 0 {
		fmt.Fprintf(os.Stderr, "recovery: adopted %d orphan container(s), %d target(s)\n", cN, tN)
	}

	// Reinstall handoff: if a prior PrepareReinstall left a checkpoint,
	// replay the recorded go-exec targets and delete it. A per-target
	// relaunch failure doesn't block startup — it surfaces as
	// "terminated, exit_reason=crashed" via the supervisor.
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
