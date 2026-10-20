package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	"github.com/a-novel-kit/stack/cli/internal/daemon"
	"github.com/a-novel-kit/stack/cli/internal/setup"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	"github.com/a-novel-kit/stack/cli/internal/version"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// daemonInternalCmd is the hidden subcommand name `a-novel core start` re-execs
// itself with to switch into daemon mode. Cobra reads a leading "--" as a flag,
// so the double-underscore prefix keeps the name parseable while making an
// accidental human invocation implausible.
const daemonInternalCmd = "__daemon"

// daemonLogTailLimit caps how much of a failed start's output is quoted back.
// Enough for a stack trace or a handful of discovery errors; not enough for a
// crash-looping daemon to bury the error under its own retries.
const daemonLogTailLimit = 4 << 10

// IsDaemonReexec reports whether args (typically os.Args) invoke the hidden
// daemon re-exec, `a-novel core __daemon`. main() uses it to skip the
// version-update check in the detached daemon process, which has no user
// watching and must not phone home on shutdown.
func IsDaemonReexec(args []string) bool {
	return len(args) >= 3 && args[1] == commandCore && args[2] == daemonInternalCmd
}

// newCoreCmd groups the verbs that manage the daemon's lifecycle and the
// workspace it serves. `core start` is the .zshrc-friendly entrypoint: silent
// when a daemon already runs, it detaches one into the background, and it
// refuses with a hint when setup is missing.
func newCoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   commandCore,
		Short: "Manage the daemon and the workspace",
		Long: `The daemon is the background process that runs your services: one per user,
for every registered stack. The CLI and the UI reach it over a unix socket.
'core setup' makes each new shell run 'core start', which does nothing while
the daemon runs.`,
	}
	cmd.AddCommand(newCoreStartCmd())
	cmd.AddCommand(newCoreSetupCmd())
	cmd.AddCommand(newCoreSyncCmd())
	cmd.AddCommand(newCoreStacksCmd())
	cmd.AddCommand(newCoreBotCommentCmd())
	cmd.AddCommand(newCoreKillCmd())
	cmd.AddCommand(newCoreRestartCmd())
	cmd.AddCommand(newCoreStatusCmd())
	cmd.AddCommand(newCorePrepareReinstallCmd())
	cmd.AddCommand(newCoreDaemonInternalCmd())
	return cmd
}

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
	return startDetached()
}

func newCoreKillCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "kill",
		Short: "Stop the a-novel daemon",
		Long: `Stop the daemon and the go-exec targets it runs, each with a 10s grace.
Containers keep running. --force also tears down every service's infra,
stopping the targets left first. The next 'core start' relaunches nothing;
'core restart --preserve-targets' brings the go-exec targets back.`,
		Example: `  a-novel core kill
  a-novel core kill --force`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return killDaemon(cmd, force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "also tear down all infra (cascade-kill remaining targets)")
	return cmd
}

// killDaemon stops a running daemon and waits for it to exit. force also tears
// down every service's infra. A target the shutdown could not stop fails the
// command: whoever ran it to get a clean environment learns from the shell
// that they did not.
func killDaemon(cmd *cobra.Command, force bool) error {
	ctx, out := cmd.Context(), cmd.OutOrStdout()
	c := rpc.New("")
	if up, err := daemonUp(ctx, c); !up {
		if err == nil {
			_, _ = fmt.Fprintln(os.Stderr, "a-novel: daemon not running")
		}
		return err
	}
	resp, err := shutdown(ctx, c, force, out)
	if err != nil {
		return err
	}
	// The daemon exits even after a failure, so it is waited on either way.
	waitErr := waitDaemonGone(ctx, c, out)
	if fails := resp.GetFailures(); len(fails) > 0 {
		for _, f := range fails {
			_, _ = fmt.Fprintf(os.Stderr, "a-novel: shutdown could not stop %s\n", f)
		}
		return fmt.Errorf("shutdown left %d target(s) running", len(fails))
	}
	return waitErr
}

// daemonUp pings the daemon briefly. A daemon that is not listening is down,
// not an error; any other failure is returned.
func daemonUp(ctx context.Context, c *rpc.Client) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	_, err := c.Ping(ctx)
	if rpc.IsNotRunning(err) {
		return false, nil
	}
	return err == nil, err
}

// daemonExitTimeout bounds how long a stopped daemon may take to release its
// socket.
const daemonExitTimeout = 10 * time.Second

// waitDaemonGone polls until the daemon stops answering, a deterministic "the
// daemon is gone" signal so a script can run `core start` on the next line.
func waitDaemonGone(ctx context.Context, c *rpc.Client, out io.Writer) error {
	deadline := time.Now().Add(daemonExitTimeout)
	for time.Now().Before(deadline) {
		if up, err := daemonUp(ctx, c); !up && err == nil {
			_, _ = fmt.Fprintln(out, "daemon stopped.")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon didn't shut down within %s", daemonExitTimeout)
}

// shutdown asks the daemon to exit and reports what it stopped. force also
// tears down every service's infra.
func shutdown(ctx context.Context, c *rpc.Client, force bool, out io.Writer) (*anovelv1.ShutdownResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := c.Shutdown(ctx, force)
	if err != nil {
		return nil, err
	}
	if force {
		_, _ = fmt.Fprintf(out, "shutdown: %d go-exec target(s) killed, %d service infra torn down\n",
			resp.GetGoExecKilled(), resp.GetInfraServicesTornDown())
	} else {
		_, _ = fmt.Fprintf(out, "shutdown: %d go-exec target(s) killed (containers left running)\n",
			resp.GetGoExecKilled())
	}
	return resp, nil
}

// prepareReinstall asks the daemon to checkpoint its go-exec targets and exit.
func prepareReinstall(ctx context.Context, c *rpc.Client, out io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := c.PrepareReinstall(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "checkpoint written: %s (%d go-exec target(s))\n",
		resp.GetCheckpointPath(), resp.GetGoExecTargetCount())
	return nil
}

func newCoreRestartCmd() *cobra.Command {
	var force bool
	var preserve bool
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Stop and start the daemon, optionally keeping its go-exec targets",
		Long: `Stop the daemon as 'core kill' does, then start it again.
--preserve-targets checkpoints the go-exec targets first, and the new daemon
relaunches them on their ports, as 'a-novel install' does. A daemon already
down just starts.`,
		Example: `  a-novel core restart
  a-novel core restart --preserve-targets
  a-novel core restart --force`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return restartDaemon(cmd, force, preserve)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "also tear down all infra during the stop step")
	cmd.Flags().BoolVar(&preserve, "preserve-targets", false, "write reinstall checkpoint first; go-exec targets relaunch on start")
	return cmd
}

// restartDaemon stops a running daemon, then starts a detached one. preserve
// checkpoints the go-exec targets for the new daemon to relaunch, the way
// prepare-reinstall does; force also tears down every service's infra.
func restartDaemon(cmd *cobra.Command, force, preserve bool) error {
	if preserve && force {
		return usageError(errors.New("--preserve-targets and --force are mutually exclusive " +
			"(force tears down everything; preserve assumes the targets continue)"))
	}
	ctx, out := cmd.Context(), cmd.OutOrStdout()
	c := rpc.New("")
	up, err := daemonUp(ctx, c)
	if err != nil {
		return err
	}
	if up {
		if preserve {
			err = prepareReinstall(ctx, c, out)
		} else {
			_, err = shutdown(ctx, c, force, out)
		}
		if err != nil {
			return err
		}
		if err := waitDaemonGone(ctx, c, out); err != nil {
			return err
		}
	}
	return startDetached()
}

func newCoreStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether the daemon runs, with its socket, stacks and uptime",
		Long: `Print whether the daemon runs, with its version, socket, uptime and stacks,
and whether a reinstall checkpoint waits. A stopped daemon is a normal state,
and the command exits 0 for it.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return printDaemonStatus(cmd.Context(), cmd.OutOrStdout())
		},
	}
}

// printDaemonStatus prints whether the daemon runs and, when it does, its
// version, socket, uptime and stacks, the default one starred.
func printDaemonStatus(ctx context.Context, out io.Writer) error {
	c := rpc.New("")
	s, err := c.Status(ctx)
	if rpc.IsNotRunning(err) {
		_, _ = fmt.Fprintln(out, "a-novel daemon: not running")
		_, _ = fmt.Fprintf(out, "  socket: %s (no listener)\n", c.SocketPath())
		return nil
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(out, "a-novel daemon: running")
	_, _ = fmt.Fprintf(out, "  version: %s\n", s.GetDaemonVersion())
	_, _ = fmt.Fprintf(out, "  socket : %s\n", s.GetSocketPath())
	_, _ = fmt.Fprintf(out, "  started: %s (%.0fs ago)\n",
		s.GetStartedAt().AsTime().Format(time.RFC3339), s.GetUptime().AsDuration().Seconds())
	_, _ = fmt.Fprintf(out, "  stacks : %d\n", len(s.GetStacks()))
	for _, st := range s.GetStacks() {
		marker := " "
		if st.GetIsDefault() {
			marker = "*"
		}
		_, _ = fmt.Fprintf(out, "    %s %s\t%s\n", marker, st.GetName(), st.GetPath())
	}
	if s.GetReinstallCheckpointPending() {
		_, _ = fmt.Fprintln(out, "  reinstall checkpoint: pending")
	}
	return nil
}

func newCorePrepareReinstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prepare-reinstall",
		Short: "Checkpoint the go-exec targets and stop the daemon",
		Long: `Checkpoint the running go-exec targets and stop the daemon. Containers keep
running. The next 'core start' relaunches the checkpointed targets on their
ports, then deletes the checkpoint. 'a-novel install' and
'core restart --preserve-targets' run this step for you.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return stopWithCheckpoint(cmd)
		},
	}
}

// stopWithCheckpoint has a running daemon checkpoint its go-exec targets and
// exit, and waits until it is gone.
func stopWithCheckpoint(cmd *cobra.Command) error {
	ctx, out := cmd.Context(), cmd.OutOrStdout()
	c := rpc.New("")
	if up, err := daemonUp(ctx, c); !up {
		if err == nil {
			_, _ = fmt.Fprintln(cmd.OutOrStderr(), "a-novel: daemon not running (nothing to checkpoint)")
		}
		return err
	}
	if err := prepareReinstall(ctx, c, out); err != nil {
		return err
	}
	return waitDaemonGone(ctx, c, out)
}

func newCoreSetupCmd() *cobra.Command {
	var opts setup.Options
	var nonInteractive bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up this machine: tools, directories, stacks and shell profile",
		Long: `Set up this machine for the workspace. Each step checks first, so a second
run changes nothing:

  1. Check that podman, git and SSH access to GitHub work.
  2. Create the state and data directories.
  3. Clone each registered stack that is missing, after asking for the
     default one.
  4. Write a marked block into your shell profile, after a backup. It exports
     $A_NOVEL_STACKS when needed, loads completion and runs
     'a-novel core start'.
  5. Start the daemon.

--rc picks the profile, and --no-shell-rc skips step 4, as a dotfile manager
wants.`,
		Example: `  a-novel core setup
  a-novel core setup --rc ~/.bashrc
  a-novel core setup --no-shell-rc`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return setupMachine(cmd, opts, nonInteractive)
		},
	}
	cmd.Flags().StringVar(&opts.RCPath, "rc", "", "explicit shell rc path (overrides $SHELL detection)")
	cmd.Flags().BoolVar(&opts.NoShellRC, "no-shell-rc", false, "skip the shell rc integration step")
	cmd.Flags().BoolVar(&opts.NoStartDaemon, "no-start-daemon", false, "skip the auto-start-daemon step at the end")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "never prompt (missing default stack becomes 'skipped')")
	return cmd
}

// setupMachine runs the setup steps opts selects, prompting on stdin unless
// nonInteractive is set. Its last step starts the daemon as `core start` does,
// so a re-run beside a running daemon changes nothing.
func setupMachine(cmd *cobra.Command, opts setup.Options, nonInteractive bool) error {
	opts.StartDaemon = func() error { return startDaemon(cmd.Context(), false) }
	var prompter setup.Prompter
	if !nonInteractive {
		prompter = &setup.StdinPrompter{Out: cmd.OutOrStderr()}
	}
	return setup.Run(opts, cmd.OutOrStdout(), prompter)
}

// newCoreDaemonInternalCmd is the hidden subcommand 'core start' re-execs the
// binary with, to run the daemon in a detached child.
func newCoreDaemonInternalCmd() *cobra.Command {
	return &cobra.Command{
		Use:    daemonInternalCmd,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return daemon.Run(cmd.Context(), daemon.Options{Version: version.String()})
		},
	}
}

// startDetached re-execs the current binary with the hidden daemon
// subcommand, in its own session so it survives this process's exit, and waits
// for it to answer. Callers check that no daemon runs already.
func startDetached() error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate own binary: %w", err)
	}
	c := exec.Command(bin, commandCore, daemonInternalCmd)
	// Detach: new process group, no terminal.
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// A detached daemon has nowhere to write but a file. Keeping its output
	// is what separates a bad $A_NOVEL_STACKS from an unreadable stack path
	// or an already-bound port; discard it and every startup failure reads
	// alike as "did not become ready".
	logFile, logErr := openDaemonLog()
	// Where this attempt's output begins, so a failure quotes only what this
	// start wrote.
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
	if !daemonReady(rpcClient, daemonStartTimeout) {
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
func daemonReady(c *rpc.Client, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_, err := c.Ping(ctx)
		cancel()
		if err == nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
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

// daemonFailureDetail renders what the daemon said before giving up, as a
// suffix to the readiness error. It reads from offset, so a crash-looping
// daemon is diagnosed by its latest words. When the log could not be opened at
// all it falls back to pointing the operator at --foreground.
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

// readDaemonLogFrom returns the trimmed tail of path starting at offset, capped
// so a runaway daemon cannot flood the terminal with its own failure.
func readDaemonLogFrom(path string, offset int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	buf, err := io.ReadAll(io.LimitReader(f, daemonLogTailLimit))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(buf))
}
