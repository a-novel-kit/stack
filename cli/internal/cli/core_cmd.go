// Daemon-control subcommands (`a-novel core ...`).
//
// `core start` is the .zshrc-friendly entrypoint — silent on already-running,
// detaches the daemon in the background, refuses with a hint on missing setup.
// `core setup`, `core kill`, `core status`, `core prepare-reinstall` round out
// the lifecycle.

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/client/rpc"
	"github.com/a-novel-kit/stack/cli/internal/daemon"
	"github.com/a-novel-kit/stack/cli/internal/setup"
	"github.com/a-novel-kit/stack/cli/internal/version"
)

// daemonInternalCmd is the hidden subcommand name `a-novel core start` re-execs
// itself with to switch into "I am the daemon" mode. Cobra subcommand names
// must not start with "--" (those are flags); we use a double-underscore prefix
// to make accidental human invocation implausible without confusing the parser.
const daemonInternalCmd = "__daemon"

// IsDaemonReexec reports whether args (typically os.Args) invoke the hidden
// daemon re-exec — `a-novel core __daemon`. main() uses it to skip the
// version-update check in the detached, long-lived daemon process (which has no
// user watching and must not phone home on shutdown).
func IsDaemonReexec(args []string) bool {
	return len(args) >= 3 && args[1] == "core" && args[2] == daemonInternalCmd
}

func newCoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "core",
		Short: "Manage the a-novel daemon (long-lived background process)",
		Long: `The a-novel daemon is a long-lived background process that supervises
running targets and serves the CLI / TUI / future web UI over a unix socket.

A single daemon instance per user manages every registered stack (see
'a-novel stacks'). Designed to live in your .zshrc as 'a-novel core start',
which is silent on already-running and idempotent.`,
	}
	cmd.AddCommand(newCoreStartCmd())
	cmd.AddCommand(newCoreSetupCmd())
	cmd.AddCommand(newCoreSyncCmd())
	cmd.AddCommand(newCoreBotTokenCmd())
	cmd.AddCommand(newCoreBotGhCmd())
	cmd.AddCommand(newCoreKillCmd())
	cmd.AddCommand(newCoreRestartCmd())
	cmd.AddCommand(newCoreStatusCmd())
	cmd.AddCommand(newCorePrepareReinstallCmd())
	// Internal: handles re-exec by `core start` to become the daemon
	// process. Hidden from --help; only invoked by our own start path.
	cmd.AddCommand(newCoreDaemonInternalCmd())
	return cmd
}

func newCoreStartCmd() *cobra.Command {
	var foreground bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the a-novel daemon (silent if already running)",
		Long: `Start the long-lived a-novel daemon. Idempotent + silent if a healthy
daemon is already running — designed to be safe in .zshrc / shell init.

By default, detaches the daemon into the background and returns immediately.
With --foreground, blocks and serves in the current terminal (useful for
systemd units or debugging).

Refuses with a clear message (not an interactive prompt) if the default
stack isn't set up; run 'a-novel core setup' to bootstrap.`,
		Example: `  # In ~/.zshrc — silent no-op if already running
  a-novel core start

  # Foreground (systemd / debugging)
  a-novel core start --foreground`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// Already running? Exit silently.
			c := rpc.New("")
			pingCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			defer cancel()
			if _, err := c.Ping(pingCtx); err == nil {
				return nil
			}
			// Foreground mode: just call into the daemon package directly.
			if foreground {
				return daemon.Run(ctx, daemon.Options{Version: version.String()})
			}
			// Background mode: re-exec ourselves with the hidden internal
			// flag, detached. Saves us from forking + duplicating Go state.
			return startDetached()
		},
	}
	cmd.Flags().BoolVar(&foreground, "foreground", false, "run daemon in foreground (don't detach)")
	return cmd
}

func newCoreKillCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "kill",
		Short: "Stop the a-novel daemon",
		Long: `Stop the running a-novel daemon WITHOUT writing a reinstall checkpoint —
on next 'core start' nothing is auto-relaunched.

Without --force, SIGTERMs every running go-exec target (10s grace each)
and leaves containers alone — those have their own podman lifecycle.

With --force, ALSO tears down every service's infra (cascade-kills any
remaining targets first). Use when you want a clean local environment;
loses any go-exec target state without a checkpoint.

For graceful 'restart-the-daemon-and-relaunch-my-targets', prefer
'a-novel core prepare-reinstall' followed by 'core start', or use the
'core restart' convenience.`,
		Example: `  a-novel core kill
  a-novel core kill --force`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			pingCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			if _, err := c.Ping(pingCtx); err != nil {
				cancel()
				if rpc.IsNotRunning(err) {
					_, _ = fmt.Fprintln(os.Stderr, "a-novel: daemon not running")
					return nil
				}
				return err
			}
			cancel()
			// Shutdown RPC — the daemon signals its own exit after the
			// response is on the wire. We poll Ping until the socket is
			// gone so the caller (a shell, a script) knows the daemon's
			// done — same UX shape as PrepareReinstall.
			shutCtx, shutCancel := context.WithTimeout(ctx, 60*time.Second)
			defer shutCancel()
			resp, err := c.Shutdown(shutCtx, force)
			if err != nil {
				return err
			}
			if force {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"shutdown: %d go-exec target(s) killed, %d service infra torn down\n",
					resp.GetGoExecKilled(), resp.GetInfraServicesTornDown())
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"shutdown: %d go-exec target(s) killed (containers left running)\n",
					resp.GetGoExecKilled())
			}
			return waitForDaemonGone(ctx, c, 10*time.Second, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "also tear down all infra (cascade-kill remaining targets)")
	return cmd
}

// waitForDaemonGone polls Ping until the daemon's socket is unresponsive,
// up to `timeout`. Used by core kill + core restart to give the caller a
// deterministic "the daemon's actually gone" signal — without this, scripts
// that immediately `core start` race the shutdown.
func waitForDaemonGone(ctx context.Context, c *rpc.Client, timeout time.Duration, out io.Writer) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pingCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		_, err := c.Ping(pingCtx)
		cancel()
		if err != nil && rpc.IsNotRunning(err) {
			_, _ = fmt.Fprintln(out, "daemon stopped.")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon didn't shut down within %s", timeout)
}

func newCoreRestartCmd() *cobra.Command {
	var force bool
	var preserve bool
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Stop and start the daemon (optionally preserving running go-exec targets)",
		Long: `Convenience wrapper for the common "stop the daemon, start it back up"
loop. Honors the same flags as 'core kill':

  --force            tear down all infra alongside the daemon
  --preserve-targets write a reinstall checkpoint first, so go-exec
                     targets relaunch on next start (the same path
                     scripts/install.sh uses)

Without flags, the daemon stops cleanly (containers survive, go-exec
targets are SIGTERMed and NOT relaunched) and then starts fresh. With
--preserve-targets the new daemon comes back with the same go-exec
target list it had before.

If the daemon is already down, this is just 'core start'.`,
		Example: `  a-novel core restart
  a-novel core restart --preserve-targets
  a-novel core restart --force`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			pingCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			_, pingErr := c.Ping(pingCtx)
			cancel()
			if pingErr != nil && !rpc.IsNotRunning(pingErr) {
				return pingErr
			}
			daemonUp := pingErr == nil
			if daemonUp {
				if preserve && force {
					return errors.New("--preserve-targets and --force are mutually exclusive (force tears down everything; preserve assumes the targets continue)")
				}
				if preserve {
					// Same path as 'core prepare-reinstall' — write the
					// checkpoint, daemon exits, next start replays it.
					rCtx, rCancel := context.WithTimeout(ctx, 30*time.Second)
					resp, err := c.PrepareReinstall(rCtx)
					rCancel()
					if err != nil {
						return err
					}
					_, _ = fmt.Fprintf(cmd.OutOrStdout(),
						"checkpoint written: %s (%d go-exec target(s))\n",
						resp.GetCheckpointPath(), resp.GetGoExecTargetCount())
				} else {
					sCtx, sCancel := context.WithTimeout(ctx, 60*time.Second)
					resp, err := c.Shutdown(sCtx, force)
					sCancel()
					if err != nil {
						return err
					}
					if force {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(),
							"shutdown: %d go-exec target(s) killed, %d service infra torn down\n",
							resp.GetGoExecKilled(), resp.GetInfraServicesTornDown())
					} else {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(),
							"shutdown: %d go-exec target(s) killed (containers left running)\n",
							resp.GetGoExecKilled())
					}
				}
				if err := waitForDaemonGone(ctx, c, 10*time.Second, cmd.OutOrStdout()); err != nil {
					return err
				}
			}
			return startDetached()
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "also tear down all infra during the stop step")
	cmd.Flags().BoolVar(&preserve, "preserve-targets", false, "write reinstall checkpoint first; go-exec targets relaunch on start")
	return cmd
}

func newCoreStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status (running? socket? stacks? uptime?)",
		Long: `Reports whether the daemon is currently running, the socket it's listening
on, the registered stacks (with paths and default marker), uptime, and
whether a reinstall checkpoint is pending.

Exits 0 always — "daemon down" is a normal observable state, not an error.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			s, err := c.Status(ctx)
			if err != nil {
				if rpc.IsNotRunning(err) {
					fmt.Println("a-novel daemon: not running")
					fmt.Println("  socket: " + c.SocketPath() + " (no listener)")
					return nil
				}
				return err
			}
			fmt.Printf("a-novel daemon: running\n")
			fmt.Printf("  version: %s\n", s.GetDaemonVersion())
			fmt.Printf("  socket : %s\n", s.GetSocketPath())
			fmt.Printf("  started: %s (%.0fs ago)\n",
				s.GetStartedAt().AsTime().Format(time.RFC3339),
				s.GetUptime().AsDuration().Seconds())
			fmt.Printf("  stacks : %d\n", len(s.GetStacks()))
			for _, st := range s.GetStacks() {
				marker := " "
				if st.GetIsDefault() {
					marker = "*"
				}
				fmt.Printf("    %s %s\t%s\n", marker, st.GetName(), st.GetPath())
			}
			if s.GetReinstallCheckpointPending() {
				fmt.Println("  reinstall checkpoint: pending")
			}
			return nil
		},
	}
}

func newCorePrepareReinstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prepare-reinstall",
		Short: "Write reinstall checkpoint + shut down for a graceful upgrade",
		Long: `Asks the running daemon to checkpoint its in-flight go-exec targets to
reinstall.json and exit cleanly. Containers survive the shutdown unchanged.

The next 'a-novel core start' reads the checkpoint and relaunches the
recorded go-exec targets, then deletes the checkpoint — yielding a steady
state identical to pre-restart.

Used by scripts/install.sh to make 'go install ./cmd/a-novel' non-disruptive
during development. Not for direct manual use — for an immediate stop use
'core kill' instead.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			c := rpc.New("")
			if _, err := c.Ping(ctx); err != nil {
				if rpc.IsNotRunning(err) {
					_, _ = fmt.Fprintln(cmd.OutOrStderr(), "a-novel: daemon not running (nothing to checkpoint)")
					return nil
				}
				return err
			}
			resp, err := c.PrepareReinstall(ctx)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "checkpoint written: %s (%d go-exec target(s))\n",
				resp.GetCheckpointPath(), resp.GetGoExecTargetCount())
			return waitForDaemonGone(ctx, c, 10*time.Second, cmd.OutOrStdout())
		},
	}
}

func newCoreSetupCmd() *cobra.Command {
	var rcPath string
	var noShellRC bool
	var noStartDaemon bool
	var nonInteractive bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive first-time bootstrap (stacks, dirs, .zshrc edits)",
		Long: `One-time interactive bootstrap. Run after installing the CLI; idempotent
on re-run (every step checks current state first, performs zero filesystem
writes on a fully-set-up system).

What it does:

  1. Checks podman, git, and GitHub SSH access are available.
  2. Creates $XDG_STATE_HOME/a-novel and $XDG_DATA_HOME/a-novel (mode 0700).
  3. For each registered stack (A_NOVEL_STACKS, or implicit default at
     ~/git-projects/a-novel): verifies the path exists and contains the
     right git repo; prompts to clone if the default stack is missing;
     unconditionally clones non-default stacks if missing.
  4. Edits your shell rc (detected via --rc, $ZDOTDIR, $SHELL, then
     ~/.zshrc fallback) — manages a single block delimited by
     '# >>> a-novel setup >>>' / '# <<< a-novel setup <<<' markers, with
     a timestamped backup before any change.
  5. Prints a summary with the "next step" hint.

Use --no-shell-rc to skip step 4 entirely (for dotfile-manager users).`,
		Example: `  a-novel core setup
  a-novel core setup --rc ~/.bashrc
  a-novel core setup --no-shell-rc`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := setup.Options{
				RCPath:         rcPath,
				NoShellRC:      noShellRC,
				NoStartDaemon:  noStartDaemon,
				NonInteractive: nonInteractive,
				// StartDaemon idempotency: if the daemon is already
				// running, treat that as success without re-spawning. This
				// matches the behavior `a-novel core start` already
				// guarantees and keeps re-runs of `core setup` a no-op.
				StartDaemon: func() error {
					ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
					defer cancel()
					if _, err := rpc.New("").Ping(ctx); err == nil {
						return nil // already up
					}
					return startDetached()
				},
			}
			var prompter setup.Prompter
			if !nonInteractive {
				prompter = &setup.StdinPrompter{Out: cmd.OutOrStderr()}
			}
			_, err := setup.Run(opts, cmd.OutOrStdout(), prompter)
			return err
		},
	}
	cmd.Flags().StringVar(&rcPath, "rc", "", "explicit shell rc path (overrides $SHELL detection)")
	cmd.Flags().BoolVar(&noShellRC, "no-shell-rc", false, "skip the shell rc integration step")
	cmd.Flags().BoolVar(&noStartDaemon, "no-start-daemon", false, "skip the auto-start-daemon step at the end")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "never prompt (missing default stack becomes 'skipped')")
	return cmd
}

func newCoreDaemonInternalCmd() *cobra.Command {
	// Internal subcommand: re-exec target for 'core start' to switch into
	// daemon mode in a detached child. Hidden from --help so users don't
	// invoke it directly.
	return &cobra.Command{
		Use:    daemonInternalCmd,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return daemon.Run(cmd.Context(), daemon.Options{Version: version.String()})
		},
	}
}

// startDetached re-execs the current binary with the hidden daemon flag
// and detaches the child so it survives our exit. Standard "spawn a
// background daemon from a CLI" pattern.
func startDetached() error {
	bin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate own binary: %w", err)
	}
	c := exec.Command(bin, "core", daemonInternalCmd)
	// Detach: new process group, no shared file descriptors.
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	c.Stdin = nil
	c.Stdout = nil
	c.Stderr = nil
	if err := c.Start(); err != nil {
		return fmt.Errorf("spawn daemon: %w", err)
	}
	// Poll briefly for the daemon to come up so the caller knows it
	// worked. Spec §3.1: print socket path after listening.
	deadline := time.Now().Add(5 * time.Second)
	rpcClient := rpc.New("")
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_, err := rpcClient.Ping(ctx)
		cancel()
		if err == nil {
			fmt.Printf("a-novel daemon started on %s (pid %d)\n", rpcClient.SocketPath(), c.Process.Pid)
			// Release so the child outlives us.
			_ = c.Process.Release()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("daemon did not become ready within 5s; check stderr by running `a-novel core start --foreground`")
}
