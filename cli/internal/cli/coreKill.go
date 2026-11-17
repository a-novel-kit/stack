package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
)

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
			stopped, err := stopDaemon(cmd.Context(), rpc.New(), cmd.OutOrStdout(), false, force)
			if !stopped && err == nil {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "a-novel: daemon not running")
			}
			return err
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "also tear down all infra (cascade-kill remaining targets)")
	return cmd
}
