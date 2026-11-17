package cli

import (
	"errors"

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
			if preserve && force {
				return usageError(errors.New("--preserve-targets and --force are mutually exclusive " +
					"(force tears down everything; preserve assumes the targets continue)"))
			}
			ctx, out := cmd.Context(), cmd.OutOrStdout()
			if _, err := stopDaemon(ctx, rpc.New(), out, preserve, force); err != nil {
				return err
			}
			return startDetached(ctx, out)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "also tear down all infra during the stop step")
	cmd.Flags().BoolVar(&preserve, "preserve-targets", false, "write reinstall checkpoint first; go-exec targets relaunch on start")
	return cmd
}
