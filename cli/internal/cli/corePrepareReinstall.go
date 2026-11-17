package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
)

func newCorePrepareReinstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prepare-reinstall",
		Short: "Checkpoint the go-exec targets and stop the daemon",
		Long: `Checkpoint the running go-exec targets and stop the daemon. Containers keep
running. The next 'core start' relaunches the checkpointed targets on their
ports, then deletes the checkpoint. 'a-novel install' and
'core restart --preserve-targets' run this step for you.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stopped, err := stopDaemon(cmd.Context(), rpc.New(), cmd.OutOrStdout(), true, false)
			if !stopped && err == nil {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "a-novel: daemon not running (nothing to checkpoint)")
			}
			return err
		},
	}
}
