package cli

import (
	"context"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
)

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
		if err := stopForRestart(ctx, c, out, force, preserve); err != nil {
			return err
		}
	}
	return startDetached()
}

// stopForRestart stops a running daemon and waits until it is gone. preserve
// checkpoints the go-exec targets first. Otherwise the daemon shuts down as
// `core kill` does.
func stopForRestart(ctx context.Context, c *rpc.Client, out io.Writer, force, preserve bool) error {
	if preserve {
		if err := prepareReinstall(ctx, c, out); err != nil {
			return err
		}
	} else if _, err := shutdown(ctx, c, force, out); err != nil {
		return err
	}
	return waitDaemonGone(ctx, c, out)
}
