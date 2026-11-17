package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/daemon"
	"github.com/a-novel-kit/stack/cli/internal/rpc"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	"github.com/a-novel-kit/stack/cli/internal/shared/poll"
	"github.com/a-novel-kit/stack/cli/internal/version"
)

// daemonLogQuoteLimit caps how much of a failed start's output is quoted back.
// 4 KiB holds a stack trace or a handful of discovery errors. A daemon that
// keeps retrying cannot push its first error out of view.
const daemonLogQuoteLimit = 4 << 10

func newCoreStartCmd() *cobra.Command {
	var foreground bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the a-novel daemon (silent if already running)",
		Long: `Start the daemon in the background, and wait until it answers. A running
daemon is left as it is, so a shell profile can run this on every start.
--foreground serves in this terminal instead, as for a debugging session or
a systemd unit. A missing default stack stops the start; 'core setup' creates
it.`,
		Example: `  a-novel core start
  a-novel core start --foreground`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return startDaemon(cmd.Context(), foreground)
		},
	}
	cmd.Flags().BoolVar(&foreground, "foreground", false, "run daemon in foreground (don't detach)")
	return cmd
}

// startDaemon starts the daemon unless one already answers: detached, or
// serving in this process when foreground is set.
func startDaemon(ctx context.Context, foreground bool) error {
	if up, _ := daemonUp(ctx, rpc.New("")); up {
		return nil
	}
	if foreground {
		return daemon.Run(ctx, daemon.Options{Version: version.String()})
	}
	return startDetached(ctx)
}

// startDetached re-execs the current binary with the hidden daemon
// subcommand, in its own session so it survives this process's exit, and waits
// for it to answer. Callers check that no daemon runs already.
func startDetached(ctx context.Context) error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate own binary: %w", err)
	}
	//nolint:noctx // the daemon outlives this command, so no context may end it.
	c := exec.Command(bin, commandCore, daemonInternalCmd)
	// Detach: new process group, no terminal.
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// A detached daemon can only write to a file. Its log tells a bad
	// $A_NOVEL_STACKS apart from an unreadable stack path or a bound port.
	logFile, logErr := openDaemonLog()
	// logOffset is where this start's output begins, so a failure quotes only
	// what this start wrote. A failed Seek leaves it at 0, and the quote then
	// starts at the top of the log.
	var logOffset int64
	if logErr == nil {
		defer func() { _ = logFile.Close() }()
		c.Stdout, c.Stderr = logFile, logFile
		logOffset, _ = logFile.Seek(0, io.SeekEnd)
	}
	if err := c.Start(); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	rpcClient := rpc.New("")
	if !daemonReady(ctx, rpcClient, daemonStartTimeout) {
		return fmt.Errorf("daemon did not become ready within %s%s", daemonStartTimeout, daemonFailureDetail(logErr, logOffset))
	}
	fmt.Printf("a-novel daemon started on %s (pid %d)\n", rpcClient.SocketPath(), c.Process.Pid)
	// Release so the child outlives us.
	_ = c.Process.Release()
	return nil
}

// daemonStartTimeout bounds how long a detached daemon may take to answer.
const daemonStartTimeout = 5 * time.Second

// daemonReady polls the daemon until it answers a ping or timeout elapses.
func daemonReady(ctx context.Context, c *rpc.Client, timeout time.Duration) bool {
	err := poll.Until(ctx, 50*time.Millisecond, timeout, func(ctx context.Context) (bool, error) {
		pingCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, err := c.Ping(pingCtx)
		return err == nil, nil
	})
	return err == nil
}

// openDaemonLog opens the detached daemon's log for appending, creating its
// parent directory. A start that fails is often one of several, and the
// earlier attempts are the context for the last one.
func openDaemonLog() (*os.File, error) {
	path := paths.DaemonLog()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// daemonFailureDetail renders what the daemon wrote during this start, as a
// suffix to the readiness error. The log before offset belongs to earlier
// starts and is left out. When the log could not be opened, the suffix points
// the operator at --foreground.
func daemonFailureDetail(logErr error, offset int64) string {
	if logErr != nil {
		return "; check stderr by running `a-novel core start --foreground`"
	}
	out := readDaemonLogFrom(paths.DaemonLog(), offset)
	if out == "" {
		return fmt.Sprintf(" and wrote nothing to %s; run `a-novel core start --foreground` to watch it",
			paths.DaemonLog())
	}
	return fmt.Sprintf(":\n%s\n(full log: %s)", out, paths.DaemonLog())
}

// readDaemonLogFrom returns the first daemonLogQuoteLimit bytes of path after
// offset, trimmed. A read failure returns "".
func readDaemonLogFrom(path string, offset int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	buf, err := io.ReadAll(io.LimitReader(f, daemonLogQuoteLimit))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(buf))
}
